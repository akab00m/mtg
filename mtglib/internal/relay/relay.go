package relay

import (
	"context"
	"errors"
	"io"

	"github.com/9seconds/mtg/v2/essentials"
)

// copyRelay копирует данные между соединениями через io.CopyBuffer.
// splice() (zero-copy) невозможен: все соединения обёрнуты в obfuscated2.Conn
// и faketls.Conn для AES-CTR шифрования — данные ДОЛЖНЫ проходить через userspace.
func copyRelay(dst, src essentials.Conn, buf []byte) (int64, error) {
	return io.CopyBuffer(dst, src, buf)
}

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

	// Graceful shutdown: при отмене контекста корректно закрываем соединения
	// Это стандартный TCP behaviour (FIN/RST) — не создаёт fingerprint
	go func() {
		<-ctx.Done()
		// Graceful close: отправляем FIN (CloseWrite) чтобы pump функции
		// увидели EOF и завершились. Полный Close() выполнят defer'ы выше.
		telegramConn.CloseWrite() //nolint: errcheck
		clientConn.CloseWrite()   //nolint: errcheck
	}()

	closeChan := make(chan struct{})

	// Оптимизация TCP для всех соединений — критично для множества мелких пакетов
	setTCPNoDelay(telegramConn)
	setTCPNoDelay(clientConn)

	// TCP_WINDOW_CLAMP: ограничение receive window для соединения к Telegram.
	// 128KB (131072) — значение из оригинального MTProxy (DEFAULT_WINDOW_CLAMP).
	// Предотвращает buffer bloat: без clamp TCP window растёт неограниченно,
	// и Telegram может отправить больше данных, чем proxy успевает relay.
	// Применяется ТОЛЬКО к telegramConn — клиентское соединение не ограничиваем.
	setTCPWindowClamp(telegramConn, 131072) //nolint: gomnd

	// TCP_USER_TIMEOUT: закрыть соединение если нет ACK 30 секунд.
	// Без этого мёртвые соединения висят до TCP retransmit timeout (~15 мин),
	// расходуя file descriptors и goroutine worker slots.
	setTCPUserTimeout(telegramConn, 30000)
	setTCPUserTimeout(clientConn, 30000)

	// Upload: client -> telegram (обычный приоритет)
	go func() {
		defer close(closeChan)
		pump(log, telegramConn, clientConn, "client -> telegram", dirUpload)
	}()

	// Download: telegram -> client (высокий приоритет)
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

	// TCP оптимизации для обоих направлений (много мелких пакетов)
	// TCP_NODELAY уже установлен в Relay(), здесь дополнительные настройки

	if dir == dirDownload {
		// Download: telegram -> client (приоритетное направление)

		// TCP_QUICKACK - немедленные ACK для снижения latency
		setTCPQuickACK(src)
		setTCPQuickACK(dst)

		// TCP_NOTSENT_LOWAT — порог неотправленных данных для wake-up epoll.
		// 128KB = значение Cloudflare в production (blog 2022).
		// Меньшие значения дают больше write events и overhead.
		setTCPNotSentLowat(dst, 131072)
	} else {
		// Upload: client -> telegram
		// Также применяем QUICKACK для снижения latency в обоих направлениях
		setTCPQuickACK(src)
		setTCPQuickACK(dst)
	}

	n, err := copyRelay(dst, src, *copyBuffer)

	switch {
	case err == nil:
		log.Printf("%s has been finished", directionStr)
	case errors.Is(err, io.EOF):
		log.Printf("%s has been finished because of EOF. Written %d bytes", directionStr, n)
	default:
		log.Printf("%s has been finished (written %d bytes): %v", directionStr, n, err)
	}
}
