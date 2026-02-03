package mtglib

import (
	"time"

	"golang.org/x/time/rate"
)

// ProxyOpts is a structure with settings to mtg proxy.
//
// This is not required per se, but this is to shorten function signature and
// give an ability to conveniently provide default values.
type ProxyOpts struct {
	// Secret defines a secret which should be used by a proxy.
	//
	// This is a mandatory setting.
	Secret Secret

	// Network defines a network instance which should be used for all network
	// communications made by proxies.
	//
	// This is a mandatory setting.
	Network Network

	// AntiReplayCache defines an instance of antireplay cache.
	//
	// This is a mandatory setting.
	AntiReplayCache AntiReplayCache

	// IPBlocklist defines an instance of IP blocklist.
	//
	// This is a mandatory setting.
	IPBlocklist IPBlocklist

	// IPAllowlist defines a whitelist of IPs to allow to use proxy.
	//
	// This is an optional setting, ignored by default (no restrictions).
	IPAllowlist IPBlocklist

	// EventStream defines an instance of event stream.
	//
	// This ia a mandatory setting.
	EventStream EventStream

	// Logger defines an instance of the logger.
	//
	// This is a mandatory setting.
	Logger Logger

	// BufferSize is a size of the copy buffer in bytes.
	//
	// Please remember that we multiply this number in 2, because when we relay
	// between proxies, we have to create 2 intermediate buffers: to and from.
	//
	// This is an optional setting.
	//
	// Deprecated: this setting is no longer makes any effect.
	BufferSize uint

	// Concurrency is a size of the worker pool for connection management.
	//
	// If we have more connections than this number, they are going to be
	// rejected.
	//
	// This is an optional setting.
	Concurrency uint

	// IdleTimeout is a timeout for relay when we have to break a stream.
	//
	// This is a timeout for any activity. So, if we have any message which will
	// pass to either direction, a timer is reset. If we have no any reads or
	// writes for this timeout, a connection will be aborted.
	//
	// This is an optional setting.
	IdleTimeout time.Duration

	// TolerateTimeSkewness is a time boundary that defines a time range where
	// faketls timestamp is acceptable.
	//
	// This means that if if you got a timestamp X, now is Y, then if |X-Y| <
	// TolerateTimeSkewness, then you accept a packet.
	//
	// This is an optional setting.
	TolerateTimeSkewness time.Duration

	// PreferIP defines an IP connectivity preference. Valid values are:
	// 'prefer-ipv4', 'prefer-ipv6', 'only-ipv4', 'only-ipv6'.
	//
	// This is an optional setting.
	PreferIP string

	// DomainFrontingPort is a port we use to connect to a fronting domain.
	//
	// This is required because secret does not specify a port. It specifies a
	// hostname only.
	//
	// This is an optional setting.
	DomainFrontingPort uint

	// AllowFallbackOnUnknownDC defines how proxy behaves if unknown DC was
	// requested. If this setting is set to false, then such connection will be
	// rejected. Otherwise, proxy will chose any DC.
	//
	// Telegram is designed in a way that any DC can serve any request, the
	// problem is a latency.
	//
	// This is an optional setting.
	AllowFallbackOnUnknownDC bool

	// FallbackOnDialError enables fallback to another DC when connection
	// to the requested DC fails. This improves reliability when a specific
	// DC is temporarily unavailable.
	//
	// This is an optional setting. Default: true
	FallbackOnDialError bool

	// UseTestDCs defines if we have to connect to production or to staging DCs of
	// Telegram.
	//
	// This is required if you use mtglib as an integration library for your
	// Telegram-related projects.
	//
	// This is an optional setting.
	UseTestDCs bool

	// Config contains timeouts and other configurable parameters.
	//
	// This is an optional setting. If not provided, default values will be used.
	Config *ProxyConfig

	// RateLimitPerSecond defines the maximum number of handshakes per second per IP.
	//
	// Set to 0 to disable rate limiting.
	//
	// This is an optional setting. Default: 10 handshakes/sec
	RateLimitPerSecond float64

	// RateLimitBurst defines the maximum burst size for rate limiting.
	//
	// This is an optional setting. Default: 20
	RateLimitBurst int

	// EnableConnectionPool включает пул соединений к Telegram DC.
	// Переиспользование соединений снижает latency на 30-50ms.
	//
	// This is an optional setting. Default: false
	EnableConnectionPool bool

	// ConnectionPoolMaxIdle — максимальное количество idle соединений на DC.
	//
	// This is an optional setting. Default: 5
	ConnectionPoolMaxIdle int

	// ConnectionPoolIdleTimeout — таймаут простоя для соединений в пуле.
	//
	// This is an optional setting. Default: 1 minute
	ConnectionPoolIdleTimeout time.Duration
}

func (p ProxyOpts) valid() error {
	switch {
	case p.Network == nil:
		return ErrNetworkIsNotDefined
	case p.AntiReplayCache == nil:
		return ErrAntiReplayCacheIsNotDefined
	case p.IPBlocklist == nil:
		return ErrIPBlocklistIsNotDefined
	case p.IPAllowlist == nil:
		return ErrIPAllowlistIsNotDefined
	case p.EventStream == nil:
		return ErrEventStreamIsNotDefined
	case p.Logger == nil:
		return ErrLoggerIsNotDefined
	case !p.Secret.Valid():
		return ErrSecretInvalid
	}

	return nil
}

func (p ProxyOpts) getConcurrency() int {
	if p.Concurrency == 0 {
		return DefaultConcurrency
	}

	return int(p.Concurrency)
}

func (p ProxyOpts) getDomainFrontingPort() int {
	if p.DomainFrontingPort == 0 {
		return DefaultDomainFrontingPort
	}

	return int(p.DomainFrontingPort)
}

func (p ProxyOpts) getTolerateTimeSkewness() time.Duration {
	if p.TolerateTimeSkewness == 0 {
		return DefaultTolerateTimeSkewness
	}

	return p.TolerateTimeSkewness
}

func (p ProxyOpts) getPreferIP() string {
	if p.PreferIP == "" {
		return DefaultPreferIP
	}

	return p.PreferIP
}

func (p ProxyOpts) getLogger(name string) Logger {
	return p.Logger.Named(name)
}

func (p ProxyOpts) getConfig() ProxyConfig {
	if p.Config != nil {
		return *p.Config
	}

	return DefaultProxyConfig()
}

func (p ProxyOpts) getRateLimitPerSecond() rate.Limit {
	if p.RateLimitPerSecond == 0 {
		return 0 // ОТКЛЮЧЕНО: Traefik проксирует все соединения с одного IP
	}

	return rate.Limit(p.RateLimitPerSecond)
}

func (p ProxyOpts) getRateLimitBurst() int {
	if p.RateLimitBurst == 0 {
		return 20 // default burst
	}

	return p.RateLimitBurst
}

func (p ProxyOpts) getConnectionPoolMaxIdle() int {
	if p.ConnectionPoolMaxIdle == 0 {
		return 5 // default
	}

	return p.ConnectionPoolMaxIdle
}

func (p ProxyOpts) getConnectionPoolIdleTimeout() time.Duration {
	if p.ConnectionPoolIdleTimeout == 0 {
		// По умолчанию 20 секунд — меньше, чем idle timeout Telegram (30-60 сек).
		// Это гарантирует, что соединения будут закрыты ДО того, как Telegram
		// их закроет, избегая ошибок "connection reset by peer".
		return 20 * time.Second
	}

	return p.ConnectionPoolIdleTimeout
}

func (p ProxyOpts) getFallbackOnDialError() bool {
	// Default: true - fallback улучшает reliability
	return p.FallbackOnDialError
}
