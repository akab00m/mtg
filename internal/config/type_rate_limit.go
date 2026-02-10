package config

import (
	"fmt"
	"strconv"
)

// TypeRateLimit — количество запросов в секунду (uint, 0 = отключено).
type TypeRateLimit struct {
	Value uint
}

func (t *TypeRateLimit) Set(value string) error {
	v, err := strconv.ParseUint(value, 10, 16) //nolint: gomnd
	if err != nil {
		return fmt.Errorf("value is not uint (%s): %w", value, err)
	}

	t.Value = uint(v)

	return nil
}

func (t TypeRateLimit) Get(defaultValue uint) uint {
	if t.Value == 0 {
		return defaultValue
	}

	return t.Value
}

func (t *TypeRateLimit) UnmarshalJSON(data []byte) error {
	return t.Set(string(data))
}

func (t TypeRateLimit) MarshalJSON() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t TypeRateLimit) String() string {
	return strconv.FormatUint(uint64(t.Value), 10) //nolint: gomnd
}
