package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
		t.Fatalf("expected prefix ur-0001-, got %s", raw)
	}
	// raw = "ur-0001-<32 hex chars>" = 8 + 32 = 40 chars
	if len(raw) != 40 {
		t.Fatalf("expected 40-char key, got %d: %s", len(raw), raw)
	}
	if !strings.HasPrefix(prefix, "ur-0001-") {
		t.Fatalf("prefix should start with ur-0001-, got %s", prefix)
	}
	if len(prefix) != 20 {
		t.Fatalf("expected 20-char display prefix, got %d: %s", len(prefix), prefix)
	}
	if !strings.HasPrefix(raw, prefix) {
		t.Fatalf("raw key should start with its display prefix: raw=%s prefix=%s", raw, prefix)
	}
	if _, err := hex.DecodeString(prefix[8:]); err != nil {
		t.Fatalf("display prefix random segment should be 12 hex characters: %s", prefix)
	}
}

func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	keys := make(map[string]bool)
	prefixes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		raw, prefix, err := GenerateAPIKey(1)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if keys[raw] {
			t.Fatalf("duplicate key generated: %s", raw)
		}
		keys[raw] = true
		if prefixes[prefix] {
			t.Fatalf("duplicate display prefix generated: %s", prefix)
		}
		prefixes[prefix] = true
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

	token, session, err := GenerateAdminSessionJWT(userID, orgID, RolePlatformAdmin, role, 3, secret, 24)
	if err != nil {
		t.Fatalf("GenerateJWT error: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if len(session.SIDHash) != 32 {
		t.Fatal("session should expose only its SHA-256 persistence key")
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
	if claims.SchemaVersion != adminJWTVersion || claims.OrganizationRole != RolePlatformAdmin || claims.EffectiveOrganizationRole() != RolePlatformAdmin {
		t.Fatalf("v3 organization-role claims mismatch: %#v", claims)
	}
	if claims.AuthEpoch != 3 || !bytes.Equal(session.SIDHash, HashAdminSessionID(claims.SessionID)) {
		t.Fatalf("session claims mismatch: %#v", claims)
	}
}

func TestJWT_V2CompatibilityDerivesOrganizationRoleWithoutTrustingProjectShadow(t *testing.T) {
	for _, test := range []struct {
		role string
		want string
	}{
		{role: RoleOrgAdmin, want: RoleOrgAdmin},
		{role: RoleBillingAdmin, want: RoleBillingAdmin},
		{role: RoleOrgViewer, want: RoleOrgViewer},
		{role: RoleProjectAdmin, want: RoleMember},
		{role: RoleDeveloper, want: RoleMember},
		{role: RoleViewer, want: RoleMember},
	} {
		t.Run(test.role, func(t *testing.T) {
			token := version2AdminJWTForTest(t, test.role, "v2-secret")
			claims, err := ParseJWT(token, "v2-secret")
			if err != nil {
				t.Fatalf("ParseJWT v2: %v", err)
			}
			if claims.SchemaVersion != adminJWTLegacyVersion || claims.OrganizationRole != "" || claims.EffectiveOrganizationRole() != test.want {
				t.Fatalf("v2 compatibility claims = %#v, want organization role %q", claims, test.want)
			}
		})
	}
}

func TestJWT_V3RejectsMismatchedOrganizationRole(t *testing.T) {
	token, _, err := GenerateAdminSessionJWT(1, 1, RoleOrgAdmin, RoleOrgAdmin, 0, "secret", 1)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := new(jwt.Parser).ParseUnverified(token, &Claims{})
	if err != nil {
		t.Fatal(err)
	}
	claims := parsed.Claims.(*Claims)
	claims.OrganizationRole = RoleMember
	tampered, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseJWT(tampered, "secret"); err == nil {
		t.Fatal("v3 JWT with organization_role inconsistent with its compatibility role was accepted")
	}
}

func TestGenerateAdminSessionJWTRejectsMismatchedRoleAxes(t *testing.T) {
	for _, test := range []struct {
		name             string
		organizationRole string
		compatibility    string
	}{
		{name: "org viewer with project admin shadow", organizationRole: RoleOrgViewer, compatibility: RoleProjectAdmin},
		{name: "member with organization admin shadow", organizationRole: RoleMember, compatibility: RoleOrgAdmin},
		{name: "noncanonical legacy organization role", organizationRole: RoleLegacyAdmin, compatibility: RoleLegacyAdmin},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := GenerateAdminSessionJWT(1, 1, test.organizationRole, test.compatibility, 0, "secret", 1); err == nil {
				t.Fatal("mismatched role axes must fail before generating bearer or session state")
			}
		})
	}

	for _, test := range []struct {
		organizationRole string
		compatibility    string
	}{
		{organizationRole: RolePlatformAdmin, compatibility: RoleLegacyAdmin},
		{organizationRole: RoleMember, compatibility: RoleProjectAdmin},
		{organizationRole: RoleMember, compatibility: RoleDeveloper},
		{organizationRole: RoleMember, compatibility: RoleViewer},
		{organizationRole: RoleOrgViewer, compatibility: RoleOrgViewer},
	} {
		if _, _, err := GenerateAdminSessionJWT(1, 1, test.organizationRole, test.compatibility, 0, "secret", 1); err != nil {
			t.Fatalf("valid axes %q/%q rejected: %v", test.organizationRole, test.compatibility, err)
		}
	}
}

func version2AdminJWTForTest(t *testing.T, role, secret string) string {
	t.Helper()
	now := time.Now().UTC()
	sid := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, adminSessionIDBytes))
	claims := Claims{
		UserID: 1, OrgID: 1, Role: role, SessionID: sid, AuthEpoch: 0,
		SchemaVersion: adminJWTLegacyVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "uniroute", Subject: "1",
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestJWT_WrongSecret(t *testing.T) {
	token, _, _ := GenerateAdminSessionJWT(1, 1, RolePlatformAdmin, "admin", 0, "secret-a", 24)
	_, err := ParseJWT(token, "secret-b")
	if err == nil {
		t.Fatal("ParseJWT with wrong secret should fail")
	}
}

func TestJWT_LegacyStatelessTokenRejected(t *testing.T) {
	token, err := legacyStatelessJWTForTest(1, 1, "admin", "secret", 24)
	if err != nil {
		t.Fatalf("generate legacy token: %v", err)
	}
	if _, err := ParseJWT(token, "secret"); err == nil {
		t.Fatal("legacy JWT without a versioned sid must fail closed")
	}
}

func legacyStatelessJWTForTest(userID, orgID int64, role, secret string, expireHours int) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		UserID: userID,
		OrgID:  orgID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "uniroute",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func TestJWT_InvalidToken(t *testing.T) {
	_, err := ParseJWT("not.a.valid.jwt", "secret")
	if err == nil {
		t.Fatal("ParseJWT with invalid token should fail")
	}
}
