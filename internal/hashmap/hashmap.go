package hashmap

import "kv-engine/internal"

type HashMap struct {
	data map[string]internal.MemtableEntry
}

func New() *HashMap {
	return &HashMap{
		data: make(map[string]internal.MemtableEntry),
	}
}

func (h *HashMap) Put(key string, value []byte) {
	h.data[key] = internal.MemtableEntry{
		Value:   append([]byte(nil), value...),
		Deleted: false,
	}
}

func (h *HashMap) Get(key string) ([]byte, bool) {
	entry, ok := h.data[key]
	if !ok || entry.Deleted {
		return nil, false
	}
	return append([]byte(nil), entry.Value...), true
}

func (h *HashMap) Delete(key string) bool {
	entry, ok := h.data[key]
	if !ok {
		return false
	}

	entry.Deleted = true
	entry.Value = nil
	h.data[key] = entry
	return true
}

func (h *HashMap) Entries() map[string]internal.MemtableEntry {
	result := make(map[string]internal.MemtableEntry, len(h.data))
	for key, entry := range h.data {
		cloned := internal.MemtableEntry{
			Deleted: entry.Deleted,
		}
		if entry.Value != nil {
			cloned.Value = append([]byte(nil), entry.Value...)
		}
		result[key] = cloned
	}
	return result
}
