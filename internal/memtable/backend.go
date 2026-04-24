package memtable

import (
	"fmt"

	"kv-engine/internal"
	"kv-engine/internal/btree"
	"kv-engine/internal/hashmap"
	"kv-engine/internal/skiplist"
)

type backend interface {
	Put(key string, value []byte)
	Get(key string) ([]byte, bool)
	Delete(key string) bool
	Entries() map[string]internal.MemtableEntry
}

func newBackend(implementation string) (backend, error) {
	switch implementation {
	case "", "hashmap":
		return hashmap.New(), nil
	case "skiplist":
		return skiplist.New(), nil
	case "btree":
		return nil, fmt.Errorf("memtable backend %q not implemented yet", implementation)
	default:
		return hashmap.New(), nil
	}
}

func resetBackend(implementation string) backend {
	switch implementation {
	case "", "hashmap":
		return hashmap.New()
	case "skiplist":
		return skiplist.New()
	case "btree":
		return btree.New(4)
	default:
		return hashmap.New()
	}
}
