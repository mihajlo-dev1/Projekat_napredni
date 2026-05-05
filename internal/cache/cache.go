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

func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

func (c *Cache) Capacity() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.capacity
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return nil, false
	}

	c.lru.MoveToFront(element)
	entry := element.Value.(*cacheEntry)
	return cloneBytes(entry.value), true
}

func (c *Cache) Put(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacity <= 0 {
		return
	}

	if element, ok := c.items[key]; ok {
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
		c.evictOldest()
	}
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.items[key]; ok {
		c.removeElement(element)
	}
}

func (c *Cache) evictOldest() {
	element := c.lru.Back()
	if element != nil {
		c.removeElement(element)
	}
}

func (c *Cache) removeElement(element *list.Element) {
	entry := element.Value.(*cacheEntry)
	delete(c.items, entry.key)
	c.lru.Remove(element)
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}

	clone := make([]byte, len(value))
	copy(clone, value)
	return clone
}
