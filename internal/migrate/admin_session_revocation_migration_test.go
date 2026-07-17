package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestAdminSessionRevocationMigrationHasGuardedRollback(t *testing.T) {
	up, err := os.ReadFile("../../migrations/000038_v1_admin_session_revocation.up.sql")
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	down, err := os.ReadFile("../../migrations/000038_v1_admin_session_revocation.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	for _, fragment := range []string{
		"ADD COLUMN auth_epoch",
		"trg_users_auth_epoch_monotonic",
		"CREATE TABLE admin_sessions",
		"octet_length(sid_hash) = 32",
		"ON DELETE CASCADE",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Fatalf("up migration missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"successor migration exists",
		"server-side session evidence exists",
		"authentication revocation epoch evidence exists",
		"DROP TABLE IF EXISTS admin_sessions",
		"DROP TRIGGER IF EXISTS trg_users_auth_epoch_monotonic",
		"DROP FUNCTION IF EXISTS enforce_user_auth_epoch",
		"DROP COLUMN IF EXISTS auth_epoch",
		"DELETE FROM schema_migrations",
	} {
		if !strings.Contains(string(down), fragment) {
			t.Fatalf("down migration missing %q", fragment)
		}
	}
}
