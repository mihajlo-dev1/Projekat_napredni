package wal

import (
	"fmt"
	"io"
	"os"
	"sync"
)

type WAL struct {
	file                 *os.File
	mu                   sync.Mutex
	dir                  string
	currentSegmentIndex  int
	currentRecordCount   int
	maxRecordsPerSegment int
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
	_, err := w.file.Write(data)
	if err != nil {
		return err
	}

	w.currentRecordCount++
	if w.isSegmentFull() {
		w.currentSegmentIndex++
		w.currentRecordCount = 0

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

func (w *WAL) Replay(applyPut func(key, value []byte), applyDelete func(key []byte),
) error {
	file, err := os.Open(w.file.Name())
	if err != nil {
		return err
	}
	defer file.Close()

	for {
		record, err := ReadRecord((file))
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		switch record.Type {
		case RecordPut:
			applyPut(record.Key, record.Value)

		case RecordDelete:
			applyDelete(record.Key)
		}

	}
	return nil

}

func (w *WAL) Close() error {
	return w.file.Close()
}

func Open(path string) (*WAL, error) {
	tempWAL := &WAL{
		dir:                 path,
		currentSegmentIndex: 1,
	}
	file, err := tempWAL.openSegment(path)
	if err != nil {
		return nil, err
	}

	return &WAL{
		file:                 file,
		dir:                  path,
		currentSegmentIndex:  1,
		currentRecordCount:   0,
		maxRecordsPerSegment: 3,
	}, nil
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
