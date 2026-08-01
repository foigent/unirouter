package mediajob

import (
	"github.com/uniroute/unirouter/internal/auth"
)

type ApplicationCipher struct {
	key []byte
}

func NewApplicationCipher(key []byte) (*ApplicationCipher, error) {
	if len(key) != 32 {
		return nil, domainError(ErrUnavailable, "payload_cipher_invalid")
	}
	return &ApplicationCipher{key: append([]byte(nil), key...)}, nil
}

func (c *ApplicationCipher) Seal(plaintext []byte) (string, error) {
	if c == nil || len(c.key) != 32 || len(plaintext) == 0 {
		return "", domainError(ErrUnavailable, "payload_cipher_unavailable")
	}
	value, err := auth.EncryptKey(plaintext, c.key)
	if err != nil {
		return "", domainError(ErrUnavailable, "payload_encrypt_failed")
	}
	return value, nil
}

func (c *ApplicationCipher) Open(ciphertext string) ([]byte, error) {
	if c == nil || len(c.key) != 32 || ciphertext == "" {
		return nil, domainError(ErrUnavailable, "payload_cipher_unavailable")
	}
	value, err := auth.DecryptKey(ciphertext, c.key)
	if err != nil {
		return nil, domainError(ErrUnavailable, "payload_decrypt_failed")
	}
	return []byte(value), nil
}
