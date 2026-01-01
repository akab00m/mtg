package network

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestDNSResolver_LookupBoth_CacheOnly проверяет параллельный lookup с кэшем
func TestDNSResolver_LookupBoth_CacheOnly(t *testing.T) {
	cache := NewLRUDNSCache(100)

	// Pre-populate cache with both IPv4 and IPv6 results
	cache.Set("\x00example.com", []string{"1.1.1.1", "1.1.1.2"}, 300)
	cache.Set("\x01example.com", []string{"2001:db8::1", "2001:db8::2"}, 300)

	resolver := &dnsResolver{
		cache:      cache,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	start := time.Now()
	result := resolver.LookupBoth("example.com")
	duration := time.Since(start)

	// Cache hits should be fast (<10ms)
	if duration > 50*time.Millisecond {
		t.Errorf("Cache lookup took too long: %v (expected <50ms)", duration)
	}

	// Should have all 4 IPs
	if len(result) != 4 {
		t.Errorf("Expected 4 results, got %d: %v", len(result), result)
	}

	// IPv4 should come first
	if result[0] != "1.1.1.1" {
		t.Errorf("Expected IPv4 first, got %s", result[0])
	}

	// Check cache metrics
	metrics := cache.GetMetrics()
	if metrics.Hits != 2 {
		t.Errorf("Expected 2 cache hits, got %d", metrics.Hits)
	}
}

// TestDNSResolver_LookupBoth_PartialCache проверяет частичное попадание в кэш
func TestDNSResolver_LookupBoth_PartialCache(t *testing.T) {
	cache := NewLRUDNSCache(100)

	// Only IPv4 in cache
	cache.Set("\x00partial.com", []string{"1.2.3.4"}, 300)

	resolver := &dnsResolver{
		cache:      cache,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		dohServer:  "1.1.1.1", // Will fail but that's ok for this test
	}

	result := resolver.LookupBoth("partial.com")

	// Should at least have IPv4 from cache
	if len(result) < 1 {
		t.Error("Expected at least 1 result from cache")
	}

	if result[0] != "1.2.3.4" {
		t.Errorf("Expected 1.2.3.4, got %s", result[0])
	}

	// Check that cache was hit for A record
	metrics := cache.GetMetrics()
	if metrics.Hits < 1 {
		t.Errorf("Expected at least 1 cache hit, got %d", metrics.Hits)
	}
}

// TestDNSResolver_LookupBoth_Concurrent проверяет конкурентный доступ
func TestDNSResolver_LookupBoth_Concurrent(t *testing.T) {
	cache := NewLRUDNSCache(1000)

	// Pre-populate cache
	cache.Set("\x00concurrent.com", []string{"10.0.0.1"}, 300)
	cache.Set("\x01concurrent.com", []string{"2001:db8::1"}, 300)

	resolver := &dnsResolver{
		dohServer:  "1.1.1.1",
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cache:      cache,
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Simulate concurrent requests
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			result := resolver.LookupBoth("concurrent.com")
			if len(result) != 2 {
				t.Errorf("Expected 2 results, got %d", len(result))
			}
		}()
	}

	wg.Wait()

	// Check that cache worked under concurrent load
	metrics := cache.GetMetrics()
	if metrics.Hits < uint64(numGoroutines*2-10) { // Allow some slack
		t.Errorf("Expected ~%d cache hits, got %d", numGoroutines*2, metrics.Hits)
	}
}

// BenchmarkDNSResolver_LookupBoth_CacheHit бенчмарк для cache hits
func BenchmarkDNSResolver_LookupBoth_CacheHit(b *testing.B) {
	cache := NewLRUDNSCache(1000)
	cache.Set("\x00bench.com", []string{"1.1.1.1"}, 300)
	cache.Set("\x01bench.com", []string{"2001:db8::1"}, 300)

	resolver := &dnsResolver{
		cache:      cache,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resolver.LookupBoth("bench.com")
	}
}

// BenchmarkDNSResolver_Sequential сравнение последовательных вызовов
func BenchmarkDNSResolver_Sequential(b *testing.B) {
	cache := NewLRUDNSCache(1000)
	cache.Set("\x00bench.com", []string{"1.1.1.1"}, 300)
	cache.Set("\x01bench.com", []string{"2001:db8::1"}, 300)

	resolver := &dnsResolver{
		cache:      cache,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resolver.LookupA("bench.com")
		_ = resolver.LookupAAAA("bench.com")
	}
}
