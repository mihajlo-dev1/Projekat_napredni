package main

import (
	"fmt"
	"log"

	"kv-engine/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	fmt.Printf(
		"project starting with memtable=%s, walBlocks=%d, blockSizeKB=%d\n",
		cfg.Memtable.Implementation,
		cfg.WAL.SegmentSizeBlocks,
		cfg.BlockManager.BlockSizeKB,
	)
}
