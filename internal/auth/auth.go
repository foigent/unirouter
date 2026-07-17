package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
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

	apiKeyDisplayPrefixLength = 20
	adminJWTVersion           = 2
	adminSessionIDBytes       = 32
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
// display prefix uses the full VARCHAR(20) field: the fixed org segment plus
// twelve random hex characters (48 bits). Authentication always uses the
// full-key HMAC hash, so existing shorter stored prefixes remain compatible.
func GenerateAPIKey(orgID int64) (rawKey string, keyPrefix string, err error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate random: %w", err)
	}
	hexStr := hex.EncodeToString(buf) // 32 chars
	orgPart := fmt.Sprintf("%04d", orgID%10000)
	rawKey = fmt.Sprintf("ur-%s-%s", orgPart, hexStr)
	keyPrefix = rawKey[:apiKeyDisplayPrefixLength]
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
	UserID        int64  `json:"user_id"`
	OrgID         int64  `json:"org_id"`
	Role          string `json:"role"`
	SessionID     string `json:"sid"`
	AuthEpoch     int64  `json:"auth_epoch"`
	SchemaVersion int    `json:"ver"`
	jwt.RegisteredClaims
}

// AdminSession is the non-sensitive server-side record required to authorize
// an admin JWT. The raw bearer sid never leaves the token generator.
type AdminSession struct {
	SIDHash   []byte
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// GenerateAdminSessionJWT creates a versioned admin JWT and the hashed session
// record that must be persisted before the token is returned to a client.
func GenerateAdminSessionJWT(userID, orgID int64, role string, authEpoch int64, secret string, expireHours int) (string, AdminSession, error) {
	if userID <= 0 || orgID <= 0 || !IsValidRole(role) || authEpoch < 0 || expireHours <= 0 {
		return "", AdminSession{}, fmt.Errorf("invalid admin session claims")
	}
	sidBytes := make([]byte, adminSessionIDBytes)
	if _, err := rand.Read(sidBytes); err != nil {
		return "", AdminSession{}, fmt.Errorf("generate admin session id: %w", err)
	}
	sessionID := base64.RawURLEncoding.EncodeToString(sidBytes)
	issuedAt := time.Now().UTC()
	expiresAt := issuedAt.Add(time.Duration(expireHours) * time.Hour)
	claims := Claims{
		UserID:        userID,
		OrgID:         orgID,
		Role:          role,
		SessionID:     sessionID,
		AuthEpoch:     authEpoch,
		SchemaVersion: adminJWTVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			Issuer:    "uniroute",
			Subject:   strconv.FormatInt(userID, 10),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", AdminSession{}, err
	}
	hash := sha256.Sum256([]byte(sessionID))
	return signed, AdminSession{
		SIDHash:   append([]byte(nil), hash[:]...),
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

// HashAdminSessionID returns the SHA-256 session lookup key stored by the
// server. The raw sid is bearer material and must not be persisted or logged.
func HashAdminSessionID(sessionID string) []byte {
	hash := sha256.Sum256([]byte(sessionID))
	return append([]byte(nil), hash[:]...)
}

// ParseJWT parses and validates a JWT token.
func ParseJWT(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("uniroute"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	decodedSID, decodeErr := base64.RawURLEncoding.DecodeString(claims.SessionID)
	if claims.SchemaVersion != adminJWTVersion || claims.UserID <= 0 || claims.OrgID <= 0 ||
		!IsValidRole(claims.Role) || claims.AuthEpoch < 0 || claims.IssuedAt == nil ||
		claims.Subject != strconv.FormatInt(claims.UserID, 10) ||
		len(decodedSID) != adminSessionIDBytes || decodeErr != nil {
		return nil, fmt.Errorf("invalid admin session token")
	}
	return claims, nil
}

// --- Permission helpers ---

// NormalizeRole maps legacy aliases while preserving roles with different scopes.
func NormalizeRole(role string) string {
	switch role {
	case RoleLegacyAdmin:
		return RolePlatformAdmin
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
