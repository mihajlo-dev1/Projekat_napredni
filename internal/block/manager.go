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

// Manager cita i pise fajlove po fiksnim blokovima.
type Manager struct {
	blockSize int
	cache     *blockcache.Cache
}

type Reader interface {
	io.Reader
	io.Seeker
	io.Closer
}

// fileReader je reader koji ispod koristi Manager i njegov block cache.
type fileReader struct {
	manager *Manager
	path    string
	size    int64
	offset  int64
	block   []byte
	index   uint64
}

// New prima velicinu u KB, a interno radi sa bajtovima.
func New(blockSizeKB int, cache *blockcache.Cache) *Manager {
	return &Manager{
		blockSize: blockSizeKB * 1024,
		cache:     cache,
	}
}

func (m *Manager) BlockSize() int {
	return m.blockSize
}

// ReadBlock cita tacno jedan pun blok.
func (m *Manager) ReadBlock(path string, index uint64) ([]byte, error) {
	if err := m.validateBlockSize(); err != nil {
		return nil, err
	}

	if m.cache != nil {
		if data, ok := m.cache.Get(path, index); ok {
			// Cache hit: ne idemo do diska.
			return data, nil
		}
	}

	// Indeks bloka se prevodi u byte offset u fajlu.
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
		// Procitani blok se pamti za sledeci read.
		m.cache.Put(path, index, data)
	}

	return data, nil
}

// WriteBlock upisuje jedan blok; kraci data se dopunjava nulama.
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
	// Fajl na disku uvek dobija ceo blok.
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
		// Cache mora da vidi novu verziju bloka.
		m.cache.Put(path, index, block)
	}

	return nil
}

// ReadFile cita ceo fajl preko blokova.
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
		// Poslednji blok sme biti kraci od blockSize.
		block, err := m.readBlockAllowPartial(path, index)
		if err != nil {
			return nil, err
		}
		data = append(data, block...)
	}

	// Vraca se tacna velicina fajla, bez padding nula iz poslednjeg bloka.
	return data[:info.Size()], nil
}

// OpenReader daje sekvencijalni reader preko block manager-a.
func (m *Manager) OpenReader(path string) (Reader, error) {
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

// EnsureFileSize pravi fajl i postavlja mu tacnu velicinu.
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
			// Posle truncate-a stari blokovi u cache-u nisu pouzdani.
			m.cache.Delete(path, index)
		}
	}

	return nil
}

// WriteAt upisuje proizvoljan niz bajtova na proizvoljan offset.
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
			// Za delimican upis prvo citamo stari blok da ne pregazimo ostatak.
			existing, err := m.readBlockAllowPartial(path, blockIndex)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			copy(blockData, existing)
		}

		// Upisujemo samo deo data koji staje u trenutni blok.
		copy(blockData[blockOffset:], data[:chunkSize])
		if err := m.WriteBlock(path, blockIndex, blockData); err != nil {
			return err
		}

		data = data[chunkSize:]
		offset += int64(chunkSize)
	}

	return nil
}

// Read cita iz fajla preko blokova i postuje trenutni offset reader-a.
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
			// Cita se novi blok samo kad predjemo granicu trenutnog bloka.
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
			// Ne citamo padding posle kraja stvarnog fajla.
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

// Seek pomera offset reader-a kao standardni os.File Seek.
func (r *fileReader) Seek(offset int64, whence int) (int64, error) {
	var next int64

	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.offset + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return 0, fmt.Errorf("block manager: invalid seek whence %d", whence)
	}

	if next < 0 {
		return 0, fmt.Errorf("block manager: negative seek offset %d", next)
	}
	if next > r.size {
		// Reader ne ide dalje od kraja fajla.
		next = r.size
	}

	r.offset = next
	return r.offset, nil
}

func (r *fileReader) Close() error {
	return nil
}

// WriteFile prepisuje ceo fajl datim podacima.
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
		// Data se sece na blokove i upisuje redom.
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

// AppendFile cita postojece podatke, doda nove i prepise fajl.
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

// readBlockAllowPartial cita blok, ali tolerise EOF kod poslednjeg kraceg bloka.
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
		// U cache-u se drzi blok fiksne velicine.
		m.cache.Put(path, index, block)
	}

	return data, nil
}

// validateBlockSize ogranicava projekat na trazene velicine blokova.
func (m *Manager) validateBlockSize() error {
	switch m.blockSize {
	case 4 * 1024, 8 * 1024, 16 * 1024:
		return nil
	default:
		return fmt.Errorf("%w: got %d bytes", ErrInvalidBlockSize, m.blockSize)
	}
}

// blockOffset prevodi indeks bloka u offset bajtova, uz overflow proveru.
func (m *Manager) blockOffset(index uint64) (int64, error) {
	const maxInt64 = uint64(1<<63 - 1)

	blockSize := uint64(m.blockSize)
	if index > maxInt64/blockSize {
		return 0, fmt.Errorf("block manager: block index %d overflows file offset for block size %d", index, m.blockSize)
	}

	return int64(index * blockSize), nil
}

// filepathDir je mala lokalna zamena za filepath.Dir.
func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == os.PathSeparator {
			return path[:i]
		}
	}
	return "."
}
