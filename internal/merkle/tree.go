package merkle

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const hashSize = sha256.Size

type Tree struct {
	levels [][][]byte
}

func New(values [][]byte) *Tree {
	leaves := make([][]byte, 0, len(values))
	for _, value := range values {
		hash := sha256.Sum256(value)
		leaves = append(leaves, append([]byte(nil), hash[:]...))
	}

	return newFromLeaves(leaves)
}

func (t *Tree) Root() []byte {
	if t == nil || len(t.levels) == 0 {
		return nil
	}

	rootLevel := t.levels[len(t.levels)-1]
	if len(rootLevel) == 0 {
		return nil
	}

	return append([]byte(nil), rootLevel[0]...)
}

func (t *Tree) Validate(values [][]byte) []int {
	current := New(values)
	if t == nil || len(t.levels) == 0 {
		if len(values) == 0 {
			return nil
		}
		changed := make([]int, len(values))
		for i := range changed {
			changed[i] = i
		}
		return changed
	}

	oldLeaves := t.levels[0]
	newLeaves := current.levels[0]
	limit := len(oldLeaves)
	if len(newLeaves) < limit {
		limit = len(newLeaves)
	}

	changed := make([]int, 0)
	for i := 0; i < limit; i++ {
		if !equalHash(oldLeaves[i], newLeaves[i]) {
			changed = append(changed, i)
		}
	}
	for i := limit; i < len(oldLeaves); i++ {
		changed = append(changed, i)
	}
	for i := limit; i < len(newLeaves); i++ {
		changed = append(changed, i)
	}

	return changed
}

func (t *Tree) Serialize() []byte {
	if t == nil || len(t.levels) == 0 {
		buf := make([]byte, 4)
		return buf
	}

	leaves := t.levels[0]
	buf := make([]byte, 4+len(leaves)*hashSize)
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(leaves)))

	offset := 4
	for _, leaf := range leaves {
		copy(buf[offset:], leaf)
		offset += hashSize
	}

	return buf
}

func Deserialize(data []byte) (*Tree, error) {
	if len(data) < 4 {
		return nil, errors.New("merkle: missing leaf count")
	}

	count := int(binary.BigEndian.Uint32(data[0:4]))
	expected := 4 + count*hashSize
	if len(data) != expected {
		return nil, errors.New("merkle: invalid data length")
	}

	leaves := make([][]byte, 0, count)
	offset := 4
	for i := 0; i < count; i++ {
		leaf := append([]byte(nil), data[offset:offset+hashSize]...)
		leaves = append(leaves, leaf)
		offset += hashSize
	}

	return newFromLeaves(leaves), nil
}

func newFromLeaves(leaves [][]byte) *Tree {
	levels := [][][]byte{copyLevel(leaves)}
	if len(leaves) == 0 {
		return &Tree{levels: levels}
	}

	current := copyLevel(leaves)
	for len(current) > 1 {
		next := make([][]byte, 0, (len(current)+1)/2)
		for i := 0; i < len(current); i += 2 {
			left := current[i]
			right := left
			if i+1 < len(current) {
				right = current[i+1]
			}

			combined := make([]byte, 0, len(left)+len(right))
			combined = append(combined, left...)
			combined = append(combined, right...)
			hash := sha256.Sum256(combined)
			next = append(next, append([]byte(nil), hash[:]...))
		}

		levels = append(levels, copyLevel(next))
		current = next
	}

	return &Tree{levels: levels}
}

func copyLevel(level [][]byte) [][]byte {
	copied := make([][]byte, 0, len(level))
	for _, hash := range level {
		copied = append(copied, append([]byte(nil), hash...))
	}
	return copied
}

func equalHash(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
