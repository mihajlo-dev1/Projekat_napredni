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
	Entries() map[string]MemtableEntry
	Clear()
}

type SSTable interface {
	Get(key string) ([]byte, bool)
}
