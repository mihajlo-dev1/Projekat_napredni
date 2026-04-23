package btree

import "kv-engine/internal"

type BTree struct{}

func New(order int) *BTree {
	return &BTree{}
}

func (t *BTree) Put(key string, value []byte) {}

func (t *BTree) Get(key string) ([]byte, bool) {
	return nil, false
}

func (t *BTree) Delete(key string) bool {
	return false
}

func (t *BTree) Entries() map[string]internal.MemtableEntry {
	return nil
}
