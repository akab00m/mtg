package telegram

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
)

// Конфигурация по умолчанию для connection pool.
const (
	DefaultPoolSize    = 5  // Размер пула на один DC
	DefaultDialTimeout = 10 * time.Second
	DefaultHealthCheck = 30 * time.Second

	// DefaultIdleTimeout — агрессивный таймаут для избежания проблемы с Telegram.
	// Telegram закрывает idle-соединения через ~30-60 секунд.
	// Используем 20 секунд — достаточно консервативно, чтобы гарантировать
	// что соединения будут закрыты ДО того, как их закроет Telegram.
	// Это устраняет ошибки "connection reset by peer" при переиспользовании.
	DefaultIdleTimeout = 20 * time.Second

	// keepalivePeriod — интервал TCP keepalive для обнаружения мёртвых соединений.
	// 10 секунд — достаточно агрессивно для fast failover.
	keepalivePeriod = 10 * time.Second
)

var (
	ErrPoolClosed    = errors.New("connection pool is closed")
	ErrPoolExhausted = errors.New("connection pool exhausted")
)

// PoolConfig — конфигурация пула соединений.
type PoolConfig struct {
	// MaxIdleConns — максимальное количество idle соединений на DC.
	MaxIdleConns int

	// IdleTimeout — через сколько закрыть idle соединение.
	IdleTimeout time.Duration

	// HealthCheckInterval — интервал проверки соединений.
	HealthCheckInterval time.Duration
}

// DefaultPoolConfig возвращает конфигурацию по умолчанию.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleConns:        DefaultPoolSize,
		IdleTimeout:         DefaultIdleTimeout,
		HealthCheckInterval: DefaultHealthCheck,
	}
}

// pooledConn — соединение в пуле с метаданными.
type pooledConn struct {
	essentials.Conn
	dc         int
	createdAt  time.Time
	lastUsedAt time.Time
	usageCount uint64
}

// maxConnectionAge — максимальный возраст соединения независимо от активности.
// Telegram закрывает соединения через 30-60 сек, используем 30 сек.
const maxConnectionAge = 30 * time.Second

// isHealthy проверяет, можно ли использовать соединение.
// Только проверка idle timeout — TCP-level проверки ненадёжны для MTProxy.
// При broken pipe на handshake — соединение будет закрыто и создано новое.
func (p *pooledConn) isHealthy(idleTimeout time.Duration) bool {
	// Строгая проверка idle timeout
	// Telegram закрывает соединения через 30-60 сек
	if time.Since(p.lastUsedAt) > idleTimeout {
		return false
	}

	// Не переиспользуем если соединение старше maxConnectionAge
	if time.Since(p.createdAt) > maxConnectionAge {
		return false
	}

	return true
}

// markUsed обновляет метаданные использования.
func (p *pooledConn) markUsed() {
	p.lastUsedAt = time.Now()
	p.usageCount++
}

// DCPool — пул соединений для одного DC.
type DCPool struct {
	dc      int
	dialer  Dialer
	addrs   []tgAddr
	config  PoolConfig

	conns  chan *pooledConn
	mu     sync.Mutex
	closed atomic.Bool
	stopCh chan struct{} // сигнал остановки background cleanup

	// Статистика
	stats struct {
		hits      atomic.Uint64
		misses    atomic.Uint64
		created   atomic.Uint64
		closed    atomic.Uint64
		unhealthy atomic.Uint64
	}
}

// NewDCPool создаёт пул для конкретного DC.
func NewDCPool(dc int, dialer Dialer, addrs []tgAddr, config PoolConfig) *DCPool {
	if config.MaxIdleConns <= 0 {
		config.MaxIdleConns = DefaultPoolSize
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = DefaultIdleTimeout
	}

	pool := &DCPool{
		dc:     dc,
		dialer: dialer,
		addrs:  addrs,
		config: config,
		conns:  make(chan *pooledConn, config.MaxIdleConns),
		stopCh: make(chan struct{}),
	}

	// Background cleanup — вычищает stale/expired соединения из пула,
	// чтобы первый клиент после паузы не получил мёртвое соединение.
	go pool.cleanupLoop()

	return pool
}

// Get получает соединение из пула или создаёт новое.
func (p *DCPool) Get(ctx context.Context) (essentials.Conn, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	// Пробуем взять из пула
	for {
		// Проверяем context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case conn := <-p.conns:
			if conn.isHealthy(p.config.IdleTimeout) {
				conn.markUsed()
				p.stats.hits.Add(1)
				return conn, nil
			}
			// Соединение протухло
			conn.Close()
			p.stats.unhealthy.Add(1)
			continue
		default:
			// Пул пуст — создаём новое соединение
			p.stats.misses.Add(1)
			return p.dial(ctx)
		}
	}
}

// Put возвращает соединение в пул.
func (p *DCPool) Put(conn essentials.Conn) {
	if p.closed.Load() {
		conn.Close()
		p.stats.closed.Add(1)
		return
	}

	// Проверяем, наше ли это соединение
	pc, ok := conn.(*pooledConn)
	if !ok {
		// Оборачиваем внешнее соединение
		pc = &pooledConn{
			Conn:       conn,
			dc:         p.dc,
			createdAt:  time.Now(),
			lastUsedAt: time.Now(),
		}
	}

	if !pc.isHealthy(p.config.IdleTimeout) {
		pc.Close()
		p.stats.unhealthy.Add(1)
		return
	}

	pc.lastUsedAt = time.Now()

	select {
	case p.conns <- pc:
		// Успешно вернули в пул
	default:
		// Пул полон — закрываем
		pc.Close()
		p.stats.closed.Add(1)
	}
}

// dial создаёт новое соединение к DC.
// Устанавливает TCP keepalive для быстрого обнаружения мёртвых соединений.
func (p *DCPool) dial(ctx context.Context) (essentials.Conn, error) {
	p.mu.Lock()
	addrs := make([]tgAddr, len(p.addrs))
	copy(addrs, p.addrs)
	p.mu.Unlock()

	var lastErr error
	for _, addr := range addrs {
		conn, err := p.dialer.DialContext(ctx, addr.network, addr.address)
		if err != nil {
			lastErr = err
			continue
		}

		// Настраиваем TCP keepalive для pooled-соединений.
		// Это критично для обнаружения закрытых Telegram-ом соединений.
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetKeepAlive(true)
			_ = tcpConn.SetKeepAlivePeriod(keepalivePeriod)
		}

		p.stats.created.Add(1)
		return &pooledConn{
			Conn:       conn,
			dc:         p.dc,
			createdAt:  time.Now(),
			lastUsedAt: time.Now(),
		}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errNoAddresses
}

// Close закрывает пул и все соединения.
func (p *DCPool) Close() error {
	if p.closed.Swap(true) {
		return nil // Уже закрыт
	}

	// Останавливаем background cleanup
	close(p.stopCh)

	// Дренируем и закрываем оставшиеся соединения.
	// Не закрываем channel — cleanupLoop может ещё писать в него.
	for {
		select {
		case conn := <-p.conns:
			conn.Close()
			p.stats.closed.Add(1)
		default:
			return nil
		}
	}
}

// cleanupLoop периодически удаляет stale соединения из пула.
// Интервал = IdleTimeout/2 — достаточно часто чтобы stale соединения
// не накапливались, но не слишком агрессивно.
func (p *DCPool) cleanupLoop() {
	interval := p.config.IdleTimeout / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.evictStale()
		}
	}
}

// evictStale вычищает протухшие соединения из channel-based пула.
// Дренит все соединения, проверяет здоровье, живые кладёт обратно.
func (p *DCPool) evictStale() {
	// Считаем сколько сейчас в пуле — дренируем ровно столько
	n := len(p.conns)
	for i := 0; i < n; i++ {
		select {
		case conn := <-p.conns:
			if conn.isHealthy(p.config.IdleTimeout) {
				// Живое — возвращаем
				select {
				case p.conns <- conn:
				default:
					// Пул заполнился (race с Put) — закрываем лишнее
					conn.Close()
					p.stats.closed.Add(1)
				}
			} else {
				conn.Close()
				p.stats.unhealthy.Add(1)
			}
		default:
			return // Пул уже пуст
		}
	}
}

// Stats возвращает статистику пула.
func (p *DCPool) Stats() PoolStats {
	return PoolStats{
		DC:        p.dc,
		Hits:      p.stats.hits.Load(),
		Misses:    p.stats.misses.Load(),
		Created:   p.stats.created.Load(),
		Closed:    p.stats.closed.Load(),
		Unhealthy: p.stats.unhealthy.Load(),
		Idle:      len(p.conns),
	}
}

// PoolStats — статистика пула.
type PoolStats struct {
	DC        int
	Hits      uint64 // Успешные взятия из пула
	Misses    uint64 // Промахи (создание нового)
	Created   uint64 // Всего создано соединений
	Closed    uint64 // Закрыто соединений
	Unhealthy uint64 // Отклонено нездоровых
	Idle      int    // Текущее количество idle
}

// ConnectionPoolManager управляет пулами для всех DC.
type ConnectionPoolManager struct {
	pools  map[int]*DCPool
	mu     sync.RWMutex
	dialer Dialer
	config PoolConfig
	closed atomic.Bool
}

// NewConnectionPoolManager создаёт менеджер пулов.
func NewConnectionPoolManager(dialer Dialer, config PoolConfig) *ConnectionPoolManager {
	return &ConnectionPoolManager{
		pools:  make(map[int]*DCPool),
		dialer: dialer,
		config: config,
	}
}

// GetPool возвращает пул для DC, создаёт при необходимости.
func (m *ConnectionPoolManager) GetPool(dc int, addrs []tgAddr) *DCPool {
	m.mu.RLock()
	pool, exists := m.pools[dc]
	m.mu.RUnlock()

	if exists {
		return pool
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if pool, exists = m.pools[dc]; exists {
		return pool
	}

	pool = NewDCPool(dc, m.dialer, addrs, m.config)
	m.pools[dc] = pool
	return pool
}

// Get получает соединение для DC.
func (m *ConnectionPoolManager) Get(ctx context.Context, dc int, addrs []tgAddr) (essentials.Conn, error) {
	if m.closed.Load() {
		return nil, ErrPoolClosed
	}
	pool := m.GetPool(dc, addrs)
	return pool.Get(ctx)
}

// Put возвращает соединение в соответствующий пул.
func (m *ConnectionPoolManager) Put(dc int, conn essentials.Conn) {
	m.mu.RLock()
	pool, exists := m.pools[dc]
	m.mu.RUnlock()

	if exists {
		pool.Put(conn)
	} else {
		conn.Close()
	}
}

// Close закрывает все пулы.
func (m *ConnectionPoolManager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, pool := range m.pools {
		pool.Close()
	}
	return nil
}

// AllStats возвращает статистику всех пулов.
func (m *ConnectionPoolManager) AllStats() []PoolStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]PoolStats, 0, len(m.pools))
	for _, pool := range m.pools {
		stats = append(stats, pool.Stats())
	}
	return stats
}

// PooledConn — обёртка для возврата соединения в пул при закрытии.
// Используется для автоматического возврата в пул.
type PooledConn struct {
	essentials.Conn
	dc      int
	manager *ConnectionPoolManager
	closed  atomic.Bool
}

// Close возвращает соединение в пул вместо закрытия.
func (c *PooledConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	// Возвращаем в пул, а не закрываем
	c.manager.Put(c.dc, c.Conn)
	return nil
}

// ForceClose принудительно закрывает соединение.
func (c *PooledConn) ForceClose() error {
	if c.closed.Swap(true) {
		return nil
	}
	return c.Conn.Close()
}

// RemoteAddr реализует net.Conn для совместимости.
func (c *PooledConn) RemoteAddr() net.Addr {
	return c.Conn.RemoteAddr()
}
