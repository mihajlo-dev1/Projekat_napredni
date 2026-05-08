package memtable

import (
	"sort"

	"kv-engine/internal"
)

type Entry = internal.MemtableEntry

type Memtable struct {
	backend     backend
	backendName string
	maxEntries  int
}

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

func (m *Memtable) Delete(key string) bool {
	return m.backend.Delete(key)
}

func (m *Memtable) Size() int {
	return len(m.backend.Entries())
}

func (m *Memtable) IsFull() bool {
	return m.Size() >= m.maxEntries
}

func (m *Memtable) Entries() []Entry {
	entries := m.backend.Entries()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	return entries
}

func (m *Memtable) Clear() {
	m.backend = resetBackend(m.backendName)
}

func (m *Memtable) IsDeleted(key string) bool {
	for _, entry := range m.backend.Entries() {
		if entry.Key == key {
			return entry.Deleted
		}
	}
	return false
}
