package engine

import "kv-engine/internal"

type Engine struct {
	wal      internal.WAL
	memtable internal.Memtable
	sstable  internal.SSTable
}

func New(
	wal internal.WAL,
	memtable internal.Memtable,
	sstable internal.SSTable,
) *Engine {
	return &Engine{
		wal:      wal,
		memtable: memtable,
		sstable:  sstable,
	}
}
