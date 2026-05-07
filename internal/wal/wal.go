package wal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"kv-engine/internal"
	"kv-engine/internal/block"
)

const defaultBlockSize = 32 * 1024
const frameHeaderSize = 5
const defaultMaxRecordsPerSegment = 1000
const defaultSegmentSizeBlocks = 16

// WAL cuva promene pre memtable-a da bi baza mogla da se oporavi posle crash-a.
type WAL struct {
	file                 *os.File
	blocks               *block.Manager
	mu                   sync.Mutex
	dir                  string
	currentSegmentIndex  int
	currentRecordCount   int
	maxRecordsPerSegment int
	currentBlockOffset   int
	currentSegmentBytes  int
	blockSize            int
	maxSegmentBytes      int
}

// segmentWriter sakriva razliku izmedju obicnog fajla i block manager upisa.
type segmentWriter struct {
	path   string
	blocks *block.Manager
	file   *os.File
	offset int64
}

func (w *segmentWriter) Write(data []byte) (int, error) {
	var err error
	if w.blocks != nil {
		// Kada postoji block manager, sav disk I/O ide kroz njega.
		err = w.blocks.WriteAt(w.path, w.offset, data)
	} else {
		_, err = w.file.WriteAt(data, w.offset)
	}
	if err != nil {
		return 0, err
	}
	w.offset += int64(len(data))
	return len(data), nil
}

func (w *WAL) AppendPut(key []byte, value []byte) error {
	// PUT se pretvara u genericki WAL record.
	record := &internal.Record{
		Type:  internal.RecordPut,
		Key:   key,
		Value: value,
	}

	return w.Append(record)
}

// Append serijalizuje zapis i upisuje ga u trenutni WAL segment.
func (w *WAL) Append(record *internal.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := SerializeRecord(record)
	// Jedan logicki record mora moci da stane u jedan segment, makar iz vise frame-ova.
	emptySegmentSize := framedSize(0, len(data), w.blockSize)
	if emptySegmentSize > w.maxSegmentBytes {
		return fmt.Errorf("wal: record is larger than one segment")
	}

	// Ako trenutni segment nema mesta za ceo record, prelazi se na sledeci segment.
	if w.shouldRotateBeforeWrite(len(data)) {
		if err := w.rotateSegment(); err != nil {
			return err
		}
	}

	remaining := data

	for len(remaining) > 0 {
		space := w.remainingBlockSpace()

		// Ako nema mesta ni za frame header, ostatak bloka se popuni nulama.
		if space < frameHeaderSize {
			if err := w.padBlock(); err != nil {
				return err
			}
			space = w.remainingBlockSpace()
		}

		chunkSize := space - frameHeaderSize
		if len(remaining) < chunkSize {
			chunkSize = len(remaining)
		}

		chunk := remaining[:chunkSize]
		remaining = remaining[chunkSize:]

		// Veliki record moze biti podeljen na FIRST/MIDDLE/LAST fragmente.
		var fragmentType internal.RecordType

		if len(data) == len(chunk) {
			fragmentType = RecordFull
		} else if len(remaining) == 0 {
			fragmentType = RecordLast
		} else if len(data) == len(chunk)+len(remaining) {
			fragmentType = RecordFirst
		} else {
			fragmentType = RecordMiddle
		}

		// Frame je komad record-a sa svojim tipom i duzinom.
		writer := w.currentWriter()
		written, err := writeFrame(writer, fragmentType, chunk)
		if err != nil {
			return err
		}

		w.currentBlockOffset += written
		w.currentSegmentBytes += written
	}

	w.currentRecordCount++

	// Segment se rotira po broju zapisa ili po velicini u bajtovima.
	if w.isSegmentFull() {
		return w.rotateSegment()
	}

	return nil
}

// Replay cita sve WAL segmente i za svaki record poziva odgovarajuci callback.
func (w *WAL) Replay(applyPut func(key, value []byte), applyDelete func(key []byte)) error {
	for _, path := range w.segmentPaths() {
		// Segmenti se citaju redom: wal_0001.log, wal_0002.log, ...
		reader, err := w.openSegmentReader(path)
		if err != nil {
			return err
		}

		frameReader := newFrameReader(reader, w.blockSize)
		for {
			// ReadNextRecord sklapa fragmentisane frame-ove nazad u jedan record.
			record, err := ReadNextRecord(frameReader)
			if err == io.EOF {
				break
			}

			if err != nil {
				return err
			}

			switch record.Type {
			case internal.RecordPut:
				// Engine odlucuje kako se PUT primenjuje.
				applyPut(record.Key, record.Value)
			case internal.RecordDelete:
				// Engine odlucuje kako se DELETE primenjuje.
				applyDelete(record.Key)
			}
		}
	}

	return nil
}

func (w *WAL) Close() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// Reset brise stare segmente posle uspesnog flush-a u SSTable.
func (w *WAL) Reset() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	paths := w.segmentPaths()

	// Prvo zatvaramo otvoren fajl da bi mogao bezbedno da se obrise.
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	// Brisu se svi poznati WAL segmenti.
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	w.currentSegmentIndex = 1
	w.currentRecordCount = 0
	w.currentBlockOffset = 0
	w.currentSegmentBytes = 0

	// Posle reset-a odmah postoji prazan prvi segment.
	if err := w.ensureSegmentSize(w.currentSegmentPath()); err != nil {
		return err
	}

	if w.blocks == nil {
		file, err := w.openSegment(w.currentSegmentPath())
		if err != nil {
			return err
		}
		w.file = file
	}

	return nil
}

func Open(path string) (*WAL, error) {
	return OpenConfigured(path, nil, defaultBlockSize, defaultSegmentSizeBlocks, defaultMaxRecordsPerSegment)
}

func OpenWithBlockManager(path string, blocks *block.Manager) (*WAL, error) {
	return OpenConfigured(path, blocks, defaultBlockSize, defaultSegmentSizeBlocks, defaultMaxRecordsPerSegment)
}

// OpenConfigured otvara postojeci WAL ili pravi novi, uz parametre iz config-a.
func OpenConfigured(path string, blocks *block.Manager, blockSize int, segmentSizeBlocks int, recordsPerSegment int) (*WAL, error) {
	// Lose vrednosti se vracaju na default da WAL ostane upotrebljiv.
	if blockSize <= frameHeaderSize {
		blockSize = defaultBlockSize
	}
	if segmentSizeBlocks < 1 {
		segmentSizeBlocks = defaultSegmentSizeBlocks
	}
	if recordsPerSegment < 1 {
		recordsPerSegment = defaultMaxRecordsPerSegment
	}

	tempWAL := &WAL{
		dir:                  path,
		blocks:               blocks,
		maxRecordsPerSegment: recordsPerSegment,
		blockSize:            blockSize,
		maxSegmentBytes:      blockSize * segmentSizeBlocks,
	}

	// Ako segmenti vec postoje, nastavlja se od poslednjeg.
	tempWAL.currentSegmentIndex = tempWAL.findLastSegmentIndex()
	if tempWAL.currentSegmentIndex == 0 {
		tempWAL.currentSegmentIndex = 1
	} else {
		// Moramo znati gde tacno nastavlja sledeci upis u poslednjem segmentu.
		recordCount, blockOffset, segmentBytes, err := tempWAL.scanSegmentState(tempWAL.currentSegmentPath())
		if err != nil {
			return nil, err
		}

		tempWAL.currentRecordCount = recordCount
		tempWAL.currentBlockOffset = blockOffset
		tempWAL.currentSegmentBytes = segmentBytes
		if tempWAL.isSegmentFull() {
			// Ako je poslednji segment pun, novi upis pocinje u sledecem.
			if err := tempWAL.ensureSegmentSize(tempWAL.currentSegmentPath()); err != nil {
				return nil, err
			}
			tempWAL.currentSegmentIndex++
			tempWAL.currentRecordCount = 0
			tempWAL.currentBlockOffset = 0
			tempWAL.currentSegmentBytes = 0
		}
	}

	// Svi segment fajlovi do trenutnog se osiguravaju na fiksnu velicinu.
	for index := 1; index <= tempWAL.currentSegmentIndex; index++ {
		if err := tempWAL.ensureSegmentSize(tempWAL.segmentPath(index)); err != nil {
			return nil, err
		}
	}

	var file *os.File
	if blocks == nil {
		// Bez block manager-a cuva se obican os.File za trenutni segment.
		var err error
		file, err = tempWAL.openSegment(tempWAL.currentSegmentPath())
		if err != nil {
			return nil, err
		}
	}

	return &WAL{
		file:                 file,
		blocks:               blocks,
		dir:                  path,
		currentSegmentIndex:  tempWAL.currentSegmentIndex,
		currentRecordCount:   tempWAL.currentRecordCount,
		maxRecordsPerSegment: recordsPerSegment,
		currentBlockOffset:   tempWAL.currentBlockOffset,
		currentSegmentBytes:  tempWAL.currentSegmentBytes,
		blockSize:            blockSize,
		maxSegmentBytes:      blockSize * segmentSizeBlocks,
	}, nil
}

// scanSegmentState procita segment da izracuna broj zapisa i offset za nastavak upisa.
func (w *WAL) scanSegmentState(path string) (int, int, int, error) {
	reader, err := w.openSegmentReader(path)
	if err != nil {
		return 0, 0, 0, err
	}

	frameReader := newFrameReader(reader, w.blockSize)
	recordCount := 0

	for {
		if _, err := ReadNextRecord(frameReader); err != nil {
			if err == io.EOF {
				// EOF znaci da smo dosli do praznog dela segmenta.
				return recordCount, frameReader.blockOffset, frameReader.bytesRead, nil
			}
			return 0, 0, 0, err
		}
		recordCount++
	}
}

// findLastSegmentIndex trazi najveci wal_XXXX.log koji postoji.
func (w *WAL) findLastSegmentIndex() int {
	index := 1

	for {
		path := fmt.Sprintf("%s_%04d.log", w.dir, index)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		index++
	}

	return index - 1
}

func (w *WAL) remainingBlockSpace() int {
	return w.blockSize - w.currentBlockOffset
}

// padBlock popunjava ostatak bloka nulama kad nema mesta za sledeci frame header.
func (w *WAL) padBlock() error {
	remaining := w.remainingBlockSpace()
	if remaining == w.blockSize {
		return nil
	}

	padding := make([]byte, remaining)
	if _, err := w.currentWriter().Write(padding); err != nil {
		return err
	}

	w.currentBlockOffset = 0
	w.currentSegmentBytes += remaining
	return nil
}

func (w *WAL) AppendDelete(key []byte) error {
	// DELETE record nema value, dovoljan je key.
	record := &internal.Record{
		Type: internal.RecordDelete,
		Key:  key,
	}

	return w.Append(record)
}

func (w *WAL) isSegmentFull() bool {
	return w.currentRecordCount >= w.maxRecordsPerSegment || w.currentSegmentBytes >= w.maxSegmentBytes
}

// shouldRotateBeforeWrite proverava da li ce ceo record stati u trenutni segment.
func (w *WAL) shouldRotateBeforeWrite(remainingRecordBytes int) bool {
	if w.currentRecordCount == 0 && w.currentSegmentBytes == 0 {
		return false
	}

	needed := framedSize(w.currentBlockOffset, remainingRecordBytes, w.blockSize)
	return w.currentSegmentBytes+needed > w.maxSegmentBytes
}

// rotateSegment prelazi na sledeci WAL fajl.
func (w *WAL) rotateSegment() error {
	w.currentSegmentIndex++
	w.currentRecordCount = 0
	w.currentBlockOffset = 0
	w.currentSegmentBytes = 0

	if err := w.Close(); err != nil {
		return err
	}

	if err := w.ensureSegmentSize(w.currentSegmentPath()); err != nil {
		return err
	}

	if w.blocks == nil {
		newFile, err := w.openSegment(w.currentSegmentPath())
		if err != nil {
			return err
		}
		w.file = newFile
	}

	return nil
}

// openSegment pravi segment fiksne velicine i otvara ga za pisanje.
func (w *WAL) openSegment(path string) (*os.File, error) {
	if err := ensureFileSize(path, int64(w.maxSegmentBytes)); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
}

// openSegmentReader bira obican fajl ili block manager reader.
func (w *WAL) openSegmentReader(path string) (io.Reader, error) {
	if w.blocks == nil {
		return os.Open(path)
	}

	return w.blocks.OpenReader(path)
}

// currentWriter vraca writer koji pise tacno na kraj trenutnog WAL segmenta.
func (w *WAL) currentWriter() io.Writer {
	if w.blocks != nil {
		return &segmentWriter{
			path:   w.currentSegmentPath(),
			blocks: w.blocks,
			offset: int64(w.currentSegmentBytes),
		}
	}
	return &segmentWriter{
		path:   w.currentSegmentPath(),
		file:   w.file,
		offset: int64(w.currentSegmentBytes),
	}
}

func (w *WAL) currentSegmentPath() string {
	return w.segmentPath(w.currentSegmentIndex)
}

func (w *WAL) segmentPath(index int) string {
	return fmt.Sprintf("%s_%04d.log", w.dir, index)
}

func (w *WAL) ensureSegmentSize(path string) error {
	if w.blocks != nil {
		return w.blocks.EnsureFileSize(path, int64(w.maxSegmentBytes))
	}
	return ensureFileSize(path, int64(w.maxSegmentBytes))
}

// ensureFileSize napravi parent folder i podesi fajl na trazenu velicinu.
func ensureFileSize(path string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return os.Truncate(path, size)
}

// framedSize racuna koliko bajtova ce record zauzeti kad se podeli u frame-ove.
func framedSize(blockOffset int, dataSize int, blockSize int) int {
	total := 0
	remaining := dataSize

	for remaining > 0 {
		space := blockSize - blockOffset
		if space < frameHeaderSize {
			// Ostatak bloka se trosi na padding.
			total += space
			blockOffset = 0
			space = blockSize
		}

		chunkSize := space - frameHeaderSize
		if chunkSize > remaining {
			chunkSize = remaining
		}

		total += frameHeaderSize + chunkSize
		blockOffset += frameHeaderSize + chunkSize
		if blockOffset == blockSize {
			blockOffset = 0
		}

		remaining -= chunkSize
	}

	return total
}

// segmentPaths vraca postojece WAL segmente redom.
func (w *WAL) segmentPaths() []string {
	paths := make([]string, 0, w.currentSegmentIndex)

	for i := 1; i <= w.currentSegmentIndex; i++ {
		path := fmt.Sprintf("%s_%04d.log", w.dir, i)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		paths = append(paths, path)
	}

	return paths
}
