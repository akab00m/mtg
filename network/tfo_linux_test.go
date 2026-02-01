//go:build linux
// +build linux

package network

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTFOSupported(t *testing.T) {
	// На Linux с современным ядром TFO обычно поддерживается
	supported := IsTFOSupported()
	mode := GetTFOMode()

	t.Logf("TFO supported: %v, mode: %d", supported, mode)

	// Проверяем консистентность
	if supported {
		assert.Greater(t, mode, TFOModeDisabled)
	} else {
		assert.Equal(t, TFOModeDisabled, mode)
	}
}

func TestTFOServerEnabled(t *testing.T) {
	mode := GetTFOMode()
	serverEnabled := IsTFOServerEnabled()

	switch mode {
	case TFOModeServerOnly, TFOModeClientServer:
		assert.True(t, serverEnabled)
	default:
		assert.False(t, serverEnabled)
	}
}

func TestTFOClientEnabled(t *testing.T) {
	mode := GetTFOMode()
	clientEnabled := IsTFOClientEnabled()

	switch mode {
	case TFOModeClientOnly, TFOModeClientServer:
		assert.True(t, clientEnabled)
	default:
		assert.False(t, clientEnabled)
	}
}

func TestDefaultTFOConfig(t *testing.T) {
	config := DefaultTFOConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, DefaultTFOQueueLen, config.QueueLen)
	assert.True(t, config.Fallback)
}

func TestListenTFO_Disabled(t *testing.T) {
	config := TFOConfig{
		Enabled:  false,
		Fallback: true,
	}

	listener, err := ListenTFO("tcp", "127.0.0.1:0", config)
	require.NoError(t, err)
	defer listener.Close()

	assert.NotNil(t, listener)
}

func TestListenTFO_WithFallback(t *testing.T) {
	config := TFOConfig{
		Enabled:  true,
		QueueLen: 128,
		Fallback: true,
	}

	listener, err := ListenTFO("tcp", "127.0.0.1:0", config)
	require.NoError(t, err)
	defer listener.Close()

	assert.NotNil(t, listener)
	t.Logf("Listener address: %s", listener.Addr())
}

func TestDialerTFO_Basic(t *testing.T) {
	// Создаём тестовый сервер
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	serverAddr := listener.Addr().String()

	// Принимаем соединения в горутине
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	// Создаём TFO dialer
	baseDialer := &net.Dialer{Timeout: 5 * time.Second}
	tfoDialer := NewDialerTFO(baseDialer, true)

	// Пробуем соединиться
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := tfoDialer.DialContext(ctx, "tcp", serverAddr)
	require.NoError(t, err)
	defer conn.Close()

	assert.NotNil(t, conn)
}

func TestDialerTFO_Disabled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	// TFO отключен
	baseDialer := &net.Dialer{Timeout: 5 * time.Second}
	tfoDialer := NewDialerTFO(baseDialer, false)

	conn, err := tfoDialer.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	assert.NotNil(t, conn)
}

func TestNewDefaultDialerWithTFO(t *testing.T) {
	// С TFO
	dialerTFO, err := NewDefaultDialerWithTFO(5*time.Second, 0, true)
	require.NoError(t, err)
	assert.NotNil(t, dialerTFO)

	// Без TFO
	dialerNoTFO, err := NewDefaultDialerWithTFO(5*time.Second, 0, false)
	require.NoError(t, err)
	assert.NotNil(t, dialerNoTFO)
}
