package memtable

import (
	"kv-engine/internal"
	"kv-engine/internal/btree"
	"kv-engine/internal/hashmap"
	"kv-engine/internal/skiplist"
)

// backend je zajednicki interfejs za hashmap, skiplist i btree implementacije.
type backend interface {
	Put(key string, value []byte)
	Get(key string) ([]byte, bool)
	Delete(key string) bool
	Entries() []internal.MemtableEntry
}

// newBackend bira strukturu koju je korisnik zadao u konfiguraciji.
func newBackend(implementation string) (backend, error) {
	switch implementation {
	case "", "hashmap":
		return hashmap.New(), nil
	case "skiplist":
		return skiplist.New(), nil
	case "btree":
		return btree.New(4), nil
	default:
		return hashmap.New(), nil
	}
}

// resetBackend pravi praznu strukturu istog tipa posle flush-a.
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
