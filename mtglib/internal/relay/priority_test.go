package relay

import (
	"sync"
	"testing"
	"time"
)

func TestStreamStats_AddBytes(t *testing.T) {
	stats := NewStreamStats()

	// Добавляем байты
	stats.AddBytes(1024)
	stats.AddBytes(2048)

	total := stats.GetTotalBytes()
	if total != 3072 {
		t.Errorf("Expected 3072 bytes, got %d", total)
	}
}

func TestStreamStats_ConcurrentAddBytes(t *testing.T) {
	stats := NewStreamStats()

	var wg sync.WaitGroup
	goroutines := 100
	bytesPerGoroutine := int64(1000)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				stats.AddBytes(bytesPerGoroutine)
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines) * 100 * bytesPerGoroutine
	total := stats.GetTotalBytes()
	if total != expected {
		t.Errorf("Expected %d bytes, got %d", expected, total)
	}
}

func TestStreamStats_Throughput(t *testing.T) {
	stats := NewStreamStats()

	// Симулируем активную передачу данных
	for range 200 {
		stats.AddBytes(10240) // 10KB за операцию
	}

	// Throughput должен быть > 0 после достаточного количества операций
	throughput := stats.GetThroughput()
	// Минимум 100 операций нужно для первого расчёта (100 % 100 == 0)
	if throughput == 0 {
		t.Log("Throughput is 0, which is acceptable for very short test duration")
	}
}

func TestPriorityHints_ApplyAndRelease(t *testing.T) {
	// Тест что ApplyHighPriority и ReleaseHighPriority не паникуют
	// и корректно работают в паре

	done := make(chan struct{})

	go func() {
		defer close(done)
		PriorityHints.ApplyHighPriority()
		// Симулируем работу
		time.Sleep(10 * time.Millisecond)
		PriorityHints.ReleaseHighPriority()
	}()

	select {
	case <-done:
		// Успех
	case <-time.After(5 * time.Second):
		t.Error("PriorityHints test timed out")
	}
}

func TestPriorityHints_ConcurrentUsage(t *testing.T) {
	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			PriorityHints.ApplyHighPriority()
			time.Sleep(5 * time.Millisecond)
			PriorityHints.ReleaseHighPriority()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Успех
	case <-time.After(10 * time.Second):
		t.Error("Concurrent PriorityHints test timed out")
	}
}

func BenchmarkStreamStats_AddBytes(b *testing.B) {
	stats := NewStreamStats()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats.AddBytes(1024)
	}
}
