package mtglib

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"

	"github.com/9seconds/mtg/v2/essentials"
)

const (
	// trafficFlushThreshold — порог накопленного трафика для эмиссии EventTraffic.
	// 32KB: при 256KB буфере это ~8 событий на io.CopyBuffer итерацию вместо ~16.
	// Уменьшает количество heap-аллокаций (interface boxing) и channel sends.
	trafficFlushThreshold uint64 = 32 * 1024
)

type connTraffic struct {
	essentials.Conn

	streamID string
	stream   EventStream
	ctx      context.Context

	// Атомарные аккумуляторы для батчинга EventTraffic.
	// Pointer-based: connTraffic копируется через value receivers в обёртках,
	// но все копии разделяют один и тот же accumulator.
	readAcc  *atomic.Uint64
	writeAcc *atomic.Uint64
}

func (c connTraffic) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)

	if n > 0 {
		accumulated := c.readAcc.Add(uint64(n))
		if accumulated >= trafficFlushThreshold {
			// Сбрасываем аккумулятор и эмитим событие
			c.readAcc.Store(0)
			c.stream.Send(c.ctx, NewEventTraffic(c.streamID, uint(accumulated), true))
		}
	}

	return n, err //nolint: wrapcheck
}

func (c connTraffic) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)

	if n > 0 {
		accumulated := c.writeAcc.Add(uint64(n))
		if accumulated >= trafficFlushThreshold {
			c.writeAcc.Store(0)
			c.stream.Send(c.ctx, NewEventTraffic(c.streamID, uint(accumulated), false))
		}
	}

	return n, err //nolint: wrapcheck
}

// FlushTraffic эмитит оставшийся накопленный трафик.
func (c connTraffic) FlushTraffic() {
	if r := c.readAcc.Swap(0); r > 0 {
		c.stream.Send(c.ctx, NewEventTraffic(c.streamID, uint(r), true))
	}

	if w := c.writeAcc.Swap(0); w > 0 {
		c.stream.Send(c.ctx, NewEventTraffic(c.streamID, uint(w), false))
	}
}

// Close сбрасывает накопленный трафик перед закрытием соединения.
// Вызывается автоматически через цепочку Close: relay.Relay() → obfuscated2.Conn → connTraffic → tcp.
func (c connTraffic) Close() error {
	c.FlushTraffic()

	return c.Conn.Close() //nolint: wrapcheck
}

// newConnTraffic создаёт connTraffic с инициализированными аккумуляторами.
func newConnTraffic(conn essentials.Conn, streamID string, stream EventStream, ctx context.Context) connTraffic {
	return connTraffic{
		Conn:     conn,
		streamID: streamID,
		stream:   stream,
		ctx:      ctx,
		readAcc:  &atomic.Uint64{},
		writeAcc: &atomic.Uint64{},
	}
}

type connRewind struct {
	essentials.Conn

	// active хранит текущий reader через atomic — lock-free переключение.
	// До Rewind(): TeeReader(conn, buf). После: MultiReader(buf, conn).
	active atomic.Pointer[io.Reader]
	buf    bytes.Buffer
}

func (c *connRewind) Read(p []byte) (int, error) {
	r := c.active.Load()

	return (*r).Read(p) //nolint: wrapcheck
}

func (c *connRewind) Rewind() {
	mr := io.Reader(io.MultiReader(&c.buf, c.Conn))
	c.active.Store(&mr)
}

func newConnRewind(conn essentials.Conn) *connRewind {
	rv := &connRewind{
		Conn: conn,
	}
	tr := io.Reader(io.TeeReader(conn, &rv.buf))
	rv.active.Store(&tr)

	return rv
}
