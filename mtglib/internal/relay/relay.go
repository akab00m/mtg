package relay

import (
	"context"
	"errors"
	"io"

	"github.com/9seconds/mtg/v2/essentials"
)

// Направление передачи данных
type direction int

const (
	dirUpload   direction = iota // client -> telegram
	dirDownload                  // telegram -> client (приоритетное)
)

// Глобальный адаптивный буфер для управления размером буферов
// Безопасно для concurrent использования
var globalAdaptiveBuffer = NewAdaptiveBuffer(
	64*1024,  // Min: 64KB
	512*1024, // Max: 512KB
)

func Relay(ctx context.Context, log Logger, telegramConn, clientConn essentials.Conn) {
	defer telegramConn.Close()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Graceful shutdown: при отмене контекста корректно закрываем соединения
	// Это стандартный TCP behaviour (FIN/RST) — не создаёт fingerprint
	go func() {
		<-ctx.Done()
		// Graceful close: сначала отключаем запись (FIN), потом закрываем
		telegramConn.CloseWrite() //nolint: errcheck
		clientConn.CloseWrite()   //nolint: errcheck
		telegramConn.Close()
		clientConn.Close()
	}()

	closeChan := make(chan struct{})

	// Оптимизация TCP для всех соединений - критично для множества мелких пакетов
	setTCPNoDelay(telegramConn) // Upload: client -> telegram
	setTCPNoDelay(clientConn)   // Download: telegram -> client

	// Создаём статистику для адаптивного управления буферами
	downloadStats := NewStreamStats()
	uploadStats := NewStreamStats()

	// Upload: client -> telegram (обычный приоритет)
	go func() {
		defer close(closeChan)
		pump(log, telegramConn, clientConn, "client -> telegram", dirUpload, uploadStats)
	}()

	// Download: telegram -> client (высокий приоритет)
	// Для download настраиваем TCP для минимальной latency
	setTCPQuickACK(clientConn) // Немедленные ACK

	pump(log, clientConn, telegramConn, "telegram -> client", dirDownload, downloadStats)

	<-closeChan
}

func pump(log Logger, src, dst essentials.Conn, directionStr string, dir direction, stats *StreamStats) {
	defer src.CloseRead()  //nolint: errcheck
	defer dst.CloseWrite() //nolint: errcheck

	copyBuffer := acquireCopyBuffer()
	defer releaseCopyBuffer(copyBuffer)

	// Применяем приоритетные hints для download направления
	// ВАЖНО: Это влияет только на внутренний scheduling, НЕ на wire-level pattern
	if dir == dirDownload {
		PriorityHints.ApplyHighPriority()
		defer PriorityHints.ReleaseHighPriority()
	}

	// Оптимизации TCP для обоих направлений (много мелких пакетов)
	// TCP_NODELAY уже установлен в Relay(), здесь дополнительные настройки

	if dir == dirDownload {
		// Download: telegram -> client (приоритетное направление)

		// TCP_QUICKACK - немедленные ACK для снижения latency
		setTCPQuickACK(src)
		setTCPQuickACK(dst)

		// TCP_NOTSENT_LOWAT - 16KB для лучшего throughput при download медиа
		// Было 4KB - слишком агрессивно для больших файлов
		setTCPNotSentLowat(dst, 16384)
	} else {
		// Upload: client -> telegram
		// Также применяем QUICKACK для снижения latency в обоих направлениях
		setTCPQuickACK(src)
		setTCPQuickACK(dst)
	}

	// Try zero-copy first (Linux splice), fallback to standard copy
	// Передаём статистику для адаптивного управления
	n, err := copyWithZeroCopyAdaptive(src, dst, *copyBuffer, stats)

	switch {
	case err == nil:
		log.Printf("%s has been finished", directionStr)
	case errors.Is(err, io.EOF):
		log.Printf("%s has been finished because of EOF. Written %d bytes", directionStr, n)
	default:
		log.Printf("%s has been finished (written %d bytes): %v", directionStr, n, err)
	}
}
