package mtglib

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter provides IP-based rate limiting for connections.
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	r        rate.Limit
	b        int
	cleanup  time.Duration
	lastUsed map[string]time.Time
	stopCh   chan struct{}
}

// NewRateLimiter creates a new rate limiter.
// r is the rate limit (requests per second).
// b is the burst size (max requests in a burst).
// cleanup is how often to clean up old entries.
func NewRateLimiter(r rate.Limit, b int, cleanup time.Duration) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		lastUsed: make(map[string]time.Time),
		r:        r,
		b:        b,
		cleanup:  cleanup,
		stopCh:   make(chan struct{}),
	}

	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip net.IP) bool {
	// string(ip) — raw bytes (4/16 байт), дешевле ip.String() (форматирование "1.2.3.4")
	key := string(ip)

	// Fast path: для существующих IP достаточно RLock (read-only).
	// lastUsed не обновляем — worst case: limiter пересоздастся при cleanup,
	// что безопасно (rate сбросится, клиент получит больше, не меньше).
	rl.mu.RLock()
	limiter, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if exists {
		return limiter.Allow()
	}

	// Slow path: новый IP — нужен write lock
	rl.mu.Lock()
	// Double-check после escalation — другая goroutine могла добавить
	limiter, exists = rl.limiters[key]
	if !exists {
		limiter = rate.NewLimiter(rl.r, rl.b)
		rl.limiters[key] = limiter
	}
	rl.lastUsed[key] = time.Now()
	rl.mu.Unlock()

	return limiter.Allow()
}

// Stop gracefully stops the rate limiter cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

// cleanupLoop removes old rate limiters that haven't been used recently.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for key, lastUsed := range rl.lastUsed {
				if now.Sub(lastUsed) > rl.cleanup*2 {
					delete(rl.limiters, key)
					delete(rl.lastUsed, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}
