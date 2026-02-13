package faketls

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
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
		// Если это произошло — система в критическом состоянии (нет /dev/urandom).
		// panic предпочтительнее: предсказуемые значения = обход шифрования.
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}

	return int(binary.LittleEndian.Uint64(buf[:]) % uint64(n)) //nolint: gosec
}
