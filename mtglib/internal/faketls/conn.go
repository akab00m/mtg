package faketls

import (
	"bytes"
	"fmt"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/9seconds/mtg/v2/mtglib/internal/faketls/record"
)

// A5: CCS padding удалён — RFC 8446 Appendix D.4 допускает CCS ТОЛЬКО перед
// ServerHello (compatibility mode), но НЕ между ApplicationData records.
// CCS в data stream = аномалия → DPI (GFW, Roskomnadzor) может детектировать proxy.
//
// Исследование (GFW Report, USENIX Security 2023): "Любое изменение traffic
// pattern создаёт новый fingerprint." TLS Record Layer padding (RFC 8449)
// также контрпродуктивен — реальные TLS-стеки его не используют в data phase.
//
// Текущая anti-fingerprint стратегия:
// - Chrome-like TLS record sizes (полные 16384-байтные records)
// - Стандартное TLS 1.3 поведение без модификаций
// - "looks like nothing" ≠ safe, но "looks like Chrome" = safe

type Conn struct {
	essentials.Conn

	readBuffer bytes.Buffer
}

func (c *Conn) Read(p []byte) (int, error) {
	if n, _ := c.readBuffer.Read(p); n > 0 {
		return n, nil
	}

	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	for {
		if err := rec.Read(c.Conn); err != nil {
			return 0, err //nolint: wrapcheck
		}

		switch rec.Type { //nolint: exhaustive
		case record.TypeApplicationData:
			rec.Payload.WriteTo(&c.readBuffer) //nolint: errcheck

			return c.readBuffer.Read(p) //nolint: wrapcheck
		case record.TypeChangeCipherSpec:
			// Backward compatibility: игнорируем CCS если клиент или
			// предыдущая версия сервера отправляет их.
		default:
			return 0, fmt.Errorf("unsupported record type %v", rec.Type)
		}
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	rec.Type = record.TypeApplicationData
	rec.Version = record.Version12

	sendBuffer := acquireBytesBuffer()
	defer releaseBytesBuffer(sendBuffer)

	lenP := len(p)

	for len(p) > 0 {
		// Chrome/Firefox TLS 1.3 профиль: полные 16384-байтные records.
		// Реальные TLS-стеки всегда заполняют records до максимума при bulk transfer.
		// Последний record содержит оставшиеся данные (< 16384).
		//
		// Предыдущее поведение (uniform random [256, 16384]) создавало уникальный
		// fingerprint: ни один реальный TLS-стек не генерирует равномерно случайные
		// размеры records. DPI-системы (GFW, Roskomnadzor) детектируют это.
		chunkSize := record.TLSMaxWriteRecordSize
		if chunkSize > len(p) {
			chunkSize = len(p)
		}

		rec.Payload.Reset()
		rec.Payload.Write(p[:chunkSize])
		rec.Dump(sendBuffer) //nolint: errcheck

		p = p[chunkSize:]
	}

	if _, err := c.Conn.Write(sendBuffer.Bytes()); err != nil {
		return 0, err //nolint: wrapcheck
	}

	return lenP, nil
}
