package blockcache

import (
	"container/list"
	"sync"
)

type Cache struct {
	capacity int
	mu       sync.Mutex
	items    map[cacheKey]*list.Element
	lru      *list.List
}

type cacheKey struct {
	path       string
	blockIndex uint64
}

type cacheEntry struct {
	key  cacheKey
	data []byte
}

// New pravi LRU cache za blokove fajlova.
func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		items:    make(map[cacheKey]*list.Element),
		lru:      list.New(),
	}
}

// Get vraca blok ako je vec ucitan iz istog fajla.
func (c *Cache) Get(path string, blockIndex uint64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[cacheKey{path: path, blockIndex: blockIndex}]
	if !ok {
		return nil, false
	}

	// Blok koji je pogodjen postaje najskorije koriscen.
	c.lru.MoveToFront(element)
	entry := element.Value.(*cacheEntry)
	return cloneBytes(entry.data), true
}

// Put pamti blok pod kombinacijom putanja + indeks bloka.
func (c *Cache) Put(path string, blockIndex uint64, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacity <= 0 {
		// Kapacitet 0 iskljucuje block cache.
		return
	}

	key := cacheKey{path: path, blockIndex: blockIndex}
	if element, ok := c.items[key]; ok {
		// Ako blok vec postoji, samo mu osvezimo podatke i LRU poziciju.
		entry := element.Value.(*cacheEntry)
		entry.data = cloneBytes(data)
		c.lru.MoveToFront(element)
		return
	}

	element := c.lru.PushFront(&cacheEntry{
		key:  key,
		data: cloneBytes(data),
	})
	c.items[key] = element

	if len(c.items) > c.capacity {
		// Najstariji blok ispada kad se predje kapacitet.
		c.evictOldest()
	}
}

// Delete se koristi kada je blok prepisan ili fajl promenjen.
func (c *Cache) Delete(path string, blockIndex uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{path: path, blockIndex: blockIndex}
	if element, ok := c.items[key]; ok {
		c.removeElement(element)
	}
}

// evictOldest izbacuje najmanje skoro koriscen blok.
func (c *Cache) evictOldest() {
	element := c.lru.Back()
	if element != nil {
		c.removeElement(element)
	}
}

// removeElement cisti i mapu i LRU listu.
func (c *Cache) removeElement(element *list.Element) {
	entry := element.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.lru.Remove(element)
}

// cloneBytes cuva cache od spoljnog menjanja slice-a.
func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}

	clone := make([]byte, len(data))
	copy(clone, data)
	return clone
}
