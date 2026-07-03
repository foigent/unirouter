//go:build ignore

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/licensing"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultBatch22DBDSN = "postgres://uniroute:password@127.0.0.1:5432/uniroute?sslmode=disable"

	batch22OrgName       = "PRD2 Batch 22 License Smoke Org"
	batch22AdminUsername = "prd2-b22-admin"
	batch22AdminPassword = "prd2B22Admin123!"
)

type batch22SeedOutput struct {
	AdminUsername string                     `json:"admin_username"`
	AdminPassword string                     `json:"admin_password"`
	OrgID         int64                      `json:"org_id"`
	UserID        int64                      `json:"user_id"`
	PublicKey     string                     `json:"public_key"`
	Licenses      map[string]json.RawMessage `json:"licenses"`
}

func main() {
	action := flag.String("action", "seed", "seed, store-invalid, clear, or print-public-key")
	flag.Parse()

	if err := run(*action); err != nil {
		fmt.Fprintf(os.Stderr, "batch22 license seed failed: %v\n", err)
		os.Exit(1)
	}
}

func run(action string) error {
	publicKey, privateKey := batch22TestKey("primary")
	if action == "print-public-key" {
		fmt.Println(base64.StdEncoding.EncodeToString(publicKey))
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, firstNonEmpty(os.Getenv("UNIROUTE_SMOKE_DB_DSN"), os.Getenv("DB_DSN"), defaultBatch22DBDSN))
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	switch action {
	case "seed":
		return seedBatch22(ctx, pool, publicKey, privateKey)
	case "store-invalid":
		requireLicenseResetConfirmation()
		docs, err := batch22LicenseDocs(privateKey)
		if err != nil {
			return err
		}
		return storeLicense(ctx, pool, docs["invalid"])
	case "clear":
		requireLicenseResetConfirmation()
		_, err := pool.Exec(ctx, `DELETE FROM settings WHERE key = 'license.offline'`)
		return err
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func seedBatch22(ctx context.Context, pool *pgxpool.Pool, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) error {
	requireLicenseResetConfirmation()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var orgID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, status, settings, org_type, created_at, updated_at)
		 VALUES ($1, 'active', '{}'::jsonb, 'customer', NOW(), NOW())
		 ON CONFLICT (name) DO UPDATE
		   SET status = 'active', updated_at = NOW()
		 RETURNING id`,
		batch22OrgName,
	).Scan(&orgID); err != nil {
		return fmt.Errorf("upsert org: %w", err)
	}

	if err := cleanupBatch22Org(ctx, tx, orgID); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(batch22AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	var userID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'platform_admin', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, username) DO UPDATE
		   SET password_hash = EXCLUDED.password_hash,
		       role = 'platform_admin',
		       status = 'active',
		       updated_at = NOW()
		 RETURNING id`,
		orgID, batch22AdminUsername, string(hash),
	).Scan(&userID); err != nil {
		return fmt.Errorf("upsert platform admin: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM settings WHERE key = 'license.offline'`); err != nil {
		return fmt.Errorf("clear license setting: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	docs, err := batch22LicenseDocs(privateKey)
	if err != nil {
		return err
	}
	out := batch22SeedOutput{
		AdminUsername: batch22AdminUsername,
		AdminPassword: batch22AdminPassword,
		OrgID:         orgID,
		UserID:        userID,
		PublicKey:     base64.StdEncoding.EncodeToString(publicKey),
		Licenses:      docs,
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func cleanupBatch22Org(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, orgID int64) error {
	statements := []string{
		`DELETE FROM notification_deliveries WHERE event_id IN (SELECT id FROM notification_events WHERE org_id = $1)`,
		`DELETE FROM notification_events WHERE org_id = $1`,
		`DELETE FROM audit_logs WHERE org_id = $1`,
	}
	for _, statement := range statements {
		if _, err := q.Exec(ctx, statement, orgID); err != nil {
			return fmt.Errorf("cleanup batch22 org: %w", err)
		}
	}
	return nil
}

func storeLicense(ctx context.Context, pool *pgxpool.Pool, raw json.RawMessage) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('license.offline', $1::jsonb, 'PRD2 Batch 22 invalid status fixture', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, description = EXCLUDED.description, updated_at = NOW()`,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("store license fixture: %w", err)
	}
	return nil
}

func batch22LicenseDocs(privateKey ed25519.PrivateKey) (map[string]json.RawMessage, error) {
	now := time.Now().UTC().Truncate(time.Second)
	docs := map[string]licensing.Document{}
	for name, payload := range map[string]licensing.Payload{
		"active": {
			Customer:     "PRD2 Batch 22 Active Customer",
			DeploymentID: "prd2-b22-active",
			Tier:         "enterprise",
			IssuedAt:     now.Add(-time.Hour).Format(time.RFC3339),
			ExpiresAt:    now.AddDate(0, 6, 0).Format(time.RFC3339),
			Features:     []string{"projects", "native_adapters", "notifications"},
			Limits:       map[string]int64{"projects": 20, "monthly_requests": 100000},
			Metadata:     map[string]string{"fixture": "prd2-batch22"},
		},
		"expiring": {
			Customer:     "PRD2 Batch 22 Expiring Customer",
			DeploymentID: "prd2-b22-expiring",
			Tier:         "enterprise",
			IssuedAt:     now.Add(-time.Hour).Format(time.RFC3339),
			ExpiresAt:    now.AddDate(0, 0, 7).Format(time.RFC3339),
			Features:     []string{"projects", "license_warning"},
			Limits:       map[string]int64{"projects": 10},
			Metadata:     map[string]string{"fixture": "prd2-batch22"},
		},
		"expired": {
			Customer:     "PRD2 Batch 22 Expired Customer",
			DeploymentID: "prd2-b22-expired",
			Tier:         "enterprise",
			IssuedAt:     now.AddDate(0, -1, 0).Format(time.RFC3339),
			ExpiresAt:    now.AddDate(0, 0, -1).Format(time.RFC3339),
			Features:     []string{"projects"},
			Limits:       map[string]int64{"projects": 5},
			Metadata:     map[string]string{"fixture": "prd2-batch22"},
		},
	} {
		doc, err := licensing.SignPayload(payload, privateKey)
		if err != nil {
			return nil, fmt.Errorf("sign %s license: %w", name, err)
		}
		docs[name] = doc
	}

	_, wrongPrivateKey := batch22TestKey("invalid")
	invalidDoc, err := licensing.SignPayload(licensing.Payload{
		Customer:     "PRD2 Batch 22 Invalid Customer",
		DeploymentID: "prd2-b22-invalid",
		Tier:         "enterprise",
		IssuedAt:     now.Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:    now.AddDate(0, 6, 0).Format(time.RFC3339),
		Features:     []string{"projects"},
		Metadata:     map[string]string{"fixture": "prd2-batch22"},
	}, wrongPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("sign invalid license: %w", err)
	}
	docs["invalid"] = invalidDoc

	raw := make(map[string]json.RawMessage, len(docs))
	for name, doc := range docs {
		encoded, err := json.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("marshal %s license: %w", name, err)
		}
		raw[name] = encoded
	}
	return raw, nil
}

func batch22TestKey(label string) (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := sha256.Sum256([]byte("unirouter-prd2-batch22-license-smoke-test-only:" + label))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	return privateKey.Public().(ed25519.PublicKey), privateKey
}

func requireLicenseResetConfirmation() {
	if os.Getenv("UNIROUTE_BATCH22_ALLOW_LICENSE_RESET") != "non-production-license-reset" {
		fmt.Fprintln(os.Stderr, "UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset is required because this smoke clears or overwrites global settings.license.offline")
		os.Exit(2)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
