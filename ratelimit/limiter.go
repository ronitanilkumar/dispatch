package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
}

type Limiter struct {
	buckets    sync.Map
	maxTokens  float64
	refillRate float64
}

func NewLimiter(maxTokens float64, refillRate float64) *Limiter {
	return &Limiter{
		maxTokens:  maxTokens,
		refillRate: refillRate,
	}
}

func (b *bucket) allow(maxTokens float64, refillRate float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.lastRefill = now

	b.tokens = b.tokens + (elapsed * refillRate)
	b.tokens = min(b.tokens, maxTokens)

	if b.tokens >= 1 {
		b.tokens--
		return true
	}

	return false
}

func (l *Limiter) Allow(host string) bool {
	actual, _ := l.buckets.LoadOrStore(host, &bucket{tokens: l.maxTokens, lastRefill: time.Now()})
	b := actual.(*bucket)
	return b.allow(l.maxTokens, l.refillRate)
}
