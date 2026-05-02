package sstable

import (
	"encoding/binary"
	"kv-engine/internal/bloom"
	"os"
	"sort"
)

type Entry struct {
	Key   string
	Value []byte
}

type IndexEntry struct {
	Key    string
	Offset int64
}

type SumaryEntry struct {
	Key         string
	IndexOffSet int
}

func BuildSummary(index []IndexEntry, sparsity int) []SumaryEntry {
	if sparsity <= 0 {
		sparsity = 1
	}

	summary := make([]SumaryEntry, 0)

	for i := 0; i < len(index); i += sparsity {
		summary = append(summary, SumaryEntry{
			Key:         index[i].Key,
			IndexOffSet: i,
		})

	}
	return summary
}

func FindIndexRange(summary []SumaryEntry, key string) (int, int) {
	if len(summary) == 0 {
		return 0, 0
	}

	for i := 0; i < len(summary); i++ {
		if key >= summary[i].Key && key < summary[i+1].Key {
			return summary[i].IndexOffSet, summary[i+1].IndexOffSet
		}
	}
	return summary[len(summary)-1].IndexOffSet, -1
}

func SerializeEntry(e Entry) []byte {
	keyBytes := []byte(e.Key)
	keySize := uint32(len(keyBytes))

	valueSize := uint32(len(e.Value))

	buf := make([]byte, 4+4+len(keyBytes)+len(e.Value))
	offset := 0

	binary.BigEndian.PutUint32(buf[offset:], keySize)
	offset += 4

	binary.BigEndian.PutUint32(buf[offset:], valueSize)
	offset += 4

	copy(buf[offset:], keyBytes)
	offset += len(keyBytes)

	copy(buf[offset:], e.Value)

	return buf

}

func Write(path string, entries map[string][]byte) (*bloom.Filter, []IndexEntry, error) {

	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	filter := bloom.New(1000)
	index := make([]IndexEntry, 0)

	var offset int64 = 0

	for _, k := range keys {
		entry := Entry{
			Key:   k,
			Value: entries[k],
		}

		data := SerializeEntry(entry)

		index = append(index, IndexEntry{
			Key:    k,
			Offset: offset,
		})

		n, err := file.Write(data)
		if err != nil {
			return nil, nil, err
		}

		offset += int64(n)

		filter.Add(k)
	}

	return filter, index, nil
}
