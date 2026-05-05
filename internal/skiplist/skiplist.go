package skiplist

import (
	"math/rand"
	"time"

	"kv-engine/internal"
)

type Node struct {
	key     string
	value   []byte
	deleted bool
	forward []*Node
}

type SkipList struct {
	head     *Node
	level    int
	maxLevel int
	size     int
	rng      *rand.Rand
}

func New() *SkipList {
	const defaultMaxLevel = 16

	head := &Node{
		forward: make([]*Node, defaultMaxLevel),
	}

	return &SkipList{
		head:     head,
		level:    1,
		maxLevel: defaultMaxLevel,
		size:     0,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *SkipList) Put(key string, value []byte) {
	update := make([]*Node, s.maxLevel)
	current := s.head

	for level := s.level - 1; level >= 0; level-- {
		for current.forward[level] != nil && current.forward[level].key < key {
			current = current.forward[level]
		}
		update[level] = current
	}

	current = current.forward[0]
	if current != nil && current.key == key {
		current.value = append([]byte(nil), value...)
		current.deleted = false
		return
	}

	nodeLevel := s.randomLevel()
	if nodeLevel > s.level {
		for level := s.level; level < nodeLevel; level++ {
			update[level] = s.head
		}
		s.level = nodeLevel
	}

	newNode := &Node{
		key:     key,
		value:   append([]byte(nil), value...),
		deleted: false,
		forward: make([]*Node, nodeLevel),
	}

	for level := 0; level < nodeLevel; level++ {
		newNode.forward[level] = update[level].forward[level]
		update[level].forward[level] = newNode
	}

	s.size++
}

func (s *SkipList) randomLevel() int {
	level := 1
	for level < s.maxLevel && s.rng.Intn(2) == 0 {
		level++
	}
	return level
}

func (s *SkipList) Get(key string) ([]byte, bool) {
	current := s.head

	for level := s.level - 1; level >= 0; level-- {
		for current.forward[level] != nil && current.forward[level].key < key {
			current = current.forward[level]
		}
	}

	current = current.forward[0]
	if current != nil && current.key == key && !current.deleted {
		return append([]byte(nil), current.value...), true
	}

	return nil, false
}

func (s *SkipList) Delete(key string) bool {
	current := s.head

	for level := s.level - 1; level >= 0; level-- {
		for current.forward[level] != nil && current.forward[level].key < key {
			current = current.forward[level]
		}
	}

	current = current.forward[0]
	if current != nil && current.key == key && !current.deleted {
		current.deleted = true
		current.value = nil
		return true
	}

	return false
}

func (s *SkipList) Entries() []internal.MemtableEntry {
	entries := make([]internal.MemtableEntry, 0, s.size)
	current := s.head.forward[0]

	for current != nil {
		entry := internal.MemtableEntry{
			Key:     current.key,
			Deleted: current.deleted,
		}
		if current.value != nil {
			entry.Value = append([]byte(nil), current.value...)
		}

		entries = append(entries, entry)
		current = current.forward[0]
	}

	return entries
}
