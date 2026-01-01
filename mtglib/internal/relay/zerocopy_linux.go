//go:build linux

package relay

import (
	"io"
	"net"
	"syscall"

	"github.com/9seconds/mtg/v2/essentials"
)

// zeroCopyRelay пытается использовать splice() для zero-copy передачи данных.
// Возвращает true если splice успешен, false если нужен fallback.
func zeroCopyRelay(src, dst essentials.Conn) (int64, error) {
	// Получаем raw file descriptors
	srcTCP, srcOk := src.(*net.TCPConn)
	dstTCP, dstOk := dst.(*net.TCPConn)

	if !srcOk || !dstOk {
		// Not TCP connections, use standard copy
		return -1, nil
	}

	srcFile, err := srcTCP.File()
	if err != nil {
		return -1, nil // Fallback to standard copy
	}
	defer srcFile.Close()

	dstFile, err := dstTCP.File()
	if err != nil {
		return -1, nil // Fallback to standard copy
	}
	defer dstFile.Close()

	srcFd := int(srcFile.Fd())
	dstFd := int(dstFile.Fd())

	// Создаём pipe для splice
	var pipeFds [2]int
	if err := syscall.Pipe(pipeFds[:]); err != nil {
		return -1, nil // Fallback to standard copy
	}
	defer syscall.Close(pipeFds[0])
	defer syscall.Close(pipeFds[1])

	// Увеличиваем размер pipe буфера для лучшей производительности
	const pipeSize = 65536 // 64KB
	syscall.Syscall(syscall.SYS_FCNTL, uintptr(pipeFds[0]), syscall.F_SETPIPE_SZ, pipeSize)

	var totalBytes int64

	for {
		// splice: src socket -> pipe
		n1, err := syscall.Splice(srcFd, nil, pipeFds[1], nil, pipeSize, syscall.SPLICE_F_MOVE|syscall.SPLICE_F_NONBLOCK)
		if err != nil {
			if err == syscall.EAGAIN {
				continue
			}
			if n1 == 0 {
				// EOF
				return totalBytes, nil
			}
			return totalBytes, err
		}

		if n1 == 0 {
			// EOF
			return totalBytes, nil
		}

		// splice: pipe -> dst socket
		var written int64
		for written < n1 {
			n2, err := syscall.Splice(pipeFds[0], nil, dstFd, nil, int(n1-written), syscall.SPLICE_F_MOVE|syscall.SPLICE_F_MORE)
			if err != nil {
				if err == syscall.EAGAIN {
					continue
				}
				return totalBytes + written, err
			}
			written += n2
		}

		totalBytes += written
	}
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
