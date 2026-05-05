package memtable

import (
	"sort"

	"kv-engine/internal"
)

type MemtableManager struct {
	tables         []*Memtable
	implementation string
	maxEntries     int
	maxInstances   int
}

func NewMemtableManager(implementation string, maxEntries int, maxInstances int) (*MemtableManager, error) {
	if maxInstances < 1 {
		maxInstances = 1
	}

	table, err := NewWithBackend(implementation, maxEntries)
	if err != nil {
		return nil, err
	}

	return &MemtableManager{
		tables:         []*Memtable{table},
		implementation: implementation,
		maxEntries:     maxEntries,
		maxInstances:   maxInstances,
	}, nil
}

func (m *MemtableManager) Put(key string, value []byte) bool {
	active := m.tables[len(m.tables)-1]
	active.Put(key, value)

	if !active.IsFull() {
		return false
	}

	if len(m.tables) >= m.maxInstances {
		// All allowed memtable instances are full, so the caller should flush them.
		return true
	}

	newTable, _ := NewWithBackend(m.implementation, m.maxEntries)
	m.tables = append(m.tables, newTable)

	return false
}

func (m *MemtableManager) Get(key string) ([]byte, bool) {
	for i := len(m.tables) - 1; i >= 0; i-- {
		value, found := m.tables[i].Get(key)
		if found {
			return value, true
		}

		if m.tables[i].IsDeleted(key) {
			return nil, false
		}
	}

	return nil, false
}

func (m *MemtableManager) Delete(key string) bool {
	active := m.tables[len(m.tables)-1]
	deleted := active.Delete(key)
	if !deleted {
		active.Put(key, nil)
		active.Delete(key)
	}

	if !active.IsFull() {
		return false
	}

	if len(m.tables) >= m.maxInstances {
		// All allowed memtable instances are full, so the caller should flush them.
		return true
	}

	newTable, _ := NewWithBackend(m.implementation, m.maxEntries)
	m.tables = append(m.tables, newTable)

	return false
}

func (m *MemtableManager) Entries() []internal.MemtableEntry {
	totalSize := 0
	for _, table := range m.tables {
		totalSize += table.Size()
	}

	entries := make([]internal.MemtableEntry, 0, totalSize)

	for i := len(m.tables) - 1; i >= 0; i-- {
		for _, entry := range m.tables[i].Entries() {
			exists := false
			for _, existing := range entries {
				if existing.Key == entry.Key {
					exists = true
					break
				}
			}

			if !exists {
				entries = append(entries, entry)
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	return entries
}

func (m *MemtableManager) Clear() {
	table, _ := NewWithBackend(m.implementation, m.maxEntries)
	m.tables = []*Memtable{table}
}
