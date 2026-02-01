//go:build !windows
// +build !windows

package relay

import (
	"net"

	"golang.org/x/sys/unix"
)

// TCP socket options константы для Linux
// Эти значения не экспортированы в golang.org/x/sys/unix
const (
	// TCP_NOTSENT_LOWAT - уведомляет приложение когда в буфере
	// отправки осталось меньше threshold байт.
	// Доступен с kernel 3.12
	TCP_NOTSENT_LOWAT = 25

	// TCP_USER_TIMEOUT - таймаут для обнаружения мертвых соединений.
	// Соединение закрывается если нет ACK в течение указанного времени.
	// Доступен с kernel 2.6.37
	TCP_USER_TIMEOUT = 18
)

// setTCPCork включает/выключает TCP_CORK для batching пакетов.
// TCP_CORK заставляет ядро накапливать данные и отправлять их большими пакетами.
func setTCPCork(conn net.Conn, cork bool) error {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return nil //nolint: nilerr
	}

	value := 0
	if cork {
		value = 1
	}

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_CORK, value) //nolint: nosnakecase,errcheck
	})

	return nil
}

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

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1) //nolint: nosnakecase,errcheck
	})
}

// setTCPNoDelay отключает алгоритм Nagle для немедленной отправки мелких пакетов.
// Критично для Telegram где много коротких сообщений.
func setTCPNoDelay(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	// SetNoDelay отключает buffering мелких пакетов
	_ = tcpConn.SetNoDelay(true)
}

// setTCPNotSentLowat устанавливает TCP_NOTSENT_LOWAT для снижения latency.
// Уведомляет приложение когда в буфере отправки осталось меньше threshold байт.
// Это позволяет быстрее реагировать и поддерживать низкую задержку.
func setTCPNotSentLowat(conn net.Conn, threshold int) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_NOTSENT_LOWAT, threshold) //nolint: errcheck
	})
}

// setTCPMaxSegSize устанавливает максимальный размер TCP сегмента.
// Помогает обойти MTU проблемы на некоторых провайдерах.
func setTCPMaxSegSize(conn net.Conn, mss int) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG, mss) //nolint: errcheck
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

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_USER_TIMEOUT, timeoutMs) //nolint: errcheck
	})
}

// configureTCPKeepalive настраивает TCP keepalive для быстрого обнаружения мертвых соединений.
func configureTCPKeepalive(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	// Включаем keepalive
	_ = tcpConn.SetKeepAlive(true)

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		// Интервал между keepalive пакетами: 10 секунд
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPINTVL, 10) //nolint: errcheck
		// Время до первого keepalive: 30 секунд
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPIDLE, 30) //nolint: errcheck
		// Количество попыток: 3
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPCNT, 3) //nolint: errcheck
	})
}
