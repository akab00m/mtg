package telegram

import (
	"context"
	"net"
	"sync"
	"time"
)

// DCHealth represents the health status of a datacenter.
type DCHealth struct {
	DC          int           `json:"dc"`
	Available   bool          `json:"available"`
	Latency     time.Duration `json:"latency_ms"`
	LastChecked time.Time     `json:"last_checked"`
	Failures    int           `json:"failures"`
}

// DCHealthChecker periodically checks DC availability.
type DCHealthChecker struct {
	mu           sync.RWMutex
	health       map[int]*DCHealth
	pool         *addressPool
	checkTimeout time.Duration
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewDCHealthChecker creates a new health checker.
func NewDCHealthChecker(pool *addressPool, checkTimeout time.Duration) *DCHealthChecker {
	return &DCHealthChecker{
		health:       make(map[int]*DCHealth),
		pool:         pool,
		checkTimeout: checkTimeout,
		stopCh:       make(chan struct{}),
	}
}

// Start begins periodic health checks.
func (h *DCHealthChecker) Start(interval time.Duration) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		// Initial check
		h.checkAllDCs()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-h.stopCh:
				return
			case <-ticker.C:
				h.checkAllDCs()
			}
		}
	}()
}

// Stop stops the health checker.
func (h *DCHealthChecker) Stop() {
	close(h.stopCh)
	h.wg.Wait()
}

// checkAllDCs performs health check on all DCs.
func (h *DCHealthChecker) checkAllDCs() {
	numDCs := len(h.pool.v4)

	var wg sync.WaitGroup
	for dc := 1; dc <= numDCs; dc++ {
		wg.Add(1)
		go func(dcNum int) {
			defer wg.Done()
			h.checkDC(dcNum)
		}(dc)
	}
	wg.Wait()
}

// checkDC checks a single DC's availability.
func (h *DCHealthChecker) checkDC(dc int) {
	addrs := h.pool.getV4(dc)
	if len(addrs) == 0 {
		return
	}

	addr := addrs[0]
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), h.checkTimeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp4", addr.address)
	latency := time.Since(start)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.health[dc] == nil {
		h.health[dc] = &DCHealth{DC: dc}
	}

	health := h.health[dc]
	health.LastChecked = time.Now()

	if err != nil {
		health.Available = false
		health.Failures++
		health.Latency = 0
	} else {
		conn.Close()
		health.Available = true
		health.Failures = 0
		health.Latency = latency
	}
}

// GetHealth returns health status for a specific DC.
func (h *DCHealthChecker) GetHealth(dc int) *DCHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if health, ok := h.health[dc]; ok {
		// Return a copy to avoid race conditions
		copy := *health
		return &copy
	}
	return nil
}

// GetAllHealth returns health status for all DCs.
func (h *DCHealthChecker) GetAllHealth() []DCHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]DCHealth, 0, len(h.health))
	for _, health := range h.health {
		result = append(result, *health)
	}
	return result
}

// GetBestDC returns the DC with lowest latency that's available.
// Returns 0 if no DC is available.
func (h *DCHealthChecker) GetBestDC() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var bestDC int
	var bestLatency time.Duration = time.Hour // Large initial value

	for dc, health := range h.health {
		if health.Available && health.Latency > 0 && health.Latency < bestLatency {
			bestDC = dc
			bestLatency = health.Latency
		}
	}

	return bestDC
}

// IsAvailable returns whether a DC is currently available.
func (h *DCHealthChecker) IsAvailable(dc int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if health, ok := h.health[dc]; ok {
		return health.Available
	}
	// If not checked yet, assume available
	return true
}
