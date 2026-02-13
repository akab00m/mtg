package cli

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/9seconds/mtg/v2/internal/utils"
)

// healthCheckTimeout — максимальное время ожидания ответа от metrics endpoint.
// 5 секунд достаточно для проверки доступности, при этом docker не считает
// контейнер unhealthy из-за случайных задержек.
const healthCheckTimeout = 5 * time.Second

// Health проверяет работоспособность proxy через Prometheus metrics endpoint.
// Используется в Dockerfile HEALTHCHECK и docker-compose healthcheck.
//
// Алгоритм:
// 1. Парсит конфиг для определения адреса Prometheus metrics
// 2. Если Prometheus не включён — fallback на TCP connect к proxy порту
// 3. HTTP GET /metrics — ожидает 200 OK
type Health struct {
	ConfigPath string `kong:"arg,required,type='existingfile',help='Path to config file.',name='config-path'"` //nolint: lll
}

func (h Health) Run(cli *CLI, version string) error {
	conf, err := utils.ReadConfig(h.ConfigPath)
	if err != nil {
		return fmt.Errorf("cannot parse config: %w", err)
	}

	// Проверяем Prometheus metrics endpoint (предпочтительно)
	if conf.Stats.Prometheus.Enabled.Get(false) {
		bindTo := conf.Stats.Prometheus.BindTo.Value
		httpPath := conf.Stats.Prometheus.HTTPPath.Value

		if bindTo == "" {
			bindTo = "0.0.0.0:9401"
		}

		if httpPath == "" {
			httpPath = "/metrics"
		}

		// Для healthcheck всегда подключаемся к localhost
		_, port, _ := net.SplitHostPort(bindTo)
		if port == "" {
			port = "9401"
		}

		url := fmt.Sprintf("http://127.0.0.1:%s%s", port, httpPath)

		return checkHTTP(url)
	}

	// Fallback: TCP connect к proxy порту
	bindTo := conf.BindTo.Value
	if bindTo == "" {
		return fmt.Errorf("prometheus not enabled and no bind address configured")
	}

	return checkTCP(bindTo)
}

// checkHTTP проверяет HTTP endpoint — ожидает 200 OK.
func checkHTTP(url string) error {
	client := &http.Client{
		Timeout: healthCheckTimeout,
	}

	resp, err := client.Get(url) //nolint: noctx
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain body для корректного закрытия соединения
	io.Copy(io.Discard, resp.Body) //nolint: errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}

// checkTCP проверяет TCP-доступность порта.
func checkTCP(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, healthCheckTimeout)
	if err != nil {
		return fmt.Errorf("health check TCP connect failed: %w", err)
	}

	conn.Close()

	return nil
}
