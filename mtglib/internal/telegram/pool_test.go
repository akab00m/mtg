package telegram

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConn — тестовая имплементация essentials.Conn
type mockConn struct {
	net.Conn
	closed    bool
	closeOnce sync.Once
	mu        sync.Mutex
}

func newMockConn() *mockConn {
	server, client := net.Pipe()
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := server.Read(buf)
			if err != nil {
				return
			}
		}
	}()
	return &mockConn{Conn: client}
}

func (m *mockConn) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		m.Conn.Close()
	})
	return nil
}

func (m *mockConn) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *mockConn) CloseRead() error  { return nil }
func (m *mockConn) CloseWrite() error { return nil }

// mockDialer — тестовый dialer
type mockDialer struct {
	mu        sync.Mutex
	dialCount int
	failAfter int // Fail after N successful dials
	err       error
}

func (m *mockDialer) DialContext(ctx context.Context, network, address string) (essentials.Conn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}

	if m.failAfter > 0 && m.dialCount >= m.failAfter {
		return nil, errors.New("dial failed")
	}

	m.dialCount++
	return newMockConn(), nil
}

func (m *mockDialer) DialCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dialCount
}

func TestDCPool_GetPut(t *testing.T) {
	dialer := &mockDialer{}
	addrs := []tgAddr{{network: "tcp4", address: "127.0.0.1:443"}}

	config := PoolConfig{
		MaxIdleConns: 3,
		IdleTimeout:  time.Minute,
	}

	pool := NewDCPool(1, dialer, addrs, config)
	defer pool.Close()

	ctx := context.Background()

	// Первый Get — создаёт новое соединение (miss)
	conn1, err := pool.Get(ctx)
	require.NoError(t, err)
	assert.NotNil(t, conn1)
	assert.Equal(t, 1, dialer.DialCount())

	// Возвращаем в пул
	pool.Put(conn1)

	// Второй Get — берёт из пула (hit)
	conn2, err := pool.Get(ctx)
	require.NoError(t, err)
	assert.NotNil(t, conn2)
	assert.Equal(t, 1, dialer.DialCount()) // Dial не вызывался

	stats := pool.Stats()
	assert.Equal(t, uint64(1), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
}

func TestDCPool_MaxIdleConns(t *testing.T) {
	dialer := &mockDialer{}
	addrs := []tgAddr{{network: "tcp4", address: "127.0.0.1:443"}}

	config := PoolConfig{
		MaxIdleConns: 2, // Максимум 2 idle соединения
		IdleTimeout:  time.Minute,
	}

	pool := NewDCPool(1, dialer, addrs, config)
	defer pool.Close()

	ctx := context.Background()

	// Создаём 3 соединения
	conn1, _ := pool.Get(ctx)
	conn2, _ := pool.Get(ctx)
	conn3, _ := pool.Get(ctx)

	assert.Equal(t, 3, dialer.DialCount())

	// Возвращаем все в пул
	pool.Put(conn1)
	pool.Put(conn2)
	pool.Put(conn3) // Это соединение должно закрыться (пул полон)

	stats := pool.Stats()
	assert.Equal(t, 2, stats.Idle) // Только 2 в пуле
}

func TestDCPool_Close(t *testing.T) {
	dialer := &mockDialer{}
	addrs := []tgAddr{{network: "tcp4", address: "127.0.0.1:443"}}

	config := DefaultPoolConfig()
	pool := NewDCPool(1, dialer, addrs, config)

	ctx := context.Background()

	// Получаем соединение и возвращаем
	conn, _ := pool.Get(ctx)
	pool.Put(conn)

	// Закрываем пул
	err := pool.Close()
	require.NoError(t, err)

	// Get после Close должен вернуть ошибку
	_, err = pool.Get(ctx)
	assert.Equal(t, ErrPoolClosed, err)
}

func TestConnectionPoolManager_MultiDC(t *testing.T) {
	dialer := &mockDialer{}
	config := DefaultPoolConfig()

	manager := NewConnectionPoolManager(dialer, config)
	defer manager.Close()

	ctx := context.Background()

	// Соединения к разным DC
	addrs1 := []tgAddr{{network: "tcp4", address: "dc1.example.com:443"}}
	addrs2 := []tgAddr{{network: "tcp4", address: "dc2.example.com:443"}}

	conn1, err := manager.Get(ctx, 1, addrs1)
	require.NoError(t, err)
	assert.NotNil(t, conn1)

	conn2, err := manager.Get(ctx, 2, addrs2)
	require.NoError(t, err)
	assert.NotNil(t, conn2)

	// Возвращаем в соответствующие пулы
	manager.Put(1, conn1)
	manager.Put(2, conn2)

	// Проверяем статистику
	stats := manager.AllStats()
	assert.Len(t, stats, 2)
}

func TestPooledConn_AutoReturn(t *testing.T) {
	dialer := &mockDialer{}
	config := DefaultPoolConfig()

	manager := NewConnectionPoolManager(dialer, config)
	defer manager.Close()

	ctx := context.Background()
	addrs := []tgAddr{{network: "tcp4", address: "127.0.0.1:443"}}

	// Получаем соединение
	rawConn, _ := manager.Get(ctx, 1, addrs)

	// Оборачиваем в PooledConn
	pooledConn := &PooledConn{
		Conn:    rawConn,
		dc:      1,
		manager: manager,
	}

	// Close должен вернуть в пул, а не закрыть
	err := pooledConn.Close()
	require.NoError(t, err)

	// Проверяем, что соединение вернулось в пул
	pool := manager.GetPool(1, addrs)
	assert.Equal(t, 1, pool.Stats().Idle)

	// Dial не должен вызываться при следующем Get
	dialsBefore := dialer.DialCount()
	conn2, _ := manager.Get(ctx, 1, addrs)
	assert.NotNil(t, conn2)
	assert.Equal(t, dialsBefore, dialer.DialCount())
}

func TestDCPool_ConcurrentAccess(t *testing.T) {
	dialer := &mockDialer{}
	addrs := []tgAddr{{network: "tcp4", address: "127.0.0.1:443"}}

	config := PoolConfig{
		MaxIdleConns: 10,
		IdleTimeout:  time.Minute,
	}

	pool := NewDCPool(1, dialer, addrs, config)
	defer pool.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// 100 горутин параллельно Get/Put
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Get(ctx)
			if err != nil {
				return
			}
			time.Sleep(time.Millisecond)
			pool.Put(conn)
		}()
	}

	wg.Wait()

	stats := pool.Stats()
	assert.True(t, stats.Hits+stats.Misses >= 100)
}

// TestDCPool_UnhealthyConnectionRejected проверяет, что соединения с истёкшим
// idle timeout отклоняются при попытке взять из пула.
// Примечание: проверка closed socket убрана, т.к. Telegram закрывает соединения
// без RST, и TCP-level проверки ненадёжны. Вместо этого используется retry
// при broken pipe на уровне handshake (см. proxy.go).
func TestDCPool_UnhealthyConnectionRejected(t *testing.T) {
	dialer := &mockDialer{}
	addrs := []tgAddr{{network: "tcp4", address: "127.0.0.1:443"}}

	// Очень короткий idle timeout для теста
	config := PoolConfig{
		MaxIdleConns: 3,
		IdleTimeout:  10 * time.Millisecond,
	}

	pool := NewDCPool(1, dialer, addrs, config)
	defer pool.Close()

	ctx := context.Background()

	// Получаем соединение
	conn1, err := pool.Get(ctx)
	require.NoError(t, err)

	// Возвращаем в пул
	pool.Put(conn1)

	// Ждём истечения idle timeout
	time.Sleep(50 * time.Millisecond)

	// Пробуем взять — должно создаться новое (старое отклонено как unhealthy)
	conn2, err := pool.Get(ctx)
	require.NoError(t, err)
	defer conn2.Close()

	stats := pool.Stats()
	assert.Equal(t, uint64(1), stats.Unhealthy, "expired connection should be rejected as unhealthy")
	assert.Equal(t, uint64(2), stats.Misses, "should create new connection after unhealthy rejection")
}
