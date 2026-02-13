package relay

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// mockConn имитирует essentials.Conn для бенчмарков
type mockConn struct {
	reader io.Reader
	writer io.Writer
	closed bool
	mu     sync.Mutex
}

func newMockConn(data []byte) *mockConn {
	return &mockConn{
		reader: bytes.NewReader(data),
		writer: io.Discard,
	}
}

func (m *mockConn) Read(b []byte) (int, error) {
	return m.reader.Read(b)
}

func (m *mockConn) Write(b []byte) (int, error) {
	return m.writer.Write(b)
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockConn) CloseRead() error  { return nil }
func (m *mockConn) CloseWrite() error { return nil }

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// BenchmarkCopyRelay тестирует скорость копирования
func BenchmarkCopyRelay(b *testing.B) {
	sizes := []int{
		1024,        // 1 KB
		64 * 1024,   // 64 KB
		1024 * 1024, // 1 MB
	}

	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}

			b.Run(formatSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				src := newMockConn(data)
				dst := newMockConn(nil)
				buf := make([]byte, 32*1024)

				_, _ = copyRelay(dst, src, buf)
			}
		})
	}
}

// BenchmarkStandardCopy для сравнения с io.CopyBuffer
func BenchmarkStandardCopy(b *testing.B) {
	sizes := []int{
		1024,        // 1 KB
		64 * 1024,   // 64 KB
		1024 * 1024, // 1 MB
	}

	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run(formatSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				src := bytes.NewReader(data)
				dst := io.Discard
				buf := make([]byte, 32*1024)

				_, _ = io.CopyBuffer(dst, src, buf)
			}
		})
	}
}

func formatSize(bytes int) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%dMB", bytes/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%dKB", bytes/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// BenchmarkConcurrentRelay тестирует производительность при параллельных соединениях
func BenchmarkConcurrentRelay(b *testing.B) {
	concurrencies := []int{10, 100, 1000}
	dataSize := 64 * 1024 // 64KB per connection

	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	for _, concurrency := range concurrencies {
		b.Run(fmt.Sprintf("%d_concurrent", concurrency), func(b *testing.B) {
			b.SetBytes(int64(dataSize * concurrency))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				wg.Add(concurrency)

				for j := 0; j < concurrency; j++ {
					go func() {
						defer wg.Done()
						src := newMockConn(data)
						dst := newMockConn(nil)
						buf := make([]byte, 32*1024)

					_, _ = copyRelay(dst, src, buf)
					}()
				}

				wg.Wait()
			}
		})
	}
}
