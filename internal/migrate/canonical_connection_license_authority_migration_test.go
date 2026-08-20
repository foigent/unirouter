package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestCanonicalConnectionLicenseAuthorityMigrationSwitchesOnlyCountAuthority(t *testing.T) {
	upBody, err := os.ReadFile("../../migrations/000079_canonical_connection_license_authority.up.sql")
	if err != nil {
		t.Fatalf("read migration 79 up: %v", err)
	}
	up := string(upBody)
	beginOffset := strings.Index(up, "BEGIN;")
	globalLockOffset := strings.Index(up, modelExecutionMigrationAdvisoryLock)
	writerLockOffset := strings.Index(up, "LOCK TABLE provider_connections, channels, schema_migrations")
	resourceLockOffset := strings.Index(up, "unirouter.commercial_limit.active_channels")
	entitlementLockOffset := strings.Index(up, "LOCK TABLE commercial_license_entitlements")
	preflightOffset := strings.Index(up, "enabled_connections > signed_limit")
	dropLegacyOffset := strings.Index(up, "DROP TRIGGER channels_commercial_limit ON channels")
	createCanonicalOffset := strings.Index(up, "CREATE TRIGGER provider_connections_commercial_limit")
	if beginOffset < 0 || globalLockOffset <= beginOffset || writerLockOffset <= globalLockOffset ||
		resourceLockOffset <= writerLockOffset || entitlementLockOffset <= resourceLockOffset ||
		preflightOffset <= entitlementLockOffset ||
		dropLegacyOffset <= preflightOffset || createCanonicalOffset <= dropLegacyOffset {
		t.Fatalf("migration 79 up order invalid: begin=%d global=%d writers=%d resource=%d entitlement=%d preflight=%d drop=%d create=%d",
			beginOffset, globalLockOffset, writerLockOffset, resourceLockOffset, entitlementLockOffset,
			preflightOffset, dropLegacyOffset, createCanonicalOffset)
	}
	for _, required := range []string{
		"LOCK TABLE provider_connections, channels, schema_migrations",
		"LOCK TABLE commercial_license_entitlements",
		"WHERE admin_state='enabled'",
		"BEFORE INSERT OR UPDATE OF admin_state ON provider_connections",
		"ELSIF resource_name='active_channels'",
		"FROM provider_connections",
		"CONSTRAINT='commercial_license_active_channels'",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 79 up is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"legacy_channels_canonical_control_plane_seal",
		"legacy_channel_keys_canonical_control_plane_seal",
		"DISABLE TRIGGER",
		"current_setting(",
	} {
		if strings.Contains(up, forbidden) {
			t.Fatalf("migration 79 must not add a legacy DML test backdoor or global seal: %q", forbidden)
		}
	}
}

func TestCanonicalConnectionLicenseAuthorityDownFailsClosedAndRestoresM78(t *testing.T) {
	downBody, err := os.ReadFile("../../migrations/000079_canonical_connection_license_authority.down.sql")
	if err != nil {
		t.Fatalf("read migration 79 down: %v", err)
	}
	down := string(downBody)
	globalLockOffset := strings.Index(down, modelExecutionMigrationAdvisoryLock)
	writerLockOffset := strings.Index(down, "LOCK TABLE provider_connections, channels, schema_migrations")
	resourceLockOffset := strings.Index(down, "unirouter.commercial_limit.active_channels")
	entitlementLockOffset := strings.Index(down, "LOCK TABLE commercial_license_entitlements")
	guardOffset := strings.Index(down, "version>'000079_canonical_connection_license_authority'")
	projectionOffset := strings.Index(down, "enabled provider connection lacks an exact active legacy projection")
	limitOffset := strings.Index(down, "active_legacy_channels > signed_limit")
	dropCanonicalOffset := strings.Index(down, "DROP TRIGGER provider_connections_commercial_limit")
	restoreLegacyOffset := strings.Index(down, "CREATE TRIGGER channels_commercial_limit")
	ledgerOffset := strings.Index(down, "DELETE FROM schema_migrations")
	if globalLockOffset < 0 || writerLockOffset <= globalLockOffset ||
		resourceLockOffset <= writerLockOffset || entitlementLockOffset <= resourceLockOffset ||
		guardOffset <= entitlementLockOffset || projectionOffset <= guardOffset || limitOffset <= projectionOffset ||
		dropCanonicalOffset <= limitOffset || restoreLegacyOffset <= dropCanonicalOffset ||
		ledgerOffset <= restoreLegacyOffset {
		t.Fatalf("migration 79 down order invalid: global=%d writers=%d resource=%d entitlement=%d guard=%d projection=%d limit=%d drop=%d restore=%d ledger=%d",
			globalLockOffset, writerLockOffset, resourceLockOffset, entitlementLockOffset,
			guardOffset, projectionOffset, limitOffset, dropCanonicalOffset,
			restoreLegacyOffset, ledgerOffset)
	}
	for _, required := range []string{
		"connection.admin_state='enabled'",
		"connection.legacy_channel_id IS NULL",
		"legacy.status<>'active'",
		"connection.config_revision IS DISTINCT FROM legacy.routing_epoch",
		"WHERE status='active'",
		"WHEN 'channels' THEN",
		"WHERE version='000079_canonical_connection_license_authority'",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("migration 79 down is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE",
		"legacy_channels_canonical_control_plane_seal",
		"legacy_channel_keys_canonical_control_plane_seal",
		"DISABLE TRIGGER",
		"current_setting(",
	} {
		if strings.Contains(down, forbidden) {
			t.Fatalf("migration 79 down must retain canonical M78 data and contain no legacy DML backdoor/seal: %q", forbidden)
		}
	}
	if strings.Count(down, modelExecutionMigrationAdvisoryLock) != 1 {
		t.Fatal("migration 79 down must acquire the shared migration lock exactly once")
	}
}
