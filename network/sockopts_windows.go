//go:build windows
// +build windows

package network

import (
	"fmt"
	"syscall"
)

const (
	// Windows socket buffer size
	socketBufferSize = 256 * 1024 // 256 KB
)

func setSocketReuseAddrPort(conn syscall.RawConn) error {
	var err error

	conn.Control(func(fd uintptr) { //nolint: errcheck
		// SO_REUSEADDR on Windows
		err = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		if err != nil {
			err = fmt.Errorf("cannot set SO_REUSEADDR: %w", err)
			return
		}

		// Увеличиваем буферы приёма и отправки
		_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, socketBufferSize) //nolint: errcheck
		_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF, socketBufferSize) //nolint: errcheck

		// TCP_NODELAY
		_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1) //nolint: errcheck
	})

	return err
}
