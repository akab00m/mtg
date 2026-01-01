//go:build !windows
// +build !windows

package network

import (
	"fmt"
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
