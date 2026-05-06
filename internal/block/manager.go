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

type fileReader struct {
	manager *Manager
	path    string
	size    int64
	offset  int64
	block   []byte
	index   uint64
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

func (m *Manager) OpenReader(path string) (io.Reader, error) {
	if err := m.validateBlockSize(); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("block manager: stat %q: %w", path, err)
	}

	return &fileReader{
		manager: m,
		path:    path,
		size:    info.Size(),
	}, nil
}

func (m *Manager) EnsureFileSize(path string, size int64) error {
	if err := m.validateBlockSize(); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("block manager: negative file size %d", size)
	}
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return fmt.Errorf("block manager: create parent for %q: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("block manager: open %q for sizing: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Truncate(path, size); err != nil {
		return fmt.Errorf("block manager: resize %q: %w", path, err)
	}

	if m.cache != nil {
		blockCount := uint64((size + int64(m.blockSize) - 1) / int64(m.blockSize))
		for index := uint64(0); index < blockCount; index++ {
			m.cache.Delete(path, index)
		}
	}

	return nil
}

func (m *Manager) WriteAt(path string, offset int64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if err := m.validateBlockSize(); err != nil {
		return err
	}
	if offset < 0 {
		return fmt.Errorf("block manager: negative write offset %d", offset)
	}
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return fmt.Errorf("block manager: create parent for %q: %w", path, err)
	}

	for len(data) > 0 {
		blockIndex := uint64(offset / int64(m.blockSize))
		blockOffset := int(offset % int64(m.blockSize))
		chunkSize := m.blockSize - blockOffset
		if chunkSize > len(data) {
			chunkSize = len(data)
		}

		blockData := make([]byte, m.blockSize)
		if blockOffset != 0 || chunkSize != m.blockSize {
			existing, err := m.readBlockAllowPartial(path, blockIndex)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			copy(blockData, existing)
		}

		copy(blockData[blockOffset:], data[:chunkSize])
		if err := m.WriteBlock(path, blockIndex, blockData); err != nil {
			return err
		}

		data = data[chunkSize:]
		offset += int64(chunkSize)
	}

	return nil
}

func (r *fileReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.offset >= r.size {
		return 0, io.EOF
	}

	total := 0
	for len(p) > 0 && r.offset < r.size {
		blockIndex := uint64(r.offset / int64(r.manager.blockSize))
		if r.block == nil || r.index != blockIndex {
			block, err := r.manager.readBlockAllowPartial(r.path, blockIndex)
			if err != nil {
				if total > 0 {
					return total, nil
				}
				return 0, err
			}
			r.block = block
			r.index = blockIndex
		}

		blockOffset := int(r.offset % int64(r.manager.blockSize))
		remainingInBlock := len(r.block) - blockOffset
		remainingInFile := int(r.size - r.offset)
		if remainingInBlock > remainingInFile {
			remainingInBlock = remainingInFile
		}
		if remainingInBlock <= 0 {
			break
		}

		n := copy(p, r.block[blockOffset:blockOffset+remainingInBlock])
		p = p[n:]
		r.offset += int64(n)
		total += n
	}

	if total == 0 {
		return 0, io.EOF
	}
	return total, nil
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
