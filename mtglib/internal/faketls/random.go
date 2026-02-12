package faketls

import (
	"crypto/rand"
	"encoding/binary"
)

// secureRandIntn возвращает криптографически случайное число в диапазоне [0, n).
// Используется вместо math/rand для предотвращения предсказуемости
// размеров TLS-записей и padding, что может быть использовано DPI
// для fingerprinting трафика (traffic analysis атака).
func secureRandIntn(n int) int {
	if n <= 0 {
		return 0
	}

	var buf [8]byte

	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand.Read не должен возвращать ошибку на поддерживаемых ОС.
		// Если это произошло — система в критическом состоянии.
		return 0
	}

	return int(binary.LittleEndian.Uint64(buf[:]) % uint64(n)) //nolint: gosec
}
