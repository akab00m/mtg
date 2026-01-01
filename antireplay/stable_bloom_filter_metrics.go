package antireplay

import (
	"sync"
	"sync/atomic"

	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/OneOfOne/xxhash"
	boom "github.com/tylertreat/BoomFilters"
)

// stableBloomFilterWithMetrics wraps stableBloomFilter with performance and security metrics.
type stableBloomFilterWithMetrics struct {
	filter boom.StableBloomFilter
	mutex  sync.Mutex

	// Metrics (atomic counters for lock-free reads)
	totalChecks   uint64 // Total number of SeenBefore calls
	replayDetected uint64 // Number of replays detected (duplicates found)
	uniqueMessages uint64 // Number of unique messages (first-time seen)
}

func (s *stableBloomFilterWithMetrics) SeenBefore(digest []byte) bool {
	atomic.AddUint64(&s.totalChecks, 1)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	isDuplicate := s.filter.TestAndAdd(digest)

	if isDuplicate {
		atomic.AddUint64(&s.replayDetected, 1)
	} else {
		atomic.AddUint64(&s.uniqueMessages, 1)
	}

	return isDuplicate
}

// Metrics returns current anti-replay statistics.
type Metrics struct {
	TotalChecks     uint64  // Total number of messages checked
	ReplayDetected  uint64  // Number of replays detected
	UniqueMessages  uint64  // Number of unique messages
	ReplayRate      float64 // Percentage of replays (0.0 to 100.0)
	EstimatedFPRate float64 // Estimated false positive rate (based on filter state)
}

// GetMetrics returns current statistics. Thread-safe (uses atomic loads).
func (s *stableBloomFilterWithMetrics) GetMetrics() Metrics {
	totalChecks := atomic.LoadUint64(&s.totalChecks)
	replayDetected := atomic.LoadUint64(&s.replayDetected)
	uniqueMessages := atomic.LoadUint64(&s.uniqueMessages)

	var replayRate float64
	if totalChecks > 0 {
		replayRate = float64(replayDetected) / float64(totalChecks) * 100.0
	}

	s.mutex.Lock()
	estimatedFPRate := s.filter.FillRatio() // BoomFilters provides fill ratio estimation
	s.mutex.Unlock()

	return Metrics{
		TotalChecks:     totalChecks,
		ReplayDetected:  replayDetected,
		UniqueMessages:  uniqueMessages,
		ReplayRate:      replayRate,
		EstimatedFPRate: estimatedFPRate,
	}
}

// ResetMetrics resets all counters to zero. Does NOT reset the bloom filter itself.
func (s *stableBloomFilterWithMetrics) ResetMetrics() {
	atomic.StoreUint64(&s.totalChecks, 0)
	atomic.StoreUint64(&s.replayDetected, 0)
	atomic.StoreUint64(&s.uniqueMessages, 0)
}

// NewStableBloomFilterWithMetrics returns an instrumented anti-replay cache.
//
// This version tracks:
//   - Total number of checks
//   - Number of replays detected
//   - Number of unique messages
//   - Replay rate percentage
//   - Estimated false positive rate
//
// Use GetMetrics() to retrieve statistics for monitoring/alerting.
//
// Parameters are the same as NewStableBloomFilter:
//   - byteSize: memory allocation in bytes (0 for default 1 MB)
//   - errorRate: desired false positive rate (negative for default 1%)
func NewStableBloomFilterWithMetrics(byteSize uint, errorRate float64) *stableBloomFilterWithMetrics {
	if byteSize == 0 {
		byteSize = DefaultStableBloomFilterMaxSize
	}

	if errorRate < 0 {
		errorRate = DefaultStableBloomFilterErrorRate
	}

	sf := boom.NewDefaultStableBloomFilter(byteSize*8, errorRate) //nolint: gomnd
	sf.SetHash(xxhash.New64())

	return &stableBloomFilterWithMetrics{
		filter: *sf,
	}
}

// Ensure interface compliance
var _ mtglib.AntiReplayCache = (*stableBloomFilterWithMetrics)(nil)
