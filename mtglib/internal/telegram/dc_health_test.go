package telegram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDCHealthChecker_Unit тестирует логику без реальных сетевых вызовов
func TestDCHealthChecker_Unit(t *testing.T) {
	t.Run("NewDCHealthChecker", func(t *testing.T) {
		pool := &addressPool{
			v4: productionV4Addresses,
			v6: productionV6Addresses,
		}

		checker := NewDCHealthChecker(pool, 5*time.Second)
		assert.NotNil(t, checker)
		assert.NotNil(t, checker.health)
		assert.NotNil(t, checker.stopCh)
	})

	t.Run("IsAvailable_BeforeCheck", func(t *testing.T) {
		pool := &addressPool{
			v4: productionV4Addresses,
			v6: productionV6Addresses,
		}

		checker := NewDCHealthChecker(pool, 5*time.Second)

		// Before any check, should assume available
		assert.True(t, checker.IsAvailable(1))
		assert.True(t, checker.IsAvailable(5))
		assert.True(t, checker.IsAvailable(99)) // Unknown DC - assume available
	})

	t.Run("GetHealth_BeforeCheck", func(t *testing.T) {
		pool := &addressPool{
			v4: productionV4Addresses,
			v6: productionV6Addresses,
		}

		checker := NewDCHealthChecker(pool, 5*time.Second)

		// Before any check, GetHealth returns nil
		assert.Nil(t, checker.GetHealth(1))
		assert.Nil(t, checker.GetHealth(99))
	})

	t.Run("GetBestDC_BeforeCheck", func(t *testing.T) {
		pool := &addressPool{
			v4: productionV4Addresses,
			v6: productionV6Addresses,
		}

		checker := NewDCHealthChecker(pool, 5*time.Second)

		// Before any check, returns 0
		assert.Equal(t, 0, checker.GetBestDC())
	})

	t.Run("GetAllHealth_BeforeCheck", func(t *testing.T) {
		pool := &addressPool{
			v4: productionV4Addresses,
			v6: productionV6Addresses,
		}

		checker := NewDCHealthChecker(pool, 5*time.Second)

		// Before any check, returns empty
		assert.Empty(t, checker.GetAllHealth())
	})

	t.Run("ManualHealthUpdate", func(t *testing.T) {
		pool := &addressPool{
			v4: productionV4Addresses,
			v6: productionV6Addresses,
		}

		checker := NewDCHealthChecker(pool, 5*time.Second)

		// Manually set health data (simulating check results)
		checker.mu.Lock()
		checker.health[1] = &DCHealth{
			DC:          1,
			Available:   true,
			Latency:     50 * time.Millisecond,
			LastChecked: time.Now(),
			Failures:    0,
		}
		checker.health[2] = &DCHealth{
			DC:          2,
			Available:   true,
			Latency:     30 * time.Millisecond, // Better latency
			LastChecked: time.Now(),
			Failures:    0,
		}
		checker.health[3] = &DCHealth{
			DC:          3,
			Available:   false,
			Latency:     0,
			LastChecked: time.Now(),
			Failures:    5,
		}
		checker.mu.Unlock()

		// Test IsAvailable
		assert.True(t, checker.IsAvailable(1))
		assert.True(t, checker.IsAvailable(2))
		assert.False(t, checker.IsAvailable(3))

		// Test GetBestDC - should return DC 2 (lowest latency)
		assert.Equal(t, 2, checker.GetBestDC())

		// Test GetHealth
		health1 := checker.GetHealth(1)
		assert.NotNil(t, health1)
		assert.Equal(t, 1, health1.DC)
		assert.True(t, health1.Available)

		health3 := checker.GetHealth(3)
		assert.NotNil(t, health3)
		assert.False(t, health3.Available)
		assert.Equal(t, 5, health3.Failures)

		// Test GetAllHealth
		allHealth := checker.GetAllHealth()
		assert.Len(t, allHealth, 3)
	})

	t.Run("StartStop_NoBlock", func(t *testing.T) {
		pool := &addressPool{
			v4: [][]tgAddr{}, // Empty pool - no network calls
			v6: [][]tgAddr{},
		}

		checker := NewDCHealthChecker(pool, 100*time.Millisecond)

		// Start with long interval
		checker.Start(1 * time.Hour)

		// Stop should not block
		done := make(chan struct{})
		go func() {
			checker.Stop()
			close(done)
		}()

		select {
		case <-done:
			// OK
		case <-time.After(1 * time.Second):
			t.Fatal("Stop() blocked for too long")
		}
	})
}
