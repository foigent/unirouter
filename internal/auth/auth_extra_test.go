package auth

import (
	"strings"
	"testing"
)

// --- GenerateAPIKey edge cases ---

func TestGenerateAPIKey_LargeOrgID(t *testing.T) {
	rawKey, _, err := GenerateAPIKey(99999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 99999 % 10000 = 9999
	if !strings.HasPrefix(rawKey, "ur-9999-") {
		t.Fatalf("expected prefix ur-9999-, got %s", rawKey)
	}
}

func TestGenerateAPIKey_ZeroOrgID(t *testing.T) {
	rawKey, _, err := GenerateAPIKey(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(rawKey, "ur-0000-") {
		t.Fatalf("expected prefix ur-0000-, got %s", rawKey)
	}
}

func TestGenerateAPIKey_HexChars(t *testing.T) {
	rawKey, _, _ := GenerateAPIKey(1)
	hexPart := rawKey[8:] // skip "ur-XXXX-"
	for _, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex char %c in key: %s", c, rawKey)
		}
	}
}

// --- Permission helper tests ---

func TestIsPlatformAdmin(t *testing.T) {
	if !IsPlatformAdmin("platform_admin") {
		t.Fatal("platform_admin should be platform admin")
	}
	if IsPlatformAdmin("org_admin") {
		t.Fatal("org_admin should not be platform admin")
	}
	if IsPlatformAdmin("org_viewer") {
		t.Fatal("org_viewer should not be platform admin")
	}
	if IsPlatformAdmin("") {
		t.Fatal("empty should not be platform admin")
	}
}

func TestIsOrgAdmin(t *testing.T) {
	if !IsOrgAdmin("platform_admin") {
		t.Fatal("platform_admin should be org admin")
	}
	if !IsOrgAdmin("org_admin") {
		t.Fatal("org_admin should be org admin")
	}
	if IsOrgAdmin("org_viewer") {
		t.Fatal("org_viewer should not be org admin")
	}
	if IsOrgAdmin("") {
		t.Fatal("empty should not be org admin")
	}
}

func TestCanWrite(t *testing.T) {
	if !CanWrite("platform_admin") {
		t.Fatal("platform_admin should be able to write")
	}
	if !CanWrite("org_admin") {
		t.Fatal("org_admin should be able to write")
	}
	if CanWrite("org_viewer") {
		t.Fatal("org_viewer should not be able to write")
	}
	if CanWrite("") {
		t.Fatal("empty should not be able to write")
	}
}

// --- Encrypt/Decrypt edge cases ---

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypted, err := EncryptKey([]byte(""), key)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	decrypted, err := DecryptKey(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if decrypted != "" {
		t.Fatalf("expected empty, got %s", decrypted)
	}
}

func TestEncryptDecrypt_LongPlaintext(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plaintext := strings.Repeat("sk-test-", 100)
	encrypted, err := EncryptKey([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	decrypted, err := DecryptKey(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if decrypted != plaintext {
		t.Fatal("decrypted text doesn't match original")
	}
}

func TestEncryptDecrypt_SpecialChars(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plaintext := "key with 特殊字符 and \n\t special!@#$%"
	encrypted, err := EncryptKey([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	decrypted, err := DecryptKey(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestDecryptKey_InvalidHex(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	_, err := DecryptKey("not-valid-hex", key)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestDecryptKey_TooShortCiphertext(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	_, err := DecryptKey("aabb", key)
	if err == nil {
		t.Fatal("expected error for too-short ciphertext")
	}
}

// --- VerifyAPIKey edge cases ---

func TestVerifyAPIKey_CorrectMatch(t *testing.T) {
	rawKey := "ur-0001-abcdef1234567890abcdef1234567890"
	secret := "test-secret"
	hash := HashAPIKey(rawKey, secret)
	if !VerifyAPIKey(rawKey, hash, secret) {
		t.Fatal("VerifyAPIKey should return true for matching key")
	}
}

func TestVerifyAPIKey_WrongKey(t *testing.T) {
	secret := "test-secret"
	hash := HashAPIKey("ur-0001-aaaa", secret)
	if VerifyAPIKey("ur-0001-bbbb", hash, secret) {
		t.Fatal("VerifyAPIKey should return false for non-matching key")
	}
}

func TestVerifyAPIKey_WrongSecret(t *testing.T) {
	rawKey := "ur-0001-abcdef1234567890abcdef1234567890"
	hash := HashAPIKey(rawKey, "secret1")
	if VerifyAPIKey(rawKey, hash, "secret2") {
		t.Fatal("VerifyAPIKey should return false with wrong secret")
	}
}

func TestHashAPIKey_HexLength(t *testing.T) {
	hash := HashAPIKey("any-key", "any-secret")
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hex, got %d", len(hash))
	}
}

// --- JWT edge cases ---

func TestGenerateJWT_ExpiredToken(t *testing.T) {
	token, err := GenerateJWT(1, 1, "admin", "secret", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ParseJWT(token, "secret")
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestGenerateJWT_DifferentRoles(t *testing.T) {
	roles := []string{"platform_admin", "org_admin", "org_viewer"}
	for _, role := range roles {
		token, err := GenerateJWT(1, 1, role, "secret", 1)
		if err != nil {
			t.Fatalf("role %s: unexpected error: %v", role, err)
		}
		claims, err := ParseJWT(token, "secret")
		if err != nil {
			t.Fatalf("role %s: parse error: %v", role, err)
		}
		if claims.Role != role {
			t.Fatalf("expected role %s, got %s", role, claims.Role)
		}
	}
}

func TestParseJWT_InvalidFormat(t *testing.T) {
	_, err := ParseJWT("not.a.jwt", "secret")
	if err == nil {
		t.Fatal("expected error for invalid JWT format")
	}
}

func TestParseJWT_EmptyToken(t *testing.T) {
	_, err := ParseJWT("", "secret")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}
