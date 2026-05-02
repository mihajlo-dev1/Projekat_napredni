package bloom

import (
	"hash/fnv"
)

type Filter struct {
	bitset []bool
	size   uint
}

func New(size uint) *Filter {
	return &Filter{
		bitset: make([]bool, size),
		size:   size,
	}
}
func (f *Filter) Add(key string) {
	hash := fnv.New32a()
	hash.Write([]byte(key))

	index := hash.Sum32() % uint32(f.size)
	f.bitset[index] = true
}

func (f *Filter) MightContain(key string) bool {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	index := hash.Sum32() % uint32(f.size)
	return f.bitset[index]
}
