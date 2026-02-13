package mtglib

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sync"
)

// ipHashSaltSize — размер криптографической соли для hashIP.
// 32 байта = 256 бит — обеспечивает невозможность rainbow table атаки
// даже при утечке хэшей из логов.
const ipHashSaltSize = 32

// ipHashSalt — per-instance криптографическая соль.
// Генерируется один раз при старте процесса через crypto/rand.
// Хранится только в памяти — при перезапуске хэши меняются,
// что предотвращает корреляцию логов между запусками.
var (
	ipHashSalt     []byte
	ipHashSaltOnce sync.Once
)

// initIPHashSalt генерирует криптографически стойкую соль при первом вызове.
// Panic при сбое crypto/rand — без соли хэши обратимы (IPv4 = 2^32 пространство).
func initIPHashSalt() {
	ipHashSaltOnce.Do(func() {
		salt := make([]byte, ipHashSaltSize)

		if _, err := rand.Read(salt); err != nil {
			panic("crypto/rand.Read failed for IP hash salt: " + err.Error())
		}

		ipHashSalt = salt
	})
}

// hashIP хэширует IP-адрес для записи в логи.
// Предотвращает утечку реальных IP-адресов клиентов при компрометации логов.
//
// Алгоритм: HMAC-like конструкция SHA-256(salt || ip).
// Per-instance salt (32 байта, crypto/rand) делает rainbow table атаку
// невычислимой: даже для IPv4 (2^32) нужна отдельная таблица на каждый запуск.
//
// Truncated output (12 hex символов = 48 бит) — достаточно для корреляции
// записей в логах одного запуска без возможности восстановления IP.
func hashIP(ip net.IP) string {
	initIPHashSalt()

	h := sha256.New()
	h.Write(ipHashSalt)
	h.Write(ip)
	sum := h.Sum(nil)

	return hex.EncodeToString(sum[:6]) // 12 hex chars = 48 бит
}
