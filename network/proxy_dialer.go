package network

import (
	"net/url"
	"strconv"
	"time"
)

func newProxyDialer(baseDialer Dialer, proxyURL *url.URL) Dialer {
	params := proxyURL.Query()

	var (
		openThreshold    uint32 = ProxyDialerOpenThreshold
		reconnectTimeout        = ProxyDialerReconnectTimeout
	)

	if param := params.Get("open_threshold"); param != "" {
		if intNum, err := strconv.ParseUint(param, 10, 32); err == nil { //nolint: gomnd
			openThreshold = uint32(intNum)
		}
	}

	// Основной параметр — reconnect_timeout. Для обратной совместимости
	// также принимаем устаревший half_open_timeout.
	if param := params.Get("reconnect_timeout"); param != "" {
		if dur, err := time.ParseDuration(param); err == nil && dur > 0 {
			reconnectTimeout = dur
		}
	} else if param := params.Get("half_open_timeout"); param != "" {
		if dur, err := time.ParseDuration(param); err == nil && dur > 0 {
			reconnectTimeout = dur
		}
	}

	return newCooldownDialer(baseDialer, openThreshold, reconnectTimeout)
}
