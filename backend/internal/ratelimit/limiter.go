package ratelimit

import (
	"math"
	"sync"
	"time"
)

const (
	defaultMaxEntries = 20_000
	idleEntryTTL      = 10 * time.Minute
)

// Decision is the result of one token-bucket check.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// Limiter is a bounded, per-process token bucket. Production ingress should
// still enforce a cluster-wide limit; this limiter protects each API replica
// from one merchant exhausting all local capacity.
type Limiter struct {
	mu         sync.Mutex
	rate       float64
	burst      int
	maxEntries int
	entries    map[string]bucket
	now        func() time.Time
}

func New(requestsPerSecond, burst int) *Limiter {
	return &Limiter{
		rate:       float64(requestsPerSecond),
		burst:      burst,
		maxEntries: defaultMaxEntries,
		entries:    make(map[string]bucket),
		now:        time.Now,
	}
}

func (l *Limiter) Enabled() bool {
	return l != nil && l.rate > 0 && l.burst > 0
}

func (l *Limiter) Allow(key string) Decision {
	if !l.Enabled() {
		return Decision{Allowed: true}
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	item, exists := l.entries[key]
	if !exists {
		l.makeRoom(now)
		item = bucket{tokens: float64(l.burst), lastSeen: now}
	}
	if elapsed := now.Sub(item.lastSeen).Seconds(); elapsed > 0 {
		item.tokens = math.Min(float64(l.burst), item.tokens+elapsed*l.rate)
	}
	item.lastSeen = now

	decision := Decision{Limit: l.burst}
	if item.tokens >= 1 {
		item.tokens--
		decision.Allowed = true
		decision.Remaining = int(math.Floor(item.tokens))
	} else {
		decision.RetryAfter = time.Duration(math.Ceil((1-item.tokens)/l.rate*1000)) * time.Millisecond
		if decision.RetryAfter < time.Second {
			decision.RetryAfter = time.Second
		}
	}
	l.entries[key] = item
	return decision
}

func (l *Limiter) makeRoom(now time.Time) {
	if len(l.entries) < l.maxEntries {
		return
	}
	cutoff := now.Add(-idleEntryTTL)
	for key, item := range l.entries {
		if item.lastSeen.Before(cutoff) {
			delete(l.entries, key)
		}
	}
	if len(l.entries) < l.maxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, item := range l.entries {
		if oldestKey == "" || item.lastSeen.Before(oldest) {
			oldestKey, oldest = key, item.lastSeen
		}
	}
	delete(l.entries, oldestKey)
}
