//go:build !linux

package relay

import (
	"io"

	"github.com/9seconds/mtg/v2/essentials"
)

// copyWithZeroCopy — fallback для non-Linux: используем io.CopyBuffer.
// splice() недоступен, zero-copy невозможен.
func copyWithZeroCopy(src, dst essentials.Conn, buf []byte) (int64, error) {
	return io.CopyBuffer(dst, src, buf)
}
