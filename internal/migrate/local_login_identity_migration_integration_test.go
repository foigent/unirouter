//go:build integration

package migrate_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1LocalLoginIdentityMigrationRejectsUnsafeLegacyRowsAtomically(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000041_v1_stream_lease_fencing.up.sql")
	ctx := context.Background()
	orgIDs := insertLocalLoginMigrationOrgs(t, pool, 4)
	_, err := pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider,oidc_issuer,oidc_subject)
		VALUES
			($1,'CollisionAdmin','hash','org_admin','active','local',NULL,NULL),
			($2,E'\t' || U&'\00A0' || 'COLLISIONADMIN' || U&'\3000','hash','org_viewer','disabled','',NULL,NULL),
			($3,E'\t' || U&'\00A0\3000','hash','org_viewer','active','local',NULL,NULL),
			($4,'collisionadmin','oidc:placeholder','org_viewer','active','oidc','https://issuer.example','subject-one')`,
		orgIDs[0], orgIDs[1], orgIDs[2], orgIDs[3])
	require.NoError(t, err)

	throughIdentity := migrationsThrough(t, testutil.MigrationsDir(t), "000042_v1_local_login_identity.up.sql")
	err = migrate.Run(ctx, pool, throughIdentity)
	require.ErrorContains(t, err, "blank_count=1, untrimmed_count=2, collision_groups=1")

	var functionExists, indexExists bool
	var constraintCount, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT to_regprocedure('public.normalize_local_login_id(text)') IS NOT NULL`).Scan(&functionExists))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT to_regclass('public.users_local_login_id_unique') IS NOT NULL`).Scan(&indexExists))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN ('users_local_login_id_nonblank','users_local_login_username_canonical')`).Scan(&constraintCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
		WHERE version='000042_v1_local_login_identity'`).Scan(&ledgerCount))
	require.False(t, functionExists, "failed migration must roll back the normalization function")
	require.False(t, indexExists, "failed migration must roll back the unique index")
	require.Zero(t, constraintCount, "failed migration must roll back the nonblank constraint")
	require.Zero(t, ledgerCount, "failed migration must not record its ledger row")
}

func TestV1LocalLoginIdentityMigrationRejectsUntrimmedLegacyRowAtomically(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000041_v1_stream_lease_fencing.up.sql")
	ctx := context.Background()
	orgID := insertLocalLoginMigrationOrgs(t, pool, 1)[0]
	_, err := pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,$2,'hash','org_viewer','active','local')`, orgID, " \tUniqueLegacyIdentity\u3000")
	require.NoError(t, err)

	throughIdentity := migrationsThrough(t, testutil.MigrationsDir(t), "000042_v1_local_login_identity.up.sql")
	err = migrate.Run(ctx, pool, throughIdentity)
	require.ErrorContains(t, err, "blank_count=0, untrimmed_count=1, collision_groups=0")
	assertLocalLoginMigrationContract(t, pool, false)
}

func TestV1LocalLoginIdentityMigrationEnforcesNormalizedLocalOnlyContract(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000041_v1_stream_lease_fencing.up.sql")
	ctx := context.Background()
	orgIDs := insertLocalLoginMigrationOrgs(t, pool, 9)
	_, err := pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'CasePreservedIdentity','hash','org_admin','active','local')`, orgIDs[0])
	require.NoError(t, err)

	throughIdentity := migrationsThrough(t, testutil.MigrationsDir(t), "000042_v1_local_login_identity.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, throughIdentity))

	var normalized string
	require.NoError(t, pool.QueryRow(ctx, `SELECT public.normalize_local_login_id($1)`,
		" \t\u00a0CASEPRESERVEDIDENTITY\u3000 ").Scan(&normalized))
	require.Equal(t, "casepreservedidentity", normalized)
	for _, whitespace := range localLoginUnicodeWhitespace() {
		require.NoError(t, pool.QueryRow(ctx, `SELECT public.normalize_local_login_id($1)`,
			string(whitespace)+"MiXeD"+string(whitespace)).Scan(&normalized),
			"normalize U+%04X", whitespace)
		require.Equal(t, "mixed", normalized, "normalize U+%04X", whitespace)
	}

	var unique, valid, ready bool
	var predicate string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT i.indisunique, i.indisvalid, i.indisready, pg_get_expr(i.indpred,i.indrelid)
		FROM pg_index i
		JOIN pg_class c ON c.oid=i.indexrelid
		WHERE c.relname='users_local_login_id_unique'`).Scan(&unique, &valid, &ready, &predicate))
	require.True(t, unique)
	require.True(t, valid)
	require.True(t, ready)
	require.Contains(t, predicate, "COALESCE(NULLIF((auth_provider)::text, ''::text), 'local'::text) = 'local'::text")

	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,$2,'hash','org_viewer','active','local')`,
		orgIDs[1], "casepreservedidentity")
	requireConstraintError(t, err, "23505", "users_local_login_id_unique")

	// OIDC display usernames remain outside the global local-login namespace,
	// including an exact same-organization overlap with a local display name.
	_, err = pool.Exec(ctx, `
		INSERT INTO users (
			org_id,username,password_hash,role,status,auth_provider,oidc_issuer,oidc_subject
		) VALUES ($1,'CasePreservedIdentity','oidc:placeholder','org_viewer','active','oidc',$2,$3)`,
		orgIDs[0], "https://issuer.example", "subject-two")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO users (
			org_id,username,password_hash,role,status,auth_provider,oidc_issuer,oidc_subject
		) VALUES ($1,'CasePreservedIdentity','oidc:placeholder','org_viewer','active','oidc',$2,$3)`,
		orgIDs[0], "https://issuer.example", "subject-three")
	requireConstraintError(t, err, "23505", "users_external_org_username_unique")

	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'DisabledIdentity','hash','org_viewer','disabled','local')`, orgIDs[3])
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'disabledidentity','hash','org_viewer','active','local')`, orgIDs[4])
	requireConstraintError(t, err, "23505", "users_local_login_id_unique")

	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'EmptyProviderIdentity','hash','org_viewer','active','')`, orgIDs[5])
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,'emptyprovideridentity','hash','org_viewer','active','local')`, orgIDs[6])
	requireConstraintError(t, err, "23505", "users_local_login_id_unique")

	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,$2,'hash','org_viewer','active','local')`, orgIDs[7], "\t\u00a0\u3000")
	requireConstraintError(t, err, "23514", "users_local_login_id_nonblank")

	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
		VALUES ($1,$2,'hash','org_viewer','active','local')`, orgIDs[8], "\tCanonicalIdentity\u3000")
	requireConstraintError(t, err, "23514", "users_local_login_username_canonical")
}

func TestV1LocalLoginIdentityMigrationStrictDownAndRollForward(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000042_v1_local_login_identity.up.sql")
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000042_v1_local_login_identity.down.sql"))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000043_future_contract')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	assertLocalLoginMigrationContract(t, pool, true)

	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000043_future_contract'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	assertLocalLoginMigrationContract(t, pool, false)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertLocalLoginMigrationContract(t, pool, true)
}

func TestV1LocalLoginIdentityMigrationDownRejectsActivatedCrossProviderOverlapAtomically(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000042_v1_local_login_identity.up.sql")
	ctx := context.Background()
	orgID := insertLocalLoginMigrationOrgs(t, pool, 1)[0]
	_, err := pool.Exec(ctx, `
		INSERT INTO users (
			org_id,username,password_hash,role,status,auth_provider,oidc_issuer,oidc_subject
		) VALUES
			($1,'SharedDisplayName','hash','org_admin','active','local',NULL,NULL),
			($1,'SharedDisplayName','oidc:placeholder','org_viewer','active','oidc',$2,$3)`,
		orgID, "https://issuer.example", "down-overlap-subject")
	require.NoError(t, err)

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000042_v1_local_login_identity.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "incompatible_org_username_groups=1")
	assertLocalLoginMigrationContract(t, pool, true)

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE org_id=$1 AND username='SharedDisplayName'`, orgID).Scan(&rows))
	require.Equal(t, 2, rows, "refused down must not delete or rename either identity")
}

func insertLocalLoginMigrationOrgs(t *testing.T, pool *pgxpool.Pool, count int) []int64 {
	t.Helper()
	orgIDs := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		var orgID int64
		require.NoError(t, pool.QueryRow(t.Context(), `
			INSERT INTO orgs(name,status,org_type) VALUES ($1,'active','standard') RETURNING id`,
			fmt.Sprintf("local-login-migration-%d-%d", time.Now().UnixNano(), i),
		).Scan(&orgID))
		orgIDs = append(orgIDs, orgID)
	}
	return orgIDs
}

func requireConstraintError(t *testing.T, err error, code, constraint string) {
	t.Helper()
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected PostgreSQL error, got %v", err)
	require.Equal(t, code, pgErr.Code)
	require.Equal(t, constraint, pgErr.ConstraintName)
}

func assertLocalLoginMigrationContract(t *testing.T, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	var functionExists, indexExists, externalIndexExists, nonblankExists, canonicalExists, legacyConstraintExists, ledgerExists bool
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT to_regprocedure('public.normalize_local_login_id(text)') IS NOT NULL`).Scan(&functionExists))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT to_regclass('public.users_local_login_id_unique') IS NOT NULL`).Scan(&indexExists))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT to_regclass('public.users_external_org_username_unique') IS NOT NULL`).Scan(&externalIndexExists))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM pg_constraint WHERE conname='users_local_login_id_nonblank')`).Scan(&nonblankExists))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM pg_constraint WHERE conname='users_local_login_username_canonical')`).Scan(&canonicalExists))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM pg_constraint WHERE conname='users_org_username_unique')`).Scan(&legacyConstraintExists))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM schema_migrations WHERE version='000042_v1_local_login_identity')`).Scan(&ledgerExists))
	require.Equal(t, expected, functionExists)
	require.Equal(t, expected, indexExists)
	require.Equal(t, expected, externalIndexExists)
	require.Equal(t, expected, nonblankExists)
	require.Equal(t, expected, canonicalExists)
	require.Equal(t, !expected, legacyConstraintExists)
	require.Equal(t, expected, ledgerExists)
}

func localLoginUnicodeWhitespace() []rune {
	return []rune{
		'\u0009', '\u000A', '\u000B', '\u000C', '\u000D', '\u0020',
		'\u0085', '\u00A0', '\u1680', '\u2000', '\u2001', '\u2002',
		'\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008',
		'\u2009', '\u200A', '\u2028', '\u2029', '\u202F', '\u205F',
		'\u3000',
	}
}
