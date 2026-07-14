package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("public key is empty")
	}
	var lengthErr error
	for _, decoder := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		hex.DecodeString,
	} {
		decoded, err := decoder(value)
		if err != nil {
			continue
		}
		if len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
		lengthErr = fmt.Errorf("public key decoded to %d bytes, want %d", len(decoded), ed25519.PublicKeySize)
	}
	if lengthErr != nil {
		return nil, lengthErr
	}
	return nil, fmt.Errorf("public key must be base64 or hex encoded")
}

// ParsePublicKeyRing accepts one strict JSON object mapping stable key IDs to
// Ed25519 public keys. The fieldName is used only to make operator errors clear.
func ParsePublicKeyRing(value, fieldName string) (map[string]ed25519.PublicKey, error) {
	if strings.TrimSpace(fieldName) == "" {
		fieldName = "public-key ring"
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]ed25519.PublicKey{}, nil
	}
	var encoded map[string]string
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&encoded); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", fieldName, err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, fmt.Errorf("%s must contain one JSON object: %w", fieldName, err)
	}
	if len(encoded) == 0 {
		return nil, fmt.Errorf("%s must contain at least one key", fieldName)
	}
	if len(encoded) > 32 {
		return nil, fmt.Errorf("%s must contain at most 32 keys", fieldName)
	}
	ring := make(map[string]ed25519.PublicKey, len(encoded))
	for rawKeyID, rawKey := range encoded {
		keyID := strings.TrimSpace(rawKeyID)
		if !ValidKeyID(keyID) {
			return nil, fmt.Errorf("%s contains invalid key id %q", fieldName, rawKeyID)
		}
		parsed, err := ParsePublicKey(rawKey)
		if err != nil {
			return nil, fmt.Errorf("%s[%s]: %w", fieldName, keyID, err)
		}
		ring[keyID] = parsed
	}
	return ring, nil
}

func ParsePrivateKey(value string) (ed25519.PrivateKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("private key is empty")
	}
	var lengthErr error
	for _, decoder := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		hex.DecodeString,
	} {
		decoded, err := decoder(value)
		if err != nil {
			continue
		}
		switch len(decoded) {
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(decoded), nil
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(decoded), nil
		default:
			lengthErr = fmt.Errorf("private key decoded to %d bytes, want %d-byte seed or %d-byte private key", len(decoded), ed25519.SeedSize, ed25519.PrivateKeySize)
		}
	}
	if lengthErr != nil {
		return nil, lengthErr
	}
	return nil, fmt.Errorf("private key must be base64 or hex encoded")
}

func EncodePrivateKey(key ed25519.PrivateKey) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes", ed25519.PrivateKeySize)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func EncodePublicKey(key ed25519.PublicKey) (string, error) {
	if len(key) != ed25519.PublicKeySize {
		return "", fmt.Errorf("public key must be %d bytes", ed25519.PublicKeySize)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func ValidKeyID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
