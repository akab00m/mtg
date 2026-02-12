package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type TypeBlocklistURI struct {
	Value string
}

func (t *TypeBlocklistURI) Set(value string) error {
	if stat, err := os.Stat(value); err == nil || os.IsExist(err) {
		switch {
		case stat.IsDir():
			return fmt.Errorf("value is correct filepath but directory")
		case stat.Mode().Perm()&0o400 == 0:
			return fmt.Errorf("value is correct filepath but not readable")
		}

		value, err = filepath.Abs(value)
		if err != nil {
			return fmt.Errorf(
				"value is correct filepath but cannot resolve absolute (%s): %w",
				value, err)
		}

		t.Value = value

		return nil
	}

	parsedURL, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("incorrect url (%s): %w", value, err)
	}

	switch parsedURL.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("unknown schema %s (%s)", parsedURL.Scheme, value)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("incorrect url %s", value)
	}

	if parsedURL.User != nil {
		return fmt.Errorf("credentials in url are not allowed (%s)", value)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("incorrect host in url %s", value)
	}

	if port := parsedURL.Port(); port != "" {
		portNo, err := strconv.Atoi(port)
		if err != nil || portNo <= 0 || portNo > 65535 {
			return fmt.Errorf("incorrect port in url %s", value)
		}
	}

	if isBlockedRemoteHost(hostname) {
		return fmt.Errorf("blocked host in url %s", value)
	}

	t.Value = parsedURL.String()

	return nil
}

func (t TypeBlocklistURI) Get(defaultValue string) string {
	if t.Value == "" {
		return defaultValue
	}

	return t.Value
}

func isBlockedRemoteHost(hostname string) bool {
	host := strings.ToLower(strings.TrimSpace(hostname))

	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast()
}

func (t TypeBlocklistURI) IsRemote() bool {
	return !filepath.IsAbs(t.Value)
}

func (t *TypeBlocklistURI) UnmarshalText(data []byte) error {
	return t.Set(string(data))
}

func (t TypeBlocklistURI) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t TypeBlocklistURI) String() string {
	return t.Value
}
