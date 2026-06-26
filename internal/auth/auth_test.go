package auth

import (
	"strings"
	"testing"
)

func TestHashAPIKey_Deterministic(t *testing.T) {
	key := "ur-test-abcdef1234567890"
	secret := "test-secret"
	h1 := HashAPIKey(key, secret)
	h2 := HashAPIKey(key, secret)
	if h1 != h2 {
		t.Fatalf("HashAPIKey not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex, got %d chars", len(h1))
	}
}

func TestHashAPIKey_DifferentKeys(t *testing.T) {
	secret := "test-secret"
	h1 := HashAPIKey("key-a", secret)
	h2 := HashAPIKey("key-b", secret)
	if h1 == h2 {
		t.Fatal("different keys should produce different hashes")
	}
}

func TestHashAPIKey_DifferentSecrets(t *testing.T) {
	key := "ur-test-same"
	h1 := HashAPIKey(key, "secret-a")
	h2 := HashAPIKey(key, "secret-b")
	if h1 == h2 {
		t.Fatal("different secrets should produce different hashes")
	}
}

func TestVerifyAPIKey(t *testing.T) {
	raw := "ur-test-abcdef1234567890"
	secret := "my-secret"
	hash := HashAPIKey(raw, secret)

	if !VerifyAPIKey(raw, hash, secret) {
		t.Fatal("VerifyAPIKey should return true for matching key")
	}
	if VerifyAPIKey("wrong-key", hash, secret) {
		t.Fatal("VerifyAPIKey should return false for wrong key")
	}
	if VerifyAPIKey(raw, hash, "wrong-secret") {
		t.Fatal("VerifyAPIKey should return false for wrong secret")
	}
}

func TestGenerateAPIKey_Format(t *testing.T) {
	raw, prefix, err := GenerateAPIKey(1)
	if err != nil {
		t.Fatalf("GenerateAPIKey error: %v", err)
	}
	if !strings.HasPrefix(raw, "ur-0001-") {
		t.Fatalf("expected prefix ur-test-, got %s", raw)
	}
	// raw = "ur-test-<32 hex chars>" = 8 + 32 = 40 chars
	if len(raw) != 40 {
		t.Fatalf("expected 40-char key, got %d: %s", len(raw), raw)
	}
	if !strings.HasPrefix(prefix, "ur-0001-") {
		t.Fatalf("prefix should start with ur-test-, got %s", prefix)
	}
}

func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		raw, _, err := GenerateAPIKey(1)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if keys[raw] {
			t.Fatalf("duplicate key generated: %s", raw)
		}
		keys[raw] = true
	}
}

func TestEncryptDecryptKey_Roundtrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	plaintext := "sk-proj-abc123def456"

	encrypted, err := EncryptKey([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("EncryptKey error: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("encrypted should differ from plaintext")
	}

	decrypted, err := DecryptKey(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptKey error: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("decrypt mismatch: got %s, want %s", decrypted, plaintext)
	}
}

func TestEncryptKey_DifferentCiphertexts(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plaintext := "same-plaintext"

	e1, _ := EncryptKey([]byte(plaintext), key)
	e2, _ := EncryptKey([]byte(plaintext), key)
	if e1 == e2 {
		t.Fatal("AES-GCM should produce different ciphertexts (random nonce)")
	}
}

func TestDecryptKey_WrongKey(t *testing.T) {
	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("fedcba9876543210fedcba9876543210")
	plaintext := "secret-data"

	encrypted, err := EncryptKey([]byte(plaintext), key1)
	if err != nil {
		t.Fatalf("EncryptKey error: %v", err)
	}

	_, err = DecryptKey(encrypted, key2)
	if err == nil {
		t.Fatal("DecryptKey with wrong key should return error")
	}
}

func TestJWT_GenerateAndParse(t *testing.T) {
	secret := "jwt-test-secret"
	userID := int64(42)
	orgID := int64(7)
	role := "admin"

	token, err := GenerateJWT(userID, orgID, role, secret, 24)
	if err != nil {
		t.Fatalf("GenerateJWT error: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}

	claims, err := ParseJWT(token, secret)
	if err != nil {
		t.Fatalf("ParseJWT error: %v", err)
	}
	if claims.UserID != userID {
		t.Fatalf("UserID mismatch: got %d, want %d", claims.UserID, userID)
	}
	if claims.OrgID != orgID {
		t.Fatalf("OrgID mismatch: got %d, want %d", claims.OrgID, orgID)
	}
	if claims.Role != role {
		t.Fatalf("Role mismatch: got %s, want %s", claims.Role, role)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	token, _ := GenerateJWT(1, 1, "admin", "secret-a", 24)
	_, err := ParseJWT(token, "secret-b")
	if err == nil {
		t.Fatal("ParseJWT with wrong secret should fail")
	}
}

func TestJWT_InvalidToken(t *testing.T) {
	_, err := ParseJWT("not.a.valid.jwt", "secret")
	if err == nil {
		t.Fatal("ParseJWT with invalid token should fail")
	}
}
