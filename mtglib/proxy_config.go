package mtglib

import "time"

// ProxyConfig contains configurable parameters for Proxy.
type ProxyConfig struct {
	// HandshakeTimeout is the timeout for completing the handshake process.
	// Default: 30 seconds
	HandshakeTimeout time.Duration

	// ConnectionReadTimeout is the timeout for read operations on connections.
	// Default: 5 minutes
	ConnectionReadTimeout time.Duration

	// ConnectionWriteTimeout is the timeout for write operations on connections.
	// Default: 5 minutes
	ConnectionWriteTimeout time.Duration

	// TelegramDialTimeout is the timeout for dialing to Telegram servers.
	// Default: 10 seconds
	TelegramDialTimeout time.Duration
}

// DefaultProxyConfig returns default configuration for Proxy.
func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		HandshakeTimeout:       30 * time.Second,
		ConnectionReadTimeout:  5 * time.Minute,
		ConnectionWriteTimeout: 5 * time.Minute,
		TelegramDialTimeout:    10 * time.Second,
	}
}
