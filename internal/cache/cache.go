package cache

import (
	"container/list"
	"sync"
)

type Cache struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*list.Element
	lru      *list.List
}

type cacheEntry struct {
	key   string
	value []byte
}

// New pravi LRU cache za vrednosti procitane po kljucu.
func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

func (c *Cache) Capacity() int {
	// Lock postoji jer cache moze da se koristi iz vise gorutina.
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.capacity
}

// Get vraca vrednost i pomera je na pocetak LRU liste.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return nil, false
	}

	// Skoro koriscen element ide na front, da se kasnije ne izbaci prvi.
	c.lru.MoveToFront(element)
	entry := element.Value.(*cacheEntry)
	return cloneBytes(entry.value), true
}

// Put dodaje ili osvezava vrednost u cache-u.
func (c *Cache) Put(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacity <= 0 {
		// Kapacitet 0 znaci da je cache prakticno iskljucen.
		return
	}

	if element, ok := c.items[key]; ok {
		// Postojeci key dobija novu vrednost i postaje najskorije koriscen.
		entry := element.Value.(*cacheEntry)
		entry.value = cloneBytes(value)
		c.lru.MoveToFront(element)
		return
	}

	element := c.lru.PushFront(&cacheEntry{
		key:   key,
		value: cloneBytes(value),
	})
	c.items[key] = element

	if len(c.items) > c.capacity {
		// Kad predjemo kapacitet, izbacuje se najstarije koriscen element.
		c.evictOldest()
	}
}

// Delete uklanja key iz cache-a, npr. posle DELETE operacije.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.items[key]; ok {
		c.removeElement(element)
	}
}

// evictOldest izbacuje element sa kraja LRU liste.
func (c *Cache) evictOldest() {
	element := c.lru.Back()
	if element != nil {
		c.removeElement(element)
	}
}

// removeElement brise element i iz mape i iz liste.
func (c *Cache) removeElement(element *list.Element) {
	entry := element.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.lru.Remove(element)
}

// cloneBytes sprecava deljenje istog slice-a sa ostatkom programa.
func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}

	clone := make([]byte, len(value))
	copy(clone, value)
	return clone
}
