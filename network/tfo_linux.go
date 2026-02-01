//go:build linux
// +build linux

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// TCP Fast Open constants
const (
	// TCP_FASTOPEN queue length для listener (количество pending TFO connections)
	// Рекомендуемое значение: 256-1024 для серверов с высокой нагрузкой
	DefaultTFOQueueLen = 256

	// TCP_FASTOPEN_CONNECT для исходящих соединений
	// Значение из Linux kernel (не экспортировано в golang.org/x/sys/unix)
	TCP_FASTOPEN_CONNECT = 30 //nolint:revive,stylecheck
)

// TFO modes (значения из /proc/sys/net/ipv4/tcp_fastopen)
const (
	TFOModeDisabled     = 0 // TFO отключен
	TFOModeClientOnly   = 1 // Только клиент
	TFOModeServerOnly   = 2 // Только сервер
	TFOModeClientServer = 3 // Клиент + сервер
)

var (
	// Кеш результата проверки поддержки TFO
	tfoSupported     bool
	tfoSupportedOnce sync.Once
	tfoMode          int

	// ErrTFONotSupported возвращается когда TFO не поддерживается ядром
	ErrTFONotSupported = errors.New("TCP Fast Open is not supported by kernel")
)

// IsTFOSupported проверяет поддержку TCP Fast Open в ядре.
// Результат кешируется после первого вызова.
func IsTFOSupported() bool {
	tfoSupportedOnce.Do(func() {
		tfoMode = getTFOMode()
		tfoSupported = tfoMode > TFOModeDisabled
	})
	return tfoSupported
}

// GetTFOMode возвращает текущий режим TFO из ядра.
func GetTFOMode() int {
	IsTFOSupported() // Ensure initialization
	return tfoMode
}

// IsTFOServerEnabled проверяет, включен ли TFO для серверного режима.
func IsTFOServerEnabled() bool {
	mode := GetTFOMode()
	return mode == TFOModeServerOnly || mode == TFOModeClientServer
}

// IsTFOClientEnabled проверяет, включен ли TFO для клиентского режима.
func IsTFOClientEnabled() bool {
	mode := GetTFOMode()
	return mode == TFOModeClientOnly || mode == TFOModeClientServer
}

// getTFOMode читает режим TFO из procfs.
func getTFOMode() int {
	data, err := os.ReadFile("/proc/sys/net/ipv4/tcp_fastopen")
	if err != nil {
		return TFOModeDisabled
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return TFOModeDisabled
	}

	// Маскируем только биты режима (0-3)
	return value & 0x3
}

// TFOConfig — конфигурация TCP Fast Open.
type TFOConfig struct {
	// Enabled включает TFO (с автоматическим fallback если не поддерживается)
	Enabled bool

	// QueueLen — размер очереди для pending TFO connections на listener
	QueueLen int

	// Fallback — использовать обычное соединение если TFO не работает
	Fallback bool
}

// DefaultTFOConfig возвращает конфигурацию по умолчанию.
func DefaultTFOConfig() TFOConfig {
	return TFOConfig{
		Enabled:  true,
		QueueLen: DefaultTFOQueueLen,
		Fallback: true,
	}
}

// ListenTFO создаёт TCP listener с поддержкой TCP Fast Open.
// Если TFO не поддерживается и Fallback=true, возвращает обычный listener.
func ListenTFO(network, address string, config TFOConfig) (net.Listener, error) {
	if !config.Enabled {
		return net.Listen(network, address)
	}

	// Проверяем поддержку TFO сервером
	if !IsTFOServerEnabled() {
		if config.Fallback {
			return net.Listen(network, address)
		}
		return nil, ErrTFONotSupported
	}

	queueLen := config.QueueLen
	if queueLen <= 0 {
		queueLen = DefaultTFOQueueLen
	}

	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				// Включаем TCP_FASTOPEN на listener socket
				opErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_FASTOPEN, queueLen)
			})
			if err != nil {
				return err
			}
			if opErr != nil {
				// TFO не удалось включить — не фатально если есть fallback
				if config.Fallback {
					return nil
				}
				return fmt.Errorf("cannot enable TCP_FASTOPEN: %w", opErr)
			}
			return nil
		},
	}

	return lc.Listen(context.Background(), network, address)
}

// DialerTFO — dialer с поддержкой TCP Fast Open для исходящих соединений.
type DialerTFO struct {
	// Dialer — базовый dialer
	Dialer *net.Dialer

	// Enabled включает TFO для исходящих соединений
	Enabled bool

	// Fallback — использовать обычное соединение если TFO не работает
	Fallback bool
}

// NewDialerTFO создаёт dialer с TFO.
func NewDialerTFO(base *net.Dialer, enabled bool) *DialerTFO {
	if base == nil {
		base = &net.Dialer{}
	}
	return &DialerTFO{
		Dialer:   base,
		Enabled:  enabled,
		Fallback: true,
	}
}

// DialContext устанавливает соединение с TFO если возможно.
func (d *DialerTFO) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !d.Enabled || !IsTFOClientEnabled() {
		return d.Dialer.DialContext(ctx, network, address)
	}

	// Создаём dialer с TFO control function
	dialer := &net.Dialer{
		Timeout:       d.Dialer.Timeout,
		Deadline:      d.Dialer.Deadline,
		LocalAddr:     d.Dialer.LocalAddr,
		FallbackDelay: d.Dialer.FallbackDelay,
		KeepAlive:     d.Dialer.KeepAlive,
		Resolver:      d.Dialer.Resolver,
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				// TCP_FASTOPEN_CONNECT включает TFO для connect()
				opErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, TCP_FASTOPEN_CONNECT, 1)
			})
			if err != nil {
				return err
			}
			if opErr != nil && !d.Fallback {
				return fmt.Errorf("cannot enable TCP_FASTOPEN_CONNECT: %w", opErr)
			}
			// Игнорируем ошибку если fallback включен
			return nil
		},
	}

	return dialer.DialContext(ctx, network, address)
}

// Dial устанавливает соединение с TFO если возможно.
func (d *DialerTFO) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

// SetTFOOnSocket включает TFO на существующем socket.
// Для listener — использует TCP_FASTOPEN.
// Для client socket — использует TCP_FASTOPEN_CONNECT.
func SetTFOOnSocket(fd int, isListener bool, queueLen int) error {
	if isListener {
		if queueLen <= 0 {
			queueLen = DefaultTFOQueueLen
		}
		return unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_FASTOPEN, queueLen)
	}
	return unix.SetsockoptInt(fd, unix.IPPROTO_TCP, TCP_FASTOPEN_CONNECT, 1)
}
