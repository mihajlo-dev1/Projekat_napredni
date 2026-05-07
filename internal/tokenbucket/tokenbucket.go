package tokenbucket

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

var ErrInvalidState = errors.New("token bucket: invalid state")

// Bucket ogranicava broj operacija u vremenskom intervalu.
type Bucket struct {
	capacity       int
	tokens         int
	refillInterval time.Duration
	lastRefill     time.Time
	mu             sync.Mutex
}

// New pocinje sa punim bucket-om.
func New(capacity int, refillInterval time.Duration) *Bucket {
	now := time.Now()
	return &Bucket{
		capacity:       capacity,
		tokens:         capacity,
		refillInterval: refillInterval,
		lastRefill:     now,
	}
}

// Allow vraca true ako ima tokena za jos jednu operaciju.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if now.Sub(b.lastRefill) >= b.refillInterval {
		// Kad prodje interval, bucket se napuni do capacity.
		b.tokens = b.capacity
		b.lastRefill = now
	}

	if b.tokens <= 0 {
		// Nema tokena, operacija se odbija.
		return false
	}

	// Svaka dozvoljena operacija potrosi jedan token.
	b.tokens--
	return true
}

// Serialize pakuje stanje u 24 bajta za cuvanje u engine-u.
func (b *Bucket) Serialize() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	data := make([]byte, 24)
	// Format: capacity, tokens, lastRefill.UnixNano.
	binary.BigEndian.PutUint64(data[0:8], uint64(b.capacity))
	binary.BigEndian.PutUint64(data[8:16], uint64(b.tokens))
	binary.BigEndian.PutUint64(data[16:24], uint64(b.lastRefill.UnixNano()))
	return data
}

// Restore vraca bucket iz binarnog stanja.
func (b *Bucket) Restore(data []byte) error {
	if len(data) != 24 {
		return ErrInvalidState
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	capacity := int(binary.BigEndian.Uint64(data[0:8]))
	tokens := int(binary.BigEndian.Uint64(data[8:16]))
	lastRefill := int64(binary.BigEndian.Uint64(data[16:24]))

	if capacity < 1 || tokens < 0 || tokens > capacity {
		// Stanje nema smisla ako tokena ima vise od capacity.
		return ErrInvalidState
	}

	b.capacity = capacity
	b.tokens = tokens
	b.lastRefill = time.Unix(0, lastRefill)
	return nil
}
