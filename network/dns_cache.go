package network

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// DNSCacheEntry represents a cached DNS record with TTL awareness
type DNSCacheEntry struct {
	IPs       []string
	ExpiresAt time.Time
	TTL       uint32 // Original TTL from DNS response
}

// Expired checks if the cache entry has expired
func (e *DNSCacheEntry) Expired() bool {
	return time.Now().After(e.ExpiresAt)
}

// LRUDNSCache is a thread-safe LRU cache for DNS records with TTL awareness
type LRUDNSCache struct {
	maxSize  int
	cache    map[string]*list.Element
	lruList  *list.List
	mutex    sync.Mutex // Полный мьютекс для структурных операций (LRU reorder, evict)

	// Метрики — атомарные счётчики, не требуют мьютекса для чтения
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

type lruCacheEntry struct {
	key   string
	value *DNSCacheEntry
}

// NewLRUDNSCache creates a new LRU DNS cache with specified max size
func NewLRUDNSCache(maxSize int) *LRUDNSCache {
	if maxSize <= 0 {
		maxSize = 1000 // Default: 1000 entries
	}

	return &LRUDNSCache{
		maxSize: maxSize,
		cache:   make(map[string]*list.Element, maxSize),
		lruList: list.New(),
	}
}

// Get retrieves a DNS entry from cache. Returns nil if not found or expired.
func (c *LRUDNSCache) Get(key string) *DNSCacheEntry {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	elem, ok := c.cache[key]
	if !ok {
		c.misses.Add(1)
		return nil
	}

	entry := elem.Value.(*lruCacheEntry).value

	// Check if expired
	if entry.Expired() {
		// Remove expired entry
		c.lruList.Remove(elem)
		delete(c.cache, key)
		c.misses.Add(1)
		return nil
	}

	// Move to front (most recently used)
	c.lruList.MoveToFront(elem)
	c.hits.Add(1)
	return entry
}

// MaxIPsPerEntry limits the number of IPs stored per DNS entry to prevent memory abuse
const MaxIPsPerEntry = 32

// Set stores a DNS entry in cache with TTL
func (c *LRUDNSCache) Set(key string, ips []string, ttl uint32) {
	// Limit IPs per entry to prevent memory abuse
	if len(ips) > MaxIPsPerEntry {
		ips = ips[:MaxIPsPerEntry]
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if key already exists
	if elem, ok := c.cache[key]; ok {
		// Update existing entry
		c.lruList.MoveToFront(elem)
		entry := elem.Value.(*lruCacheEntry)
		entry.value = &DNSCacheEntry{
			IPs:       ips,
			ExpiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
			TTL:       ttl,
		}
		return
	}

	// Create new entry
	newEntry := &lruCacheEntry{
		key: key,
		value: &DNSCacheEntry{
			IPs:       ips,
			ExpiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
			TTL:       ttl,
		},
	}

	elem := c.lruList.PushFront(newEntry)
	c.cache[key] = elem

	// Evict oldest if over capacity
	if c.lruList.Len() > c.maxSize {
		oldest := c.lruList.Back()
		if oldest != nil {
			c.lruList.Remove(oldest)
			oldEntry := oldest.Value.(*lruCacheEntry)
			delete(c.cache, oldEntry.key)
			c.evictions.Add(1)
		}
	}
}

// Size returns current cache size
func (c *LRUDNSCache) Size() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.lruList.Len()
}

// Metrics returns cache statistics
type DNSCacheMetrics struct {
	Size      int     // Current number of entries
	MaxSize   int     // Maximum capacity
	Hits      uint64  // Number of cache hits
	Misses    uint64  // Number of cache misses
	Evictions uint64  // Number of evictions due to size limit
	HitRate   float64 // Hit rate percentage
}

// GetMetrics returns current cache statistics.
// Атомарные счётчики читаются без мьютекса — lock-free для метрик.
func (c *LRUDNSCache) GetMetrics() DNSCacheMetrics {
	hits := c.hits.Load()
	misses := c.misses.Load()
	evictions := c.evictions.Load()

	totalRequests := hits + misses
	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hits) / float64(totalRequests) * 100.0
	}

	// Размер требует lock (list.Len() не атомарная операция)
	c.mutex.Lock()
	size := c.lruList.Len()
	c.mutex.Unlock()

	return DNSCacheMetrics{
		Size:      size,
		MaxSize:   c.maxSize,
		Hits:      hits,
		Misses:    misses,
		Evictions: evictions,
		HitRate:   hitRate,
	}
}

// CleanupExpired removes all expired entries from cache
func (c *LRUDNSCache) CleanupExpired() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	removed := 0
	var toRemove []*list.Element

	// Collect expired entries
	for elem := c.lruList.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*lruCacheEntry)
		if entry.value.Expired() {
			toRemove = append(toRemove, elem)
		}
	}

	// Remove them
	for _, elem := range toRemove {
		entry := elem.Value.(*lruCacheEntry)
		c.lruList.Remove(elem)
		delete(c.cache, entry.key)
		removed++
	}

	return removed
}

// StartCleanupLoop starts a background goroutine that periodically removes expired entries
func (c *LRUDNSCache) StartCleanupLoop(interval time.Duration) chan struct{} {
	stop := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.CleanupExpired()
			case <-stop:
				return
			}
		}
	}()

	return stop
}
