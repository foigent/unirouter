package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	RoleLegacyAdmin   = "admin"
	RolePlatformAdmin = "platform_admin"
	RoleOrgAdmin      = "org_admin"
	RoleBillingAdmin  = "billing_admin"
	RoleProjectAdmin  = "project_admin"
	RoleDeveloper     = "developer"
	RoleViewer        = "viewer"
	RoleOrgViewer     = "org_viewer"
)

// --- API Key hashing (HMAC-SHA256) ---

// HashAPIKey computes HMAC-SHA256 of a raw API key.
func HashAPIKey(rawKey, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(rawKey))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyAPIKey checks if a raw key matches the stored hash.
func VerifyAPIKey(rawKey, storedHash, secret string) bool {
	computed := HashAPIKey(rawKey, secret)
	return hmac.Equal([]byte(computed), []byte(storedHash))
}

// GenerateAPIKey generates a new API key for the given org.
// The raw format is ur-{4-digit org id}-{32 random hex characters}. The stored
// display prefix is the first 12 characters: the fixed org segment plus four
// random hex characters. Authentication always uses the full-key HMAC hash.
func GenerateAPIKey(orgID int64) (rawKey string, keyPrefix string, err error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate random: %w", err)
	}
	hexStr := hex.EncodeToString(buf) // 32 chars
	orgPart := fmt.Sprintf("%04d", orgID%10000)
	rawKey = fmt.Sprintf("ur-%s-%s", orgPart, hexStr)
	keyPrefix = rawKey[:12]
	return rawKey, keyPrefix, nil
}

// --- Upstream Key encryption (AES-256-GCM) ---

// EncryptKey encrypts a plaintext key using AES-256-GCM.
func EncryptKey(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptKey decrypts a hex-encoded AES-256-GCM ciphertext.
func DecryptKey(encoded string, key []byte) (string, error) {
	ciphertext, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode hex: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// --- JWT ---

// Claims represents the JWT claims for admin users.
type Claims struct {
	UserID int64  `json:"user_id"`
	OrgID  int64  `json:"org_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateJWT creates a JWT token for the given user.
func GenerateJWT(userID, orgID int64, role, secret string, expireHours int) (string, error) {
	claims := Claims{
		UserID: userID,
		OrgID:  orgID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "uniroute",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseJWT parses and validates a JWT token.
func ParseJWT(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// --- Permission helpers ---

// NormalizeRole maps legacy PRD1 roles to their PRD2 equivalents.
func NormalizeRole(role string) string {
	switch role {
	case RoleLegacyAdmin:
		return RolePlatformAdmin
	case RoleOrgViewer:
		return RoleViewer
	default:
		return role
	}
}

// IsValidRole checks whether a role is known to UniRouter.
func IsValidRole(role string) bool {
	switch role {
	case RoleLegacyAdmin, RolePlatformAdmin, RoleOrgAdmin, RoleBillingAdmin, RoleProjectAdmin, RoleDeveloper, RoleViewer, RoleOrgViewer:
		return true
	default:
		return false
	}
}

// RoleIn checks whether role matches any normalized allowed role.
func RoleIn(role string, allowed ...string) bool {
	normalized := NormalizeRole(role)
	for _, candidate := range allowed {
		if normalized == NormalizeRole(candidate) {
			return true
		}
	}
	return false
}

// IsPlatformAdmin checks if the role is a platform admin.
func IsPlatformAdmin(role string) bool {
	return NormalizeRole(role) == RolePlatformAdmin
}

// IsOrgAdmin checks if the role is an org admin.
func IsOrgAdmin(role string) bool {
	return RoleIn(role, RoleOrgAdmin, RolePlatformAdmin)
}

// IsBillingAdmin checks if the role has billing-admin privileges.
func IsBillingAdmin(role string) bool {
	return RoleIn(role, RoleBillingAdmin, RolePlatformAdmin)
}

// IsProjectScopedRole checks if the role must be constrained by project membership.
func IsProjectScopedRole(role string) bool {
	return RoleIn(role, RoleProjectAdmin, RoleDeveloper, RoleViewer)
}

// CanWrite checks if the role has write permissions.
func CanWrite(role string) bool {
	return RoleIn(role, RolePlatformAdmin, RoleOrgAdmin)
}
