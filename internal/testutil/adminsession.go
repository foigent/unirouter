package testutil

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
)

// MustAdminSessionJWT issues and persists a real admin session for integration
// tests that exercise AdminAuth. It intentionally uses the production token
// generator and schema instead of bypassing the session boundary.
func MustAdminSessionJWT(tb testing.TB, ctx context.Context, q db.DBQuerier, userID, orgID int64, role, secret string) string {
	tb.Helper()
	var authEpoch int64
	if err := q.QueryRow(ctx, `SELECT auth_epoch FROM users WHERE id = $1`, userID).Scan(&authEpoch); err != nil {
		tb.Fatalf("load admin auth epoch: %v", err)
	}
	if orgID <= 0 || !auth.IsValidRole(role) {
		return mustMalformedAdminSessionJWT(tb, userID, orgID, role, authEpoch, secret)
	}
	token, session, err := auth.GenerateAdminSessionJWT(userID, orgID, role, authEpoch, secret, 1)
	if err != nil {
		tb.Fatalf("generate admin session JWT: %v", err)
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, session.SIDHash, session.IssuedAt, session.ExpiresAt,
	); err != nil {
		tb.Fatalf("persist admin session: %v", err)
	}
	return token
}

// mustMalformedAdminSessionJWT signs deliberately invalid claims for negative
// integration tests. It does not persist a session: ParseJWT must reject these
// tokens before the server-side session lookup. Production paths must never use
// this helper.
func mustMalformedAdminSessionJWT(tb testing.TB, userID, orgID int64, role string, authEpoch int64, secret string) string {
	tb.Helper()
	sidBytes := make([]byte, 32)
	if _, err := rand.Read(sidBytes); err != nil {
		tb.Fatalf("generate malformed test session id: %v", err)
	}
	now := time.Now().UTC()
	claims := auth.Claims{
		UserID:        userID,
		OrgID:         orgID,
		Role:          role,
		SessionID:     base64.RawURLEncoding.EncodeToString(sidBytes),
		AuthEpoch:     authEpoch,
		SchemaVersion: 2,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "uniroute",
			Subject:   strconv.FormatInt(userID, 10),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		tb.Fatalf("sign malformed admin session JWT: %v", err)
	}
	return token
}
