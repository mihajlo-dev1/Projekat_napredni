package tokenbucket

import "time"

type Bucket struct {
	capacity       int
	tokens         int
	refillInterval time.Duration
	lastRefill     time.Time
}

func New(capacity int, refillInterval time.Duration) *Bucket {
	now := time.Now()
	return &Bucket{
		capacity:       capacity,
		tokens:         capacity,
		refillInterval: refillInterval,
		lastRefill:     now,
	}
}

func (b *Bucket) Allow() bool {
	return false
}
