package relay

import (
	"runtime"
	"sync"
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

// StreamStats отслеживает статистику потока для адаптивного управления буферами.
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

// AdaptiveBuffer управляет размером буфера на основе throughput.
// Безопасная реализация без изменения wire-level pattern.
type AdaptiveBuffer struct {
	mu sync.RWMutex

	// Текущий размер буфера
	currentSize int

	// Границы адаптации
	minSize int
	maxSize int

	// Пороги throughput для адаптации (bytes/sec)
	lowThroughputThreshold  int64 // Ниже этого — уменьшаем буфер
	highThroughputThreshold int64 // Выше этого — увеличиваем буфер

	// Счётчик стабильности (сколько раз throughput был в нужном диапазоне)
	stabilityCounter int
}

// NewAdaptiveBuffer создаёт адаптивный менеджер буферов.
// minSize и maxSize — границы адаптации размера буфера.
func NewAdaptiveBuffer(minSize, maxSize int) *AdaptiveBuffer {
	return &AdaptiveBuffer{
		currentSize: minSize, // Начинаем с минимума (консервативно)
		minSize:     minSize,
		maxSize:     maxSize,

		// Пороги: 1 MB/s (низкий), 10 MB/s (высокий)
		lowThroughputThreshold:  1 * 1024 * 1024,
		highThroughputThreshold: 10 * 1024 * 1024,
	}
}

// GetOptimalSize возвращает оптимальный размер буфера на основе throughput.
// Изменение размера буфера НЕ влияет на wire-level pattern — это внутренняя оптимизация.
func (ab *AdaptiveBuffer) GetOptimalSize(throughput int64) int {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	switch {
	case throughput < ab.lowThroughputThreshold:
		// Низкий throughput — уменьшаем буфер для экономии памяти
		ab.stabilityCounter++
		if ab.stabilityCounter > 5 && ab.currentSize > ab.minSize {
			ab.currentSize = ab.currentSize * 3 / 4 // Уменьшаем на 25%
			if ab.currentSize < ab.minSize {
				ab.currentSize = ab.minSize
			}
			ab.stabilityCounter = 0
		}

	case throughput > ab.highThroughputThreshold:
		// Высокий throughput — увеличиваем буфер для лучшего performance
		ab.stabilityCounter++
		if ab.stabilityCounter > 3 && ab.currentSize < ab.maxSize {
			ab.currentSize = ab.currentSize * 5 / 4 // Увеличиваем на 25%
			if ab.currentSize > ab.maxSize {
				ab.currentSize = ab.maxSize
			}
			ab.stabilityCounter = 0
		}

	default:
		// Средний throughput — сбрасываем счётчик стабильности
		ab.stabilityCounter = 0
	}

	return ab.currentSize
}

// priorityHints применяет runtime hints для приоритетных goroutines.
// ВАЖНО: Это НЕ влияет на wire-level traffic pattern.
type priorityHints struct{}

// ApplyHighPriority применяет hints для высокоприоритетных операций (download).
// Использует только безопасные методы без изменения сетевого трафика.
func (priorityHints) ApplyHighPriority() {
	// Gosched отдаёт CPU другим goroutines, но download goroutine
	// будет запускаться чаще благодаря тому, что она меньше блокируется
	// (TCP_QUICKACK уменьшает ожидание ACK)

	// LockOSThread привязывает goroutine к OS thread
	// Это уменьшает context switch overhead для критичных операций
	// НЕ влияет на wire-level — только на scheduling внутри процесса
	runtime.LockOSThread()
}

// ReleaseHighPriority освобождает ресурсы после завершения приоритетной операции.
func (priorityHints) ReleaseHighPriority() {
	runtime.UnlockOSThread()
}

// PriorityHints — синглтон для применения runtime hints.
var PriorityHints = priorityHints{}
