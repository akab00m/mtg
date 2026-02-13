package obfuscated2

import (
	"crypto/cipher"

	"github.com/9seconds/mtg/v2/essentials"
)

type Conn struct {
	essentials.Conn

	Encryptor cipher.Stream
	Decryptor cipher.Stream
}

func (c Conn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		return n, err //nolint: wrapcheck
	}

	c.Decryptor.XORKeyStream(p, p[:n])

	return n, nil
}

func (c Conn) Write(p []byte) (int, error) {
	// Получаем буфер из пула и гарантируем достаточный размер
	buf := acquireWriteBuffer(len(p))
	defer releaseWriteBuffer(buf)

	// XOR напрямую из p в buf за один проход.
	// Копия необходима: контракт io.Writer запрещает модификацию p,
	// а io.CopyBuffer переиспользует свой буфер между итерациями.
	dst := (*buf)[:len(p)]
	c.Encryptor.XORKeyStream(dst, p)

	return c.Conn.Write(dst) //nolint: wrapcheck
}
