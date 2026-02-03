//go:build linux

package relay

import (
	"io"
	"net"
	"syscall"

	"github.com/9seconds/mtg/v2/essentials"
	"golang.org/x/sys/unix"
)

const (
	// Количество splice операций между refresh TCP_QUICKACK
	// TCP_QUICKACK сбрасывается ядром после каждого delayed ACK
	// Увеличено с 4 до 8 для лучшего throughput при download медиа
	// При 256KB буфере это ~2MB между QUICKACK refresh
	quickackRefreshInterval = 8

	// pipeSize — размер pipe буфера для splice операций.
	// 256KB оптимально для медиа-трафика Telegram.
	pipeSize = 262144 // 256KB
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

	var dstFd int

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

	// Увеличиваем размер pipe буфера для лучшей производительности при медиа
	// 256KB соответствует размеру буфера копирования и уменьшает количество splice вызовов
	_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, uintptr(pipeFds[0]), syscall.F_SETPIPE_SZ, pipeSize)

	var totalBytes int64
	var fdErr error
	var spliceCount int // счётчик для периодического обновления TCP_QUICKACK

	// Используем Read для splice операций чтобы сокет оставался в non-blocking режиме
	readErr := srcRaw.Read(func(fd uintptr) bool {
		for {
			// Периодически обновляем TCP_QUICKACK - он сбрасывается ядром
			// Это критично для мобильных сетей где delayed ACK замедляет download
			spliceCount++
			if spliceCount%quickackRefreshInterval == 0 {
				_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)
			}

			// splice: src socket -> pipe
			n1, err := unix.Splice(int(fd), nil, pipeFds[1], nil, pipeSize, unix.SPLICE_F_MOVE|unix.SPLICE_F_NONBLOCK)
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
				n2, err := unix.Splice(pipeFds[0], nil, dstFd, nil, int(n1-written), unix.SPLICE_F_MOVE|unix.SPLICE_F_MORE)
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
	// Пробуем zero-copy splice для Linux
	// Для TCP соединений это значительно эффективнее стандартного копирования
	n, err := zeroCopyRelay(src, dst)
	if n >= 0 {
		return n, err
	}

	// Fallback на стандартное копирование для non-TCP
	return io.CopyBuffer(dst, src, buf)
}

// copyWithZeroCopyAdaptive — версия с адаптивным буфером и статистикой.
// Отслеживает throughput для оптимизации размера буфера.
// ВАЖНО: НЕ влияет на wire-level pattern — только внутренняя оптимизация.
func copyWithZeroCopyAdaptive(src, dst essentials.Conn, buf []byte, stats *StreamStats) (int64, error) {
	// Пробуем zero-copy splice для Linux
	// При успехе статистика обновляется внутри zeroCopyRelayWithStats
	n, err := zeroCopyRelayWithStats(src, dst, stats)
	if n >= 0 {
		return n, err
	}

	// Fallback на стандартное копирование для non-TCP
	// Используем адаптивный размер буфера на основе текущего throughput
	optimalSize := globalAdaptiveBuffer.GetOptimalSize(stats.GetThroughput())
	if optimalSize < len(buf) {
		buf = buf[:optimalSize]
	}

	total, err := io.CopyBuffer(dst, src, buf)
	if stats != nil {
		stats.AddBytes(total)
	}
	return total, err
}

// zeroCopyRelayWithStats — версия с отслеживанием статистики.
func zeroCopyRelayWithStats(src, dst essentials.Conn, stats *StreamStats) (int64, error) {
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

	var dstFd int

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

	// Адаптивный размер pipe буфера на основе throughput
	currentPipeSize := pipeSize
	if stats != nil {
		throughput := stats.GetThroughput()
		// Для высокого throughput (>10MB/s) увеличиваем буфер
		if throughput > 10*1024*1024 {
			currentPipeSize = 512 * 1024 // 512KB
		}
	}
	_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, uintptr(pipeFds[0]), syscall.F_SETPIPE_SZ, uintptr(currentPipeSize))

	var totalBytes int64
	var fdErr error
	var spliceCount int

	readErr := srcRaw.Read(func(fd uintptr) bool {
		for {
			// Периодически обновляем TCP_QUICKACK
			spliceCount++
			if spliceCount%quickackRefreshInterval == 0 {
				_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)
			}

			// splice: src socket -> pipe
			n1, err := unix.Splice(int(fd), nil, pipeFds[1], nil, currentPipeSize, unix.SPLICE_F_MOVE|unix.SPLICE_F_NONBLOCK)
			if err != nil {
				if err == syscall.EAGAIN {
					return false
				}
				fdErr = err
				return true
			}

			if n1 == 0 {
				return true // EOF
			}

			// splice: pipe -> dst socket
			var written int64
			for written < n1 {
				n2, err := unix.Splice(pipeFds[0], nil, dstFd, nil, int(n1-written), unix.SPLICE_F_MOVE|unix.SPLICE_F_MORE)
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

			// Обновляем статистику
			if stats != nil {
				stats.AddBytes(written)
			}
		}
	})

	if readErr != nil {
		return totalBytes, readErr
	}

	return totalBytes, fdErr
}
