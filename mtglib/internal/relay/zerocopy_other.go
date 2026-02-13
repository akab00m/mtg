//go:build !linux

package relay

import (
	"io"

	"github.com/9seconds/mtg/v2/essentials"
)

// copyWithZeroCopyAdaptive — версия с адаптивным буфером для non-Linux.
// На не-Linux системах адаптивность ограничена размером буфера.
func copyWithZeroCopyAdaptive(src, dst essentials.Conn, buf []byte, stats *StreamStats) (int64, error) {
	// Адаптируем размер буфера на основе throughput
	if stats != nil {
		optimalSize := globalAdaptiveBuffer.GetOptimalSize(stats.GetThroughput())
		if optimalSize < len(buf) {
			buf = buf[:optimalSize]
		}
	}

	total, err := io.CopyBuffer(dst, src, buf)
	if stats != nil {
		stats.AddBytes(total)
	}
	return total, err
}
