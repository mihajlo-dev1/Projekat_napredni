package wal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenConfiguredUsesRecordsPerSegment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal", "segment")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	w, err := OpenConfigured(path, nil, defaultBlockSize, defaultSegmentSizeBlocks, 1)
	if err != nil {
		t.Fatalf("OpenConfigured() error = %v", err)
	}

	if err := w.AppendPut([]byte("a"), []byte("one")); err != nil {
		t.Fatalf("AppendPut(a) error = %v", err)
	}
	if err := w.AppendPut([]byte("b"), []byte("two")); err != nil {
		t.Fatalf("AppendPut(b) error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := os.Stat(path + "_0001.log"); err != nil {
		t.Fatalf("expected first segment: %v", err)
	}
	if _, err := os.Stat(path + "_0002.log"); err != nil {
		t.Fatalf("expected second segment: %v", err)
	}
}
