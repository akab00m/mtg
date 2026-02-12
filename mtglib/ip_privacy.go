package mtglib

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
)

// hashIP хэширует IP-адрес для записи в логи.
// Предотвращает утечку реальных IP-адресов клиентов при компрометации логов.
// Используется truncated SHA-256 (первые 12 hex символов) — достаточно
// для корреляции записей без возможности восстановления исходного адреса.
func hashIP(ip net.IP) string {
	h := sha256.Sum256(ip)

	return hex.EncodeToString(h[:6]) // 12 hex chars = 48 бит
}
