package engine

import (
	"errors"
	"path/filepath"
	"testing"

	"kv-engine/internal/config"
)

func testConfig(dir string) config.Config {
	// Testovi koriste privremene foldere da ne diraju realne data fajlove.
	cfg := config.Default()
	cfg.WAL.Directory = filepath.Join(dir, "wal", "segment")
	cfg.Memtable.Implementation = "hashmap"
	cfg.Memtable.MaxEntries = 1
	cfg.Memtable.Instances = 1
	cfg.SSTable.Directory = filepath.Join(dir, "sstable")
	cfg.SSTable.SummaryStep = 1
	cfg.Cache.Capacity = 2
	cfg.TokenBucket.Capacity = 100
	return cfg
}

// Proverava da se flushovana SSTable vrednost vidi i posle novog Engine-a.
func TestGetReadsFlushedSSTableAfterReopen(t *testing.T) {
	cfg := testConfig(t.TempDir())

	first, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := first.Put("alpha", []byte("one")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := New(cfg)
	if err != nil {
		t.Fatalf("New() after reopen error = %v", err)
	}
	defer second.Close()

	value, ok := second.Get("alpha")
	if !ok {
		t.Fatalf("Get(alpha) ok = false, want true")
	}
	if string(value) != "one" {
		t.Fatalf("Get(alpha) = %q, want one", value)
	}
}

// Proverava da DELETE u novijoj tabeli sakrije staru vrednost iz starije tabele.
func TestDeleteTombstoneHidesOlderSSTableValue(t *testing.T) {
	cfg := testConfig(t.TempDir())

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := e.Put("alpha", []byte("one")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := e.Delete("alpha"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(cfg)
	if err != nil {
		t.Fatalf("New() after reopen error = %v", err)
	}
	defer reopened.Close()

	value, ok := reopened.Get("alpha")
	if ok || value != nil {
		t.Fatalf("Get(alpha) = (%q, %v), want (nil, false)", value, ok)
	}
}

// Proverava da tombstone u memtable sakrije vrednost koja je vec na disku.
func TestMemtableTombstoneHidesOlderSSTableValue(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	first, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := first.Put("alpha", []byte("one")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cfg.Memtable.MaxEntries = 10
	second, err := New(cfg)
	if err != nil {
		t.Fatalf("New() after reopen error = %v", err)
	}
	defer second.Close()

	if err := second.Delete("alpha"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	value, ok := second.Get("alpha")
	if ok || value != nil {
		t.Fatalf("Get(alpha) = (%q, %v), want (nil, false)", value, ok)
	}
}

// Proverava da Start procita sve WAL segmente pre nego sto ih eventualno resetuje.
func TestStartReplaysAllWALSegmentsBeforeReset(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.WAL.RecordsPerSegment = 1
	cfg.Memtable.MaxEntries = 100

	first, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Namerno pisemo direktno u WAL da simuliramo stanje pre recovery-ja.
	if err := first.wal.AppendPut([]byte("alpha"), []byte("one")); err != nil {
		t.Fatalf("AppendPut(alpha) error = %v", err)
	}
	if err := first.wal.AppendPut([]byte("bravo"), []byte("two")); err != nil {
		t.Fatalf("AppendPut(bravo) error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cfg.Memtable.MaxEntries = 1
	second, err := New(cfg)
	if err != nil {
		t.Fatalf("New() after reopen error = %v", err)
	}
	defer second.Close()

	if err := second.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for key, want := range map[string]string{
		"alpha": "one",
		"bravo": "two",
	} {
		value, ok := second.Get(key)
		if !ok {
			t.Fatalf("Get(%s) ok = false, want true", key)
		}
		if string(value) != want {
			t.Fatalf("Get(%s) = %q, want %q", key, value, want)
		}
	}
}

// Proverava da je token bucket sistemski key sakriven i da se stanje obnavlja.
func TestTokenBucketStateIsHiddenAndRestored(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.Memtable.MaxEntries = 10
	cfg.TokenBucket.Capacity = 2
	cfg.TokenBucket.RefillIntervalSeconds = 3600

	first, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := first.Put("alpha", []byte("one")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, _, err := first.GetWithError(tokenBucketStateKey); !errors.Is(err, ErrReservedKey) {
		t.Fatalf("GetWithError(system key) error = %v, want ErrReservedKey", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := New(cfg)
	if err != nil {
		t.Fatalf("New() after reopen error = %v", err)
	}
	defer second.Close()
	if err := second.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, ok, err := second.GetWithError("alpha"); err != nil || !ok {
		t.Fatalf("first GetWithError(alpha) = ok %v err %v, want ok true err nil", ok, err)
	}
	if _, _, err := second.GetWithError("alpha"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second GetWithError(alpha) error = %v, want ErrRateLimited", err)
	}
}

// Proverava da engine samo prosledi Merkle proveru odgovarajucoj SSTable tabeli.
func TestValidateMerkleThroughEngine(t *testing.T) {
	cfg := testConfig(t.TempDir())

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer e.Close()

	if err := e.Put("alpha", []byte("one")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	valid, changed, err := e.ValidateMerkle(1)
	if err != nil {
		t.Fatalf("ValidateMerkle() error = %v", err)
	}
	if !valid || len(changed) != 0 {
		t.Fatalf("ValidateMerkle() = (%v, %v), want (true, nil)", valid, changed)
	}
}
