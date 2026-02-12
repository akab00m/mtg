package faketls

import (
	"bytes"
	"fmt"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/9seconds/mtg/v2/mtglib/internal/faketls/record"
)

// ccsPaddingProbabilityPercent — вероятность инъекции dummy CCS record
// после блока ApplicationData records. CCS records невидимы для клиента
// (Read() молча их игнорирует), но ломают DPI-анализ по подсчёту records,
// размеру burst и межпакетным интервалам.
const ccsPaddingProbabilityPercent = 15

type Conn struct {
	essentials.Conn

	readBuffer bytes.Buffer

	// EnableCCSPadding включает инъекцию dummy ChangeCipherSpec records.
	// Эффект: затрудняет traffic analysis (подсчёт records в burst).
	// Клиентская сторона (Read) уже игнорирует CCS records.
	EnableCCSPadding bool
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

	// CCS cover traffic: инъекция dummy ChangeCipherSpec record.
	// Эффект: ломает DPI-анализ по подсчёту records в burst и timing.
	// Клиентский Read() игнорирует CCS records (case TypeChangeCipherSpec: пустой).
	if c.EnableCCSPadding && secureRandIntn(100) < ccsPaddingProbabilityPercent {
		c.injectDummyCCS(sendBuffer) //nolint: errcheck
	}

	if _, err := c.Conn.Write(sendBuffer.Bytes()); err != nil {
		return 0, err //nolint: wrapcheck
	}

	return lenP, nil
}

// injectDummyCCS записывает dummy ChangeCipherSpec record в буфер.
// CCS record с payload [0x01] — стандартная часть TLS handshake flow,
// его присутствие после handshake не является аномалией для DPI.
func (c *Conn) injectDummyCCS(buf *bytes.Buffer) {
	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	rec.Type = record.TypeChangeCipherSpec
	rec.Version = record.Version12
	rec.Payload.WriteByte(0x01) // ChangeCipherSpec value
	rec.Dump(buf) //nolint: errcheck
}
