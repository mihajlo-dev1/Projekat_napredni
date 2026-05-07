package internal

type RecordType uint8

const (
	// RecordPut predstavlja upis ili izmenu vrednosti.
	RecordPut RecordType = iota
	// RecordDelete predstavlja brisanje preko tombstone zapisa.
	RecordDelete
)

// Record je logicki zapis koji WAL cuva na disku.
type Record struct {
	Key       []byte
	Value     []byte
	Type      RecordType
	Timestamp int64
	TTL       int64
}

// MemtableEntry je zajednicki oblik zapisa koji memtable salje u SSTable.
type MemtableEntry struct {
	Key     string
	Value   []byte
	Deleted bool
}

// WAL opisuje minimum operacija koje engine ocekuje od write-ahead loga.
type WAL interface {
	AppendPut(key []byte, value []byte) error
	AppendDelete(key []byte) error
	Replay(applyPut func(key, value []byte), applyDelete func(key []byte)) error
	Close() error
}

// Memtable je interfejs za memorijski sloj pre flush-a na disk.
type Memtable interface {
	Put(key string, value []byte)
	Get(key string) ([]byte, bool)
	Delete(key string) bool
	IsFull() bool
	Entries() []MemtableEntry
	Clear()
}

// SSTable je disk tabela iz koje se cita po kljucu.
type SSTable interface {
	Get(key string) ([]byte, bool)
}
