package admin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/oidc"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestOIDCMutationAuditAtomicityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	require.NoError(t, pool.QueryRow(ctx,
		`UPDATE orgs SET org_type = 'customer' WHERE id = $1 RETURNING id`, orgID,
	).Scan(&orgID))
	var projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status)
		 VALUES ($1, 'OIDC Default', 'default', 'active') RETURNING id`, orgID,
	).Scan(&projectID))
	require.NoError(t, installOIDCAuditRejectTrigger(ctx, pool))

	svc, err := oidc.NewService(oidc.Config{
		AutoProvision:      true,
		DefaultOrgID:       orgID,
		DefaultRole:        auth.RoleDeveloper,
		DefaultProjectSlug: "default",
	})
	require.NoError(t, err)
	h := NewHandler(pool, "oidc-audit-test-secret", []byte("0123456789abcdef01234567"), nil)
	h.SetOIDCService(svc)

	t.Run("inactive default project rejects and rolls back provision", func(t *testing.T) {
		require.NoError(t, setOIDCProjectStatus(ctx, pool, projectID, "disabled"))
		t.Cleanup(func() {
			require.NoError(t, setOIDCProjectStatus(context.Background(), pool, projectID, "active"))
		})
		identity := oidcAuditIdentity(orgID, "inactive-default", "inactive-default@example.test")
		_, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-inactive-default"), identity)
		require.Error(t, err)
		assertOIDCUserCount(t, pool, identity, 0)
		assertOIDCAuditCount(t, pool, "oidc-audit-inactive-default", 0)
		require.NoError(t, setOIDCProjectStatus(ctx, pool, projectID, "active"))
	})

	t.Run("provision audit failure rolls back user and membership", func(t *testing.T) {
		identity := oidcAuditIdentity(orgID, "reject-provision", "reject-provision@example.test")
		_, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-reject-provision"), identity)
		require.Error(t, err)
		assertOIDCUserCount(t, pool, identity, 0)
		assertOIDCAuditCount(t, pool, "oidc-audit-reject-provision", 0)
	})

	var provisioned oidcUser
	t.Run("provision commits sanitized audit with user and membership", func(t *testing.T) {
		identity := oidcAuditIdentity(orgID, "success-provision", "success-provision@example.test")
		provisioned, err = h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-success-provision"), identity)
		require.NoError(t, err)
		require.Positive(t, provisioned.ID)
		assertOIDCUserCount(t, pool, identity, 1)
		assertOIDCProjectMembership(t, pool, provisioned.ID, projectID, 1)
		assertOIDCSanitizedAudit(t, pool, "oidc-audit-success-provision", "user.oidc_provision", provisioned.ID, orgID, identity)
	})

	t.Run("same-organization local display name does not block OIDC provisioning", func(t *testing.T) {
		const sharedUsername = "SameOrgSharedIdentity"
		_, err := pool.Exec(ctx, `
			INSERT INTO users (org_id,username,password_hash,role,status,auth_provider)
			VALUES ($1,$2,'local:test','org_viewer','active','local')`, orgID, sharedUsername)
		require.NoError(t, err)
		identity := oidcAuditIdentity(orgID, "same-org-shared-subject", "same-org-shared@example.test")
		identity.Username = sharedUsername
		user, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-same-org-shared"), identity)
		require.NoError(t, err)
		require.Equal(t, sharedUsername, user.Username)

		var localCount, externalCount int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE COALESCE(NULLIF(auth_provider,''),'local')='local'),
			       COUNT(*) FILTER (WHERE auth_provider='oidc')
			  FROM users WHERE org_id=$1 AND username=$2`, orgID, sharedUsername,
		).Scan(&localCount, &externalCount))
		require.Equal(t, 1, localCount)
		require.Equal(t, 1, externalCount)
		assertOIDCSanitizedAudit(t, pool, "oidc-audit-same-org-shared", "user.oidc_provision", user.ID, orgID, identity)
	})

	t.Run("different OIDC identities with one display name use deterministic suffix", func(t *testing.T) {
		const sharedUsername = "SameExternalDisplayName"
		firstIdentity := oidcAuditIdentity(orgID, "external-display-first", "external-display-first@example.test")
		firstIdentity.Username = sharedUsername
		first, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-external-display-first"), firstIdentity)
		require.NoError(t, err)
		require.Equal(t, sharedUsername, first.Username)

		secondIdentity := oidcAuditIdentity(orgID, "external-display-second", "external-display-second@example.test")
		secondIdentity.Username = sharedUsername
		second, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-external-display-second"), secondIdentity)
		require.NoError(t, err)
		require.NotEqual(t, first.ID, second.ID)
		require.Equal(t,
			oidcUsernameWithSuffix(sharedUsername, shortOIDCHash(secondIdentity.Issuer, secondIdentity.Subject)),
			second.Username,
		)
		assertOIDCUserCount(t, pool, firstIdentity, 1)
		assertOIDCUserCount(t, pool, secondIdentity, 1)
		assertOIDCSanitizedAudit(t, pool, "oidc-audit-external-display-second", "user.oidc_provision", second.ID, orgID, secondIdentity)
	})

	t.Run("concurrent first login for one OIDC identity converges", func(t *testing.T) {
		type result struct {
			user oidcUser
			err  error
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			requestID := fmt.Sprintf("oidc-audit-concurrent-%d", i)
			identity := oidcAuditIdentity(orgID, "concurrent-first-login", "concurrent-first-login@example.test")
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				user, err := h.upsertOIDCUser(oidcAuditRequestContext(requestID), identity)
				results <- result{user: user, err: err}
			}()
		}
		close(start)
		wg.Wait()
		close(results)

		var winnerID int64
		for got := range results {
			require.NoError(t, got.err)
			require.Positive(t, got.user.ID)
			if winnerID == 0 {
				winnerID = got.user.ID
			} else {
				require.Equal(t, winnerID, got.user.ID)
			}
		}
		identity := oidcAuditIdentity(orgID, "concurrent-first-login", "concurrent-first-login@example.test")
		assertOIDCUserCount(t, pool, identity, 1)
		var provisionAudits int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_logs
			 WHERE action='user.oidc_provision' AND resource='user' AND resource_id=$1`,
			fmt.Sprintf("%d", winnerID),
		).Scan(&provisionAudits))
		require.Equal(t, 1, provisionAudits)
	})

	var existingUserID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status, email, auth_provider, oidc_issuer, oidc_subject)
		 VALUES ($1, 'oidc-sync-user', 'oidc:test', 'developer', 'active', 'old@example.test', 'oidc', $2, $3)
		 RETURNING id`, orgID, "https://issuer.example.test", "reject-sync",
	).Scan(&existingUserID))

	t.Run("sync audit failure rolls back email and membership restoration", func(t *testing.T) {
		identity := oidcAuditIdentity(orgID, "reject-sync", "new@example.test")
		_, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-reject-sync"), identity)
		require.Error(t, err)
		assertOIDCUserEmail(t, pool, existingUserID, "old@example.test")
		assertOIDCProjectMembership(t, pool, existingUserID, projectID, 0)
		assertOIDCAuditCount(t, pool, "oidc-audit-reject-sync", 0)
	})

	t.Run("sync commits sanitized audit with email and membership restoration", func(t *testing.T) {
		identity := oidcAuditIdentity(orgID, "reject-sync", "new@example.test")
		user, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-success-sync"), identity)
		require.NoError(t, err)
		require.Equal(t, existingUserID, user.ID)
		assertOIDCUserEmail(t, pool, existingUserID, "new@example.test")
		assertOIDCProjectMembership(t, pool, existingUserID, projectID, 1)
		assertOIDCSanitizedAudit(t, pool, "oidc-audit-success-sync", "user.oidc_sync", existingUserID, orgID, identity)
	})

	t.Run("unchanged existing identity is a true no-op", func(t *testing.T) {
		identity := oidcAuditIdentity(orgID, "reject-sync", "new@example.test")
		beforeUpdatedAt := oidcUserUpdatedAt(t, pool, existingUserID)
		user, err := h.upsertOIDCUser(oidcAuditRequestContext("oidc-audit-noop-sync"), identity)
		require.NoError(t, err)
		require.Equal(t, existingUserID, user.ID)
		require.Equal(t, beforeUpdatedAt, oidcUserUpdatedAt(t, pool, existingUserID))
		assertOIDCAuditCount(t, pool, "oidc-audit-noop-sync", 0)
	})
}

func oidcAuditIdentity(orgID int64, subject, email string) *oidc.Identity {
	return &oidc.Identity{
		Issuer:           "https://issuer.example.test",
		Subject:          subject,
		Email:            email,
		Username:         subject,
		Role:             auth.RoleDeveloper,
		OrganizationRole: auth.RoleMember,
		ProjectRole:      auth.ProjectRoleUser,
		OrgID:            orgID,
	}
}

func oidcAuditRequestContext(requestID string) context.Context {
	return context.WithValue(context.Background(), middleware.CtxRequestID, requestID)
}

func installOIDCAuditRejectTrigger(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_oidc_mutation_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.action IN ('user.oidc_provision', 'user.oidc_sync')
			   AND NEW.request_id LIKE 'oidc-audit-reject-%' THEN
				RAISE EXCEPTION 'forced OIDC mutation audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `
		CREATE TRIGGER reject_oidc_mutation_audit_for_test
		BEFORE INSERT ON audit_logs
		FOR EACH ROW EXECUTE FUNCTION reject_oidc_mutation_audit_for_test()`)
	return err
}

func assertOIDCUserCount(t *testing.T, pool *pgxpool.Pool, identity *oidc.Identity, want int) {
	t.Helper()
	var got int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM users WHERE oidc_issuer = $1 AND oidc_subject = $2`,
		identity.Issuer, identity.Subject,
	).Scan(&got))
	require.Equal(t, want, got)
}

func assertOIDCProjectMembership(t *testing.T, pool *pgxpool.Pool, userID, projectID int64, want int) {
	t.Helper()
	var got int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_members WHERE user_id = $1 AND project_id = $2`, userID, projectID,
	).Scan(&got))
	require.Equal(t, want, got)
}

func assertOIDCUserEmail(t *testing.T, pool *pgxpool.Pool, userID int64, want string) {
	t.Helper()
	var got string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, userID).Scan(&got))
	require.Equal(t, want, got)
}

func assertOIDCAuditCount(t *testing.T, pool *pgxpool.Pool, requestID string, want int) {
	t.Helper()
	var got int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE request_id = $1`, requestID,
	).Scan(&got))
	require.Equal(t, want, got)
}

func assertOIDCSanitizedAudit(t *testing.T, pool *pgxpool.Pool, requestID, action string, userID, orgID int64, identity *oidc.Identity) {
	t.Helper()
	var gotOrgID, gotUserID int64
	var gotAction, detail, source string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT org_id, user_id, action, detail::text, source FROM audit_logs WHERE request_id = $1`, requestID,
	).Scan(&gotOrgID, &gotUserID, &gotAction, &detail, &source))
	require.Equal(t, orgID, gotOrgID)
	require.Equal(t, userID, gotUserID)
	require.Equal(t, action, gotAction)
	require.Equal(t, "system", source)
	require.Contains(t, detail, `"auth_provider": "oidc"`)
	require.Contains(t, detail, fmt.Sprintf(`"org_id": %d`, orgID))
	for _, sensitive := range []string{identity.Subject, identity.Email, "oidc:test", "password_hash", "id_token", "access_token"} {
		require.False(t, strings.Contains(detail, sensitive), "audit detail leaked %q: %s", sensitive, detail)
	}
}

func setOIDCProjectStatus(ctx context.Context, pool *pgxpool.Pool, projectID int64, status string) error {
	_, err := pool.Exec(ctx, `UPDATE projects SET status = $1, updated_at = NOW() WHERE id = $2`, status, projectID)
	return err
}

func oidcUserUpdatedAt(t *testing.T, pool *pgxpool.Pool, userID int64) time.Time {
	t.Helper()
	var updatedAt time.Time
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT updated_at FROM users WHERE id = $1`, userID).Scan(&updatedAt))
	return updatedAt
}
