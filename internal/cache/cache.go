package cache

type Cache struct {
	capacity int
}

func New(capacity int) *Cache {
	return &Cache{capacity: capacity}
}

func (c *Cache) Capacity() int {
	return c.capacity
}

func (c *Cache) Get(key string) ([]byte, bool) {
	return nil, false
}

func (c *Cache) Put(key string, value []byte) {}

func (c *Cache) Delete(key string) {}
