//go:build !linux && !windows

package relay

import "net"

// No-op реализации для macOS/FreeBSD/etc.
// TCP_QUICKACK, TCP_NOTSENT_LOWAT, TCP_USER_TIMEOUT — Linux-specific.

func setTCPQuickACK(conn net.Conn) {}

func setTCPNoDelay(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	_ = tcpConn.SetNoDelay(true)
}

func setTCPNotSentLowat(conn net.Conn, threshold int) {}

func setTCPUserTimeout(conn net.Conn, timeoutMs int) {}

func setTCPWindowClamp(conn net.Conn, clampBytes int) {}
