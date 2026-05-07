package hashmap

import "kv-engine/internal"

const (
	defaultBucketCount = 16
	maxLoadFactor      = 0.75
)

type Node struct {
	key     string
	value   []byte
	deleted bool
	next    *Node
}

// HashMap je memtable backend sa bucket-ima i ulanacavanjem kolizija.
type HashMap struct {
	buckets []*Node
	size    int
}

func New() *HashMap {
	return &HashMap{
		buckets: make([]*Node, defaultBucketCount),
	}
}

// bucketIndex racuna u koji bucket ide kljuc.
func (h *HashMap) bucketIndex(key string) int {
	hash := uint64(0)

	for i := 0; i < len(key); i++ {
		// Prosta hash funkcija dovoljna za projektni backend.
		hash = hash*31 + uint64(key[i])
	}

	return int(hash % uint64(len(h.buckets)))
}

// resize duplira broj bucket-a kad load factor postane previsok.
func (h *HashMap) resize() {
	oldBuckets := h.buckets
	h.buckets = make([]*Node, len(oldBuckets)*2)

	for _, bucket := range oldBuckets {
		for current := bucket; current != nil; current = current.next {
			// Svaki stari cvor mora ponovo da se rasporedi po novom broju bucket-a.
			index := h.bucketIndex(current.key)
			newNode := &Node{
				key:     current.key,
				value:   append([]byte(nil), current.value...),
				deleted: current.deleted,
			}

			if h.buckets[index] == nil {
				h.buckets[index] = newNode
				continue
			}

			last := h.buckets[index]
			for last.next != nil {
				last = last.next
			}
			last.next = newNode
		}
	}
}

// Put ubacuje novu vrednost ili ozivljava obrisan kljuc.
func (h *HashMap) Put(key string, value []byte) {
	index := h.bucketIndex(key)
	current := h.buckets[index]

	for current != nil {
		if current.key == key {
			// Update postojeceg kljuca uklanja tombstone ako ga je bilo.
			current.value = append([]byte(nil), value...)
			current.deleted = false
			return
		}
		current = current.next
	}

	newNode := &Node{
		key:     key,
		value:   append([]byte(nil), value...),
		deleted: false,
		// Novi cvor se ubacuje na pocetak lanca u bucket-u.
		next: h.buckets[index],
	}

	h.buckets[index] = newNode
	h.size++

	if float64(h.size)/float64(len(h.buckets)) > maxLoadFactor {
		h.resize()
	}
}

// Get vraca kopiju vrednosti da pozivalac ne menja internu memoriju.
func (h *HashMap) Get(key string) ([]byte, bool) {
	index := h.bucketIndex(key)
	current := h.buckets[index]

	for current != nil {
		if current.key == key {
			if current.deleted {
				return nil, false
			}
			return append([]byte(nil), current.value...), true
		}
		current = current.next
	}

	return nil, false
}

// Delete ne izbacuje cvor, nego ga oznaci kao tombstone.
func (h *HashMap) Delete(key string) bool {
	index := h.bucketIndex(key)
	current := h.buckets[index]

	for current != nil {
		if current.key == key {
			if current.deleted {
				return false
			}
			current.deleted = true
			current.value = nil
			return true
		}
		current = current.next
	}

	return false
}

// Entries vraca i aktivne zapise i tombstone zapise.
func (h *HashMap) Entries() []internal.MemtableEntry {
	entries := make([]internal.MemtableEntry, 0, h.size)

	for _, bucket := range h.buckets {
		current := bucket
		for current != nil {
			entry := internal.MemtableEntry{
				Key:     current.key,
				Deleted: current.deleted,
			}
			if current.value != nil {
				entry.Value = append([]byte(nil), current.value...)
			}

			entries = append(entries, entry)
			current = current.next
		}
	}

	return entries
}
