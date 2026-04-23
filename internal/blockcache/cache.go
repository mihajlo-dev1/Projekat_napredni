package blockcache

type Cache struct {
	capacity int
}

func New(capacity int) *Cache {
	return &Cache{capacity: capacity}
}

func (c *Cache) Get(path string, blockIndex uint64) ([]byte, bool) {
	return nil, false
}

func (c *Cache) Put(path string, blockIndex uint64, data []byte) {}

func (c *Cache) Delete(path string, blockIndex uint64) {}
