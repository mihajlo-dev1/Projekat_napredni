package memtable

import "kv-engine/internal"

type Entry = internal.MemtableEntry

type Memtable struct {
	backend     backend
	backendName string
	maxEntries  int
}

// inicijalizacija prazne memtable
func New(maxEntries int) *Memtable {
	memtable, err := NewWithBackend("hashmap", maxEntries)
	if err != nil {
		panic(err)
	}
	return memtable
}

func NewWithBackend(implementation string, maxEntries int) (*Memtable, error) {
	storage, err := newBackend(implementation)
	if err != nil {
		return nil, err
	}

	return &Memtable{
		backend:     storage,
		backendName: implementation,
		maxEntries:  maxEntries,
	}, nil
}

func (m *Memtable) Put(key string, value []byte) {
	m.backend.Put(key, value)
}

func (m *Memtable) Get(key string) ([]byte, bool) {
	return m.backend.Get(key)
}

func (m *Memtable) Delete(key string) {
	m.backend.Delete(key)
}

func (m *Memtable) Size() int {
	return len(m.backend.Entries())
}

func (m *Memtable) IsFull() bool {
	return m.Size() >= m.maxEntries
}

func (m *Memtable) Entries() map[string]Entry {
	return m.backend.Entries()
}

func (m *Memtable) Clear() {
	m.backend = resetBackend(m.backendName)
}

func (m *Memtable) IsDeleted(key string) bool {
	entry, ok := m.backend.Entries()[key]
	if !ok {
		return false
	}
	return entry.Deleted
}
