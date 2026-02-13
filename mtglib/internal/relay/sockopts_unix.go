//go:build linux

package relay

import (
	"net"

	"golang.org/x/sys/unix"
)

// TCP socket options константы для Linux.
// Эти значения не экспортированы в golang.org/x/sys/unix.
const (
	// TCP_NOTSENT_LOWAT — уведомляет приложение когда в буфере
	// отправки осталось меньше threshold байт.
	// Доступен с kernel 3.12.
	TCP_NOTSENT_LOWAT = 25

	// TCP_USER_TIMEOUT — таймаут для обнаружения мертвых соединений.
	// Соединение закрывается если нет ACK в течение указанного времени.
	// Доступен с kernel 2.6.37.
	TCP_USER_TIMEOUT = 18
	// TCP_WINDOW_CLAMP — ограничивает размер TCP receive window.
	// Предотвращает рост буферов на стороне Telegram (buffer bloat).
	// Оригинал MTProxy использует DEFAULT_WINDOW_CLAMP = 131072 (128KB).
	// Доступен с kernel 2.4.
	TCP_WINDOW_CLAMP = 10)

// setTCPQuickACK включает TCP_QUICKACK для немедленной отправки ACK.
func setTCPQuickACK(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1) //nolint: errcheck,nosnakecase
	})
}

// setTCPNoDelay отключает алгоритм Nagle для немедленной отправки мелких пакетов.
// Критично для Telegram где много коротких сообщений.
func setTCPNoDelay(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	tcpConn.SetNoDelay(true) //nolint: errcheck
}

// setTCPNotSentLowat устанавливает TCP_NOTSENT_LOWAT для снижения latency.
// Уведомляет приложение когда в буфере отправки осталось меньше threshold байт.
func setTCPNotSentLowat(conn net.Conn, threshold int) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_NOTSENT_LOWAT, threshold) //nolint: errcheck
	})
}

// setTCPUserTimeout устанавливает таймаут для обнаружения мертвых соединений.
// После этого времени без ACK соединение будет закрыто.
func setTCPUserTimeout(conn net.Conn, timeoutMs int) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_USER_TIMEOUT, timeoutMs) //nolint: errcheck
	})
}
// setTCPWindowClamp устанавливает TCP_WINDOW_CLAMP для ограничения receive window.
// Предотвращает buffer bloat на стороне Telegram DC.
// Оригинал MTProxy: DEFAULT_WINDOW_CLAMP = 131072 (128KB).
func setTCPWindowClamp(conn net.Conn, clampBytes int) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_WINDOW_CLAMP, clampBytes) //nolint: errcheck
	})
}

