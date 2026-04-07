package engine

import (
	"kv-engine/internal/memtable"
	"kv-engine/internal/wal"
)

type Engine struct {
	wal      *wal.WAL
	memtable *memtable.Memtable
}

func New(walPath string) (*Engine, error) {
	w, err := wal.Open(walPath)
	if err != nil {
		return nil, err

	}

	m := memtable.New(100)

	return &Engine{
		wal:      w,
		memtable: m,
	}, nil
}

func (e *Engine) Start() error {
	return e.wal.Replay(
		func(key []byte, value []byte) {
			e.memtable.Put(string(key), value)
		},
		func(key []byte) {
			e.memtable.Delete(string(key))
		},
	)
}

func (e *Engine) Put(key string, value []byte) error {
	if err := e.wal.AppendPut([]byte(key), value); err != nil {
		return err
	}

	e.memtable.Put(key, value)

	if e.memtable.IsFull() {
		entries := e.memtable.Entries()
		//dodati kasnije
		_ = entries
		e.memtable.Clear()
	}

	return nil
}

func (e *Engine) Delete(key string) error {
	if err := e.wal.AppendDelete([]byte(key)); err != nil {
		return err
	}
	e.memtable.Delete(key)
	return nil

}

func (e *Engine) Get(key string) ([]byte, bool) {
	return e.memtable.Get(key)
}
