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

func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		items:    make(map[cacheKey]*list.Element),
		lru:      list.New(),
	}
}

func (c *Cache) Get(path string, blockIndex uint64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[cacheKey{path: path, blockIndex: blockIndex}]
	if !ok {
		return nil, false
	}

	c.lru.MoveToFront(element)
	entry := element.Value.(*cacheEntry)
	return cloneBytes(entry.data), true
}

func (c *Cache) Put(path string, blockIndex uint64, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacity <= 0 {
		return
	}

	key := cacheKey{path: path, blockIndex: blockIndex}
	if element, ok := c.items[key]; ok {
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
		c.evictOldest()
	}
}

func (c *Cache) Delete(path string, blockIndex uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey{path: path, blockIndex: blockIndex}
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

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}

	clone := make([]byte, len(data))
	copy(clone, data)
	return clone
}
