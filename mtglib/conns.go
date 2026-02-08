package mtglib

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"

	"github.com/9seconds/mtg/v2/essentials"
)

type connTraffic struct {
	essentials.Conn

	streamID string
	stream   EventStream
	ctx      context.Context
}

func (c connTraffic) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)

	if n > 0 {
		c.stream.Send(c.ctx, NewEventTraffic(c.streamID, uint(n), true))
	}

	return n, err //nolint: wrapcheck
}

func (c connTraffic) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)

	if n > 0 {
		c.stream.Send(c.ctx, NewEventTraffic(c.streamID, uint(n), false))
	}

	return n, err //nolint: wrapcheck
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
