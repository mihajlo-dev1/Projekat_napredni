package wal

import (
	"os"
	"path/filepath"
	"testing"
)

// Proverava da recordsPerSegment=1 rotira segment posle svakog zapisa.
func TestOpenConfiguredUsesRecordsPerSegment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal", "segment")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	w, err := OpenConfigured(path, nil, defaultBlockSize, defaultSegmentSizeBlocks, 1)
	if err != nil {
		t.Fatalf("OpenConfigured() error = %v", err)
	}

	// Dva PUT-a treba da zavrse u dva razlicita WAL segmenta.
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

// Proverava da su WAL segmenti uvek fiksne velicine iz config-a.
func TestOpenConfiguredCreatesFixedLengthSegments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal", "segment")
	blockSize := 4 * 1024
	segmentBlocks := 2

	w, err := OpenConfigured(path, nil, blockSize, segmentBlocks, 1)
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

	wantSize := int64(blockSize * segmentBlocks)
	for _, segment := range []string{
		path + "_0001.log",
		path + "_0002.log",
	} {
		info, err := os.Stat(segment)
		if err != nil {
			t.Fatalf("expected segment %s: %v", segment, err)
		}
		if info.Size() != wantSize {
			t.Fatalf("%s size = %d, want %d", segment, info.Size(), wantSize)
		}
	}
}
