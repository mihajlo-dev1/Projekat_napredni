package wal

import (
	"io"
	"os"
	"sync"
)

type WAL struct {
	file *os.File
	mu   sync.Mutex
}

func (w *WAL) Append(record *Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := record.Serialize()
	_, err := w.file.Write(data)
	return err
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
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)

	if err != nil {
		return nil, err
	}

	return &WAL{
		file: file,
	}, nil
}

func (w *WAL) AppendDelete(key []byte) error {
	record := &Record{
		Type: RecordDelete,
		Key:  key,
	}

	return w.Append(record)
}
