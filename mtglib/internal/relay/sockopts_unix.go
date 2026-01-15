//go:build !windows
// +build !windows

package relay

import (
	"net"

	"golang.org/x/sys/unix"
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
		// TCP_NOTSENT_LOWAT = 25 на Linux (доступен с kernel 3.12)
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, 25, threshold) //nolint: errcheck
	})
}
