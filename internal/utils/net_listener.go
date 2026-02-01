package utils

import (
	"fmt"
	"net"

	"github.com/9seconds/mtg/v2/network"
)

type Listener struct {
	net.Listener
	tfoEnabled bool
}

func (l Listener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err //nolint: wrapcheck
	}

	if err := network.SetClientSocketOptions(conn, 0); err != nil {
		conn.Close()

		return nil, fmt.Errorf("cannot set TCP options: %w", err)
	}

	return conn, nil
}

// IsTFOEnabled возвращает true если TFO включен на listener.
func (l Listener) IsTFOEnabled() bool {
	return l.tfoEnabled
}

// NewListener создаёт TCP listener.
// Если enableTFO=true и TFO поддерживается, включает TCP Fast Open.
func NewListener(bindTo string, bufferSize int) (net.Listener, error) {
	return NewListenerWithTFO(bindTo, bufferSize, false)
}

// NewListenerWithTFO создаёт TCP listener с опциональной поддержкой TFO.
func NewListenerWithTFO(bindTo string, bufferSize int, enableTFO bool) (net.Listener, error) {
	var base net.Listener
	var err error
	var tfoActive bool

	if enableTFO {
		config := network.TFOConfig{
			Enabled:  true,
			QueueLen: network.DefaultTFOQueueLen,
			Fallback: true, // Всегда fallback на обычный listener
		}
		base, err = network.ListenTFO("tcp", bindTo, config)
		if err != nil {
			return nil, fmt.Errorf("cannot build TFO listener: %w", err)
		}
		// Проверяем, действительно ли TFO включился
		tfoActive = network.IsTFOServerEnabled()
	} else {
		base, err = net.Listen("tcp", bindTo)
		if err != nil {
			return nil, fmt.Errorf("cannot build a base listener: %w", err)
		}
	}

	return Listener{
		Listener:   base,
		tfoEnabled: tfoActive,
	}, nil
}

