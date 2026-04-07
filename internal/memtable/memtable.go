package memtable

type Entry struct {
	Value   []byte
	Deleted bool
}

type Memtable struct {
	data       map[string]Entry
	maxEntries int
}

// inicijalizacija prazne memtable
func New(maxEntries int) *Memtable {
	return &Memtable{
		data:       make(map[string]Entry),
		maxEntries: maxEntries,
	}
}

func (m *Memtable) Put(key string, value []byte) {
	m.data[key] = Entry{
		Value:   value,
		Deleted: false,
	}
}

func (m *Memtable) Get(key string) ([]byte, bool) {
	entry, ok := m.data[key]
	if !ok {
		return nil, false
	}

	if entry.Deleted {
		return nil, false
	}

	return entry.Value, true
}
func (m *Memtable) Delete(key string) {
	m.data[key] = Entry{
		Deleted: true,
	}
}
func (m *Memtable) Size() int {
	return len(m.data)
}

func (m *Memtable) IsFull() bool {
	return m.Size() >= m.maxEntries
}

func (m *Memtable) Entries() map[string]Entry {
	result := make(map[string]Entry, len(m.data))
	for key, entry := range m.data {
		result[key] = entry
	}
	return result
}

func (m *Memtable) Clear() {
	m.data = make(map[string]Entry)
}

func (m *Memtable) IsDeleted(key string) bool {
	entry, ok := m.data[key]
	if !ok {
		return false
	}
	return entry.Deleted
}
