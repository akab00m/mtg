//go:build !linux

package relay

import (
	"io"

	"github.com/9seconds/mtg/v2/essentials"
)

// copyWithZeroCopy на не-Linux системах просто использует стандартный io.CopyBuffer
func copyWithZeroCopy(src, dst essentials.Conn, buf []byte) (int64, error) {
	return io.CopyBuffer(dst, src, buf)
}
