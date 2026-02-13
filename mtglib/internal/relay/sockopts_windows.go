//go:build windows
// +build windows

package relay

import (
	"net"
	"syscall"
	"unsafe"
)

// setTCPQuickACK использует SIO_TCP_SET_ACK_FREQUENCY для Windows.
func setTCPQuickACK(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}

	const SIO_TCP_SET_ACK_FREQUENCY = 0x98000017

	rawConn.Control(func(fd uintptr) { //nolint: errcheck
		var freq uint32 = 1
		var bytesReturned uint32

		_ = syscall.WSAIoctl(
			syscall.Handle(fd),
			SIO_TCP_SET_ACK_FREQUENCY,
			(*byte)(unsafe.Pointer(&freq)),
			uint32(unsafe.Sizeof(freq)),
			nil,
			0,
			&bytesReturned,
			nil,
			0,
		)
	})
}

// setTCPNoDelay отключает алгоритм Nagle.
func setTCPNoDelay(conn net.Conn) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcpConn.SetNoDelay(true)
}

// setTCPNotSentLowat не поддерживается на Windows.
func setTCPNotSentLowat(conn net.Conn, threshold int) {
	// Not supported on Windows
}

// setTCPUserTimeout не поддерживается на Windows.
func setTCPUserTimeout(conn net.Conn, timeoutMs int) {
	// Not supported on Windows
}


