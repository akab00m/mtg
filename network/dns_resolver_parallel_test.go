package network

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// mockDNSResolver для тестирования параллелизма
type mockDNSResolver struct {
	*dnsResolver
	aCallCount    uint32
	aaaaCallCount uint32
	queryDelay    time.Duration
}

func (m *mockDNSResolver) LookupA(hostname string) []string {
	atomic.AddUint32(&m.aCallCount, 1)
	if m.queryDelay > 0 {
		time.Sleep(m.queryDelay)
	}
	return []string{"1.2.3.4", "1.2.3.5"}
}

func (m *mockDNSResolver) LookupAAAA(hostname string) []string {
	atomic.AddUint32(&m.aaaaCallCount, 1)
	if m.queryDelay > 0 {
		time.Sleep(m.queryDelay)
	}
	return []string{"2001:db8::1", "2001:db8::2"}
}

func TestDNSResolver_LookupBoth_Parallel(t *testing.T) {
	cache := NewLRUDNSCache(100)
	mock := &mockDNSResolver{
		dnsResolver: &dnsResolver{
			cache: cache,
		},
		queryDelay: 100 * time.Millisecond, // Simulate network delay
	}

	start := time.Now()
	result := mock.dnsResolver.LookupBoth("example.com")
	duration := time.Since(start)

	// Both lookups should be called
	if atomic.LoadUint32(&mock.aCallCount) != 1 {
		t.Errorf("Expected 1 A lookup, got %d", mock.aCallCount)
	}
	if atomic.LoadUint32(&mock.aaaaCallCount) != 1 {
		t.Errorf("Expected 1 AAAA lookup, got %d", mock.aaaaCallCount)
	}

	// Parallel execution should take ~100ms, not ~200ms
	if duration > 150*time.Millisecond {
		t.Errorf("Parallel lookup took too long: %v (expected ~100ms)", duration)
	}

	// Results should be IPv4 first, then IPv6
	expectedLen := 4
	if len(result) != expectedLen {
		t.Errorf("Expected %d results, got %d", expectedLen, len(result))
	}
}

func TestDNSResolver_LookupBoth_CacheUsage(t *testing.T) {
	cache := NewLRUDNSCache(100)
	
	// Pre-populate cache with IPv4 results
	cache.Set("\x00example.com", []string{"1.1.1.1"}, 300)
	
	mock := &mockDNSResolver{
		dnsResolver: &dnsResolver{
			cache: cache,
		},
	}

	mock.dnsResolver.LookupBoth("example.com")

	// A lookup should use cache (0 calls), AAAA should query (1 call)
	if atomic.LoadUint32(&mock.aCallCount) != 0 {
		t.Errorf("Expected 0 A lookups (cache hit), got %d", mock.aCallCount)
	}
	if atomic.LoadUint32(&mock.aaaaCallCount) != 1 {
		t.Errorf("Expected 1 AAAA lookup (cache miss), got %d", mock.aaaaCallCount)
	}
}

func BenchmarkDNSResolver_LookupBoth_Sequential(b *testing.B) {
	cache := NewLRUDNSCache(1000)
	resolver := &dnsResolver{
		cache: cache,
	}

	// Simulate sequential lookups (old behavior)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hostname := "benchmark.com"
		_ = resolver.LookupA(hostname)
		_ = resolver.LookupAAAA(hostname)
	}
}

func BenchmarkDNSResolver_LookupBoth_Parallel(b *testing.B) {
	cache := NewLRUDNSCache(1000)
	resolver := &dnsResolver{
		cache: cache,
	}

	// Use new parallel method
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hostname := "benchmark.com"
		_ = resolver.LookupBoth(hostname)
	}
}

func BenchmarkDNSResolver_LookupBoth_WithMockDelay(b *testing.B) {
	cache := NewLRUDNSCache(1000)
	mock := &mockDNSResolver{
		dnsResolver: &dnsResolver{
			cache: cache,
		},
		queryDelay: 10 * time.Millisecond, // Simulate realistic network delay
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hostname := "benchmark.com"
		_ = mock.dnsResolver.LookupBoth(hostname)
	}
}

// Test concurrent LookupBoth calls (stress test)
func TestDNSResolver_LookupBoth_Concurrent(t *testing.T) {
	cache := NewLRUDNSCache(1000)
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
			_ = resolver.LookupBoth("concurrent-test.com")
		}()
	}

	wg.Wait()

	// No panics = success
	// Check that cache is working correctly under concurrent load
	metrics := cache.GetMetrics()
	if metrics.Size == 0 {
		t.Error("Expected cache to have entries after concurrent lookups")
	}
}
