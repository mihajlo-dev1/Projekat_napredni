package sstable

import (
	"encoding/binary"
	"io"
	"kv-engine/internal/bloom"
	"kv-engine/internal/merkle"
	"os"
	"path/filepath"
	"sort"
)

const (
	dataFileName     = "data.bin"
	indexFileName    = "index.bin"
	summaryFileName  = "summary.bin"
	filterFileName   = "filter.bin"
	metadataFileName = "metadata.bin"
)

type SSTable struct {
	Dir          string
	DataPath     string
	IndexPath    string
	SummaryPath  string
	FilterPath   string
	MetadataPath string
	SummaryStep  int
}

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

func New(dir string, summaryStep int) *SSTable {
	if summaryStep <= 0 {
		summaryStep = 1
	}

	return &SSTable{
		Dir:          dir,
		DataPath:     filepath.Join(dir, dataFileName),
		IndexPath:    filepath.Join(dir, indexFileName),
		SummaryPath:  filepath.Join(dir, summaryFileName),
		FilterPath:   filepath.Join(dir, filterFileName),
		MetadataPath: filepath.Join(dir, metadataFileName),
		SummaryStep:  summaryStep,
	}
}

func Create(dir string, entries map[string][]byte, summaryStep int) (*SSTable, *bloom.Filter, []IndexEntry, error) {
	table := New(dir, summaryStep)

	if err := os.MkdirAll(table.Dir, 0755); err != nil {
		return nil, nil, nil, err
	}

	filter, index, err := table.WriteData(entries)
	if err != nil {
		return nil, nil, nil, err
	}

	return table, filter, index, nil
}

func (s *SSTable) WriteData(entries map[string][]byte) (*bloom.Filter, []IndexEntry, error) {
	return Write(s.DataPath, entries)
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

	for i := 0; i < len(summary)-1; i++ {
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

func DeserializeEntry(r io.Reader) (Entry, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Entry{}, err
	}

	keySize := binary.BigEndian.Uint32(header[0:4])
	valueSize := binary.BigEndian.Uint32(header[4:8])

	keyBytes := make([]byte, int(keySize))
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return Entry{}, err
	}

	value := make([]byte, int(valueSize))
	if _, err := io.ReadFull(r, value); err != nil {
		return Entry{}, err
	}

	return Entry{
		Key:   string(keyBytes),
		Value: value,
	}, nil
}

func SerializeIndexEntry(e IndexEntry) []byte {
	keyBytes := []byte(e.Key)
	keySize := uint32(len(keyBytes))

	buf := make([]byte, 4+8+len(keyBytes))
	offset := 0

	binary.BigEndian.PutUint32(buf[offset:], keySize)
	offset += 4

	binary.BigEndian.PutUint64(buf[offset:], uint64(e.Offset))
	offset += 8

	copy(buf[offset:], keyBytes)

	return buf
}

func DeserializeIndexEntry(r io.Reader) (IndexEntry, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return IndexEntry{}, err
	}

	keySize := binary.BigEndian.Uint32(header[0:4])
	offset := int64(binary.BigEndian.Uint64(header[4:12]))

	keyBytes := make([]byte, int(keySize))
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return IndexEntry{}, err
	}

	return IndexEntry{
		Key:    string(keyBytes),
		Offset: offset,
	}, nil
}

func SerializeSummaryEntry(e SumaryEntry) []byte {
	keyBytes := []byte(e.Key)
	keySize := uint32(len(keyBytes))

	buf := make([]byte, 4+8+len(keyBytes))
	offset := 0

	binary.BigEndian.PutUint32(buf[offset:], keySize)
	offset += 4

	binary.BigEndian.PutUint64(buf[offset:], uint64(e.IndexOffSet))
	offset += 8

	copy(buf[offset:], keyBytes)

	return buf
}

func DeserializeSummaryEntry(r io.Reader) (SumaryEntry, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return SumaryEntry{}, err
	}

	keySize := binary.BigEndian.Uint32(header[0:4])
	indexOffset := int(binary.BigEndian.Uint64(header[4:12]))

	keyBytes := make([]byte, int(keySize))
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return SumaryEntry{}, err
	}

	return SumaryEntry{
		Key:         string(keyBytes),
		IndexOffSet: indexOffset,
	}, nil
}

func SerializeSummaryBounds(minKey string, maxKey string) []byte {
	minKeyBytes := []byte(minKey)
	maxKeyBytes := []byte(maxKey)

	buf := make([]byte, 4+4+len(minKeyBytes)+len(maxKeyBytes))
	offset := 0

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(minKeyBytes)))
	offset += 4

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(maxKeyBytes)))
	offset += 4

	copy(buf[offset:], minKeyBytes)
	offset += len(minKeyBytes)

	copy(buf[offset:], maxKeyBytes)

	return buf
}

func DeserializeSummaryBounds(r io.Reader) (string, string, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return "", "", err
	}

	minKeySize := binary.BigEndian.Uint32(header[0:4])
	maxKeySize := binary.BigEndian.Uint32(header[4:8])

	minKeyBytes := make([]byte, int(minKeySize))
	if _, err := io.ReadFull(r, minKeyBytes); err != nil {
		return "", "", err
	}

	maxKeyBytes := make([]byte, int(maxKeySize))
	if _, err := io.ReadFull(r, maxKeyBytes); err != nil {
		return "", "", err
	}

	return string(minKeyBytes), string(maxKeyBytes), nil
}

func SerializeBloomFilter(filter *bloom.Filter) []byte {
	return filter.Serialize()
}

func DeserializeBloomFilter(r io.Reader) (*bloom.Filter, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return bloom.Deserialize(data)
}

func SerializeMerkleTree(tree *merkle.Tree) []byte {
	return tree.Serialize()
}

func DeserializeMerkleTree(r io.Reader) (*merkle.Tree, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return merkle.Deserialize(data)
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
