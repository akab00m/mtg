package relay

const (
	// Оптимизировано для Telegram: используем 64KB буфер
	// Telegram делает множество коротких соединений (не длинные потоки)
	// Большие буферы (1MB) расходуют память без пользы для таких паттернов
	copyBufferSize = 65536 // 64 KB (оптимально для мелких запросов Telegram)
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
