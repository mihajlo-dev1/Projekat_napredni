package wal

import (
	"os"
	"sync"
)

type WAL struct {
	file *os.File
	mu   sync.Mutex
}

func OpenWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &WAL{
		file: f,
	}, nil
}

func (w *WAL) Append(record *Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := record.Serialize()
	_, err := w.file.Write(data)
	return err
}

func (w *WAL) Close() error {
	return w.file.Close()
}
