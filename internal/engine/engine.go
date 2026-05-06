package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"kv-engine/internal/block"
	"kv-engine/internal/blockcache"
	"kv-engine/internal/cache"
	"kv-engine/internal/config"
	"kv-engine/internal/memtable"
	"kv-engine/internal/sstable"
	"kv-engine/internal/tokenbucket"
	"kv-engine/internal/wal"
)

var ErrRateLimited = errors.New("engine: rate limit exceeded")
var ErrReservedKey = errors.New("engine: key is reserved for internal state")
var ErrTableNotFound = errors.New("engine: sstable not found")

const tokenBucketStateKey = "__system/token_bucket_state"

type Engine struct {
	wal         *wal.WAL
	memtables   *memtable.MemtableManager
	cache       *cache.Cache
	bucket      *tokenbucket.Bucket
	blocks      *block.Manager
	tables      []*sstable.SSTable
	sstableDir  string
	summaryStep int
	nextTableID int
}

func New(cfg config.Config) (*Engine, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.WAL.Directory), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.SSTable.Directory, 0755); err != nil {
		return nil, err
	}

	blocks := block.New(cfg.BlockManager.BlockSizeKB, blockcache.New(cfg.BlockManager.CacheCapacity))

	blockSizeBytes := cfg.BlockManager.BlockSizeKB * 1024
	w, err := wal.OpenConfigured(cfg.WAL.Directory, blocks, blockSizeBytes, cfg.WAL.SegmentSizeBlocks, cfg.WAL.RecordsPerSegment)
	if err != nil {
		return nil, err
	}

	manager, err := memtable.NewMemtableManager(
		cfg.Memtable.Implementation,
		cfg.Memtable.MaxEntries,
		cfg.Memtable.Instances,
	)
	if err != nil {
		w.Close()
		return nil, err
	}

	tables, nextTableID, err := loadSSTables(cfg.SSTable.Directory, cfg.SSTable.SummaryStep, blocks)
	if err != nil {
		w.Close()
		return nil, err
	}

	return &Engine{
		wal:         w,
		memtables:   manager,
		cache:       cache.New(cfg.Cache.Capacity),
		bucket:      tokenbucket.New(cfg.TokenBucket.Capacity, time.Duration(cfg.TokenBucket.RefillIntervalSeconds)*time.Second),
		blocks:      blocks,
		tables:      tables,
		sstableDir:  cfg.SSTable.Directory,
		summaryStep: cfg.SSTable.SummaryStep,
		nextTableID: nextTableID,
	}, nil
}

func (e *Engine) Start() error {
	var replayErr error

	if err := e.wal.Replay(
		func(key []byte, value []byte) {
			if replayErr != nil {
				return
			}
			needsFlush := e.memtables.Put(string(key), value)
			if needsFlush {
				replayErr = e.flushMemtables()
			}
		},
		func(key []byte) {
			if replayErr != nil {
				return
			}
			needsFlush := e.memtables.Delete(string(key))
			if needsFlush {
				replayErr = e.flushMemtables()
			}
		},
	); err != nil {
		return err
	}

	if replayErr != nil {
		return replayErr
	}

	return e.restoreTokenBucketState()
}

func (e *Engine) Put(key string, value []byte) error {
	if isReservedKey(key) {
		return ErrReservedKey
	}
	if err := e.allow(); err != nil {
		return err
	}

	if err := e.wal.AppendPut([]byte(key), value); err != nil {
		return err
	}

	needsFlush := e.memtables.Put(key, value)
	e.cache.Put(key, value)

	if needsFlush {
		return e.flushMemtables()
	}

	return nil
}

func (e *Engine) Delete(key string) error {
	if isReservedKey(key) {
		return ErrReservedKey
	}
	if err := e.allow(); err != nil {
		return err
	}

	if err := e.wal.AppendDelete([]byte(key)); err != nil {
		return err
	}

	needsFlush := e.memtables.Delete(key)
	e.cache.Delete(key)

	if needsFlush {
		return e.flushMemtables()
	}

	return nil
}

func (e *Engine) Get(key string) ([]byte, bool) {
	value, ok, _ := e.GetWithError(key)
	return value, ok
}

func (e *Engine) GetWithError(key string) ([]byte, bool, error) {
	if isReservedKey(key) {
		return nil, false, ErrReservedKey
	}
	if err := e.allow(); err != nil {
		return nil, false, err
	}

	value, ok := e.get(key)
	return value, ok, nil
}

func (e *Engine) get(key string) ([]byte, bool) {
	if value, ok := e.cache.Get(key); ok {
		fmt.Printf("[read] key=%q source=cache\n", key)
		return value, true
	}

	if value, ok := e.memtables.Get(key); ok {
		fmt.Printf("[read] key=%q source=memtable\n", key)
		e.cache.Put(key, value)
		return value, true
	}

	for i := len(e.tables) - 1; i >= 0; i-- {
		value, found, deleted := e.tables[i].Lookup(key)
		if !found {
			continue
		}
		if deleted {
			fmt.Printf("[read] key=%q source=sstable table=%d tombstone\n", key, i+1)
			return nil, false
		}
		fmt.Printf("[read] key=%q source=sstable table=%d\n", key, i+1)
		e.cache.Put(key, value)
		return value, true
	}

	fmt.Printf("[read] key=%q source=not_found\n", key)
	return nil, false
}

func (e *Engine) allow() error {
	if e.bucket != nil && !e.bucket.Allow() {
		return ErrRateLimited
	}
	if e.bucket != nil {
		return e.storeTokenBucketState()
	}
	return nil
}

func (e *Engine) storeTokenBucketState() error {
	data := e.bucket.Serialize()
	if err := e.wal.AppendPut([]byte(tokenBucketStateKey), data); err != nil {
		return err
	}
	needsFlush := e.memtables.Put(tokenBucketStateKey, data)
	if needsFlush {
		return e.flushMemtables()
	}
	return nil
}

func (e *Engine) restoreTokenBucketState() error {
	value, ok := e.getInternal(tokenBucketStateKey)
	if !ok {
		return nil
	}
	return e.bucket.Restore(value)
}

func (e *Engine) getInternal(key string) ([]byte, bool) {
	if value, ok := e.memtables.Get(key); ok {
		return value, true
	}

	for i := len(e.tables) - 1; i >= 0; i-- {
		value, found, deleted := e.tables[i].Lookup(key)
		if !found {
			continue
		}
		if deleted {
			return nil, false
		}
		return value, true
	}

	return nil, false
}

func isReservedKey(key string) bool {
	return strings.HasPrefix(key, "__system/")
}

func (e *Engine) ValidateMerkle(tableNumber int) (bool, []int, error) {
	if tableNumber < 1 || tableNumber > len(e.tables) {
		return false, nil, ErrTableNotFound
	}
	return e.tables[tableNumber-1].ValidateMerkle()
}

func (e *Engine) TableCount() int {
	return len(e.tables)
}

func (e *Engine) Close() error {
	return e.wal.Close()
}

func (e *Engine) flushMemtables() error {
	entries := e.memtables.Entries()
	if len(entries) == 0 {
		e.memtables.Clear()
		return nil
	}

	tableDir := filepath.Join(e.sstableDir, fmt.Sprintf("table_%06d", e.nextTableID))
	table, _, _, err := sstable.CreateFromEntriesWithBlockManager(tableDir, entries, e.summaryStep, e.blocks)
	if err != nil {
		return err
	}

	e.tables = append(e.tables, table)
	e.nextTableID++
	e.memtables.Clear()
	return e.wal.Reset()
}

func loadSSTables(dir string, summaryStep int, blocks *block.Manager) ([]*sstable.SSTable, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 1, nil
		}
		return nil, 0, err
	}

	type tableInfo struct {
		id    int
		table *sstable.SSTable
	}

	infos := make([]tableInfo, 0)
	maxID := 0

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "table_") {
			continue
		}

		id, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "table_"))
		if err != nil {
			continue
		}

		if id > maxID {
			maxID = id
		}
		infos = append(infos, tableInfo{
			id:    id,
			table: sstable.NewWithBlockManager(filepath.Join(dir, entry.Name()), summaryStep, blocks),
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].id < infos[j].id
	})

	tables := make([]*sstable.SSTable, 0, len(infos))
	for _, info := range infos {
		tables = append(tables, info.table)
	}

	return tables, maxID + 1, nil
}
