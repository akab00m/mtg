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

	// Upload: client -> telegram (фоновый)
	go func() {
		defer close(closeChan)
		pump(log, telegramConn, clientConn, "client -> telegram", dirUpload)
	}()

	// Download: telegram -> client (приоритетный)
	// Для download устанавливаем TCP_QUICKACK на клиентском соединении
	// чтобы ACK отправлялись быстрее и не задерживали отправку данных
	setTCPQuickACK(clientConn)

	pump(log, clientConn, telegramConn, "telegram -> client", dirDownload)

	<-closeChan
}

func pump(log Logger, src, dst essentials.Conn, directionStr string, dir direction) {
	defer src.CloseRead()  //nolint: errcheck
	defer dst.CloseWrite() //nolint: errcheck

	copyBuffer := acquireCopyBuffer()
	defer releaseCopyBuffer(copyBuffer)

	// Для download применяем оптимизации
	if dir == dirDownload {
		// ОТКЛЮЧАЕМ TCP_CORK для Telegram - он использует много мелких запросов
		// TCP_CORK задерживает отправку, что плохо для latency
		// _ = setTCPCork(dst, true)
		// defer setTCPCork(dst, false) //nolint: errcheck

		// Периодически сбрасываем TCP_QUICKACK на source (telegram)
		// для более быстрой доставки ACK
		setTCPQuickACK(src)
		
		// Также на destination для немедленных ACK
		setTCPQuickACK(dst)

		// TCP_NOTSENT_LOWAT - уменьшаем до 4KB для Telegram (много мелких пакетов)
		// Это улучшает latency для множественных коротких соединений
		setTCPNotSentLowat(dst, 4096)
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
