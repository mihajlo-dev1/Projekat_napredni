package wal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"

	"kv-engine/internal"
	"kv-engine/internal/block"
)

const defaultBlockSize = 32 * 1024
const frameHeaderSize = 5
const defaultMaxRecordsPerSegment = 1000
const defaultSegmentSizeBlocks = 16

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

type segmentWriter struct {
	path   string
	blocks *block.Manager
}

func (w segmentWriter) Write(data []byte) (int, error) {
	if err := w.blocks.AppendFile(w.path, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w *WAL) AppendPut(key []byte, value []byte) error {
	record := &internal.Record{
		Type:  internal.RecordPut,
		Key:   key,
		Value: value,
	}

	return w.Append(record)
}

func (w *WAL) Append(record *internal.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := SerializeRecord(record)

	if w.shouldRotateBeforeWrite(len(data)) {
		if err := w.rotateSegment(); err != nil {
			return err
		}
	}

	remaining := data

	for len(remaining) > 0 {
		space := w.remainingBlockSpace()

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

		written, err := writeFrame(w.currentWriter(), fragmentType, chunk)
		if err != nil {
			return err
		}

		w.currentBlockOffset += written
		w.currentSegmentBytes += written
	}

	w.currentRecordCount++

	if w.isSegmentFull() {
		return w.rotateSegment()
	}

	return nil
}

func (w *WAL) Replay(applyPut func(key, value []byte), applyDelete func(key []byte)) error {
	for _, path := range w.segmentPaths() {
		reader, err := w.openSegmentReader(path)
		if err != nil {
			return err
		}

		frameReader := newFrameReader(reader, w.blockSize)
		for {
			record, err := ReadNextRecord(frameReader)
			if err == io.EOF {
				break
			}

			if err != nil {
				return err
			}

			switch record.Type {
			case internal.RecordPut:
				applyPut(record.Key, record.Value)
			case internal.RecordDelete:
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
	return w.file.Close()
}

func Open(path string) (*WAL, error) {
	return OpenConfigured(path, nil, defaultBlockSize, defaultSegmentSizeBlocks, defaultMaxRecordsPerSegment)
}

func OpenWithBlockManager(path string, blocks *block.Manager) (*WAL, error) {
	return OpenConfigured(path, blocks, defaultBlockSize, defaultSegmentSizeBlocks, defaultMaxRecordsPerSegment)
}

func OpenConfigured(path string, blocks *block.Manager, blockSize int, segmentSizeBlocks int, recordsPerSegment int) (*WAL, error) {
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

	tempWAL.currentSegmentIndex = tempWAL.findLastSegmentIndex()
	if tempWAL.currentSegmentIndex == 0 {
		tempWAL.currentSegmentIndex = 1
	} else {
		recordCount, blockOffset, segmentBytes, err := tempWAL.scanSegmentState(tempWAL.currentSegmentPath())
		if err != nil {
			return nil, err
		}

		tempWAL.currentRecordCount = recordCount
		tempWAL.currentBlockOffset = blockOffset
		tempWAL.currentSegmentBytes = segmentBytes
		if tempWAL.isSegmentFull() {
			tempWAL.currentSegmentIndex++
			tempWAL.currentRecordCount = 0
			tempWAL.currentBlockOffset = 0
			tempWAL.currentSegmentBytes = 0
		}
	}

	var file *os.File
	if blocks == nil {
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
				return recordCount, frameReader.blockOffset, frameReader.bytesRead, nil
			}
			return 0, 0, 0, err
		}
		recordCount++
	}
}

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
	record := &internal.Record{
		Type: internal.RecordDelete,
		Key:  key,
	}

	return w.Append(record)
}

func (w *WAL) isSegmentFull() bool {
	return w.currentRecordCount >= w.maxRecordsPerSegment || w.currentSegmentBytes >= w.maxSegmentBytes
}

func (w *WAL) shouldRotateBeforeWrite(remainingRecordBytes int) bool {
	if w.currentRecordCount == 0 && w.currentSegmentBytes == 0 {
		return false
	}

	needed := frameHeaderSize + remainingRecordBytes
	return w.currentSegmentBytes+needed > w.maxSegmentBytes
}

func (w *WAL) rotateSegment() error {
	w.currentSegmentIndex++
	w.currentRecordCount = 0
	w.currentBlockOffset = 0
	w.currentSegmentBytes = 0

	if err := w.Close(); err != nil {
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

func (w *WAL) openSegment(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
}

func (w *WAL) openSegmentReader(path string) (io.Reader, error) {
	if w.blocks == nil {
		return os.Open(path)
	}

	data, err := w.blocks.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (w *WAL) currentWriter() io.Writer {
	if w.blocks != nil {
		return segmentWriter{path: w.currentSegmentPath(), blocks: w.blocks}
	}
	return w.file
}

func (w *WAL) currentSegmentPath() string {
	return fmt.Sprintf("%s_%04d.log", w.dir, w.currentSegmentIndex)
}

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
