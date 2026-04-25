package internal

type RecordType uint8

const (
	RecordPut RecordType = iota
	RecordDelete
)

type Record struct {
	Key       []byte
	Value     []byte
	Type      RecordType
	Timestamp int64
	TTL       int64
}

type MemtableEntry struct {
	Key     string
	Value   []byte
	Deleted bool
}

type WAL interface {
	AppendPut(key []byte, value []byte) error
	AppendDelete(key []byte) error
	Replay(applyPut func(key, value []byte), applyDelete func(key []byte)) error
	Close() error
}

type Memtable interface {
	Put(key string, value []byte)
	Get(key string) ([]byte, bool)
	Delete(key string)
	IsFull() bool
	Entries() []MemtableEntry
	Clear()
}

type SSTable interface {
	Get(key string) ([]byte, bool)
}
