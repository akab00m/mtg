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

func Relay(ctx context.Context, log Logger, telegramConn, clientConn essentials.Conn) {
	defer telegramConn.Close()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		telegramConn.Close()
		clientConn.Close()
	}()

	closeChan := make(chan struct{})

	// Оптимизация TCP для всех соединений - критично для множества мелких пакетов
	setTCPNoDelay(telegramConn) // Upload: client -> telegram
	setTCPNoDelay(clientConn)   // Download: telegram -> client

	// Upload: client -> telegram (фоновый)
	go func() {
		defer close(closeChan)
		pump(log, telegramConn, clientConn, "client -> telegram", dirUpload)
	}()

	// Download: telegram -> client (приоритетный)
	// Для download настраиваем TCP для минимальной latency
	setTCPQuickACK(clientConn) // Немедленные ACK

	pump(log, clientConn, telegramConn, "telegram -> client", dirDownload)

	<-closeChan
}

func pump(log Logger, src, dst essentials.Conn, directionStr string, dir direction) {
	defer src.CloseRead()  //nolint: errcheck
	defer dst.CloseWrite() //nolint: errcheck

	copyBuffer := acquireCopyBuffer()
	defer releaseCopyBuffer(copyBuffer)

	// Оптимизации TCP для обоих направлений (много мелких пакетов)
	// TCP_NODELAY уже установлен в Relay(), здесь дополнительные настройки
	
	if dir == dirDownload {
		// Download: telegram -> client (приоритетное направление)
		
		// TCP_QUICKACK - немедленные ACK для снижения latency
		setTCPQuickACK(src)
		setTCPQuickACK(dst)

		// TCP_NOTSENT_LOWAT - 2KB для Telegram (мелкие пакеты 8-16KB)
		// Уведомляет когда буфер почти пуст, улучшает responsiveness
		setTCPNotSentLowat(dst, 2048)
	} else {
		// Upload: client -> telegram
		// Также применяем QUICKACK для снижения latency в обоих направлениях
		setTCPQuickACK(src)
		setTCPQuickACK(dst)
	}

	// Try zero-copy first (Linux splice), fallback to standard copy
	n, err := copyWithZeroCopy(src, dst, *copyBuffer)

	switch {
	case err == nil:
		log.Printf("%s has been finished", directionStr)
	case errors.Is(err, io.EOF):
		log.Printf("%s has been finished because of EOF. Written %d bytes", directionStr, n)
	default:
		log.Printf("%s has been finished (written %d bytes): %v", directionStr, n, err)
	}
}
