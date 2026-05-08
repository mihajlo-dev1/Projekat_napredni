package sstable

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCreateWritesAllComponents(t *testing.T) {
	dir := t.TempDir()
	entries := map[string][]byte{
		"c": []byte("three"),
		"a": []byte("one"),
		"b": []byte("two"),
	}

	table, filter, index, err := Create(dir, entries, 2)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	for _, name := range []string{
		dataFileName,
		indexFileName,
		summaryFileName,
		filterFileName,
		metadataFileName,
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	expectedIndex := []IndexEntry{
		{Key: "a", Offset: 0},
		{Key: "b", Offset: int64(len(SerializeEntry(Entry{Key: "a", Value: entries["a"]})))},
		{Key: "c", Offset: int64(len(SerializeEntry(Entry{Key: "a", Value: entries["a"]})) + len(SerializeEntry(Entry{Key: "b", Value: entries["b"]})))},
	}
	if !reflect.DeepEqual(index, expectedIndex) {
		t.Fatalf("index = %#v, want %#v", index, expectedIndex)
	}

	data, err := os.ReadFile(table.DataPath)
	if err != nil {
		t.Fatalf("ReadFile(data) error = %v", err)
	}
	expectedRecords := [][]byte{
		SerializeEntry(Entry{Key: "a", Value: entries["a"]}),
		SerializeEntry(Entry{Key: "b", Value: entries["b"]}),
		SerializeEntry(Entry{Key: "c", Value: entries["c"]}),
	}
	if !bytes.Equal(data, bytes.Join(expectedRecords, nil)) {
		t.Fatalf("data.bin contains unexpected records")
	}

	indexEntries, err := readIndexFile(table.IndexPath)
	if err != nil {
		t.Fatalf("readIndexFile() error = %v", err)
	}
	if !reflect.DeepEqual(indexEntries, expectedIndex) {
		t.Fatalf("index.bin = %#v, want %#v", indexEntries, expectedIndex)
	}

	summaryData, err := os.Open(table.SummaryPath)
	if err != nil {
		t.Fatalf("Open(summary) error = %v", err)
	}
	defer summaryData.Close()

	minKey, maxKey, err := DeserializeSummaryBounds(summaryData)
	if err != nil {
		t.Fatalf("DeserializeSummaryBounds() error = %v", err)
	}
	if minKey != "a" || maxKey != "c" {
		t.Fatalf("summary bounds = (%q, %q), want (a, c)", minKey, maxKey)
	}

	summaryEntries, err := readSummaryEntries(summaryData)
	if err != nil {
		t.Fatalf("readSummaryEntries() error = %v", err)
	}
	expectedSummary := []SumaryEntry{
		{Key: "a", IndexOffSet: 0},
		{Key: "c", IndexOffSet: 2},
	}
	if !reflect.DeepEqual(summaryEntries, expectedSummary) {
		t.Fatalf("summary.bin = %#v, want %#v", summaryEntries, expectedSummary)
	}

	filterFile, err := os.Open(table.FilterPath)
	if err != nil {
		t.Fatalf("Open(filter) error = %v", err)
	}
	defer filterFile.Close()

	fileFilter, err := DeserializeBloomFilter(filterFile)
	if err != nil {
		t.Fatalf("DeserializeBloomFilter() error = %v", err)
	}
	for key := range entries {
		if !filter.MightContain(key) || !fileFilter.MightContain(key) {
			t.Fatalf("filter missing key %q", key)
		}
	}

	metadataFile, err := os.Open(table.MetadataPath)
	if err != nil {
		t.Fatalf("Open(metadata) error = %v", err)
	}
	defer metadataFile.Close()

	tree, err := DeserializeMerkleTree(metadataFile)
	if err != nil {
		t.Fatalf("DeserializeMerkleTree() error = %v", err)
	}
	if changed := tree.Validate(expectedRecords); len(changed) != 0 {
		t.Fatalf("metadata tree changed indexes = %v, want none", changed)
	}
}

func TestGetReadsThroughSSTableComponents(t *testing.T) {
	dir := t.TempDir()
	table, _, _, err := Create(dir, map[string][]byte{
		"alpha":   []byte("one"),
		"bravo":   []byte("two"),
		"charlie": []byte("three"),
		"delta":   []byte("four"),
	}, 2)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	value, ok := table.Get("charlie")
	if !ok {
		t.Fatalf("Get(charlie) ok = false, want true")
	}
	if string(value) != "three" {
		t.Fatalf("Get(charlie) = %q, want three", value)
	}

	if value, ok := table.Get("zulu"); ok || value != nil {
		t.Fatalf("Get(zulu) = (%q, %v), want (nil, false)", value, ok)
	}

	if value, ok := table.Get("aardvark"); ok || value != nil {
		t.Fatalf("Get(aardvark) = (%q, %v), want (nil, false)", value, ok)
	}
}

func TestValidateMerkleDetectsChangedDataRecord(t *testing.T) {
	dir := t.TempDir()
	entries := map[string][]byte{
		"alpha": []byte("one"),
		"bravo": []byte("two"),
		"delta": []byte("four"),
	}

	table, _, index, err := Create(dir, entries, 2)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	valid, changed, err := table.ValidateMerkle()
	if err != nil {
		t.Fatalf("ValidateMerkle() error = %v", err)
	}
	if !valid || len(changed) != 0 {
		t.Fatalf("ValidateMerkle() = (%v, %v), want (true, nil)", valid, changed)
	}

	valueOffset := index[1].Offset + int64(9+len(index[1].Key))
	file, err := os.OpenFile(table.DataPath, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile(data) error = %v", err)
	}
	if _, err := file.WriteAt([]byte("X"), valueOffset); err != nil {
		file.Close()
		t.Fatalf("WriteAt(data) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(data) error = %v", err)
	}

	valid, changed, err = table.ValidateMerkle()
	if err != nil {
		t.Fatalf("ValidateMerkle() after change error = %v", err)
	}
	if valid {
		t.Fatalf("ValidateMerkle() valid = true, want false")
	}
	if !reflect.DeepEqual(changed, []int{1}) {
		t.Fatalf("ValidateMerkle() changed = %v, want [1]", changed)
	}
}

func readIndexFile(path string) ([]IndexEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]IndexEntry, 0)
	for {
		entry, err := DeserializeIndexEntry(file)
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
}

func readSummaryEntries(r io.Reader) ([]SumaryEntry, error) {
	entries := make([]SumaryEntry, 0)
	for {
		entry, err := DeserializeSummaryEntry(r)
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
}
