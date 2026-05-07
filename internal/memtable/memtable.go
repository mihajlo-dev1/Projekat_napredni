package memtable

import (
	"sort"

	"kv-engine/internal"
)

type Entry = internal.MemtableEntry

// Memtable cuva najnovije promene u memoriji pre upisa u SSTable.
type Memtable struct {
	backend     backend
	backendName string
	maxEntries  int
}

// New pravi memtable sa hashmap backend-om.
func New(maxEntries int) *Memtable {
	memtable, err := NewWithBackend("hashmap", maxEntries)
	if err != nil {
		panic(err)
	}
	return memtable
}

// NewWithBackend pravi memtable nad izabranom strukturom.
func NewWithBackend(implementation string, maxEntries int) (*Memtable, error) {
	// Backend je stvarna struktura podataka, Memtable je samo tanak omotac.
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

// IsFull javlja engine-u kada treba flush.
func (m *Memtable) IsFull() bool {
	return m.Size() >= m.maxEntries
}

// Entries vraca sortirane zapise, spremne za SSTable.
func (m *Memtable) Entries() []Entry {
	entries := m.backend.Entries()
	// SSTable ocekuje sortirane kljuceve.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	return entries
}

func (m *Memtable) Clear() {
	m.backend = resetBackend(m.backendName)
}

// IsDeleted proverava da li kljuc postoji kao tombstone.
func (m *Memtable) IsDeleted(key string) bool {
	for _, entry := range m.backend.Entries() {
		if entry.Key == key {
			// Deleted=true znaci da kljuc postoji kao tombstone.
			return entry.Deleted
		}
	}
	return false
}
