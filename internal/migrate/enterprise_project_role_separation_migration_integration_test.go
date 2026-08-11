//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseProjectRoleSeparationMigrationCanonicalizesBothAxes(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000057_second_integration_reset_support.up.sql")
	ctx := context.Background()

	var orgID, projectID, orgAdminID, memberID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('role-separation-up','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'role-separation-up','role-separation-up','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'role-separation-org-admin','hash','org_admin','active') RETURNING id`, orgID).Scan(&orgAdminID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'role-separation-member','hash','developer','active') RETURNING id`, orgID).Scan(&memberID))
	_, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id,user_id,role) VALUES
		($1,$2,'project_admin'), ($1,$3,'project_developer')`, projectID, orgAdminID, memberID)
	require.NoError(t, err)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

	var organizationRole, compatibilityRole, membershipRole string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT organization_role,role FROM users WHERE id=$1`, orgAdminID,
	).Scan(&organizationRole, &compatibilityRole))
	require.Equal(t, "org_admin", organizationRole)
	require.Equal(t, "org_admin", compatibilityRole, "organization-role shadow must survive explicit membership")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, orgAdminID,
	).Scan(&membershipRole))
	require.Equal(t, "project_admin", membershipRole)

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT organization_role,role FROM users WHERE id=$1`, memberID,
	).Scan(&organizationRole, &compatibilityRole))
	require.Equal(t, "member", organizationRole)
	require.Equal(t, "developer", compatibilityRole)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, memberID,
	).Scan(&membershipRole))
	require.Equal(t, "project_user", membershipRole)

	var defaultRole string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'role-separation-default-member','hash','member','active')
		RETURNING organization_role`, orgID).Scan(&defaultRole))
	require.Equal(t, "member", defaultRole)

	legacyMappings := []struct {
		role             string
		organizationRole string
	}{
		{role: "admin", organizationRole: "platform_admin"},
		{role: "platform_admin", organizationRole: "platform_admin"},
		{role: "org_admin", organizationRole: "org_admin"},
		{role: "billing_admin", organizationRole: "billing_admin"},
		{role: "org_viewer", organizationRole: "org_viewer"},
		{role: "member", organizationRole: "member"},
		{role: "project_admin", organizationRole: "member"},
		{role: "developer", organizationRole: "member"},
		{role: "viewer", organizationRole: "member"},
	}
	for _, mapping := range legacyMappings {
		var insertedOrganizationRole string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,role,status)
			VALUES ($1,$2,'hash',$3,'active')
			RETURNING organization_role`, orgID, "legacy-insert-"+mapping.role, mapping.role,
		).Scan(&insertedOrganizationRole))
		require.Equal(t, mapping.organizationRole, insertedOrganizationRole, "legacy INSERT role=%s", mapping.role)
	}

	var legacyUpdateID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'role-separation-legacy-update','hash','member','member','active')
		RETURNING id`, orgID).Scan(&legacyUpdateID))
	for _, mapping := range legacyMappings {
		require.NoError(t, pool.QueryRow(ctx, `
			UPDATE users SET role=$2 WHERE id=$1 RETURNING organization_role`, legacyUpdateID, mapping.role,
		).Scan(&defaultRole))
		require.Equal(t, mapping.organizationRole, defaultRole, "legacy UPDATE role=%s", mapping.role)
	}

	// Current binaries write both axes. The compatibility trigger must observe
	// that explicit update rather than deriving organization authority from the
	// users.role shadow.
	require.NoError(t, pool.QueryRow(ctx, `
		UPDATE users
		SET organization_role='billing_admin', role='billing_admin'
		WHERE id=$1
		RETURNING organization_role`, legacyUpdateID,
	).Scan(&defaultRole))
	require.Equal(t, "billing_admin", defaultRole)

	// A PostgreSQL row trigger cannot tell a legacy role-only UPDATE from a
	// client that redundantly assigns organization_role to its unchanged value.
	// During the one-release rolling window this exact ambiguous shape is
	// deliberately interpreted as legacy and normalized. Current product code
	// does not issue organization-role changes in this form.
	var ambiguousUpdateID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'role-separation-ambiguous-update','hash','member','developer','active')
		RETURNING id`, orgID).Scan(&ambiguousUpdateID))
	var ambiguousOrganizationRole, ambiguousCompatibilityRole string
	require.NoError(t, pool.QueryRow(ctx, `
		UPDATE users
		SET organization_role='member', role='org_admin'
		WHERE id=$1
		RETURNING organization_role,role`, ambiguousUpdateID,
	).Scan(&ambiguousOrganizationRole, &ambiguousCompatibilityRole))
	require.Equal(t, "org_admin", ambiguousOrganizationRole)
	require.Equal(t, "org_admin", ambiguousCompatibilityRole)

	_, err = pool.Exec(ctx, `UPDATE users SET role='project_admin' WHERE id=$1`, memberID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT organization_role,role FROM users WHERE id=$1`, memberID,
	).Scan(&organizationRole, &compatibilityRole))
	require.Equal(t, "member", organizationRole, "membership shadow updates must never expand organization authority")
	require.Equal(t, "project_admin", compatibilityRole)

	var functionDefinition string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pg_get_functiondef(p.oid)
		FROM pg_proc p
		WHERE p.proname='sync_users_organization_role_from_legacy_role_write'`,
	).Scan(&functionDefinition))
	require.Contains(t, functionDefinition, "TG_OP = 'INSERT' AND NEW.organization_role = '__legacy_role_unset__'")
	require.Contains(t, functionDefinition, "NEW.organization_role IS NOT DISTINCT FROM OLD.organization_role")
	var organizationRoleDefault string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT column_default
		FROM information_schema.columns
		WHERE table_schema='public' AND table_name='users' AND column_name='organization_role'`,
	).Scan(&organizationRoleDefault))
	require.Contains(t, organizationRoleDefault, "__legacy_role_unset__")
	var persistedSentinels int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE organization_role='__legacy_role_unset__'`,
	).Scan(&persistedSentinels))
	require.Zero(t, persistedSentinels, "legacy omission sentinel must never become durable")
	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'explicit-null-organization-role','hash',NULL,'org_admin','active')`, orgID)
	require.Error(t, err, "explicit NULL must not be mistaken for an omitted legacy column")

	legacyProjectRoleMappings := []struct {
		input string
		want  string
	}{
		{input: "project_developer", want: "project_user"},
		{input: "developer", want: "project_user"},
		{input: "viewer", want: "project_viewer"},
		{input: "project_admin", want: "project_admin"},
		{input: "project_user", want: "project_user"},
		{input: "project_viewer", want: "project_viewer"},
	}
	for index, mapping := range legacyProjectRoleMappings {
		var oldWriterUserID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash','member','member','active') RETURNING id`,
			orgID, "legacy-membership-writer-"+mapping.input,
		).Scan(&oldWriterUserID))
		var storedRole string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO project_members (project_id,user_id,role)
			VALUES ($1,$2,$3) RETURNING role`, projectID, oldWriterUserID, mapping.input,
		).Scan(&storedRole))
		require.Equal(t, mapping.want, storedRole, "legacy membership INSERT role=%s", mapping.input)

		updateInput := legacyProjectRoleMappings[(index+1)%len(legacyProjectRoleMappings)]
		require.NoError(t, pool.QueryRow(ctx, `
			UPDATE project_members SET role=$3
			WHERE project_id=$1 AND user_id=$2 RETURNING role`, projectID, oldWriterUserID, updateInput.input,
		).Scan(&storedRole))
		require.Equal(t, updateInput.want, storedRole, "legacy membership UPDATE role=%s", updateInput.input)
	}

	var unknownWriterUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'legacy-membership-writer-unknown','hash','member','member','active') RETURNING id`, orgID,
	).Scan(&unknownWriterUserID))
	_, err = pool.Exec(ctx, `
		INSERT INTO project_members (project_id,user_id,role)
		VALUES ($1,$2,'unknown_project_role')`, projectID, unknownWriterUserID)
	require.ErrorContains(t, err, "project_members_role_check")

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT pg_get_functiondef(p.oid)
		FROM pg_proc p
		WHERE p.proname='normalize_project_members_role_from_legacy_write'`,
	).Scan(&functionDefinition))
	require.Contains(t, functionDefinition, "WHEN 'project_developer' THEN 'project_user'")
	require.Contains(t, functionDefinition, "WHEN 'viewer' THEN 'project_viewer'")

	validRolePairs := []struct {
		organizationRole  string
		compatibilityRole string
	}{
		{organizationRole: "platform_admin", compatibilityRole: "admin"},
		{organizationRole: "platform_admin", compatibilityRole: "platform_admin"},
		{organizationRole: "org_admin", compatibilityRole: "org_admin"},
		{organizationRole: "billing_admin", compatibilityRole: "billing_admin"},
		{organizationRole: "org_viewer", compatibilityRole: "org_viewer"},
		{organizationRole: "member", compatibilityRole: "member"},
		{organizationRole: "member", compatibilityRole: "project_admin"},
		{organizationRole: "member", compatibilityRole: "developer"},
		{organizationRole: "member", compatibilityRole: "viewer"},
	}
	for _, pair := range validRolePairs {
		_, err = pool.Exec(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active')`,
			orgID, "valid-role-pair-"+pair.organizationRole+"-"+pair.compatibilityRole,
			pair.organizationRole, pair.compatibilityRole)
		require.NoError(t, err, "valid role pair %s/%s", pair.organizationRole, pair.compatibilityRole)
	}

	invalidRolePairs := []struct {
		organizationRole  string
		compatibilityRole string
	}{
		{organizationRole: "member", compatibilityRole: "org_admin"},
		{organizationRole: "org_admin", compatibilityRole: "viewer"},
		{organizationRole: "billing_admin", compatibilityRole: "project_admin"},
	}
	for _, pair := range invalidRolePairs {
		_, err = pool.Exec(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active')`,
			orgID, "invalid-role-pair-"+pair.organizationRole+"-"+pair.compatibilityRole,
			pair.organizationRole, pair.compatibilityRole)
		require.ErrorContains(t, err, "users_organization_role_compatibility_check")
	}

	var explicitMismatchUpdateID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'explicit-mismatch-update','hash','org_admin','org_admin','active')
		RETURNING id`, orgID,
	).Scan(&explicitMismatchUpdateID))
	_, err = pool.Exec(ctx, `
		UPDATE users
		SET organization_role='member', role='org_admin'
		WHERE id=$1`, explicitMismatchUpdateID)
	require.ErrorContains(t, err, "users_organization_role_compatibility_check")
}

func TestEnterpriseProjectRoleSeparationDownFailsClosedForNonMemberMembership(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000057_second_integration_reset_support.up.sql")
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	rollbackEmptyMigrations(t, pool, []string{"000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql", "000059_enterprise_access_provisioning.down.sql"})

	var orgID, projectID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('role-down-org','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'role-down-project','role-down-project','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'role-down-org-viewer','hash','org_viewer','org_viewer','active') RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, userID)
	require.NoError(t, err)

	down := readEnterpriseRoleDownMigration(t)
	_, err = pool.Exec(ctx, down)
	require.ErrorContains(t, err, "organization role with project membership is not representable")
	assertOrganizationRoleColumn(t, pool, true)
	assertLegacyCompatibilityFunction(t, pool, true)
	assertProjectMemberCompatibilityFunction(t, pool, true)
}

func TestEnterpriseProjectRoleSeparationDownFailsClosedForHeterogeneousMemberRoles(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000057_second_integration_reset_support.up.sql")
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	rollbackEmptyMigrations(t, pool, []string{"000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql", "000059_enterprise_access_provisioning.down.sql"})

	var orgID, projectA, projectB, userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('role-down-heterogeneous','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'role-down-a','role-down-a','active') RETURNING id`, orgID).Scan(&projectA))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'role-down-b','role-down-b','active') RETURNING id`, orgID).Scan(&projectB))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'role-down-member','hash','member','project_admin','active') RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id,user_id,role) VALUES
		($1,$3,'project_admin'),($2,$3,'project_viewer')`, projectA, projectB, userID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, readEnterpriseRoleDownMigration(t))
	require.ErrorContains(t, err, "member project roles are not representable")
	assertOrganizationRoleColumn(t, pool, true)
	assertLegacyCompatibilityFunction(t, pool, true)
	assertProjectMemberCompatibilityFunction(t, pool, true)
}

func TestEnterpriseProjectRoleSeparationDownRestoresRepresentableLegacyState(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000057_second_integration_reset_support.up.sql")
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	rollbackEmptyMigrations(t, pool, []string{"000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql", "000059_enterprise_access_provisioning.down.sql"})

	var orgID, projectID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('role-down-safe','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'role-down-safe','role-down-safe','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'role-down-safe-member','hash','member','developer','active') RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, userID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, readEnterpriseRoleDownMigration(t))
	require.NoError(t, err)
	assertOrganizationRoleColumn(t, pool, false)
	assertLegacyCompatibilityFunction(t, pool, false)
	assertProjectMemberCompatibilityFunction(t, pool, false)
	var userRole, membershipRole string
	require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, userID).Scan(&userRole))
	require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID).Scan(&membershipRole))
	require.Equal(t, "developer", userRole)
	require.Equal(t, "project_developer", membershipRole)
}

func TestEnterpriseProjectRoleSeparationDownWaitsForConcurrentWriterThenFailsClosed(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000057_second_integration_reset_support.up.sql")
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	rollbackEmptyMigrations(t, pool, []string{"000061_xinsight_connector_cp2a_phase_contract.down.sql", "000060_xinsight_connector_cp1.down.sql", "000059_enterprise_access_provisioning.down.sql"})

	var orgID, projectA, projectB, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('role-down-concurrent','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'role-down-concurrent-a','role-down-concurrent-a','active') RETURNING id`, orgID,
	).Scan(&projectA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'role-down-concurrent-b','role-down-concurrent-b','active') RETURNING id`, orgID,
	).Scan(&projectB))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'role-down-concurrent-member','hash','member','project_admin','active') RETURNING id`, orgID,
	).Scan(&userID))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_admin')`, projectA, userID,
	)
	require.NoError(t, err)

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	writerDone := false
	t.Cleanup(func() {
		if !writerDone {
			_ = writer.Rollback(context.Background())
		}
	})
	_, err = writer.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_viewer')`, projectB, userID,
	)
	require.NoError(t, err)

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL := readEnterpriseRoleDownMigration(t)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(context.Background(), downSQL)
		downResult <- execErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `
			SELECT COALESCE(wait_event_type='Lock', false)
			FROM pg_stat_activity WHERE pid=$1`, downPID,
		).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 20*time.Millisecond, "down migration must wait on the writer's table lock before checking representability")

	require.NoError(t, writer.Commit(ctx))
	writerDone = true
	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "member project roles are not representable")
	case <-time.After(5 * time.Second):
		t.Fatal("down migration did not recheck representability after the concurrent writer committed")
	}
	assertOrganizationRoleColumn(t, pool, true)
	assertLegacyCompatibilityFunction(t, pool, true)
	assertProjectMemberCompatibilityFunction(t, pool, true)
	var migrationCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000058_enterprise_project_role_separation'`,
	).Scan(&migrationCount))
	require.Equal(t, 1, migrationCount)
}

func TestEnterpriseProjectRoleSeparationFutureMigrationFirstSerializesDownAndPreservesLaterLedger(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000058_enterprise_project_role_separation.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)
	var holderPID int
	require.NoError(t, holder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID))
	_, err = holder.Exec(ctx, `BEGIN; LOCK TABLE users IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	holderOpen := true
	t.Cleanup(func() {
		if holderOpen {
			_, _ = holder.Exec(context.Background(), `ROLLBACK`)
		}
		holder.Release()
	})

	runnerApplicationName := "enterprise-role-future-first"
	runnerPool := enterpriseRoleRunnerPool(t, ctx, pool, runnerApplicationName)
	runnerResult := make(chan error, 1)
	go func() {
		runnerResult <- migrate.Run(ctx, runnerPool, testutil.MigrationsDir(t))
	}()
	runnerPID := waitForApplicationBlockedBy(t, pool, runnerApplicationName, holderPID)
	require.True(t, backendHoldsMigrationAdvisoryLock(t, pool, runnerPID), "production 000059 migrator must hold the lifecycle lock before its table locks")

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL := readEnterpriseRoleDownMigration(t)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, downSQL)
		downResult <- execErr
	}()
	waitForBackendBlockedBy(t, pool, downPID, runnerPID)

	_, err = holder.Exec(ctx, `ROLLBACK`)
	require.NoError(t, err)
	holderOpen = false

	select {
	case runnerErr := <-runnerResult:
		require.NoError(t, runnerErr)
	case <-ctx.Done():
		t.Fatal("future migration did not finish after its deterministic blocker was released")
	}
	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "later migrations must be reversed first")
	case <-ctx.Done():
		t.Fatal("down migration did not recheck the ledger after the future migration released the lifecycle lock")
	}

	assertEnterpriseAccessProvisioningState(t, pool)
	assertOrganizationRoleColumn(t, pool, true)
}

func TestEnterpriseProjectRoleSeparationDownFirstSerializesFutureMigrationAndForcesRediscovery(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000058_enterprise_project_role_separation.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var orgID, projectID, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('role-down-before-future','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status)
		 VALUES ($1,'role-down-before-future','role-down-before-future','active') RETURNING id`, orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		 VALUES ($1,'role-down-before-future','hash','member','developer','active') RETURNING id`, orgID,
	).Scan(&userID))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, userID,
	)
	require.NoError(t, err)

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	writerDone := false
	t.Cleanup(func() {
		if !writerDone {
			_ = writer.Rollback(context.Background())
		}
	})
	var writerPID int
	require.NoError(t, writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
	_, err = writer.Exec(ctx, `UPDATE users SET updated_at=updated_at WHERE id=$1`, userID)
	require.NoError(t, err)

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL := readEnterpriseRoleDownMigration(t)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, downSQL)
		downResult <- execErr
	}()
	waitForBackendBlockedBy(t, pool, downPID, writerPID)
	require.True(t, backendHoldsMigrationAdvisoryLock(t, pool, downPID), "down must hold the lifecycle lock before waiting for authorization-table stabilization")

	runnerApplicationName := "enterprise-role-down-first"
	runnerPool := enterpriseRoleRunnerPool(t, ctx, pool, runnerApplicationName)
	runnerResult := make(chan error, 1)
	go func() {
		runnerResult <- migrate.Run(ctx, runnerPool, testutil.MigrationsDir(t))
	}()
	waitForApplicationBlockedBy(t, pool, runnerApplicationName, downPID)

	require.NoError(t, writer.Commit(ctx))
	writerDone = true
	select {
	case downErr := <-downResult:
		require.NoError(t, downErr)
	case <-ctx.Done():
		t.Fatal("down migration did not finish after the authorization-table writer committed")
	}
	select {
	case runnerErr := <-runnerResult:
		require.NoError(t, runnerErr)
	case <-ctx.Done():
		t.Fatal("future migrator did not rediscover and rebuild migration 000058 after down completed")
	}

	assertEnterpriseAccessProvisioningState(t, pool)
	assertOrganizationRoleColumn(t, pool, true)
	var organizationRole, compatibilityRole, membershipRole string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT organization_role,role FROM users WHERE id=$1`, userID,
	).Scan(&organizationRole, &compatibilityRole))
	require.Equal(t, "member", organizationRole)
	require.Equal(t, "developer", compatibilityRole)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID,
	).Scan(&membershipRole))
	require.Equal(t, "project_user", membershipRole)
}

func enterpriseRoleRunnerPool(t *testing.T, ctx context.Context, source *pgxpool.Pool, applicationName string) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	config.MaxConns = 1
	runnerPool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(runnerPool.Close)
	return runnerPool
}

func waitForApplicationBlockedBy(t *testing.T, pool *pgxpool.Pool, applicationName string, blockingPID int) int {
	t.Helper()
	ctx := context.Background()
	runnerPID := 0
	require.Eventually(t, func() bool {
		err := pool.QueryRow(ctx, `
			SELECT a.pid
			  FROM pg_stat_activity a
			 WHERE a.datname=current_database()
			   AND a.application_name=$1
			   AND a.wait_event_type='Lock'
			   AND $2::integer=ANY(pg_blocking_pids(a.pid))
			 ORDER BY a.backend_start DESC
			 LIMIT 1`, applicationName, blockingPID,
		).Scan(&runnerPID)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "application %s must block behind backend %d", applicationName, blockingPID)
	return runnerPID
}

func waitForBackendBlockedBy(t *testing.T, pool *pgxpool.Pool, blockedPID, blockingPID int) {
	t.Helper()
	ctx := context.Background()
	require.Eventually(t, func() bool {
		var blocked bool
		err := pool.QueryRow(ctx,
			`SELECT $2::integer=ANY(pg_blocking_pids($1))`, blockedPID, blockingPID,
		).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 20*time.Millisecond, "backend %d must block behind backend %d", blockedPID, blockingPID)
}

func backendHoldsMigrationAdvisoryLock(t *testing.T, pool *pgxpool.Pool, backendPID int) bool {
	t.Helper()
	var holds bool
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			  FROM pg_locks
			 WHERE pid=$1
			   AND locktype='advisory'
			   AND granted
			   AND classid::bigint * 4294967296 + objid::bigint=$2
		)`, backendPID, migrationAdvisoryLockIDForTest,
	).Scan(&holds))
	return holds
}

func assertEnterpriseAccessProvisioningState(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var migrationCount int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		  FROM schema_migrations
		 WHERE version IN (
			'000058_enterprise_project_role_separation',
			'000059_enterprise_access_provisioning'
		)`,
	).Scan(&migrationCount))
	require.Equal(t, 2, migrationCount)
	var accessRequestsTable string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COALESCE(to_regclass('access_requests')::text, '')`,
	).Scan(&accessRequestsTable))
	require.Equal(t, "access_requests", accessRequestsTable)
}

func readEnterpriseRoleDownMigration(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000058_enterprise_project_role_separation.down.sql"))
	require.NoError(t, err)
	return string(body)
}

func assertOrganizationRoleColumn(t *testing.T, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema='public' AND table_name='users' AND column_name='organization_role'
		)`).Scan(&exists))
	require.Equal(t, expected, exists)
}

func assertLegacyCompatibilityFunction(t *testing.T, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_proc
			WHERE proname='sync_users_organization_role_from_legacy_role_write'
		)`).Scan(&exists))
	require.Equal(t, expected, exists)
}

func assertProjectMemberCompatibilityFunction(t *testing.T, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_proc
			WHERE proname='normalize_project_members_role_from_legacy_write'
		)`).Scan(&exists))
	require.Equal(t, expected, exists)
}
