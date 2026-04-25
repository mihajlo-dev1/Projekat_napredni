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

type HashMap struct {
	buckets []*Node
	size    int
}

func New() *HashMap {
	return &HashMap{
		buckets: make([]*Node, defaultBucketCount),
	}
}

func (h *HashMap) bucketIndex(key string) int {
	hash := 0

	for i := 0; i < len(key); i++ {
		hash = hash*31 + int(key[i])
	}

	return hash % len(h.buckets)
}

func (h *HashMap) resize() {
	oldBuckets := h.buckets
	h.buckets = make([]*Node, len(oldBuckets)*2)

	for _, bucket := range oldBuckets {
		for current := bucket; current != nil; current = current.next {
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

func (h *HashMap) Put(key string, value []byte) {
	index := h.bucketIndex(key)
	current := h.buckets[index]

	for current != nil {
		if current.key == key {
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
		next:    h.buckets[index],
	}

	h.buckets[index] = newNode
	h.size++

	if float64(h.size)/float64(len(h.buckets)) > maxLoadFactor {
		h.resize()
	}
}

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
