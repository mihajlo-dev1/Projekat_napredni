package internal

type RecordType uint8

const (
	RecordPut RecordType = iota + 1
	RecordDelete
)

type Record struct {
	Key       string
	Value     []byte
	Type      RecordType
	Timestamp int64
	TTL       int64
}

type WAL interface {
	Append(r Record) error
	Replay() ([]Record, error)
}

type Memtable interface {
	Put(r Record)
	Get(key string) (Record, bool)
	IsFull() bool
	FlushSorted() []Record
}

type SSTable interface {
	Get(key string) (Record, bool)
}
