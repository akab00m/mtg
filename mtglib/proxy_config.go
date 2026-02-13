package mtglib

import "time"

// ProxyConfig contains configurable parameters for Proxy.
type ProxyConfig struct {
	// HandshakeTimeout is the timeout for completing the handshake process.
	// Default: 30 seconds
	HandshakeTimeout time.Duration

	// TelegramDialTimeout is the timeout for dialing to Telegram servers.
	// Default: 10 seconds
	TelegramDialTimeout time.Duration
}

// DefaultProxyConfig returns default configuration for Proxy.
func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		HandshakeTimeout:    30 * time.Second,
		TelegramDialTimeout: 10 * time.Second,
	}
}
