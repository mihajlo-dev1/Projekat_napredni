package bloom

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"io"
)

type Filter struct {
	bitset []bool
	size   uint
}

// New pravi Bloom filter sa zadatim brojem bitova.
func New(size uint) *Filter {
	return &Filter{
		bitset: make([]bool, size),
		size:   size,
	}
}

// Add hashira kljuc i pali jedan bit u filteru.
func (f *Filter) Add(key string) {
	hash := fnv.New32a()
	hash.Write([]byte(key))

	index := hash.Sum32() % uint32(f.size)
	f.bitset[index] = true
}

// MightContain moze vratiti false sigurno, ili true kao "mozda postoji".
func (f *Filter) MightContain(key string) bool {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	index := hash.Sum32() % uint32(f.size)
	return f.bitset[index]
}

// Serialize pakuje velicinu i bitset u bajtove za filter.bin.
func (f *Filter) Serialize() []byte {
	bitBytes := make([]byte, (len(f.bitset)+7)/8)
	for i, bit := range f.bitset {
		if bit {
			// Osam bool vrednosti se pakuje u jedan bajt.
			bitBytes[i/8] |= 1 << uint(i%8)
		}
	}

	buf := make([]byte, 4+len(bitBytes))
	binary.BigEndian.PutUint32(buf[0:4], uint32(f.size))
	copy(buf[4:], bitBytes)
	return buf
}

func Deserialize(data []byte) (*Filter, error) {
	return DeserializeFromReader(bytes.NewReader(data))
}

// DeserializeFromReader cita Bloom filter iz fajla.
func DeserializeFromReader(r io.Reader) (*Filter, error) {
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return nil, errors.New("bloom: missing size")
	}

	size := binary.BigEndian.Uint32(sizeBuf[:])
	expectedBytes := int((size + 7) / 8)
	bitBytes := make([]byte, expectedBytes)
	if _, err := io.ReadFull(r, bitBytes); err != nil {
		return nil, errors.New("bloom: invalid data length")
	}

	var extra [1]byte
	n, err := r.Read(extra[:])
	if err != io.EOF || n != 0 {
		// Posle bitset-a ne sme biti dodatnih bajtova.
		return nil, errors.New("bloom: invalid data length")
	}

	filter := New(uint(size))
	for i := uint32(0); i < size; i++ {
		// Svaki bit iz bajtova se vraca u bool bitset.
		if bitBytes[int(i/8)]&(1<<uint(i%8)) != 0 {
			filter.bitset[i] = true
		}
	}

	return filter, nil
}
