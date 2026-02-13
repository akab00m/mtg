package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// DefaultDCRefreshInterval — интервал проверки обновлений DC-адресов.
// Telegram меняет адреса крайне редко, 24ч достаточно.
const DefaultDCRefreshInterval = 24 * time.Hour

// DCConfigFile — формат JSON файла с DC-адресами.
// Позволяет обновлять адреса без пересборки образа.
//
// Пример:
//
//	{
//	  "v4": {
//	    "1": ["149.154.175.50:443"],
//	    "2": ["149.154.167.51:443", "95.161.76.100:443"],
//	    "3": ["149.154.175.100:443"],
//	    "4": ["149.154.167.91:443"],
//	    "5": ["149.154.171.5:443"]
//	  },
//	  "v6": {
//	    "1": ["[2001:b28:f23d:f001::a]:443"],
//	    "2": ["[2001:67c:04e8:f002::a]:443"],
//	    "3": ["[2001:b28:f23d:f003::a]:443"],
//	    "4": ["[2001:67c:04e8:f004::a]:443"],
//	    "5": ["[2001:b28:f23f:f005::a]:443"]
//	  }
//	}
type DCConfigFile struct {
	V4 map[string][]string `json:"v4"`
	V6 map[string][]string `json:"v6"`
}

// dcRefresher управляет периодическим обновлением DC-адресов из файла.
type dcRefresher struct {
	filePath     string
	interval     time.Duration
	fallbackPool addressPool // hardcoded адреса — всегда доступны
	stopCh       chan struct{}
	once         sync.Once
}

// newDCRefresher создаёт refresher для файла с DC-адресами.
func newDCRefresher(filePath string, interval time.Duration, fallback addressPool) *dcRefresher {
	if interval <= 0 {
		interval = DefaultDCRefreshInterval
	}

	return &dcRefresher{
		filePath:     filePath,
		interval:     interval,
		fallbackPool: fallback,
		stopCh:       make(chan struct{}),
	}
}

// loadDCConfig загружает DC-адреса из JSON файла.
// При ошибке возвращает nil — вызывающий код должен использовать fallback.
func loadDCConfig(filePath string) (*addressPool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read DC config file: %w", err)
	}

	var config DCConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("cannot parse DC config file: %w", err)
	}

	return parseDCConfig(&config)
}

// parseDCConfig конвертирует JSON-конфиг в addressPool.
// Валидирует наличие DC 1-5 для IPv4 (минимальное требование).
func parseDCConfig(config *DCConfigFile) (*addressPool, error) {
	if len(config.V4) == 0 {
		return nil, fmt.Errorf("DC config must contain at least v4 addresses")
	}

	v4 := make([][]tgAddr, 5) //nolint: gomnd
	v6 := make([][]tgAddr, 5) //nolint: gomnd

	// Парсим IPv4
	for dcStr, addrs := range config.V4 {
		dc := parseDCNumber(dcStr)
		if dc < 1 || dc > 5 {
			continue // Игнорируем невалидные DC
		}

		tgAddrs := make([]tgAddr, 0, len(addrs))
		for _, addr := range addrs {
			tgAddrs = append(tgAddrs, tgAddr{network: "tcp4", address: addr})
		}

		v4[dc-1] = tgAddrs
	}

	// Парсим IPv6
	for dcStr, addrs := range config.V6 {
		dc := parseDCNumber(dcStr)
		if dc < 1 || dc > 5 {
			continue
		}

		tgAddrs := make([]tgAddr, 0, len(addrs))
		for _, addr := range addrs {
			tgAddrs = append(tgAddrs, tgAddr{network: "tcp6", address: addr})
		}

		v6[dc-1] = tgAddrs
	}

	// Валидация: хотя бы один DC должен иметь IPv4 адрес
	hasAnyV4 := false
	for _, addrs := range v4 {
		if len(addrs) > 0 {
			hasAnyV4 = true

			break
		}
	}

	if !hasAnyV4 {
		return nil, fmt.Errorf("DC config must contain at least one valid v4 address for DC 1-5")
	}

	return &addressPool{v4: v4, v6: v6}, nil
}

// parseDCNumber парсит строковый номер DC.
func parseDCNumber(s string) int {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0
	}

	return int(s[0] - '0')
}

// stop останавливает background refresh goroutine.
func (r *dcRefresher) stop() {
	r.once.Do(func() {
		close(r.stopCh)
	})
}
