//go:build linux

package relay

import (
	"log"
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
)

// setTCPQuickACK включает TCP_QUICKACK для немедленной отправки ACK.
func setTCPQuickACK(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		log.Printf("[relay] TCP_QUICKACK: SyscallConn failed: %v", err)
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1); err != nil { //nolint: nosnakecase
			log.Printf("[relay] TCP_QUICKACK: setsockopt failed: %v", err)
		}
	})
}

// setTCPNoDelay отключает алгоритм Nagle для немедленной отправки мелких пакетов.
// Критично для Telegram где много коротких сообщений.
func setTCPNoDelay(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	if err := tcpConn.SetNoDelay(true); err != nil {
		log.Printf("[relay] TCP_NODELAY: SetNoDelay failed: %v", err)
	}
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
		log.Printf("[relay] TCP_NOTSENT_LOWAT: SyscallConn failed: %v", err)
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_NOTSENT_LOWAT, threshold); err != nil {
			log.Printf("[relay] TCP_NOTSENT_LOWAT(%d): setsockopt failed: %v", threshold, err)
		}
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
		log.Printf("[relay] TCP_USER_TIMEOUT: SyscallConn failed: %v", err)
		return
	}

	_ = rawConn.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_USER_TIMEOUT, timeoutMs); err != nil {
			log.Printf("[relay] TCP_USER_TIMEOUT(%dms): setsockopt failed: %v", timeoutMs, err)
		}
	})
}


