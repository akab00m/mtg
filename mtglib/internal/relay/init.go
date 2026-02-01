package relay

const (
	// 256KB буфер для копирования данных между соединениями
	// Согласовано с размером pipe буфера в zerocopy_linux.go (pipeSize = 262144)
	// Обеспечивает консистентность между Linux (splice) и другими ОС (io.Copy)
	// Оптимально для медиа-трафика Telegram: меньше syscalls, лучший throughput
	copyBufferSize = 262144 // 256 KB
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
