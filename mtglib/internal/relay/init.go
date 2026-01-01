package relay

const (
	// Увеличен с 64KB до 256KB для лучшей пропускной способности
	copyBufferSize = 256 * 1024
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
