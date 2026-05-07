package sstable

import (
	"bytes"
	"encoding/binary"
	"io"
	"kv-engine/internal"
	"kv-engine/internal/block"
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

// SSTable predstavlja jednu disk tabelu i putanje do njenih komponenti.
type SSTable struct {
	Dir          string
	DataPath     string
	IndexPath    string
	SummaryPath  string
	FilterPath   string
	MetadataPath string
	SummaryStep  int
	blocks       *block.Manager
}

// readSeekCloser je ono sto SSTable treba od fajl reader-a.
type readSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// Entry je jedan record u data.bin.
type Entry struct {
	Key     string
	Value   []byte
	Deleted bool
}

// IndexEntry mapira key na offset u data.bin.
type IndexEntry struct {
	Key    string
	Offset int64
}

// SumaryEntry je redji index: key pokazuje od kog index zapisa se trazi.
type SumaryEntry struct {
	Key         string
	IndexOffSet int
}

// New samo izracuna putanje fajlova za jednu SSTable.
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

// NewWithBlockManager pravi SSTable koja disk I/O radi preko block manager-a.
func NewWithBlockManager(dir string, summaryStep int, blocks *block.Manager) *SSTable {
	table := New(dir, summaryStep)
	table.blocks = blocks
	return table
}

// Create pravi SSTable iz mape key/value bez tombstone informacija.
func Create(dir string, entries map[string][]byte, summaryStep int) (*SSTable, *bloom.Filter, []IndexEntry, error) {
	return CreateWithBlockManager(dir, entries, summaryStep, nil)
}

// CreateWithBlockManager pise sve SSTable komponente: data, index, summary, filter, metadata.
func CreateWithBlockManager(dir string, entries map[string][]byte, summaryStep int, blocks *block.Manager) (*SSTable, *bloom.Filter, []IndexEntry, error) {
	table := NewWithBlockManager(dir, summaryStep, blocks)

	if err := os.MkdirAll(table.Dir, 0755); err != nil {
		return nil, nil, nil, err
	}

	// data.bin se pise prvi, jer iz njega nastaju index i Merkle record-i.
	filter, index, records, err := table.writeData(entries)
	if err != nil {
		return nil, nil, nil, err
	}

	// index.bin cuva tacan offset svakog key-a u data.bin.
	if err := table.writeIndex(index); err != nil {
		return nil, nil, nil, err
	}

	// summary.bin cuva min/max key i redji index za brzu pretragu.
	if err := table.writeSummary(index); err != nil {
		return nil, nil, nil, err
	}

	// filter.bin brzo kaze da key sigurno ne postoji ili mozda postoji.
	if err := table.writeFilter(filter); err != nil {
		return nil, nil, nil, err
	}

	// metadata.bin cuva Merkle stablo za proveru integriteta data.bin.
	if err := table.writeMetadata(merkle.New(records)); err != nil {
		return nil, nil, nil, err
	}

	return table, filter, index, nil
}

func CreateFromEntries(dir string, entries []internal.MemtableEntry, summaryStep int) (*SSTable, *bloom.Filter, []IndexEntry, error) {
	return CreateFromEntriesWithBlockManager(dir, entries, summaryStep, nil)
}

// CreateFromEntriesWithBlockManager cuva i tombstone zapise koje dobija iz memtable-a.
func CreateFromEntriesWithBlockManager(dir string, entries []internal.MemtableEntry, summaryStep int, blocks *block.Manager) (*SSTable, *bloom.Filter, []IndexEntry, error) {
	table := NewWithBlockManager(dir, summaryStep, blocks)

	if err := os.MkdirAll(table.Dir, 0755); err != nil {
		return nil, nil, nil, err
	}

	tableEntries := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		// MemtableEntry se prevodi u SSTable Entry format.
		tableEntries = append(tableEntries, Entry{
			Key:     entry.Key,
			Value:   entry.Value,
			Deleted: entry.Deleted,
		})
	}

	filter, index, records, err := table.writeEntries(tableEntries)
	if err != nil {
		return nil, nil, nil, err
	}

	// Ostale komponente se prave iz upravo upisanog data.bin.
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

// Get vraca samo vidljive vrednosti, tombstone tretira kao not found.
func (s *SSTable) Get(key string) ([]byte, bool) {
	value, found, deleted := s.Lookup(key)
	if !found || deleted {
		return nil, false
	}
	return value, true
}

// Lookup vraca value, found i deleted da engine moze da razlikuje tombstone.
func (s *SSTable) Lookup(key string) ([]byte, bool, bool) {
	filterFile, err := s.openReader(s.FilterPath)
	if err != nil {
		return nil, false, false
	}
	defer filterFile.Close()

	filter, err := DeserializeBloomFilter(filterFile)
	if err != nil || !filter.MightContain(key) {
		// Ako Bloom kaze false, key sigurno nije u ovoj tabeli.
		return nil, false, false
	}

	summaryFile, err := s.openReader(s.SummaryPath)
	if err != nil {
		return nil, false, false
	}
	defer summaryFile.Close()

	minKey, maxKey, err := DeserializeSummaryBounds(summaryFile)
	if err != nil || key < minKey || key > maxKey {
		// Summary bounds brzo odbace key van opsega ove tabele.
		return nil, false, false
	}

	// Summary suzava deo index.bin koji treba procitati.
	start, end, err := findIndexRange(summaryFile, key)
	if err != nil {
		return nil, false, false
	}

	// Index daje tacan offset u data.bin.
	indexEntry, ok := s.findIndexEntry(key, start, end)
	if !ok {
		return nil, false, false
	}

	dataFile, err := s.openReader(s.DataPath)
	if err != nil {
		return nil, false, false
	}
	defer dataFile.Close()

	if _, err := dataFile.Seek(indexEntry.Offset, io.SeekStart); err != nil {
		return nil, false, false
	}

	// Sa poznatog offseta se cita tacno jedan data entry.
	entry, err := DeserializeEntry(dataFile)
	if err != nil || entry.Key != key {
		return nil, false, false
	}

	return entry.Value, true, entry.Deleted
}

// ValidateMerkle proverava da li data.bin odgovara sacuvanom Merkle stablu.
func (s *SSTable) ValidateMerkle() (bool, []int, error) {
	metadataFile, err := s.openReader(s.MetadataPath)
	if err != nil {
		return false, nil, err
	}
	defer metadataFile.Close()

	tree, err := DeserializeMerkleTree(metadataFile)
	if err != nil {
		return false, nil, err
	}

	changed, err := s.validateDataAgainstMerkle(tree)
	if err != nil {
		return false, nil, err
	}
	return len(changed) == 0, changed, nil
}

func (s *SSTable) writeData(entries map[string][]byte) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	return writeWithBlockManager(s.DataPath, entries, s.blocks)
}

func (s *SSTable) writeEntries(entries []Entry) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	return writeEntriesWithBlockManager(s.DataPath, entries, s.blocks)
}

func (s *SSTable) writeIndex(index []IndexEntry) error {
	var file bytes.Buffer

	for _, entry := range index {
		// Svaki index entry je key + offset u data.bin.
		if err := writeFull(&file, SerializeIndexEntry(entry)); err != nil {
			return err
		}
	}

	return s.writeFile(s.IndexPath, file.Bytes())
}

func (s *SSTable) writeSummary(index []IndexEntry) error {
	var file bytes.Buffer

	minKey, maxKey := "", ""
	if len(index) > 0 {
		// Bounds idu na pocetak summary fajla.
		minKey = index[0].Key
		maxKey = index[len(index)-1].Key
	}

	if err := writeFull(&file, SerializeSummaryBounds(minKey, maxKey)); err != nil {
		return err
	}

	for _, entry := range BuildSummary(index, s.SummaryStep) {
		if err := writeFull(&file, SerializeSummaryEntry(entry)); err != nil {
			return err
		}
	}

	return s.writeFile(s.SummaryPath, file.Bytes())
}

func (s *SSTable) writeFilter(filter *bloom.Filter) error {
	return s.writeFile(s.FilterPath, SerializeBloomFilter(filter))
}

func (s *SSTable) writeMetadata(tree *merkle.Tree) error {
	return s.writeFile(s.MetadataPath, SerializeMerkleTree(tree))
}

func (s *SSTable) findIndexEntry(key string, start int, end int) (IndexEntry, bool) {
	file, err := s.openReader(s.IndexPath)
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
			// Preskacemo index zapise pre opsega koji je summary nasao.
			continue
		}
		if end >= 0 && position >= end {
			return IndexEntry{}, false
		}
		if entry.Key == key {
			return entry, true
		}
		if entry.Key > key {
			// Index je sortiran, pa posle veceg key-a nema potrebe traziti dalje.
			return IndexEntry{}, false
		}
	}
}

// findIndexRange cita summary i vraca [start, end) opseg u index.bin.
func findIndexRange(r io.Reader, key string) (int, int, error) {
	previous, err := DeserializeSummaryEntry(r)
	if err == io.EOF {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}

	for {
		next, err := DeserializeSummaryEntry(r)
		if err == io.EOF {
			return previous.IndexOffSet, -1, nil
		}
		if err != nil {
			return 0, 0, err
		}
		if key < next.Key {
			// Key pripada opsegu izmedju prethodnog i sledeceg summary entry-ja.
			return previous.IndexOffSet, next.IndexOffSet, nil
		}
		previous = next
	}
}

// validateDataAgainstMerkle poredi svaki data entry sa odgovarajucim leaf hash-om.
func (s *SSTable) validateDataAgainstMerkle(tree *merkle.Tree) ([]int, error) {
	file, err := s.openReader(s.DataPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	changed := make([]int, 0)
	index := 0

	for {
		entry, err := DeserializeEntry(file)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if !tree.MatchesLeaf(index, SerializeEntry(entry)) {
			// Indeks se vraca da korisnik zna koji record je promenjen.
			changed = append(changed, index)
		}
		index++
	}

	for index < tree.LeafCount() {
		// Metadata ocekuje vise listova nego sto data.bin trenutno ima.
		changed = append(changed, index)
		index++
	}

	return changed, nil
}

// BuildSummary uzima svaki n-ti index entry.
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

// FindIndexRange je in-memory verzija pretrage summary-ja.
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

// SerializeEntry pakuje data entry: deleted flag, velicine, key i value.
func SerializeEntry(e Entry) []byte {
	keyBytes := []byte(e.Key)
	keySize := uint32(len(keyBytes))

	valueSize := uint32(len(e.Value))

	buf := make([]byte, 1+4+4+len(keyBytes)+len(e.Value))
	offset := 0

	if e.Deleted {
		buf[offset] = 1
	}
	offset++

	binary.BigEndian.PutUint32(buf[offset:], keySize)
	offset += 4

	binary.BigEndian.PutUint32(buf[offset:], valueSize)
	offset += 4

	copy(buf[offset:], keyBytes)
	offset += len(keyBytes)

	copy(buf[offset:], e.Value)

	return buf

}

// DeserializeEntry cita jedan data entry iz reader-a.
func DeserializeEntry(r io.Reader) (Entry, error) {
	var header [9]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Entry{}, err
	}

	deleted := header[0] == 1
	keySize := binary.BigEndian.Uint32(header[1:5])
	valueSize := binary.BigEndian.Uint32(header[5:9])

	keyBytes := make([]byte, int(keySize))
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return Entry{}, err
	}

	value := make([]byte, int(valueSize))
	if _, err := io.ReadFull(r, value); err != nil {
		return Entry{}, err
	}

	return Entry{
		Key:     string(keyBytes),
		Value:   value,
		Deleted: deleted,
	}, nil
}

// SerializeIndexEntry pakuje key i offset iz data.bin.
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

// DeserializeIndexEntry cita jedan index zapis.
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

// SerializeSummaryEntry pakuje key i offset u index.bin.
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

// DeserializeSummaryEntry cita jedan summary zapis.
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

// SerializeSummaryBounds pakuje minimalni i maksimalni key SSTable-a.
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

// DeserializeSummaryBounds cita min/max key sa pocetka summary.bin.
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
	return bloom.DeserializeFromReader(r)
}

func SerializeMerkleTree(tree *merkle.Tree) []byte {
	return tree.Serialize()
}

func DeserializeMerkleTree(r io.Reader) (*merkle.Tree, error) {
	return merkle.DeserializeFromReader(r)
}

func Write(path string, entries map[string][]byte) (*bloom.Filter, []IndexEntry, error) {
	filter, index, _, err := write(path, entries)
	return filter, index, err
}

func write(path string, entries map[string][]byte) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	return writeWithBlockManager(path, entries, nil)
}

func writeWithBlockManager(path string, entries map[string][]byte, blocks *block.Manager) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	tableEntries := make([]Entry, 0, len(entries))
	for key, value := range entries {
		tableEntries = append(tableEntries, Entry{
			Key:   key,
			Value: value,
		})
	}
	return writeEntriesWithBlockManager(path, tableEntries, blocks)
}

func writeEntries(path string, entries []Entry) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	return writeEntriesWithBlockManager(path, entries, nil)
}

func writeEntriesWithBlockManager(path string, entries []Entry, blocks *block.Manager) (*bloom.Filter, []IndexEntry, [][]byte, error) {
	// SSTable je sortirana struktura, zato se entries sortiraju po key-u.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	var file bytes.Buffer

	filter := bloom.New(1000)
	index := make([]IndexEntry, 0)
	records := make([][]byte, 0, len(entries))

	var offset int64 = 0

	for _, entry := range entries {
		data := SerializeEntry(entry)

		// Offset pokazuje gde ovaj record pocinje u data.bin.
		index = append(index, IndexEntry{
			Key:    entry.Key,
			Offset: offset,
		})

		if err := writeFull(&file, data); err != nil {
			return nil, nil, nil, err
		}

		offset += int64(len(data))

		filter.Add(entry.Key)
		// records idu u Merkle stablo.
		records = append(records, data)
	}

	if blocks != nil {
		// Ako engine koristi block manager, i SSTable ide kroz isti sloj.
		if err := blocks.WriteFile(path, file.Bytes()); err != nil {
			return nil, nil, nil, err
		}
	} else if err := os.WriteFile(path, file.Bytes(), 0644); err != nil {
		return nil, nil, nil, err
	}

	return filter, index, records, nil
}

// openReader bira standardni os.Open ili block manager reader.
func (s *SSTable) openReader(path string) (readSeekCloser, error) {
	if s.blocks != nil {
		return s.blocks.OpenReader(path)
	}
	return os.Open(path)
}

// writeFile bira standardni os.WriteFile ili block manager.
func (s *SSTable) writeFile(path string, data []byte) error {
	if s.blocks != nil {
		return s.blocks.WriteFile(path, data)
	}
	return os.WriteFile(path, data, 0644)
}

// writeFull proverava da je writer primio sve bajtove.
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
