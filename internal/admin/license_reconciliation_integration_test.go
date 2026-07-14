//go:build integration

package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestReconcileStoredCommercialLicenseRestoresDerivedProjection(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	payload := licensing.Payload{
		LicenseID: "license-restored", Customer: "Restore Test", DeploymentID: "restore-deployment", Tier: "enterprise",
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(24*time.Hour + 123*time.Nanosecond).Format(time.RFC3339Nano),
		Features: []string{licensing.FeatureGateway}, Limits: map[string]int64{licensing.LimitActiveUsers: 100},
	}
	document, err := licensing.SignCommercialPayload(payload, "license-restore-key", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO settings (key, value, description)
		VALUES ($1, $2::jsonb, 'restored License fixture')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, licenseSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}

	verifier := licensing.NewCommercialVerifier(map[string]ed25519.PublicKey{"license-restore-key": publicKey}, nil, 30, payload.DeploymentID)
	runtime := licensing.NewRuntime(verifier)
	handler := NewHandler(pool, "integration-jwt", []byte("0123456789abcdef01234567"), nil)
	handler.SetLicenseVerifier(verifier)
	handler.SetLicenseRuntime(runtime)

	status, err := handler.ReconcileStoredCommercialLicense(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enforceable || !runtime.Authorize(licensing.FeatureGateway).Allowed {
		t.Fatalf("restored License was not made enforceable: %#v", status)
	}
	var licenseID string
	var activeUsers int64
	if err := pool.QueryRow(ctx, `
		SELECT license_id, active_users
		FROM commercial_license_entitlements WHERE singleton = TRUE`,
	).Scan(&licenseID, &activeUsers); err != nil {
		t.Fatal(err)
	}
	if licenseID != payload.LicenseID || activeUsers != 100 {
		t.Fatalf("unexpected restored projection: license_id=%q active_users=%d", licenseID, activeUsers)
	}
	if _, err := handler.RefreshStoredCommercialLicense(ctx); err != nil {
		t.Fatalf("freshly reconciled projection did not refresh: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE commercial_license_entitlements SET active_users = 101 WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.RefreshStoredCommercialLicense(ctx); err == nil {
		t.Fatal("mismatched derived projection unexpectedly refreshed")
	}
	if runtime.Authorize(licensing.FeatureGateway).Allowed {
		t.Fatal("projection mismatch must fail commercial runtime closed")
	}
}
