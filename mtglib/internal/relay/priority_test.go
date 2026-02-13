package relay

import (
	"testing"
)

func TestPriorityConstants(t *testing.T) {
	// Проверяем что константы определены корректно
	if PriorityNormal != 0 {
		t.Errorf("Expected PriorityNormal == 0, got %d", PriorityNormal)
	}
	if PriorityHigh != 1 {
		t.Errorf("Expected PriorityHigh == 1, got %d", PriorityHigh)
	}
	if PriorityNormal == PriorityHigh {
		t.Error("PriorityNormal should not equal PriorityHigh")
	}
}
