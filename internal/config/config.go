package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	WAL          WALConfig          `json:"wal"`
	Memtable     MemtableConfig     `json:"memtable"`
	SSTable      SSTableConfig      `json:"sstable"`
	Cache        CacheConfig        `json:"cache"`
	BlockManager BlockManagerConfig `json:"blockManager"`
	LSM          LSMConfig          `json:"lsm"`
	TokenBucket  TokenBucketConfig  `json:"tokenBucket"`
}

type WALConfig struct {
	Directory         string `json:"directory"`
	SegmentSizeBlocks int    `json:"segmentSizeBlocks"`
	RecordsPerSegment int    `json:"recordsPerSegment"`
}

type MemtableConfig struct {
	Implementation string `json:"implementation"`
	MaxEntries     int    `json:"maxEntries"`
	MaxSizeKB      int    `json:"maxSizeKB"`
	Instances      int    `json:"instances"`
}

type SSTableConfig struct {
	Directory   string `json:"directory"`
	SummaryStep int    `json:"summaryStep"`
	SingleFile  bool   `json:"singleFile"`
}

type CacheConfig struct {
	Capacity int `json:"capacity"`
}

type BlockManagerConfig struct {
	BlockSizeKB   int `json:"blockSizeKB"`
	CacheCapacity int `json:"cacheCapacity"`
}

type LSMConfig struct {
	MaxLevels           int    `json:"maxLevels"`
	CompactionAlgorithm string `json:"compactionAlgorithm"`
	Level0MaxTables     int    `json:"level0MaxTables"`
}

type TokenBucketConfig struct {
	Capacity              int `json:"capacity"`
	RefillIntervalSeconds int `json:"refillIntervalSeconds"`
}

func Default() Config {
	return Config{
		WAL: WALConfig{
			Directory:         "data/wal",
			SegmentSizeBlocks: 16,
			RecordsPerSegment: 1000,
		},
		Memtable: MemtableConfig{
			Implementation: "hashmap",
			MaxEntries:     1000,
			MaxSizeKB:      1024,
			Instances:      1,
		},
		SSTable: SSTableConfig{
			Directory:   "data/sstable",
			SummaryStep: 5,
			SingleFile:  false,
		},
		Cache: CacheConfig{
			Capacity: 128,
		},
		BlockManager: BlockManagerConfig{
			BlockSizeKB:   4,
			CacheCapacity: 64,
		},
		LSM: LSMConfig{
			MaxLevels:           3,
			CompactionAlgorithm: "size-tiered",
			Level0MaxTables:     4,
		},
		TokenBucket: TokenBucketConfig{
			Capacity:              10,
			RefillIntervalSeconds: 60,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("decode config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.WAL.Directory == "" {
		return fmt.Errorf("wal.directory must not be empty")
	}
	if c.WAL.SegmentSizeBlocks < 1 {
		return fmt.Errorf("wal.segmentSizeBlocks must be at least 1")
	}
	if c.WAL.RecordsPerSegment < 1 {
		return fmt.Errorf("wal.recordsPerSegment must be at least 1")
	}

	switch c.Memtable.Implementation {
	case "hashmap", "skiplist", "btree":
	default:
		return fmt.Errorf("memtable.implementation must be hashmap, skiplist, or btree")
	}
	if c.Memtable.MaxEntries < 1 {
		return fmt.Errorf("memtable.maxEntries must be at least 1")
	}
	if c.Memtable.MaxSizeKB < 1 {
		return fmt.Errorf("memtable.maxSizeKB must be at least 1")
	}
	if c.Memtable.Instances < 1 {
		return fmt.Errorf("memtable.instances must be at least 1")
	}

	if c.SSTable.Directory == "" {
		return fmt.Errorf("sstable.directory must not be empty")
	}
	if c.SSTable.SummaryStep < 1 {
		return fmt.Errorf("sstable.summaryStep must be at least 1")
	}

	if c.Cache.Capacity < 0 {
		return fmt.Errorf("cache.capacity must be 0 or greater")
	}

	switch c.BlockManager.BlockSizeKB {
	case 4, 8, 16:
	default:
		return fmt.Errorf("blockManager.blockSizeKB must be 4, 8, or 16")
	}
	if c.BlockManager.CacheCapacity < 0 {
		return fmt.Errorf("blockManager.cacheCapacity must be 0 or greater")
	}

	if c.LSM.MaxLevels < 1 {
		return fmt.Errorf("lsm.maxLevels must be at least 1")
	}
	switch c.LSM.CompactionAlgorithm {
	case "size-tiered", "leveled":
	default:
		return fmt.Errorf("lsm.compactionAlgorithm must be size-tiered or leveled")
	}
	if c.LSM.Level0MaxTables < 1 {
		return fmt.Errorf("lsm.level0MaxTables must be at least 1")
	}

	if c.TokenBucket.Capacity < 1 {
		return fmt.Errorf("tokenBucket.capacity must be at least 1")
	}
	if c.TokenBucket.RefillIntervalSeconds < 1 {
		return fmt.Errorf("tokenBucket.refillIntervalSeconds must be at least 1")
	}

	return nil
}
