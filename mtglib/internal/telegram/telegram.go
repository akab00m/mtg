package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/9seconds/mtg/v2/essentials"
)

type Telegram struct {
	dialer      Dialer
	preferIP    preferIP
	pool        addressPool
	connPool    *ConnectionPoolManager // Connection pool для переиспользования соединений
	useConnPool bool                   // Включен ли connection pooling
}

// Dial создаёт или переиспользует соединение к DC.
// Если включен connection pooling, соединение будет взято из пула.
// Возвращённое соединение при закрытии вернётся в пул автоматически.
func (t *Telegram) Dial(ctx context.Context, dc int) (essentials.Conn, error) {
	addresses := t.getAddresses(dc)

	// Используем connection pool если включен
	if t.useConnPool && t.connPool != nil {
		conn, err := t.connPool.Get(ctx, dc, addresses)
		if err != nil {
			return nil, fmt.Errorf("cannot get connection from pool for dc %d: %w", dc, err)
		}

		// Оборачиваем для автоматического возврата в пул
		return &PooledConn{
			Conn:    conn,
			dc:      dc,
			manager: t.connPool,
		}, nil
	}

	// Fallback: прямое соединение без pooling
	return t.dialDirect(ctx, addresses, dc)
}

// DialDirect создаёт соединение напрямую, минуя пул.
// Используется когда pooling отключён или для одноразовых соединений.
func (t *Telegram) DialDirect(ctx context.Context, dc int) (essentials.Conn, error) {
	addresses := t.getAddresses(dc)
	return t.dialDirect(ctx, addresses, dc)
}

// getAddresses возвращает адреса для DC согласно IP preference.
func (t *Telegram) getAddresses(dc int) []tgAddr {
	switch t.preferIP {
	case preferIPOnlyIPv4:
		return t.pool.getV4(dc)
	case preferIPOnlyIPv6:
		return t.pool.getV6(dc)
	case preferIPPreferIPv4:
		return append(t.pool.getV4(dc), t.pool.getV6(dc)...)
	case preferIPPreferIPv6:
		return append(t.pool.getV6(dc), t.pool.getV4(dc)...)
	default:
		return t.pool.getV4(dc)
	}
}

// dialDirect выполняет непосредственное подключение к DC.
func (t *Telegram) dialDirect(ctx context.Context, addresses []tgAddr, dc int) (essentials.Conn, error) {
	var conn essentials.Conn
	err := errNoAddresses

	for _, v := range addresses {
		conn, err = t.dialer.DialContext(ctx, v.network, v.address)
		if err == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("cannot dial to %d dc: %w", dc, err)
}

func (t *Telegram) IsKnownDC(dc int) bool {
	return t.pool.isValidDC(dc)
}

func (t *Telegram) GetFallbackDC() int {
	return t.pool.getRandomDC()
}

// GetFallbackDCExcluding returns a random DC excluding the specified one.
// Used when a specific DC is unavailable.
func (t *Telegram) GetFallbackDCExcluding(exclude int) int {
	return t.pool.getRandomDCExcluding(exclude)
}

// Close закрывает все пулы соединений.
func (t *Telegram) Close() error {
	if t.connPool != nil {
		return t.connPool.Close()
	}
	return nil
}

// PoolStats возвращает статистику пулов соединений.
// Возвращает nil если pooling отключен.
func (t *Telegram) PoolStats() []PoolStats {
	if t.connPool == nil {
		return nil
	}
	return t.connPool.AllStats()
}

// TelegramOption — опция для конфигурации Telegram.
type TelegramOption func(*Telegram)

// WithConnectionPool включает connection pooling.
func WithConnectionPool(config PoolConfig) TelegramOption {
	return func(t *Telegram) {
		t.connPool = NewConnectionPoolManager(t.dialer, config)
		t.useConnPool = true
	}
}

// WithoutConnectionPool отключает connection pooling (по умолчанию).
func WithoutConnectionPool() TelegramOption {
	return func(t *Telegram) {
		t.useConnPool = false
		t.connPool = nil
	}
}

func New(dialer Dialer, ipPreference string, useTestDCs bool, opts ...TelegramOption) (*Telegram, error) {
	var pref preferIP

	switch strings.ToLower(ipPreference) {
	case "prefer-ipv4":
		pref = preferIPPreferIPv4
	case "prefer-ipv6":
		pref = preferIPPreferIPv6
	case "only-ipv4":
		pref = preferIPOnlyIPv4
	case "only-ipv6":
		pref = preferIPOnlyIPv6
	default:
		return nil, fmt.Errorf("unknown ip preference %s", ipPreference)
	}

	pool := addressPool{
		v4: productionV4Addresses,
		v6: productionV6Addresses,
	}
	if useTestDCs {
		pool.v4 = testV4Addresses
		pool.v6 = testV6Addresses
	}

	tg := &Telegram{
		dialer:      dialer,
		preferIP:    pref,
		pool:        pool,
		useConnPool: false, // По умолчанию выключен
	}

	// Применяем опции
	for _, opt := range opts {
		opt(tg)
	}

	return tg, nil
}
