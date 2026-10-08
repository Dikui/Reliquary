// Package ratelimit provides a process-local sliding-window rate limiter.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

const localCleanupInterval = time.Minute

// Limiter enforces per-key sliding-window limits. max is evaluated per Allow
// call so callers (e.g. embed channels) can vary budgets without rebuilding
// the limiter.
type Limiter struct {
	local  *localLimiter
	window time.Duration
}

// New constructs a sliding-window limiter for this process.
func New(window time.Duration) *Limiter {
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{local: newLocalLimiter(), window: window}
}

// Allow reports whether key is within budget for the current window.
func (l *Limiter) Allow(ctx context.Context, key string, max int) bool {
	if max <= 0 {
		return true
	}
	return l.local.allow(key, l.window, max)
}

// StartCleanup runs periodic eviction of expired local entries.
func (l *Limiter) StartCleanup(stopCh <-chan struct{}) {
	l.local.startCleanup(l.window, stopCh)
}

type localEntry struct {
	mu         sync.Mutex
	timestamps []time.Time
	deleted    bool
}

type localLimiter struct {
	entries sync.Map // key -> *localEntry
}

func newLocalLimiter() *localLimiter {
	return &localLimiter{}
}

func (l *localLimiter) allow(key string, window time.Duration, max int) bool {
	now := time.Now()
	cutoff := now.Add(-window)

	for {
		val, _ := l.entries.LoadOrStore(key, &localEntry{})
		entry := val.(*localEntry)

		entry.mu.Lock()
		if entry.deleted {
			entry.mu.Unlock()
			l.entries.CompareAndDelete(key, entry)
			continue
		}

		valid := entry.timestamps[:0]
		for _, t := range entry.timestamps {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		entry.timestamps = valid

		if len(entry.timestamps) >= max {
			entry.mu.Unlock()
			return false
		}
		entry.timestamps = append(entry.timestamps, now)
		entry.mu.Unlock()
		return true
	}
}

func (l *localLimiter) startCleanup(window time.Duration, stopCh <-chan struct{}) {
	if window <= 0 {
		window = time.Minute
	}
	ticker := time.NewTicker(localCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Evict against the limiter's own window, not a hardcoded minute,
			// so non-minute limiters (e.g. a per-day cap) are not dropped early.
			cutoff := time.Now().Add(-window)
			l.entries.Range(func(key, val any) bool {
				entry := val.(*localEntry)
				entry.mu.Lock()
				allExpired := true
				for _, t := range entry.timestamps {
					if t.After(cutoff) {
						allExpired = false
						break
					}
				}
				if allExpired {
					entry.deleted = true
					l.entries.Delete(key)
				}
				entry.mu.Unlock()
				return true
			})
		case <-stopCh:
			return
		}
	}
}
