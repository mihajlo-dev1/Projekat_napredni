package sstable

import (
	"errors"

	"kv-engine/internal/block"
	"kv-engine/internal/bloom"
	"kv-engine/internal/merkle"
)

var ErrNotImplemented = errors.New("sstable: not implemented")

type Table struct {
	dir          string
	blocks       *block.Manager
	filter       *bloom.Filter
	metadataTree *merkle.Tree
}

func New(dir string, blocks *block.Manager) *Table {
	return &Table{
		dir:    dir,
		blocks: blocks,
	}
}

func (t *Table) Get(key string) ([]byte, bool) {
	return nil, false
}

func (t *Table) Flush(level int) error {
	return ErrNotImplemented
}
