package telegram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDCConfig_ValidFull(t *testing.T) {
	config := &DCConfigFile{
		V4: map[string][]string{
			"1": {"149.154.175.50:443"},
			"2": {"149.154.167.51:443", "95.161.76.100:443"},
			"3": {"149.154.175.100:443"},
			"4": {"149.154.167.91:443"},
			"5": {"149.154.171.5:443"},
		},
		V6: map[string][]string{
			"1": {"[2001:b28:f23d:f001::a]:443"},
			"2": {"[2001:67c:04e8:f002::a]:443"},
		},
	}

	pool, err := parseDCConfig(config)
	require.NoError(t, err)

	assert.Len(t, pool.v4, 5, "должно быть 5 DC для IPv4")
	assert.Len(t, pool.v6, 5, "должно быть 5 DC для IPv6")

	// DC1 — один адрес
	assert.Len(t, pool.v4[0], 1)
	assert.Equal(t, "149.154.175.50:443", pool.v4[0][0].address)
	assert.Equal(t, "tcp4", pool.v4[0][0].network)

	// DC2 — два адреса
	assert.Len(t, pool.v4[1], 2)

	// IPv6 DC1
	assert.Len(t, pool.v6[0], 1)
	assert.Equal(t, "tcp6", pool.v6[0][0].network)

	// DC без IPv6 — пустой слайс
	assert.Empty(t, pool.v6[2], "DC3 без IPv6 должен быть пустым")
}

func TestParseDCConfig_EmptyV4(t *testing.T) {
	config := &DCConfigFile{V4: map[string][]string{}}

	_, err := parseDCConfig(config)
	assert.Error(t, err, "пустой v4 должен вызывать ошибку")
}

func TestParseDCConfig_NoValidDC(t *testing.T) {
	config := &DCConfigFile{
		V4: map[string][]string{
			"0":   {"1.2.3.4:443"},   // невалидный DC
			"6":   {"5.6.7.8:443"},   // невалидный DC
			"203": {"9.10.11.12:443"}, // невалидный DC
		},
	}

	_, err := parseDCConfig(config)
	assert.Error(t, err, "конфиг без валидных DC 1-5 должен вызывать ошибку")
}

func TestParseDCConfig_PartialDCs(t *testing.T) {
	config := &DCConfigFile{
		V4: map[string][]string{
			"1": {"149.154.175.50:443"},
			"3": {"149.154.175.100:443"},
		},
	}

	pool, err := parseDCConfig(config)
	require.NoError(t, err)

	assert.Len(t, pool.v4[0], 1, "DC1 должен быть заполнен")
	assert.Empty(t, pool.v4[1], "DC2 должен быть пустым")
	assert.Len(t, pool.v4[2], 1, "DC3 должен быть заполнен")
}

func TestParseDCNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1", 1},
		{"5", 5},
		{"0", 0},  // невалидный
		{"", 0},   // пустая строка
		{"10", 0}, // двузначное число
		{"a", 0},  // буква
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseDCNumber(tt.input))
		})
	}
}

func TestLoadDCConfig_FromFile(t *testing.T) {
	// Создаём временный JSON файл
	dir := t.TempDir()
	filePath := filepath.Join(dir, "dc_addresses.json")

	config := DCConfigFile{
		V4: map[string][]string{
			"1": {"149.154.175.50:443"},
			"2": {"149.154.167.51:443"},
		},
	}

	data, err := json.Marshal(config)
	require.NoError(t, err)

	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err)

	pool, err := loadDCConfig(filePath)
	require.NoError(t, err)
	assert.NotNil(t, pool)
	assert.Len(t, pool.v4[0], 1)
}

func TestLoadDCConfig_FileNotFound(t *testing.T) {
	_, err := loadDCConfig("/nonexistent/dc_addresses.json")
	assert.Error(t, err)
}

func TestLoadDCConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "bad.json")

	err := os.WriteFile(filePath, []byte("not json"), 0644)
	require.NoError(t, err)

	_, err = loadDCConfig(filePath)
	assert.Error(t, err)
}

func TestAddressPoolIsValidDC_WithRefreshedPool(t *testing.T) {
	// Проверяем что pool.isValidDC работает с refreshed pool
	pool := addressPool{
		v4: make([][]tgAddr, 5),
		v6: make([][]tgAddr, 5),
	}

	pool.v4[0] = []tgAddr{{network: "tcp4", address: "1.2.3.4:443"}}

	assert.True(t, pool.isValidDC(1))
	assert.True(t, pool.isValidDC(5))
	assert.False(t, pool.isValidDC(0))
	assert.False(t, pool.isValidDC(6))
	assert.False(t, pool.isValidDC(203))
}
