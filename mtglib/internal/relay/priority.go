package relay

import (
	"sync/atomic"
	"time"
)

// Priority определяет приоритет потока данных.
// Используется для внутренней приоритизации CPU/memory БЕЗ изменения wire-level pattern.
type Priority int

const (
	// PriorityNormal — стандартный приоритет (upload: client -> telegram)
	PriorityNormal Priority = iota

	// PriorityHigh — высокий приоритет (download: telegram -> client)
	// Download критичнее для UX — пользователь ждёт загрузку медиа
	PriorityHigh
)

// StreamStats отслеживает статистику потока для мониторинга.
// Все операции атомарные — безопасно для concurrent использования.
type StreamStats struct {
	// Счётчики байтов
	bytesTransferred atomic.Int64

	// Время начала передачи (для расчёта throughput)
	startTime time.Time

	// Текущий throughput (bytes/sec), обновляется периодически
	currentThroughput atomic.Int64

	// Пиковый throughput за сессию
	peakThroughput atomic.Int64

	// Количество splice/copy операций
	operationCount atomic.Int64
}

// NewStreamStats создаёт новый трекер статистики.
func NewStreamStats() *StreamStats {
	return &StreamStats{
		startTime: time.Now(),
	}
}

// AddBytes добавляет переданные байты и обновляет throughput.
func (s *StreamStats) AddBytes(n int64) {
	s.bytesTransferred.Add(n)
	s.operationCount.Add(1)

	// Пересчитываем throughput каждые 100 операций (амортизация)
	if s.operationCount.Load()%100 == 0 {
		s.updateThroughput()
	}
}

// updateThroughput пересчитывает текущий throughput.
func (s *StreamStats) updateThroughput() {
	elapsed := time.Since(s.startTime).Seconds()
	if elapsed > 0 {
		throughput := int64(float64(s.bytesTransferred.Load()) / elapsed)
		s.currentThroughput.Store(throughput)

		// Обновляем пик если текущий выше
		for {
			peak := s.peakThroughput.Load()
			if throughput <= peak {
				break
			}
			if s.peakThroughput.CompareAndSwap(peak, throughput) {
				break
			}
		}
	}
}

// GetThroughput возвращает текущий throughput в bytes/sec.
func (s *StreamStats) GetThroughput() int64 {
	return s.currentThroughput.Load()
}

// GetTotalBytes возвращает общее количество переданных байт.
func (s *StreamStats) GetTotalBytes() int64 {
	return s.bytesTransferred.Load()
}

// priorityHints — placeholder для приоритизации потоков данных.
// Реальная приоритизация download выполняется на уровне TCP:
// - TCP_QUICKACK — немедленные ACK для download направления
// - TCP_NOTSENT_LOWAT — быстрое уведомление о возможности записи
// - TCP_NODELAY — отсутствие Nagle buffering
//
// ВАЖНО: runtime.LockOSThread() НЕ используется — он не приоритизирует goroutine,
// а только привязывает к OS thread, что при тысячах connections создаёт O(N)
// OS threads и деградирует Go scheduler (каждый заблокированный thread потребляет
// ~8KB stack + scheduling overhead).
type priorityHints struct{}

// ApplyHighPriority — no-op. Приоритизация выполняется через TCP socket options.
func (priorityHints) ApplyHighPriority() {
	// Намеренно оставлено пустым.
	// TCP_QUICKACK и TCP_NOTSENT_LOWAT настраиваются в pump() перед relay.
}

// ReleaseHighPriority — no-op.
func (priorityHints) ReleaseHighPriority() {
	// Намеренно оставлено пустым.
}

// PriorityHints — синглтон для применения runtime hints.
var PriorityHints = priorityHints{}
