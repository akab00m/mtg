package relay

const (
	// 256KB буфер для копирования данных между соединениями.
	// Оптимально для медиа-трафика Telegram: меньше syscalls, лучший throughput.
	// Размер выбран как BDP для мобильных сетей (100Mbps × 20ms RTT ≈ 250KB).
	copyBufferSize = 262144 // 256 KB
)

type Logger interface {
	Printf(msg string, args ...interface{})
}
