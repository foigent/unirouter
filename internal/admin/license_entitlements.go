package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/licensing"
)

type commercialEntitlementConflict struct {
	Resource string
	Current  int64
	Limit    int64
}

func (err *commercialEntitlementConflict) Error() string {
	return fmt.Sprintf("signed %s limit %d is below current active count %d", err.Resource, err.Limit, err.Current)
}

type commercialResource struct {
	name  string
	table string
}

const activeAPIKeySeatCountSQL = `SELECT COUNT(*) FROM (
	SELECT ac.id::TEXT AS seat
	FROM access_credentials ac
	WHERE ac.status='active'
	UNION ALL
	SELECT ('legacy:' || legacy_key.id)::TEXT
	FROM api_keys legacy_key
	WHERE legacy_key.status='active' AND legacy_key.access_credential_id IS NULL
) commercial_api_key_seats`

const activeCanonicalConnectionCountSQL = `SELECT COUNT(*)
FROM provider_connections
WHERE admin_state='enabled'`

var commercialResources = []commercialResource{
	{name: licensing.LimitActiveAPIKeys, table: "api_keys"},
	{name: licensing.LimitActiveChannels, table: "provider_connections"},
	{name: licensing.LimitActiveProjects, table: "projects"},
	{name: licensing.LimitActiveUsers, table: "users"},
}

// replaceCommercialEntitlements persists only the verified authorization
// projection. Per-resource advisory locks share the exact key namespace used by
// migration triggers, so a concurrent create cannot race a lower renewal.
func replaceCommercialEntitlements(ctx context.Context, tx pgx.Tx, status licensing.Status) error {
	if !status.Enforceable || !status.Valid || status.License == nil {
		return fmt.Errorf("cannot persist a non-enforceable commercial entitlement snapshot")
	}
	for _, resource := range commercialResources {
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			"unirouter.commercial_limit."+resource.name,
		); err != nil {
			return fmt.Errorf("lock commercial resource %s: %w", resource.name, err)
		}
	}
	for _, resource := range commercialResources {
		limit, constrained := status.License.Limits[resource.name]
		if !constrained {
			continue
		}
		current, err := countCommercialResourceUsage(ctx, tx, resource)
		if err != nil {
			return fmt.Errorf("count %s: %w", resource.name, err)
		}
		if current > limit {
			return &commercialEntitlementConflict{Resource: resource.name, Current: current, Limit: limit}
		}
	}

	features, err := json.Marshal(status.License.Features)
	if err != nil {
		return fmt.Errorf("marshal commercial features: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, status.License.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse commercial expiry: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO commercial_license_entitlements (
			singleton, license_id, deployment_id, key_id, expires_at, features,
			active_api_keys, active_users, active_projects, active_channels, updated_at
		) VALUES (TRUE, $1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, NOW())
		ON CONFLICT (singleton) DO UPDATE SET
			license_id = EXCLUDED.license_id,
			deployment_id = EXCLUDED.deployment_id,
			key_id = EXCLUDED.key_id,
			expires_at = EXCLUDED.expires_at,
			features = EXCLUDED.features,
			active_api_keys = EXCLUDED.active_api_keys,
			active_users = EXCLUDED.active_users,
			active_projects = EXCLUDED.active_projects,
			active_channels = EXCLUDED.active_channels,
			updated_at = NOW()`,
		status.License.LicenseID,
		status.License.DeploymentID,
		status.KeyID,
		expiresAt.UTC(),
		string(features),
		nullableCommercialLimit(status.License.Limits, licensing.LimitActiveAPIKeys),
		nullableCommercialLimit(status.License.Limits, licensing.LimitActiveUsers),
		nullableCommercialLimit(status.License.Limits, licensing.LimitActiveProjects),
		nullableCommercialLimit(status.License.Limits, licensing.LimitActiveChannels),
	)
	if err != nil {
		return fmt.Errorf("persist commercial entitlements: %w", err)
	}
	return nil
}

func countCommercialResourceUsage(ctx context.Context, tx pgx.Tx, resource commercialResource) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE status = 'active'", resource.table)
	switch resource.name {
	case licensing.LimitActiveAPIKeys:
		query = activeAPIKeySeatCountSQL
	case licensing.LimitActiveChannels:
		query = activeCanonicalConnectionCountSQL
	}
	var current int64
	if err := tx.QueryRow(ctx, query).Scan(&current); err != nil {
		return 0, err
	}
	return current, nil
}

func nullableCommercialLimit(limits map[string]int64, name string) interface{} {
	if limit, ok := limits[name]; ok {
		return limit
	}
	return nil
}

func commercialEntitlementsMatch(ctx context.Context, tx pgx.Tx, status licensing.Status) (bool, error) {
	if !status.Enforceable || !status.Valid || status.License == nil {
		return false, nil
	}
	var (
		licenseID, deploymentID, keyID string
		expiresAt                      time.Time
		activeAPIKeys, activeUsers     int64
		activeProjects, activeChannels int64
	)
	err := tx.QueryRow(ctx, `
		SELECT license_id, deployment_id, key_id, expires_at,
		       COALESCE(active_api_keys, -1), COALESCE(active_users, -1),
		       COALESCE(active_projects, -1), COALESCE(active_channels, -1)
		FROM commercial_license_entitlements
		WHERE singleton = TRUE`,
	).Scan(
		&licenseID, &deploymentID, &keyID, &expiresAt,
		&activeAPIKeys, &activeUsers, &activeProjects, &activeChannels,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read commercial entitlement projection: %w", err)
	}
	expectedExpiry, err := time.Parse(time.RFC3339, status.License.ExpiresAt)
	if err != nil {
		return false, fmt.Errorf("parse verified commercial expiry: %w", err)
	}
	expiryDelta := expiresAt.UTC().Sub(expectedExpiry.UTC())
	if expiryDelta < 0 {
		expiryDelta = -expiryDelta
	}
	return licenseID == status.License.LicenseID &&
		deploymentID == status.License.DeploymentID &&
		keyID == status.KeyID &&
		expiryDelta < time.Microsecond &&
		activeAPIKeys == commercialLimitOrAbsent(status.License.Limits, licensing.LimitActiveAPIKeys) &&
		activeUsers == commercialLimitOrAbsent(status.License.Limits, licensing.LimitActiveUsers) &&
		activeProjects == commercialLimitOrAbsent(status.License.Limits, licensing.LimitActiveProjects) &&
		activeChannels == commercialLimitOrAbsent(status.License.Limits, licensing.LimitActiveChannels), nil
}

func commercialLimitOrAbsent(limits map[string]int64, name string) int64 {
	if limit, ok := limits[name]; ok {
		return limit
	}
	return -1
}

// writeCommercialLimitViolation converts database-enforced capacity denials
// into an actionable admin response without exposing SQL or the signed document.
func writeCommercialLimitViolation(w http.ResponseWriter, err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.HasPrefix(pgErr.ConstraintName, "commercial_license_active_") {
		return false
	}
	code := "license_error.resource_limit_exceeded"
	httpStatus := http.StatusConflict
	if strings.Contains(pgErr.Message, "expired") {
		code = "license_error.expired"
		httpStatus = http.StatusForbidden
	}
	writeJSON(w, httpStatus, map[string]interface{}{
		"error": pgErr.Message,
		"code":  code,
	})
	return true
}
