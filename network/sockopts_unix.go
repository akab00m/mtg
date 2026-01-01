//go:build !windows
// +build !windows

package network

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	// Увеличенные буферы сокетов для лучшей пропускной способности
	socketBufferSize = 256 * 1024 // 256 KB
)

func setSocketReuseAddrPort(conn syscall.RawConn) error {
	var err error

	conn.Control(func(fd uintptr) { //nolint: errcheck
		// SO_REUSEADDR
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1) //nolint: nosnakecase
		if err != nil {
			err = fmt.Errorf("cannot set SO_REUSEADDR: %w", err)
			return
		}

		// SO_REUSEPORT
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1) //nolint: nosnakecase
		if err != nil {
			err = fmt.Errorf("cannot set SO_REUSEPORT: %w", err)
			return
		}

		// Увеличиваем буферы приёма и отправки
		_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF, socketBufferSize) //nolint: nosnakecase,errcheck
		_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF, socketBufferSize) //nolint: nosnakecase,errcheck

		// TCP_QUICKACK - отключаем задержку ACK для уменьшения latency
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1) //nolint: nosnakecase,errcheck
	})

	return err
}
func SetTCPCork(conn net.Conn, cork bool) error {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil // Не TCP, игнорируем
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("cannot get raw conn for TCP_CORK: %w", err)
	}

	var sysErr error
	value := 0
	if cork {
		value = 1
	}

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		sysErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_CORK, value) //nolint: nosnakecase
	})

	if sysErr != nil {
		return fmt.Errorf("cannot set TCP_CORK=%d: %w", value, sysErr)
	}

	return nil
}

// SetTCPQuickACK включает TCP_QUICKACK для немедленной отправки ACK.
// Полезно для download-направления, чтобы ускорить подтверждения.
func SetTCPQuickACK(conn net.Conn) error {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return nil //nolint: nilerr
	}

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1) //nolint: nosnakecase,errcheck
	})

	return nil
}
