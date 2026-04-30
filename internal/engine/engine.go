package engine

import (
	"kv-engine/internal/memtable"
	"kv-engine/internal/wal"
)

type Engine struct {
	wal          *wal.WAL
	memtables    []*memtable.Memtable
	activeMem    *memtable.Memtable
	maxMemtables int
}

func New(walPath string) (*Engine, error) {
	w, err := wal.Open(walPath)
	if err != nil {
		return nil, err

	}

	m := memtable.New(100)

	return &Engine{
		wal:          w,
		memtables:    []*memtable.Memtable{m},
		activeMem:    m,
		maxMemtables: 3,
	}, nil
}

func (e *Engine) Start() error {
	return e.wal.Replay(
		func(key []byte, value []byte) {
			e.activeMem.Put(string(key), value)
		},
		func(key []byte) {
			e.activeMem.Delete(string(key))
		},
	)
}

func (e *Engine) Put(key string, value []byte) error {
	if err := e.wal.AppendPut([]byte(key), value); err != nil {
		return err
	}

	e.activeMem.Put(key, value)

	if e.activeMem.IsFull() {

		if len(e.memtables) < e.maxMemtables {
			newMem := memtable.New(100)
			e.memtables = append(e.memtables, newMem)
			e.activeMem = newMem
		} else {

			e.memtables = e.memtables[1:]

			newMem := memtable.New(100)
			e.memtables = append(e.memtables, newMem)
			e.activeMem = newMem
		}
	}

	return nil
}

func (e *Engine) Delete(key string) error {
	if err := e.wal.AppendDelete([]byte(key)); err != nil {
		return err
	}
	e.activeMem.Delete(key)
	if e.activeMem.IsFull() {

		if len(e.memtables) < e.maxMemtables {
			newMem := memtable.New(100)
			e.memtables = append(e.memtables, newMem)
			e.activeMem = newMem
		} else {
			e.memtables = e.memtables[1:]

			newMem := memtable.New(100)
			e.memtables = append(e.memtables, newMem)
			e.activeMem = newMem
		}
	}
	return nil

}

func (e *Engine) Get(key string) ([]byte, bool) {
	for i := len(e.memtables) - 1; i >= 0; i-- {
		if val, ok := e.memtables[i].Get(key); ok {
			return val, true
		}
	}
	return nil, false
}

func (e *Engine) Close() error {
	return e.wal.Close()
}
