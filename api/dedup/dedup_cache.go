package dedup

import (
	"sync"
	"time"
)

type DedupCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func NewDedupCache(ttl time.Duration) *DedupCache {
	return &DedupCache{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
}

func (d *DedupCache) CheckAndRecord(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	recordedAt, exists := d.seen[key]

	if exists && time.Since(recordedAt) < d.ttl {
		return true
	}

	d.seen[key] = time.Now()
	return false
}

func (d *DedupCache) StartSweeper(sweepInterval time.Duration) {
	ticker := time.NewTicker(sweepInterval)

	for range ticker.C {
		d.mu.Lock()
		for key, recordedAt := range d.seen {
			if time.Since(recordedAt) >= d.ttl {
				delete(d.seen, key)
			}
		}
		d.mu.Unlock()
	}
}
