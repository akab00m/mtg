package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DNSMode represents the DNS resolution mode
type DNSMode int

const (
	// DNSModeDoH uses DNS-over-HTTPS (default, more secure)
	DNSModeDoH DNSMode = iota
	// DNSModePlain uses plain DNS (faster, less secure)
	DNSModePlain
)

// TypeDNSMode is a config wrapper for DNSMode
type TypeDNSMode struct {
	value DNSMode
}

// Get returns the DNS mode, or default if not set
func (t TypeDNSMode) Get(defaultValue DNSMode) DNSMode {
	if t.value == 0 {
		return defaultValue
	}
	return t.value
}

// Value returns the raw value
func (t TypeDNSMode) Value() DNSMode {
	return t.value
}

// String returns string representation
func (t TypeDNSMode) String() string {
	switch t.value {
	case DNSModeDoH:
		return "doh"
	case DNSModePlain:
		return "plain"
	default:
		return "doh"
	}
}

// UnmarshalText implements encoding.TextUnmarshaler
func (t *TypeDNSMode) UnmarshalText(data []byte) error {
	return t.parse(string(data))
}

// UnmarshalJSON implements json.Unmarshaler
func (t *TypeDNSMode) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("cannot parse dns_mode: %w", err)
	}
	return t.parse(str)
}

// MarshalJSON implements json.Marshaler
func (t TypeDNSMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func (t *TypeDNSMode) parse(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "doh", "dns-over-https", "":
		t.value = DNSModeDoH
	case "plain", "system", "standard":
		t.value = DNSModePlain
	default:
		return fmt.Errorf("unknown dns_mode %q, expected 'doh' or 'plain'", value)
	}
	return nil
}
