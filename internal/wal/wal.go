package wal

import "kv-engine/internal/hashmap"

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type WAL struct {
	file *os.File
	path string
}

func New(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644) // 0644 normalan file (dozvole za citanje i pisanje)
	if err != nil {
		return nil, err
	}
	return &WAL{
		file: f,
		path: path,
	}, nil
}

func (w *WAL) AppendPut(key string, value []byte) error {
	_, err := fmt.Fprintf(w.file, "PUT %s %s\n", key, string(value))
	return err
}

func (w *WAL) AppendDelete(key string) error {
	_, err := fmt.Fprintf(w.file, "DEL %s\n", key)
	return err
}

func (w *WAL) Close() error {
	return w.file.Close()
}

func (w *WAL) ReplayInto(hm *hashmap.HashMap) error {
	f, err := os.Open(w.path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 3)

		if len(parts) < 2 {
			continue
		}

		op := parts[0]
		key := parts[1]

		if op == "PUT" && len(parts) == 3 {
			hm.Put(key, []byte(parts[2]))
		} else if op == "DEL" {
			hm.Delete(key)
		}
	}

	return scanner.Err()
}