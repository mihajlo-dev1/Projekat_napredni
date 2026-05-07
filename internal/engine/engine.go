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

// Interni kljuc pod kojim se cuva stanje token bucketa.
const tokenBucketStateKey = "__system/token_bucket_state"

// Engine spaja sve glavne delove baze: WAL, memtable, cache i SSTable fajlove.
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

	// Koristi se samo dok se WAL obnavlja pri startovanju.
	replaying   bool
	replayFlush bool
}

// New priprema direktorijume i pravi sve komponente engine-a iz konfiguracije.
func New(cfg config.Config) (*Engine, error) {
	// WAL path je baza imena fajla, npr. data/wal -> data/wal_0001.log.
	// Zato se pravi parent folder, a ne nuzno folder "wal".
	if err := os.MkdirAll(filepath.Dir(cfg.WAL.Directory), 0755); err != nil {
		return nil, err
	}
	// SSTable koristi pravi direktorijum, jer svaka tabela ima svoj podfolder.
	if err := os.MkdirAll(cfg.SSTable.Directory, 0755); err != nil {
		return nil, err
	}

	// Block manager je zajednicki sloj za citanje/pisanje fajlova po blokovima.
	blocks := block.New(cfg.BlockManager.BlockSizeKB, blockcache.New(cfg.BlockManager.CacheCapacity))

	// WAL koristi istu velicinu bloka kao block manager.
	blockSizeBytes := cfg.BlockManager.BlockSizeKB * 1024
	w, err := wal.OpenConfigured(cfg.WAL.Directory, blocks, blockSizeBytes, cfg.WAL.SegmentSizeBlocks, cfg.WAL.RecordsPerSegment)
	if err != nil {
		return nil, err
	}

	// Memtable manager bira backend iz config-a: hashmap, skiplist ili btree.
	manager, err := memtable.NewMemtableManager(
		cfg.Memtable.Implementation,
		cfg.Memtable.MaxEntries,
		cfg.Memtable.Instances,
	)
	if err != nil {
		// Ako kasnija inicijalizacija pukne, zatvaramo vec otvoren WAL.
		w.Close()
		return nil, err
	}

	// Pri startu moramo znati koje SSTable vec postoje na disku.
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

// Start obnavlja neflushovane promene iz WAL-a i vraca sistem u konzistentno stanje.
func (e *Engine) Start() error {
	var replayErr error

	// Ove zastavice kazu flushMemtables da je flush pozvan tokom recovery-ja.
	e.replaying = true
	e.replayFlush = false
	if err := e.wal.Replay(
		// Kada WAL procita PUT, ne upisujemo ga ponovo u WAL.
		// Samo obnavljamo memtable, jer zapis vec postoji u WAL-u.
		func(key []byte, value []byte) {
			if replayErr != nil {
				return
			}
			// Ako memtable postane puna tokom replay-a, mora odmah u SSTable.
			needsFlush := e.memtables.Put(string(key), value)
			if needsFlush {
				replayErr = e.flushMemtables()
			}
		},
		// DELETE se replay-uje kao tombstone u memtable.
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
		e.replaying = false
		return err
	}
	// Od ovog trenutka WAL se vise ne cita.
	e.replaying = false

	if replayErr != nil {
		return replayErr
	}

	// Ako je replay vec pravio SSTable, onda zelimo da zavrsimo posao:
	// ili flushujemo preostale zapise, ili resetujemo WAL ako nema ostatka.
	if e.replayFlush && len(e.memtables.Entries()) > 0 {
		if err := e.flushMemtables(); err != nil {
			return err
		}
	} else if e.replayFlush {
		if err := e.wal.Reset(); err != nil {
			return err
		}
	}

	e.replayFlush = false
	// Token bucket stanje je obicno sakriveno u sistemskom key/value zapisu.
	if err := e.restoreTokenBucketState(); err != nil {
		return err
	}

	return nil
}

// Put prvo upisuje u WAL, pa tek onda menja memtable i cache.
func (e *Engine) Put(key string, value []byte) error {
	// Korisnik ne sme da dira interne engine kljuceve.
	if isReservedKey(key) {
		return ErrReservedKey
	}
	// Svaka korisnicka operacija trosi token.
	if err := e.allow(); err != nil {
		return err
	}

	// WAL ide prvi: ako program pukne posle ovoga, replay moze da obnovi PUT.
	if err := e.wal.AppendPut([]byte(key), value); err != nil {
		return err
	}

	// Posle uspesnog WAL upisa menjamo brzi memorijski sloj.
	needsFlush := e.memtables.Put(key, value)
	e.cache.Put(key, value)

	if needsFlush {
		return e.flushMemtables()
	}

	return nil
}

// Delete se u sistemu cuva kao tombstone, da starije SSTable vrednosti ne isplivaju.
func (e *Engine) Delete(key string) error {
	if isReservedKey(key) {
		return ErrReservedKey
	}
	if err := e.allow(); err != nil {
		return err
	}

	// Brisanje takodje ide u WAL, jer menja stanje baze.
	if err := e.wal.AppendDelete([]byte(key)); err != nil {
		return err
	}

	// U memtable se cuva tombstone, a cache se cisti da ne vrati staru vrednost.
	needsFlush := e.memtables.Delete(key)
	e.cache.Delete(key)

	if needsFlush {
		return e.flushMemtables()
	}

	return nil
}

// Get je kraca verzija bez detaljne greske.
func (e *Engine) Get(key string) ([]byte, bool) {
	value, ok, _ := e.GetWithError(key)
	return value, ok
}

// GetWithError proverava reserved kljuceve i rate limit pre citanja.
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

// get je read path: cache, memtable, pa SSTable od najnovije ka najstarijoj.
func (e *Engine) get(key string) ([]byte, bool) {
	// Najbrzi slucaj: vrednost je vec skoro citana ili upisana.
	if value, ok := e.cache.Get(key); ok {
		fmt.Printf("[read] key=%q source=cache\n", key)
		return value, true
	}

	// Memtable ima novije podatke od SSTable fajlova.
	if value, ok := e.memtables.Get(key); ok {
		fmt.Printf("[read] key=%q source=memtable\n", key)
		e.cache.Put(key, value)
		return value, true
	}
	// Tombstone u memtable mora da sakrije starije vrednosti na disku.
	if e.memtables.IsDeleted(key) {
		fmt.Printf("[read] key=%q source=memtable tombstone\n", key)
		return nil, false
	}

	// SSTable se proveravaju unazad, jer novija tabela ima noviju verziju kljuca.
	for i := len(e.tables) - 1; i >= 0; i-- {
		value, found, deleted := e.tables[i].Lookup(key)
		if !found {
			continue
		}
		if deleted {
			// Tombstone na disku takodje znaci da se starije tabele ignorisu.
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

// allow skida jedan token za svaku korisnicku operaciju.
func (e *Engine) allow() error {
	if e.bucket != nil && !e.bucket.Allow() {
		return ErrRateLimited
	}
	if e.bucket != nil {
		// Posle skidanja tokena odmah pamtimo novo stanje.
		return e.storeTokenBucketState()
	}
	return nil
}

// storeTokenBucketState pamti rate-limit stanje kroz isti WAL/memtable tok.
func (e *Engine) storeTokenBucketState() error {
	// Value je binarni zapis: capacity, tokens i lastRefill.
	data := e.bucket.Serialize()
	if err := e.wal.AppendPut([]byte(tokenBucketStateKey), data); err != nil {
		return err
	}
	// Upis ide kao obican key/value, ali pod reserved sistemskim kljucem.
	needsFlush := e.memtables.Put(tokenBucketStateKey, data)
	if needsFlush {
		return e.flushMemtables()
	}
	return nil
}

// restoreTokenBucketState vraca sacuvano stanje token bucketa posle restarta.
func (e *Engine) restoreTokenBucketState() error {
	value, ok := e.getInternal(tokenBucketStateKey)
	if !ok {
		return nil
	}
	return e.bucket.Restore(value)
}

// getInternal cita sistemske kljuceve, bez rate limita i reserved-key provere.
func (e *Engine) getInternal(key string) ([]byte, bool) {
	// Isti redosled kao user read path, ali bez cache-a i rate limita.
	if value, ok := e.memtables.Get(key); ok {
		return value, true
	}
	if e.memtables.IsDeleted(key) {
		return nil, false
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

// Svi __system/ kljucevi su rezervisani za engine.
func isReservedKey(key string) bool {
	return strings.HasPrefix(key, "__system/")
}

// ValidateMerkle proverava integritet konkretne SSTable tabele.
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

// flushMemtables prebacuje trenutne memtable zapise u novu SSTable.
func (e *Engine) flushMemtables() error {
	entries := e.memtables.Entries()
	if len(entries) == 0 {
		// Ako nema zapisa, samo vratimo manager na jednu praznu memtable.
		e.memtables.Clear()
		return nil
	}

	// Svaki flush pravi novu tabelu table_000001, table_000002, ...
	tableDir := filepath.Join(e.sstableDir, fmt.Sprintf("table_%06d", e.nextTableID))
	table, _, _, err := sstable.CreateFromEntriesWithBlockManager(tableDir, entries, e.summaryStep, e.blocks)
	if err != nil {
		return err
	}

	e.tables = append(e.tables, table)
	e.nextTableID++
	e.memtables.Clear()
	if e.replaying {
		// Tokom replay-a WAL se ne resetuje odmah, jer se jos cita.
		// Start ce ga srediti tek kad Replay zavrsi.
		e.replayFlush = true
		return nil
	}
	// U normalnom radu, posle uspesnog flush-a WAL vise nije potreban.
	return e.wal.Reset()
}

// loadSSTables ucitava postojece tabele i racuna sledeci slobodan table ID.
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
		// Ignorisu se svi folderi koji nisu oblika table_000001.
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
		// Ne citamo celu tabelu odmah, samo pamtimo putanje njenih fajlova.
		infos = append(infos, tableInfo{
			id:    id,
			table: sstable.NewWithBlockManager(filepath.Join(dir, entry.Name()), summaryStep, blocks),
		})
	}

	// Tabele moraju biti sortirane po ID-u da read path zna koja je najnovija.
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].id < infos[j].id
	})

	tables := make([]*sstable.SSTable, 0, len(infos))
	for _, info := range infos {
		tables = append(tables, info.table)
	}

	return tables, maxID + 1, nil
}
