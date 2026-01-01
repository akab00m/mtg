package relay

const (
	// Увеличен до 1MB для соответствия буферам Telegram клиентов (iOS/Android)
	// Это уменьшает количество системных вызовов и улучшает throughput на мобильных устройствах
	copyBufferSize = 1024 * 1024 // 1 MB (было 256 KB)
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
