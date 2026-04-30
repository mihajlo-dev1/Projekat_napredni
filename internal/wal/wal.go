package wal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const blockSize = 32 * 1024

var ErrRecordTooLarge = errors.New("record too large")

type WAL struct {
	file                 *os.File
	mu                   sync.Mutex
	dir                  string
	currentSegmentIndex  int
	currentRecordCount   int
	maxRecordsPerSegment int
	currentBlockOffSet   int
}

func (w *WAL) AppendPut(key []byte, value []byte) error {
	record := &Record{
		Type:  RecordPut,
		Key:   key,
		Value: value,
	}

	return w.Append(record)
}

func (w *WAL) Append(record *Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := record.Serialize()

	remaining := data

	for len(remaining) > 0 {
		space := w.remainingBlockSpace()

		if space == 0 {
			if err := w.padBlock(); err != nil {
				return err
			}
			space = w.remainingBlockSpace()
		}

		chunkSize := space - 1 // 1 bajt čuvamo za fragment type
		if len(remaining) < chunkSize {
			chunkSize = len(remaining)
		}

		chunk := remaining[:chunkSize]
		remaining = remaining[chunkSize:]

		var fragmentType RecordType

		if len(data) == len(chunk) {
			fragmentType = RecordFull
		} else if len(remaining) == 0 {
			fragmentType = RecordLast
		} else if len(data) == len(chunk)+len(remaining) {
			fragmentType = RecordFirst
		} else {
			fragmentType = RecordMiddle
		}

		if _, err := w.file.Write([]byte{byte(fragmentType)}); err != nil {
			return err
		}

		if _, err := w.file.Write(chunk); err != nil {
			return err
		}

		w.currentBlockOffSet += 1 + len(chunk)
	}

	w.currentRecordCount++

	if w.isSegmentFull() {
		w.currentSegmentIndex++
		w.currentRecordCount = 0
		w.currentBlockOffSet = 0

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

		for {
			record, err := ReadRecord(file)
			if err == io.EOF {
				break
			}

			if err != nil {
				file.Close()
				return err
			}

			switch record.Type {
			case RecordPut:
				applyPut(record.Key, record.Value)
			case RecordDelete:
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
		dir: path,
	}

	tempWAL.currentSegmentIndex = tempWAL.findLastSegmentIndex()
	if tempWAL.currentSegmentIndex == 0 {
		tempWAL.currentSegmentIndex = 1
	}

	file, err := tempWAL.openSegment(tempWAL.currentSegmentPath())
	if err != nil {
		return nil, err
	}

	return &WAL{
		file:                 file,
		dir:                  path,
		currentSegmentIndex:  1,
		currentRecordCount:   0,
		maxRecordsPerSegment: 3,
		currentBlockOffSet:   0,
	}, nil
}

// kako bi segmentirani wal znao gde je stao
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
	return blockSize - w.currentBlockOffSet
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

	w.currentBlockOffSet = 0
	return nil

}
func (w *WAL) AppendDelete(key []byte) error {
	record := &Record{
		Type: RecordDelete,
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
	//da path bude wal_0001.log, wal_002.log...

}
func (w *WAL) segmentPaths() []string {
	paths := make([]string, 0, w.currentSegmentIndex)

	for i := 1; i <= w.currentSegmentIndex; i++ {
		paths = append(paths, fmt.Sprintf("%s_%04d.log", w.dir, i))
	}

	return paths
}
