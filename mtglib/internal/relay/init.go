package relay

const (
	// Оптимизировано для Telegram: используем 64KB буфер
	// Баланс: достаточно большой для эффективного копирования, но не избыточный
	// 64KB соответствует размеру окна TCP и стандартному размеру pipe буфера
	copyBufferSize = 65536 // 64 KB
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
