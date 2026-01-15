package relay

const (
	// Оптимизировано для Telegram: используем 32KB буфер
	// Telegram: множество коротких соединений с мелкими пакетами (в среднем 8-16KB)
	// Меньший буфер = меньше памяти, быстрее выделение/освобождение
	copyBufferSize = 32768 // 32 KB (баланс между памятью и производительностью)
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
