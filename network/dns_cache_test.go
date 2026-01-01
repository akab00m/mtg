package network

import (
	"testing"
	"time"
)

func TestLRUDNSCache_Basic(t *testing.T) {
	cache := NewLRUDNSCache(3)

	// Test Set and Get
	cache.Set("example.com", []string{"1.2.3.4"}, 300)
	entry := cache.Get("example.com")

	if entry == nil {
		t.Fatal("Expected entry, got nil")
	}
	if len(entry.IPs) != 1 || entry.IPs[0] != "1.2.3.4" {
		t.Errorf("Expected [1.2.3.4], got %v", entry.IPs)
	}
}

func TestLRUDNSCache_Expiration(t *testing.T) {
	cache := NewLRUDNSCache(10)

	// Set entry with 1 second TTL
	cache.Set("short-ttl.com", []string{"1.1.1.1"}, 1)

	// Should exist immediately
	if cache.Get("short-ttl.com") == nil {
		t.Error("Entry should exist immediately after set")
	}

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Should be expired and removed
	if cache.Get("short-ttl.com") != nil {
		t.Error("Entry should be expired and return nil")
	}
}

func TestLRUDNSCache_LRUEviction(t *testing.T) {
	cache := NewLRUDNSCache(3)

	// Fill cache to capacity
	cache.Set("host1", []string{"1.0.0.1"}, 300)
	cache.Set("host2", []string{"1.0.0.2"}, 300)
	cache.Set("host3", []string{"1.0.0.3"}, 300)

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Add one more - should evict oldest (host1)
	cache.Set("host4", []string{"1.0.0.4"}, 300)

	if cache.Size() != 3 {
		t.Errorf("Expected size 3 after eviction, got %d", cache.Size())
	}

	// host1 should be evicted
	if cache.Get("host1") != nil {
		t.Error("host1 should have been evicted")
	}

	// host2, host3, host4 should exist
	if cache.Get("host2") == nil {
		t.Error("host2 should still exist")
	}
	if cache.Get("host3") == nil {
		t.Error("host3 should still exist")
	}
	if cache.Get("host4") == nil {
		t.Error("host4 should exist")
	}
}

func TestLRUDNSCache_UpdateExisting(t *testing.T) {
	cache := NewLRUDNSCache(10)

	// Set initial value
	cache.Set("update-test.com", []string{"1.1.1.1"}, 300)

	// Update with new value
	cache.Set("update-test.com", []string{"2.2.2.2", "3.3.3.3"}, 600)

	// Should have updated value
	entry := cache.Get("update-test.com")
	if entry == nil {
		t.Fatal("Entry should exist")
	}
	if len(entry.IPs) != 2 || entry.IPs[0] != "2.2.2.2" {
		t.Errorf("Expected updated IPs, got %v", entry.IPs)
	}

	// Size should still be 1
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}
}

func TestLRUDNSCache_Metrics(t *testing.T) {
	cache := NewLRUDNSCache(10)

	cache.Set("test1.com", []string{"1.1.1.1"}, 300)
	cache.Set("test2.com", []string{"2.2.2.2"}, 300)

	// Generate some hits and misses
	cache.Get("test1.com") // hit
	cache.Get("test1.com") // hit
	cache.Get("test2.com") // hit
	cache.Get("nonexistent.com") // miss
	cache.Get("nonexistent.com") // miss

	metrics := cache.GetMetrics()

	if metrics.Hits != 3 {
		t.Errorf("Expected 3 hits, got %d", metrics.Hits)
	}
	if metrics.Misses != 2 {
		t.Errorf("Expected 2 misses, got %d", metrics.Misses)
	}
	if metrics.Size != 2 {
		t.Errorf("Expected size 2, got %d", metrics.Size)
	}

	expectedHitRate := 60.0 // 3/(3+2) * 100
	if metrics.HitRate < expectedHitRate-0.1 || metrics.HitRate > expectedHitRate+0.1 {
		t.Errorf("Expected hit rate ~%.1f%%, got %.2f%%", expectedHitRate, metrics.HitRate)
	}
}

func TestLRUDNSCache_CleanupExpired(t *testing.T) {
	cache := NewLRUDNSCache(10)

	// Add entries with different TTLs
	cache.Set("short1", []string{"1.1.1.1"}, 1) // 1 second
	cache.Set("short2", []string{"2.2.2.2"}, 1) // 1 second
	cache.Set("long", []string{"3.3.3.3"}, 300) // 5 minutes

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Wait for short TTLs to expire
	time.Sleep(1100 * time.Millisecond)

	// Manually trigger cleanup
	removed := cache.CleanupExpired()

	if removed != 2 {
		t.Errorf("Expected 2 expired entries removed, got %d", removed)
	}
	if cache.Size() != 1 {
		t.Errorf("Expected size 1 after cleanup, got %d", cache.Size())
	}

	// Long TTL entry should still exist
	if cache.Get("long") == nil {
		t.Error("Long TTL entry should still exist")
	}
}

func TestLRUDNSCache_CleanupLoop(t *testing.T) {
	cache := NewLRUDNSCache(10)

	// Start cleanup loop with short interval
	stop := cache.StartCleanupLoop(100 * time.Millisecond)
	defer close(stop)

	// Add short-lived entry
	cache.Set("auto-cleanup", []string{"1.1.1.1"}, 1)

	// Wait for automatic cleanup
	time.Sleep(1200 * time.Millisecond)

	// Entry should be automatically removed
	if cache.Get("auto-cleanup") != nil {
		t.Error("Entry should be automatically cleaned up")
	}
}

func BenchmarkLRUDNSCache_Get(b *testing.B) {
	cache := NewLRUDNSCache(1000)

	// Populate cache
	for i := 0; i < 100; i++ {
		cache.Set(string(rune('a'+i)), []string{"1.1.1.1"}, 300)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(string(rune('a' + (i % 100))))
	}
}

func BenchmarkLRUDNSCache_Set(b *testing.B) {
	cache := NewLRUDNSCache(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(string(rune(i)), []string{"1.1.1.1"}, 300)
	}
}
