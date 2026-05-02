package skiplist

import "kv-engine/internal"

type SkipList struct{}

func New() *SkipList {
	return &SkipList{}
}

func (s *SkipList) Put(key string, value []byte) {}

func (s *SkipList) Get(key string) ([]byte, bool) {
	return nil, false
}

func (s *SkipList) Delete(key string) bool {
	return false
}

func (s *SkipList) Entries() map[string]internal.MemtableEntry {
	return nil
}
