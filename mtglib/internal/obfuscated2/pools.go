package obfuscated2

import (
	"crypto/sha256"
	"hash"
	"sync"
)

const (
	// writeBufferSize — начальный размер буфера для Write.
	// Соответствует maxWriteSize в faketls (16384 = TLS record payload).
	writeBufferSize = 16384
)

var (
	sha256HasherPool = sync.Pool{
		New: func() interface{} {
			return sha256.New()
		},
	}
	writeBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, writeBufferSize)
			return &buf
		},
	}
)

func acquireSha256Hasher() hash.Hash {
	return sha256HasherPool.Get().(hash.Hash) //nolint: forcetypeassert
}

func releaseSha256Hasher(h hash.Hash) {
	h.Reset()
	sha256HasherPool.Put(h)
}

// acquireWriteBuffer возвращает буфер из пула с гарантированным размером >= size.
// Если буфер из пула слишком мал, создаётся новый.
func acquireWriteBuffer(size int) *[]byte {
	buf := writeBufferPool.Get().(*[]byte) //nolint: forcetypeassert
	if cap(*buf) < size {
		newBuf := make([]byte, size)
		return &newBuf
	}

	*buf = (*buf)[:size]

	return buf
}

func releaseWriteBuffer(buf *[]byte) {
	// Не возвращаем слишком большие буферы (>256KB) чтобы не раздувать пул
	if cap(*buf) > 262144 {
		return
	}

	writeBufferPool.Put(buf)
}
