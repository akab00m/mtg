package network

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
)

type defaultDialer struct {
	net.Dialer
	enableTFO bool
}

func (d *defaultDialer) Dial(network, address string) (essentials.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d *defaultDialer) DialContext(ctx context.Context, network, address string) (essentials.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6": //nolint: goconst
	default:
		return nil, fmt.Errorf("unsupported network %s", network)
	}

	// Используем dialer с TFO если включено
	dialer := d.getDialer()

	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("cannot dial to %s: %w", address, err)
	}

	// we do not need to call to end user. End users call us.
	if err := SetServerSocketOptions(conn, 0); err != nil {
		conn.Close()

		return nil, fmt.Errorf("cannot set socket options: %w", err)
	}

	return conn.(essentials.Conn), nil //nolint: forcetypeassert
}

// getDialer возвращает dialer с TFO control если включено.
func (d *defaultDialer) getDialer() *net.Dialer {
	if !d.enableTFO || !IsTFOClientEnabled() {
		return &d.Dialer
	}

	// Создаём копию dialer с TFO control
	return &net.Dialer{
		Timeout:       d.Dialer.Timeout,
		Deadline:      d.Dialer.Deadline,
		LocalAddr:     d.Dialer.LocalAddr,
		FallbackDelay: d.Dialer.FallbackDelay,
		KeepAlive:     d.Dialer.KeepAlive,
		Resolver:      d.Dialer.Resolver,
		Control: func(network, address string, c syscall.RawConn) error {
			// Пытаемся включить TFO, но не фейлим если не получилось
			c.Control(func(fd uintptr) { //nolint: errcheck
				SetTFOOnSocket(int(fd), false, 0)
			})
			return nil
		},
	}
}

// NewDefaultDialer build a new dialer which dials bypassing proxies
// etc.
//
// The most default one you can imagine. But it has tunes TCP
// connections and setups SO_REUSEPORT.
//
// bufferSize is deprecated and ignored. It is kept here for backward
// compatibility.
func NewDefaultDialer(timeout time.Duration, bufferSize int) (Dialer, error) {
	return NewDefaultDialerWithTFO(timeout, bufferSize, false)
}

// NewDefaultDialerWithTFO создаёт dialer с опциональной поддержкой TCP Fast Open.
func NewDefaultDialerWithTFO(timeout time.Duration, bufferSize int, enableTFO bool) (Dialer, error) {
	switch {
	case timeout < 0:
		return nil, fmt.Errorf("timeout %v should be positive number", timeout)
	case timeout == 0:
		timeout = DefaultTimeout
	}

	return &defaultDialer{
		Dialer: net.Dialer{
			Timeout: timeout,
		},
		enableTFO: enableTFO,
	}, nil
}
