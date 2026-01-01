//go:build linux

package relay

import (
	"io"
	"net"
	"syscall"

	"github.com/9seconds/mtg/v2/essentials"
)

// zeroCopyRelay использует splice() для zero-copy передачи данных через RawConn.
// Возвращает -1 если нужен fallback на стандартный copy.
func zeroCopyRelay(src, dst essentials.Conn) (int64, error) {
	// Получаем TCP connections
	srcTCP, srcOk := src.(*net.TCPConn)
	dstTCP, dstOk := dst.(*net.TCPConn)

	if !srcOk || !dstOk {
		return -1, nil // Not TCP, use fallback
	}

	// Получаем SyscallConn для доступа к fd без дублирования
	srcRaw, err := srcTCP.SyscallConn()
	if err != nil {
		return -1, nil
	}

	dstRaw, err := dstTCP.SyscallConn()
	if err != nil {
		return -1, nil
	}

	var srcFd, dstFd int

	// Извлекаем fd из source
	if err := srcRaw.Control(func(fd uintptr) {
		srcFd = int(fd)
	}); err != nil {
		return -1, nil
	}

	// Извлекаем fd из destination
	if err := dstRaw.Control(func(fd uintptr) {
		dstFd = int(fd)
	}); err != nil {
		return -1, nil
	}

	// Создаём pipe для splice
	var pipeFds [2]int
	if err := syscall.Pipe(pipeFds[:]); err != nil {
		return -1, nil
	}
	defer syscall.Close(pipeFds[0])
	defer syscall.Close(pipeFds[1])

	// Увеличиваем размер pipe буфера для лучшей производительности
	const pipeSize = 65536 // 64KB
	_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, uintptr(pipeFds[0]), syscall.F_SETPIPE_SZ, pipeSize)

	var totalBytes int64
	var fdErr error

	// Используем Read для splice операций чтобы сокет оставался в non-blocking режиме
	readErr := srcRaw.Read(func(fd uintptr) bool {
		for {
			// splice: src socket -> pipe
			n1, err := syscall.Splice(int(fd), nil, pipeFds[1], nil, pipeSize, syscall.SPLICE_F_MOVE|syscall.SPLICE_F_NONBLOCK)
			if err != nil {
				if err == syscall.EAGAIN {
					return false // Сигнализируем что нужно ждать готовности
				}
				fdErr = err
				return true
			}

			if n1 == 0 {
				return true // EOF
			}

			// splice: pipe -> dst socket
			// SPLICE_F_MORE говорит ядру что будут еще данные (работает с TCP_CORK)
			var written int64
			for written < n1 {
				n2, err := syscall.Splice(pipeFds[0], nil, dstFd, nil, int(n1-written), syscall.SPLICE_F_MOVE|syscall.SPLICE_F_MORE)
				if err != nil {
					if err == syscall.EAGAIN {
						continue
					}
					fdErr = err
					return true
				}
				written += n2
			}

			totalBytes += written
		}
	})

	if readErr != nil {
		return totalBytes, readErr
	}

	return totalBytes, fdErr
}

// copyWithZeroCopy пытается zero-copy, fallback на io.CopyBuffer
func copyWithZeroCopy(src, dst essentials.Conn, buf []byte) (int64, error) {
	n, err := zeroCopyRelay(src, dst)
	if n >= 0 {
		return n, err
	}

	// Fallback to standard copy
	return io.CopyBuffer(dst, src, buf)
}
