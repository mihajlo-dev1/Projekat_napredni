package memtable

import (
	"sort"

	"kv-engine/internal"
)

// MemtableManager drzi jednu ili vise memtable instanci.
type MemtableManager struct {
	tables         []*Memtable
	implementation string
	maxEntries     int
	maxInstances   int
}

// NewMemtableManager pravi prvu aktivnu memtable.
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

// Put upisuje u najnoviju memtable i vraca true kad engine treba da flushuje.
func (m *MemtableManager) Put(key string, value []byte) bool {
	// Upis uvek ide u poslednju, aktivnu memtable.
	active := m.tables[len(m.tables)-1]
	active.Put(key, value)

	if !active.IsFull() {
		return false
	}

	if len(m.tables) >= m.maxInstances {
		// Sve dozvoljene instance su pune, engine treba da flushuje.
		return true
	}

	// Ako aktivna jeste puna, ali ima mesta za jos jednu instancu, otvaramo novu.
	newTable, _ := NewWithBackend(m.implementation, m.maxEntries)
	m.tables = append(m.tables, newTable)

	return false
}

// Get cita od najnovije ka najstarijoj memtable, zbog prepisivanja vrednosti.
func (m *MemtableManager) Get(key string) ([]byte, bool) {
	for i := len(m.tables) - 1; i >= 0; i-- {
		value, found := m.tables[i].Get(key)
		if found {
			return value, true
		}

		// Ako novija memtable ima tombstone, starije verzije se ne gledaju.
		if m.tables[i].IsDeleted(key) {
			return nil, false
		}
	}

	return nil, false
}

// IsDeleted trazi najnoviji tombstone za kljuc.
func (m *MemtableManager) IsDeleted(key string) bool {
	for i := len(m.tables) - 1; i >= 0; i-- {
		if _, found := m.tables[i].Get(key); found {
			return false
		}
		if m.tables[i].IsDeleted(key) {
			return true
		}
	}

	return false
}

// Delete upisuje tombstone cak i ako kljuc trenutno nije u aktivnoj memtable.
func (m *MemtableManager) Delete(key string) bool {
	active := m.tables[len(m.tables)-1]
	deleted := active.Delete(key)
	if !deleted {
		// Ako key ne postoji u aktivnoj memtable, pravimo novi tombstone zapis.
		active.Put(key, nil)
		active.Delete(key)
	}

	if !active.IsFull() {
		return false
	}

	if len(m.tables) >= m.maxInstances {
		// Sve dozvoljene instance su pune, engine treba da flushuje.
		return true
	}

	newTable, _ := NewWithBackend(m.implementation, m.maxEntries)
	m.tables = append(m.tables, newTable)

	return false
}

// Entries spaja memtable instance i zadrzava samo najnoviju verziju kljuca.
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
					// Vec smo uzeli noviju verziju istog kljuca.
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

// Clear brise sve instance i pravi novu praznu aktivnu memtable.
func (m *MemtableManager) Clear() {
	table, _ := NewWithBackend(m.implementation, m.maxEntries)
	m.tables = []*Memtable{table}
}
