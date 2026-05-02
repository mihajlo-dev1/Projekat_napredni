# Architecture Notes

Ovo je obican tekstualni dogovor za tim.

## Cilj

Skeleton:

- WAL
- Memtable sa vise instanci
- SSTable sa data/index/summary/filter/metadata slojevima
- Cache
- Block manager i block cache
- Token bucket
- izbor memtable backend-a (`hashmap`, `skiplist`, `btree`)

## Shared Files

Ove fajlove ne treba dirati bez dogovora, jer najlakse prave konflikte:

- `internal/interface.go`
- `internal/config/config.go`
- `internal/engine/*`
- `cmd/kvengine/main.go`
- `docs/architecture.md`

## Package Ownership

Paketi koje je lako podeliti po clanovima tima:

- `internal/wal`
- `internal/memtable`
- `internal/hashmap`
- `internal/skiplist`
- `internal/btree`
- `internal/sstable`
- `internal/bloom`
- `internal/merkle`
- `internal/cache`
- `internal/block`
- `internal/blockcache`
- `internal/tokenbucket`
- `internal/lsm`

## Folder Guide

- `cmd/kvengine`
  Ulazna tacka aplikacije. Ovde ide `main`, ucitavanje konfiguracije i pokretanje engine-a.

- `internal`
  Shared tipovi i interfejsi koje koristi vise paketa.

- `internal/wal`
  Write-ahead log: append, replay, segmenti, CRC i format zapisa na disku.

- `internal/memtable`
  Memtable sloj: pravila za rad u memoriji, puna tabela, vise instanci i izbor backend strukture.

- `internal/hashmap`
  Hash mapa backend za memtable. Prva i trenutno jedina funkcionalna implementacija.

- `internal/skiplist`
  Skip lista backend za memtable. Skeleton za kasniju implementaciju.

- `internal/btree`
  B stablo backend za memtable. Skeleton za kasniju implementaciju.

- `internal/sstable`
  SSTable logika: data, index, summary, filter i metadata slojevi.

- `internal/bloom`
  Bloom filter za SSTable i za kasnije probabilisticke operacije gde bude trebalo.

- `internal/merkle`
  Merkle stablo za metadata deo SSTable-a i proveru integriteta.

- `internal/cache`
  Key-value cache iz read path-a, planiran kao LRU.

- `internal/block`
  Block manager sloj. Sav pristup fajlovima na disku treba da ide preko njega.

- `internal/blockcache`
  Cache blokova koje koristi block manager.

- `internal/tokenbucket`
  Ogranicenje stope pristupa nad operacijama sistema.

- `internal/lsm`
  Organizacija SSTable-ova po nivoima i kasnije kompakcije.

- `internal/config`
  Ucitavanje i validacija spoljne konfiguracije.

- `internal/engine`
  Glavni koordinator sistema. Spaja WAL, Memtable, Cache, SSTable i ostale komponente.

- `docs`
  Tekstualna dokumentacija i dogovori tima.

## Write Path

1. `PUT` ili `DELETE` prvo ide u `WAL`.
2. Posle potvrdjenog upisa, operacija ide u `Memtable`.
3. Kada se popuni `Memtable`, radi se flush u `SSTable`.
4. Posle flush-a proverava se da li treba kompaktovati LSM nivoe.

## Read Path

1. Prvo proveriti `Cache`.
2. Zatim proveriti `Memtable`.
3. Zatim proveravati `SSTable` strukture po pravilima LSM stabla.
4. Tokom SSTable citanja proveravati redom filter, summary, index i data deo.


