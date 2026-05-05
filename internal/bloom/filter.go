package bloom

import (
	"encoding/binary"
	"errors"
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

func (f *Filter) Serialize() []byte {
	bitBytes := make([]byte, (len(f.bitset)+7)/8)
	for i, bit := range f.bitset {
		if bit {
			bitBytes[i/8] |= 1 << uint(i%8)
		}
	}

	buf := make([]byte, 4+len(bitBytes))
	binary.BigEndian.PutUint32(buf[0:4], uint32(f.size))
	copy(buf[4:], bitBytes)
	return buf
}

func Deserialize(data []byte) (*Filter, error) {
	if len(data) < 4 {
		return nil, errors.New("bloom: missing size")
	}

	size := binary.BigEndian.Uint32(data[0:4])
	expectedBytes := int((size + 7) / 8)
	if len(data) != 4+expectedBytes {
		return nil, errors.New("bloom: invalid data length")
	}

	filter := New(uint(size))
	for i := uint32(0); i < size; i++ {
		if data[4+int(i/8)]&(1<<uint(i%8)) != 0 {
			filter.bitset[i] = true
		}
	}

	return filter, nil
}
