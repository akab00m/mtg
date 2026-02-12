package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/9seconds/mtg/v2/mtglib"
)

type Optional struct {
	Enabled TypeBool `json:"enabled"`
}

type ListConfig struct {
	Optional

	DownloadConcurrency TypeConcurrency    `json:"downloadConcurrency"`
	URLs                []TypeBlocklistURI `json:"urls"`
	UpdateEach          TypeDuration       `json:"updateEach"`
}

type Config struct {
	Debug                    TypeBool        `json:"debug"`
	AllowFallbackOnUnknownDC TypeBool        `json:"allowFallbackOnUnknownDc"`
	FallbackOnDialError      TypeBool        `json:"fallbackOnDialError"`
	Secret                   mtglib.Secret   `json:"secret"`
	BindTo                   TypeHostPort    `json:"bindTo"`
	PreferIP                 TypePreferIP    `json:"preferIp"`
	DomainFrontingPort       TypePort        `json:"domainFrontingPort"`
	TolerateTimeSkewness     TypeDuration    `json:"tolerateTimeSkewness"`
	Concurrency              TypeConcurrency `json:"concurrency"`
	Defense                  struct {
		AntiReplay struct {
			Optional

			MaxSize   TypeBytes     `json:"maxSize"`
			ErrorRate TypeErrorRate `json:"errorRate"`
		} `json:"antiReplay"`
		Blocklist ListConfig `json:"blocklist"`
		Allowlist ListConfig `json:"allowlist"`
	} `json:"defense"`
	Network struct {
		Timeout struct {
			TCP  TypeDuration `json:"tcp"`
			HTTP TypeDuration `json:"http"`
			Idle TypeDuration `json:"idle"`
		} `json:"timeout"`
		DOHIP   TypeIP         `json:"dohIp"`
		DNSMode TypeDNSMode    `json:"dnsMode"`
		Proxies []TypeProxyURL `json:"proxies"`
		// TCPFastOpen включает TCP Fast Open на listener и исходящих соединениях.
		// TFO экономит 1×RTT на первом соединении (~50-100ms).
		// Требует поддержки ядром (net.ipv4.tcp_fastopen >= 3).
		// Default: false (для обратной совместимости)
		TCPFastOpen TypeBool `json:"tcpFastOpen"`
	} `json:"network"`
	// ConnectionPool — настройки пула соединений к Telegram DC.
	// Переиспользование соединений снижает latency на 30-50ms.
	ConnectionPool struct {
		Optional

		// MaxIdleConns — максимальное количество idle соединений на DC.
		// Default: 5
		MaxIdleConns TypeConcurrency `json:"maxIdleConns"`

		// IdleTimeout — таймаут простоя для соединений в пуле.
		// Default: 1m
		IdleTimeout TypeDuration `json:"idleTimeout"`
	} `json:"connectionPool"`
	// RateLimit — ограничение количества handshakes на IP.
	// Защищает от brute-force подбора секрета.
	RateLimit struct {
		Optional

		// PerSecond — максимальное количество handshakes в секунду на IP.
		// Default: 0 (отключено)
		PerSecond TypeRateLimit `json:"perSecond"`

		// Burst — максимальный burst для rate limiter.
		// Default: 20
		Burst TypeConcurrency `json:"burst"`
	} `json:"rateLimit"`
	Stats struct {
		StatsD struct {
			Optional

			Address      TypeHostPort        `json:"address"`
			MetricPrefix TypeMetricPrefix    `json:"metricPrefix"`
			TagFormat    TypeStatsdTagFormat `json:"tagFormat"`
		} `json:"statsd"`
		Prometheus struct {
			Optional

			BindTo       TypeHostPort     `json:"bindTo"`
			HTTPPath     TypeHTTPPath     `json:"httpPath"`
			MetricPrefix TypeMetricPrefix `json:"metricPrefix"`
		} `json:"prometheus"`
	} `json:"stats"`
}

func (c *Config) Validate() error {
	if !c.Secret.Valid() {
		return fmt.Errorf("invalid secret")
	}

	if c.BindTo.Get("") == "" {
		return fmt.Errorf("incorrect bind-to parameter %s", c.BindTo.String())
	}

	return nil
}

func (c *Config) String() string {
	// Маскируем секрет для безопасного логирования
	safe := *c
	safe.Secret = mtglib.Secret{} // Zero value — не сериализует реальный секрет

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)

	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(safe); err != nil {
		return "{}"
	}

	return buf.String()
}
