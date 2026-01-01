package obfuscated2

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

func makeAesCtr(key, iv []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cannot create AES cipher: %w", err)
	}

	return cipher.NewCTR(block, iv), nil
}
