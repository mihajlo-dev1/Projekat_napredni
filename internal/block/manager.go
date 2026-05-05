package block

import (
	"errors"
	"fmt"
	"io"
	"os"

	"kv-engine/internal/blockcache"
)

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

func (m *Manager) ReadFile(path string) ([]byte, error) {
	if err := m.validateBlockSize(); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("block manager: stat %q: %w", path, err)
	}
	if info.Size() == 0 {
		return nil, nil
	}

	blockCount := uint64((info.Size() + int64(m.blockSize) - 1) / int64(m.blockSize))
	data := make([]byte, 0, blockCount*uint64(m.blockSize))

	for index := uint64(0); index < blockCount; index++ {
		block, err := m.readBlockAllowPartial(path, index)
		if err != nil {
			return nil, err
		}
		data = append(data, block...)
	}

	return data[:info.Size()], nil
}

func (m *Manager) WriteFile(path string, data []byte) error {
	if err := m.validateBlockSize(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return fmt.Errorf("block manager: create parent for %q: %w", path, err)
	}

	if err := os.Truncate(path, 0); err != nil {
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
		if createErr != nil {
			return fmt.Errorf("block manager: create %q: %w", path, createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return closeErr
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("block manager: truncate %q: %w", path, err)
		}
	}

	if len(data) == 0 {
		return nil
	}

	for offset, index := 0, uint64(0); offset < len(data); offset, index = offset+m.blockSize, index+1 {
		end := offset + m.blockSize
		if end > len(data) {
			end = len(data)
		}
		if err := m.WriteBlock(path, index, data[offset:end]); err != nil {
			return err
		}
	}

	return os.Truncate(path, int64(len(data)))
}

func (m *Manager) AppendFile(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if err := m.validateBlockSize(); err != nil {
		return err
	}

	existing, err := m.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			existing = nil
		} else {
			return err
		}
	}

	existing = append(existing, data...)
	return m.WriteFile(path, existing)
}

func (m *Manager) readBlockAllowPartial(path string, index uint64) ([]byte, error) {
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
	n, err := file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("block manager: read block %d from %q: %w", index, path, err)
	}

	data = data[:n]
	if m.cache != nil {
		block := make([]byte, m.blockSize)
		copy(block, data)
		m.cache.Put(path, index, block)
	}

	return data, nil
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

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == os.PathSeparator {
			return path[:i]
		}
	}
	return "."
}
