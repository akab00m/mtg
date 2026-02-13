package relay

// Priority определяет приоритет потока данных.
// Используется для внутренней классификации направлений relay.
type Priority int

const (
	// PriorityNormal — стандартный приоритет (upload: client -> telegram)
	PriorityNormal Priority = iota

	// PriorityHigh — высокий приоритет (download: telegram -> client)
	// Download критичнее для UX — пользователь ждёт загрузку медиа.
	// Приоритизация выполняется через TCP socket options:
	// - TCP_QUICKACK — немедленные ACK для download направления
	// - TCP_NOTSENT_LOWAT — быстрое уведомление о возможности записи
	// - TCP_NODELAY — отсутствие Nagle buffering
	PriorityHigh
)
