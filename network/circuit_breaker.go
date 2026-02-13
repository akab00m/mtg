package network

import (
	"context"
	"sync"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
)

// cooldownDialer — упрощённый вариант circuit breaker.
//
// После openThreshold последовательных неудачных попыток ставит прокси
// на "остывание" (cooldown) на reconnectTimeout. По истечении таймаута
// прокси снова доступен.
//
// Аналог reconnect_timeout из оригинального MTProxy.
// Заменяет 3-state machine (Closed/HalfOpen/Opened) на 2 состояния:
// Available и Cooldown.
type cooldownDialer struct {
	Dialer

	mu               sync.Mutex
	failuresCount    uint32
	cooldownUntil    time.Time
	openThreshold    uint32
	reconnectTimeout time.Duration
}

func (c *cooldownDialer) Dial(network, address string) (essentials.Conn, error) {
	return c.DialContext(context.Background(), network, address)
}

func (c *cooldownDialer) DialContext(ctx context.Context,
	network, address string,
) (essentials.Conn, error) {
	c.mu.Lock()
	if !c.cooldownUntil.IsZero() && time.Now().Before(c.cooldownUntil) {
		c.mu.Unlock()

		return nil, ErrCircuitBreakerOpened
	}
	c.mu.Unlock()

	conn, err := c.Dialer.DialContext(ctx, network, address)

	select {
	case <-ctx.Done():
		if conn != nil {
			conn.Close()
		}

		return nil, ctx.Err() //nolint: wrapcheck
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err == nil {
		c.failuresCount = 0
		c.cooldownUntil = time.Time{}

		return conn, nil
	}

	c.failuresCount++

	if c.failuresCount >= c.openThreshold {
		c.cooldownUntil = time.Now().Add(c.reconnectTimeout)
		c.failuresCount = 0
	}

	return conn, err //nolint: wrapcheck
}

func newCooldownDialer(baseDialer Dialer,
	openThreshold uint32, reconnectTimeout time.Duration,
) Dialer {
	return &cooldownDialer{
		Dialer:           baseDialer,
		openThreshold:    openThreshold,
		reconnectTimeout: reconnectTimeout,
	}
}
