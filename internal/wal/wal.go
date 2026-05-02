package wal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"kv-engine/internal"
)

const blockSize = 32 * 1024
const frameHeaderSize = 5
const defaultMaxRecordsPerSegment = 3

var ErrRecordTooLarge = errors.New("record too large")

type WAL struct {
	file                 *os.File
	mu                   sync.Mutex
	dir                  string
	currentSegmentIndex  int
	currentRecordCount   int
	maxRecordsPerSegment int
	currentBlockOffset   int
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

		written, err := writeFrame(w.file, fragmentType, chunk)
		if err != nil {
			return err
		}

		w.currentBlockOffset += written
	}

	w.currentRecordCount++

	if w.isSegmentFull() {
		w.currentSegmentIndex++
		w.currentRecordCount = 0
		w.currentBlockOffset = 0

		if err := w.file.Close(); err != nil {
			return err
		}

		newFile, err := w.openSegment(w.currentSegmentPath())
		if err != nil {
			return err
		}

		w.file = newFile
	}

	return nil
}

func (w *WAL) Replay(applyPut func(key, value []byte), applyDelete func(key []byte)) error {
	for _, path := range w.segmentPaths() {
		file, err := os.Open(path)
		if err != nil {
			return err
		}

		reader := newFrameReader(file)
		for {
			record, err := ReadNextRecord(reader)
			if err == io.EOF {
				break
			}

			if err != nil {
				file.Close()
				return err
			}

			switch record.Type {
			case internal.RecordPut:
				applyPut(record.Key, record.Value)
			case internal.RecordDelete:
				applyDelete(record.Key)
			}
		}

		file.Close()
	}

	return nil
}

func (w *WAL) Close() error {
	return w.file.Close()
}

func Open(path string) (*WAL, error) {
	tempWAL := &WAL{
		dir:                  path,
		maxRecordsPerSegment: defaultMaxRecordsPerSegment,
	}

	tempWAL.currentSegmentIndex = tempWAL.findLastSegmentIndex()
	if tempWAL.currentSegmentIndex == 0 {
		tempWAL.currentSegmentIndex = 1
	} else {
		recordCount, blockOffset, err := scanSegmentState(tempWAL.currentSegmentPath())
		if err != nil {
			return nil, err
		}

		tempWAL.currentRecordCount = recordCount
		tempWAL.currentBlockOffset = blockOffset
		if tempWAL.isSegmentFull() {
			tempWAL.currentSegmentIndex++
			tempWAL.currentRecordCount = 0
			tempWAL.currentBlockOffset = 0
		}
	}

	file, err := tempWAL.openSegment(tempWAL.currentSegmentPath())
	if err != nil {
		return nil, err
	}

	return &WAL{
		file:                 file,
		dir:                  path,
		currentSegmentIndex:  tempWAL.currentSegmentIndex,
		currentRecordCount:   tempWAL.currentRecordCount,
		maxRecordsPerSegment: defaultMaxRecordsPerSegment,
		currentBlockOffset:   tempWAL.currentBlockOffset,
	}, nil
}

func scanSegmentState(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	reader := newFrameReader(file)
	recordCount := 0

	for {
		if _, err := ReadNextRecord(reader); err != nil {
			if err == io.EOF {
				return recordCount, reader.blockOffset, nil
			}
			return 0, 0, err
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
	return blockSize - w.currentBlockOffset
}

func (w *WAL) padBlock() error {
	remaining := w.remainingBlockSpace()
	if remaining == blockSize {
		return nil
	}

	padding := make([]byte, remaining)
	if _, err := w.file.Write(padding); err != nil {
		return err
	}

	w.currentBlockOffset = 0
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
	return w.currentRecordCount >= w.maxRecordsPerSegment
}

func (w *WAL) openSegment(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
}

func (w *WAL) currentSegmentPath() string {
	return fmt.Sprintf("%s_%04d.log", w.dir, w.currentSegmentIndex)
}

func (w *WAL) segmentPaths() []string {
	paths := make([]string, 0, w.currentSegmentIndex)

	for i := 1; i <= w.currentSegmentIndex; i++ {
		paths = append(paths, fmt.Sprintf("%s_%04d.log", w.dir, i))
	}

	return paths
}
