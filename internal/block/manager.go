package block

import (
	"errors"
	"fmt"
	"os"

	"kv-engine/internal/blockcache"
)

var ErrNotImplemented = errors.New("block manager: not implemented")
var ErrInvalidBlockSize = errors.New("block manager: supported block sizes are 4KB, 8KB, or 16KB")
var ErrBlockTooLarge = errors.New("block manager: data is larger than block size")

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
	if err := m.validateBlockSize(); err != nil {
		return nil, err
	}

	if m.cache != nil {
		if data, ok := m.cache.Get(path, index); ok {
			return data, nil
		}
	}

	offset, err := m.blockOffset(index)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("block manager: open %q for read: %w", path, err)
	}
	defer file.Close()

	data := make([]byte, m.blockSize)
	if _, err := file.ReadAt(data, offset); err != nil {
		return nil, fmt.Errorf("block manager: read block %d from %q: %w", index, path, err)
	}

	if m.cache != nil {
		m.cache.Put(path, index, data)
	}

	return data, nil
}

func (m *Manager) WriteBlock(path string, index uint64, data []byte) error {
	if err := m.validateBlockSize(); err != nil {
		return err
	}

	if len(data) > m.blockSize {
		return fmt.Errorf("%w: got %d bytes, block size is %d bytes", ErrBlockTooLarge, len(data), m.blockSize)
	}

	offset, err := m.blockOffset(index)
	if err != nil {
		return err
	}

	block := make([]byte, m.blockSize)
	copy(block, data)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("block manager: open %q for write: %w", path, err)
	}
	defer file.Close()

	if _, err := file.WriteAt(block, offset); err != nil {
		return fmt.Errorf("block manager: write block %d to %q: %w", index, path, err)
	}

	if m.cache != nil {
		m.cache.Put(path, index, block)
	}

	return nil
}

func (m *Manager) validateBlockSize() error {
	switch m.blockSize {
	case 4 * 1024, 8 * 1024, 16 * 1024:
		return nil
	default:
		return fmt.Errorf("%w: got %d bytes", ErrInvalidBlockSize, m.blockSize)
	}
}

func (m *Manager) blockOffset(index uint64) (int64, error) {
	const maxInt64 = uint64(1<<63 - 1)

	blockSize := uint64(m.blockSize)
	if index > maxInt64/blockSize {
		return 0, fmt.Errorf("block manager: block index %d overflows file offset for block size %d", index, m.blockSize)
	}

	return int64(index * blockSize), nil
}
