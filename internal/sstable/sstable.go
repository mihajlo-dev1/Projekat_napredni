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

	filter, index, records, err := table.writeData(entries)
	if err != nil {
		return nil, nil, nil, err
	}

	if err := table.writeIndex(index); err != nil {
		return nil, nil, nil, err
	}

	if err := table.writeSummary(index); err != nil {
		return nil, nil, nil, err
	}

	if err := table.writeFilter(filter); err != nil {
		return nil, nil, nil, err
	}

	if err := table.writeMetadata(merkle.New(records)); err != nil {
		return nil, nil, nil, err
	}

	return table, filter, index, nil
}

func (s *SSTable) WriteData(entries map[string][]byte) (*bloom.Filter, []IndexEntry, error) {
	return Write(s.DataPath, entries)
}

func (s *SSTable) Get(key string) ([]byte, bool) {
	filterFile, err := os.Open(s.FilterPath)
	if err != nil {
		return nil, false
	}
	defer filterFile.Close()

	filter, err := DeserializeBloomFilter(filterFile)
	if err != nil || !filter.MightContain(key) {
		return nil, false
	}

	summaryFile, err := os.Open(s.SummaryPath)
	if err != nil {
		return nil, false
	}
	defer summaryFile.Close()

	minKey, maxKey, err := DeserializeSummaryBounds(summaryFile)
	if err != nil || key < minKey || key > maxKey {
		return nil, false
	}

	summary, err := readSummary(summaryFile)
	if err != nil {
		return nil, false
	}

	start, end := FindIndexRange(summary, key)
	indexEntry, ok := s.findIndexEntry(key, start, end)
	if !ok {
		return nil, false
	}

	dataFile, err := os.Open(s.DataPath)
	if err != nil {
		return nil, false
	}
	defer dataFile.Close()

	if _, err := dataFile.Seek(indexEntry.Offset, io.SeekStart); err != nil {
		return nil, false
	}

	entry, err := DeserializeEntry(dataFile)
	if err != nil || entry.Key != key {
		return nil, false
	}

	return entry.Value, true
}

func (s *SSTable) ValidateMerkle() (bool, []int, error) {
	records, err := readDataRecords(s.DataPath)
	if err != nil {
		return false, nil, err
	}

	metadataFile, err := os.Open(s.MetadataPath)
	if err != nil {
		return false, nil, err
	}
	defer metadataFile.Close()

	tree, err := DeserializeMerkleTree(metadataFile)
	if err != nil {
		return false, nil, err
	}

	changed := tree.Validate(records)
	return len(changed) == 0, changed, nil
}

func (s *SSTable) writeData(entries map[string][]byte) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	return write(s.DataPath, entries)
}

func (s *SSTable) writeIndex(index []IndexEntry) error {
	file, err := os.Create(s.IndexPath)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, entry := range index {
		if err := writeFull(file, SerializeIndexEntry(entry)); err != nil {
			return err
		}
	}

	return nil
}

func (s *SSTable) writeSummary(index []IndexEntry) error {
	file, err := os.Create(s.SummaryPath)
	if err != nil {
		return err
	}
	defer file.Close()

	minKey, maxKey := "", ""
	if len(index) > 0 {
		minKey = index[0].Key
		maxKey = index[len(index)-1].Key
	}

	if err := writeFull(file, SerializeSummaryBounds(minKey, maxKey)); err != nil {
		return err
	}

	for _, entry := range BuildSummary(index, s.SummaryStep) {
		if err := writeFull(file, SerializeSummaryEntry(entry)); err != nil {
			return err
		}
	}

	return nil
}

func (s *SSTable) writeFilter(filter *bloom.Filter) error {
	return os.WriteFile(s.FilterPath, SerializeBloomFilter(filter), 0644)
}

func (s *SSTable) writeMetadata(tree *merkle.Tree) error {
	return os.WriteFile(s.MetadataPath, SerializeMerkleTree(tree), 0644)
}

func (s *SSTable) findIndexEntry(key string, start int, end int) (IndexEntry, bool) {
	file, err := os.Open(s.IndexPath)
	if err != nil {
		return IndexEntry{}, false
	}
	defer file.Close()

	for position := 0; ; position++ {
		entry, err := DeserializeIndexEntry(file)
		if err != nil {
			if err == io.EOF && end < 0 {
				return IndexEntry{}, false
			}
			return IndexEntry{}, false
		}

		if position < start {
			continue
		}
		if end >= 0 && position >= end {
			return IndexEntry{}, false
		}
		if entry.Key == key {
			return entry, true
		}
		if entry.Key > key {
			return IndexEntry{}, false
		}
	}
}

func readSummary(r io.Reader) ([]SumaryEntry, error) {
	summary := make([]SumaryEntry, 0)
	for {
		entry, err := DeserializeSummaryEntry(r)
		if err == io.EOF {
			return summary, nil
		}
		if err != nil {
			return nil, err
		}
		summary = append(summary, entry)
	}
}

func readDataRecords(path string) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records := make([][]byte, 0)
	for {
		entry, err := DeserializeEntry(file)
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		records = append(records, SerializeEntry(entry))
	}
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
	filter, index, _, err := write(path, entries)
	return filter, index, err
}

func write(path string, entries map[string][]byte) (*bloom.Filter, []IndexEntry, [][]byte, error) {

	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	file, err := os.Create(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer file.Close()

	filter := bloom.New(1000)
	index := make([]IndexEntry, 0)
	records := make([][]byte, 0, len(keys))

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

		if err := writeFull(file, data); err != nil {
			return nil, nil, nil, err
		}

		offset += int64(len(data))

		filter.Add(k)
		records = append(records, data)
	}

	return filter, index, records, nil
}

func writeFull(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
