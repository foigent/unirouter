//go:build ignore

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	prd4RealSSOAllowBrowser = "approved-target-real-sso"
	prd4RealSSODBDSN        = "postgres://uniroute:uniroute_pass_2024@127.0.0.1:5432/uniroute?sslmode=disable"
)

type ssoAuditEvidence struct {
	UserID          int64    `json:"user_id"`
	Username        string   `json:"username"`
	OrgID           int64    `json:"org_id"`
	Role            string   `json:"role"`
	Status          string   `json:"status"`
	Email           string   `json:"email,omitempty"`
	AuthProvider    string   `json:"auth_provider"`
	Issuer          string   `json:"issuer"`
	SubjectHash     string   `json:"subject_hash,omitempty"`
	AuditID         int64    `json:"audit_id"`
	AuditAction     string   `json:"audit_action"`
	AuditSource     string   `json:"audit_source"`
	AuditCreatedAt  string   `json:"audit_created_at"`
	AuditDetailKeys []string `json:"audit_detail_keys"`
}

func main() {
	if err := runPRD4RealSSOAuditCheck(); err != nil {
		fmt.Fprintf(os.Stderr, "real sso audit check failed: %v\n", err)
		os.Exit(1)
	}
}

func runPRD4RealSSOAuditCheck() error {
	if os.Getenv("UNIROUTE_PRD4_SSO_ALLOW_BROWSER") != prd4RealSSOAllowBrowser {
		return fmt.Errorf("UNIROUTE_PRD4_SSO_ALLOW_BROWSER=%s is required before reading real SSO audit evidence", prd4RealSSOAllowBrowser)
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("UNIROUTE_PRD4_SSO_USER_ID")), 10, 64)
	if err != nil || userID <= 0 {
		return errors.New("UNIROUTE_PRD4_SSO_USER_ID must be a positive integer")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbDSN := firstNonEmpty(os.Getenv("UNIROUTE_PRD4_SSO_DB_DSN"), os.Getenv("DB_DSN"), prd4RealSSODBDSN)
	pool, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	evidence, detail, subject, err := loadUserAndAudit(ctx, pool, userID)
	if err != nil {
		return err
	}
	if err := validateExpectedMapping(evidence); err != nil {
		return err
	}
	if evidence.AuthProvider != "oidc" {
		return fmt.Errorf("expected auth_provider=oidc, got %q", evidence.AuthProvider)
	}
	if evidence.Issuer == "" {
		return errors.New("OIDC issuer is empty in users row")
	}
	if subject == "" {
		return errors.New("OIDC subject is empty in users row")
	}
	evidence.SubjectHash = shortHash(evidence.Issuer, subject)
	if leaksRestrictedOIDCMaterial(detail, os.Getenv("OIDC_CLIENT_SECRET"), os.Getenv("UNIROUTE_PRD4_SSO_TEST_PASSWORD")) {
		return errors.New("audit detail contains restricted OIDC material")
	}

	encoded, _ := json.MarshalIndent(evidence, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func loadUserAndAudit(ctx context.Context, pool *pgxpool.Pool, userID int64) (ssoAuditEvidence, string, string, error) {
	var evidence ssoAuditEvidence
	var subject string
	err := pool.QueryRow(ctx,
		`SELECT id, username, org_id, role, status, COALESCE(email, ''),
		        COALESCE(auth_provider, ''), COALESCE(oidc_issuer, ''), COALESCE(oidc_subject, '')
		   FROM users
		  WHERE id = $1`,
		userID,
	).Scan(&evidence.UserID, &evidence.Username, &evidence.OrgID, &evidence.Role, &evidence.Status, &evidence.Email, &evidence.AuthProvider, &evidence.Issuer, &subject)
	if err != nil {
		return evidence, "", "", fmt.Errorf("load oidc user: %w", err)
	}

	var detail []byte
	var createdAt time.Time
	err = pool.QueryRow(ctx,
		`SELECT id, action, COALESCE(source, 'admin_console'), detail, created_at
		   FROM audit_logs
		  WHERE user_id = $1 AND action = 'login.oidc'
		  ORDER BY created_at DESC
		  LIMIT 1`,
		userID,
	).Scan(&evidence.AuditID, &evidence.AuditAction, &evidence.AuditSource, &detail, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return evidence, "", "", errors.New("login.oidc audit row not found")
	}
	if err != nil {
		return evidence, "", "", fmt.Errorf("load login.oidc audit row: %w", err)
	}
	evidence.AuditCreatedAt = createdAt.UTC().Format(time.RFC3339)
	evidence.AuditDetailKeys = jsonObjectKeys(detail)
	return evidence, string(detail), subject, nil
}

func validateExpectedMapping(evidence ssoAuditEvidence) error {
	if expected := strings.TrimSpace(os.Getenv("UNIROUTE_PRD4_SSO_EXPECTED_ISSUER")); expected != "" && strings.TrimRight(evidence.Issuer, "/") != strings.TrimRight(expected, "/") {
		return fmt.Errorf("issuer mismatch: got %q want %q", evidence.Issuer, expected)
	}
	if expected := strings.TrimSpace(os.Getenv("UNIROUTE_PRD4_SSO_EXPECTED_ROLE")); expected != "" && evidence.Role != expected {
		return fmt.Errorf("role mismatch: got %q want %q", evidence.Role, expected)
	}
	if expected := strings.TrimSpace(os.Getenv("UNIROUTE_PRD4_SSO_EXPECTED_EMAIL")); expected != "" && !strings.EqualFold(evidence.Email, expected) {
		return fmt.Errorf("email mismatch: got %q want %q", evidence.Email, expected)
	}
	if expected := strings.TrimSpace(os.Getenv("UNIROUTE_PRD4_SSO_EXPECTED_USERNAME")); expected != "" && evidence.Username != expected {
		return fmt.Errorf("username mismatch: got %q want %q", evidence.Username, expected)
	}
	if expectedRaw := strings.TrimSpace(os.Getenv("UNIROUTE_PRD4_SSO_EXPECTED_ORG_ID")); expectedRaw != "" {
		expected, err := strconv.ParseInt(expectedRaw, 10, 64)
		if err != nil || expected <= 0 {
			return errors.New("UNIROUTE_PRD4_SSO_EXPECTED_ORG_ID must be a positive integer when set")
		}
		if evidence.OrgID != expected {
			return fmt.Errorf("org_id mismatch: got %d want %d", evidence.OrgID, expected)
		}
	}
	return nil
}

func leaksRestrictedOIDCMaterial(detail string, secrets ...string) bool {
	lower := strings.ToLower(detail)
	for _, marker := range []string{"id_token", "access_token", "refresh_token", "client_secret"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) >= 8 && strings.Contains(detail, secret) {
			return true
		}
	}
	return false
}

func jsonObjectKeys(raw []byte) []string {
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	keys := make([]string, 0, len(parsed))
	for key := range parsed {
		keys = append(keys, key)
	}
	return keys
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
