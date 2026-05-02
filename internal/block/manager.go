package block

import (
	"errors"

	"kv-engine/internal/blockcache"
)

var ErrNotImplemented = errors.New("block manager: not implemented")

type Manager struct {
	blockSize int
	cache     *blockcache.Cache
}

func New(blockSizeKB int, cache *blockcache.Cache) *Manager {
	return &Manager{
		blockSize: blockSizeKB * 1024,
		cache:     cache,
	}
}

func (m *Manager) BlockSize() int {
	return m.blockSize
}

func (m *Manager) ReadBlock(path string, index uint64) ([]byte, error) {
	return nil, ErrNotImplemented
}

func (m *Manager) WriteBlock(path string, index uint64, data []byte) error {
	return ErrNotImplemented
}
