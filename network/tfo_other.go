//go:build !linux
// +build !linux

package network

import (
	"context"
	"errors"
	"net"
)

// TCP Fast Open constants (stubs for non-Linux)
const (
	DefaultTFOQueueLen   = 256
	TCP_FASTOPEN_CONNECT = 30 //nolint:revive,stylecheck
)

// TFO modes
const (
	TFOModeDisabled     = 0
	TFOModeClientOnly   = 1
	TFOModeServerOnly   = 2
	TFOModeClientServer = 3
)

var (
	// ErrTFONotSupported возвращается когда TFO не поддерживается
	ErrTFONotSupported = errors.New("TCP Fast Open is not supported on this platform")
)

// IsTFOSupported всегда возвращает false на не-Linux системах.
func IsTFOSupported() bool {
	return false
}

// GetTFOMode возвращает TFOModeDisabled на не-Linux системах.
func GetTFOMode() int {
	return TFOModeDisabled
}

// IsTFOServerEnabled возвращает false на не-Linux системах.
func IsTFOServerEnabled() bool {
	return false
}

// IsTFOClientEnabled возвращает false на не-Linux системах.
func IsTFOClientEnabled() bool {
	return false
}

// TFOConfig — конфигурация TCP Fast Open.
type TFOConfig struct {
	Enabled  bool
	QueueLen int
	Fallback bool
}

// DefaultTFOConfig возвращает конфигурацию по умолчанию.
func DefaultTFOConfig() TFOConfig {
	return TFOConfig{
		Enabled:  false, // Отключено на не-Linux
		QueueLen: DefaultTFOQueueLen,
		Fallback: true,
	}
}

// ListenTFO на не-Linux просто вызывает net.Listen.
func ListenTFO(network, address string, config TFOConfig) (net.Listener, error) {
	if config.Enabled && !config.Fallback {
		return nil, ErrTFONotSupported
	}
	return net.Listen(network, address)
}

// DialerTFO — dialer без TFO для не-Linux систем.
type DialerTFO struct {
	Dialer   *net.Dialer
	Enabled  bool
	Fallback bool
}

// NewDialerTFO создаёт обычный dialer на не-Linux системах.
func NewDialerTFO(base *net.Dialer, enabled bool) *DialerTFO {
	if base == nil {
		base = &net.Dialer{}
	}
	return &DialerTFO{
		Dialer:   base,
		Enabled:  false, // TFO не поддерживается
		Fallback: true,
	}
}

// DialContext использует обычный dialer.
func (d *DialerTFO) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.Dialer.DialContext(ctx, network, address)
}

// Dial использует обычный dialer.
func (d *DialerTFO) Dial(network, address string) (net.Conn, error) {
	return d.Dialer.DialContext(context.Background(), network, address)
}

// SetTFOOnSocket не делает ничего на не-Linux системах.
func SetTFOOnSocket(fd int, isListener bool, queueLen int) error {
	return ErrTFONotSupported
}
