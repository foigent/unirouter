//go:build integration

package xinsightconnector

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func communicationFixture(t *testing.T, j *PostgresPhaseJournal) (*CommunicationStore, int64, int64, int64) {
	t.Helper()
	ctx := context.Background()
	owner := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("7", 32), Artifact: "sha256:" + strings.Repeat("9", 64)}
	ledger, e := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, e)
	_, _, e = runtimegeneration.Renew(ctx, j.db.(*pgxpool.Pool), owner, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
	require.NoError(t, e)
	store := NewCommunicationStore(j.db, 1250, owner.Instance, owner.Artifact)
	var org, project, actor int64
	require.NoError(t, j.db.QueryRow(ctx, `SELECT org_id,project_id,consented_by FROM xinsight_connector_installations WHERE installation_id=$1`, cp2aInstallationID).Scan(&org, &project, &actor))
	return store, org, project, actor
}

func communicationClaim(t *testing.T, s *CommunicationStore, org, project, actor, revision int64, key string) (*CommunicationPermit, *CommunicationRun, error) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.db.Begin(ctx)
	require.NoError(t, e)
	defer tx.Rollback(ctx)
	p, r, e := s.ClaimTx(ctx, tx, org, project, actor, revision, cp2aInstallationID, key, "manual")
	if e != nil {
		return p, r, e
	}
	return p, r, tx.Commit(ctx)
}
func communicationUpdate(t *testing.T, s *CommunicationStore, org, project, actor, revision int64, enabled bool) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.db.Begin(ctx)
	require.NoError(t, e)
	defer tx.Rollback(ctx)
	require.NoError(t, s.UpdateTx(ctx, tx, org, project, actor, revision, cp2aInstallationID, enabled, 60000))
	require.NoError(t, tx.Commit(ctx))
}

func TestCommunicationManualDefaultConcurrentAdmissionPauseAndBoundsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, _ := setupRedeemInstallation(t, pool)
	s, org, project, actor := communicationFixture(t, journal)
	ctx := context.Background()
	st, e := s.Read(ctx, org, project)
	require.NoError(t, e)
	require.False(t, st.AutomaticEnabled)
	require.Equal(t, 60000, st.CadenceMS)
	require.Equal(t, "paused", st.Effective)
	require.Equal(t, "unknown", st.PeerState)
	_, e = s.ClaimAutomatic(ctx, cp2aInstallationID, "automatic")
	require.ErrorIs(t, e, ErrCommunicationPaused)
	var mu sync.Mutex
	var permits []*CommunicationPermit
	var failures []error
	var wg sync.WaitGroup
	for n := 0; n < 6; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tx, err := pool.Begin(ctx)
			if err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
				return
			}
			defer tx.Rollback(ctx)
			p, _, err := s.ClaimTx(ctx, tx, org, project, actor, 1, cp2aInstallationID, string(rune('a'+n)), "manual")
			if err == nil {
				err = tx.Commit(ctx)
			}
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				permits = append(permits, p)
			} else {
				failures = append(failures, err)
			}
		}(n)
	}
	wg.Wait()
	require.Len(t, permits, 1)
	require.Len(t, failures, 5)
	for _, e := range failures {
		require.ErrorIs(t, e, ErrCommunicationBusy)
	}
	p := permits[0]
	st, e = s.Read(ctx, org, project)
	require.NoError(t, e)
	require.Equal(t, "manual_running", st.Effective)
	c := WithCommunicationPermit(ctx, p)
	require.NoError(t, p.NextUnit(c))
	// An expired dispatch lease is not worker completion and cannot be stolen.
	_, e = pool.Exec(ctx, `UPDATE xinsight_communication_policies SET dispatch_deadline=clock_timestamp()-interval '1 second' WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, e)
	st, e = s.Read(ctx, org, project)
	require.NoError(t, e)
	require.Equal(t, "completion_unknown", st.Effective)
	_, _, e = communicationClaim(t, s, org, project, actor, 1, "cannot-steal")
	require.ErrorIs(t, e, ErrCommunicationBusy)
	_, e = pool.Exec(ctx, `UPDATE xinsight_communication_policies SET dispatch_deadline=clock_timestamp()+interval '30 seconds' WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, e)
	// Pause after admission blocks new work but allows only the exact validated continuation.
	allowCommunicationContinuation(c, http.MethodPost, "/already-committed/confirm")
	communicationUpdate(t, s, org, project, actor, 1, false)
	require.ErrorIs(t, CheckCommunicationRequest(c, http.MethodGet, connectorOperationsPath), ErrCommunicationPaused)
	require.NoError(t, CheckCommunicationRequest(c, http.MethodPost, "/already-committed/confirm"))
	require.ErrorIs(t, CheckCommunicationRequest(c, http.MethodPost, "/already-committed/confirm"), ErrCommunicationPaused)
	require.ErrorIs(t, p.NextUnit(c), ErrCommunicationPaused)
	st, e = s.Read(ctx, org, project)
	require.NoError(t, e)
	require.Equal(t, "pausing", st.Effective)
	require.NoError(t, p.Finish(ctx, "partial", true))
	st, e = s.Read(ctx, org, project)
	require.NoError(t, e)
	require.Equal(t, "paused", st.Effective)
	require.Equal(t, "partial", st.LastRun.State)
	// Cadence, old/new policy, revision and tenant binding survive a new store.
	s = NewCommunicationStore(pool, 1250, s.ownerInstance, s.ownerArtifact)
	communicationUpdate(t, s, org, project, actor, 2, true)
	a, e := s.ClaimAutomatic(ctx, cp2aInstallationID, "automatic")
	require.NoError(t, e)
	require.NoError(t, a.NextUnit(ctx))
	for n := 0; n < CommunicationMaxRequests; n++ {
		require.NoError(t, CheckCommunicationRequest(WithCommunicationPermit(ctx, a), http.MethodGet, connectorOperationsPath))
	}
	require.ErrorIs(t, CheckCommunicationRequest(WithCommunicationPermit(ctx, a), http.MethodGet, connectorOperationsPath), ErrCommunicationPaused)
	for n := 1; n < CommunicationMaxUnits; n++ { // Request budget blocks another unit immediately.
		require.ErrorIs(t, a.NextUnit(ctx), ErrCommunicationPaused)
	}
	require.NoError(t, a.Finish(ctx, "partial", true))
	_, e = s.ClaimAutomatic(ctx, cp2aInstallationID, "automatic")
	require.ErrorIs(t, e, ErrCommunicationPaused, "persisted cadence applies across replicas")
	_, e = s.Read(ctx, org+100000, project)
	require.ErrorIs(t, e, pgx.ErrNoRows)
	_, _, e = communicationClaim(t, s, org, project, actor, 1, "stale")
	require.ErrorIs(t, e, ErrConflict)
}

func TestCommunicationPauseAfterPollPreservesTerminalAndACKIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, _ := setupCP2APhaseJournal(t, pool)
	s, org, project, actor := communicationFixture(t, journal)
	ctx := context.Background()
	p, _, e := communicationClaim(t, s, org, project, actor, 1, "poll-pause")
	require.NoError(t, e)
	calls := []string{}
	operation := agentTestThreePhaseOperations(t)[0]
	client := &http.Client{Transport: roundTripConnectorFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			communicationUpdate(t, s, org, project, actor, 1, false)
			return connectorHTTPResponse(r, 200, "application/json", operation), nil
		}
		return connectorHTTPResponse(r, 200, "application/json", []byte(`{}`)), nil
	})}
	c, e := NewHTTPControlCounterpart("https://control.example.test", RuntimeInstallationDescriptor{InstallationID: cp2aInstallationID, XDeploymentID: "xid_ZZZZZZZZZZZZZZZZZZZZZZ", ControlProjectID: cp2aControlProject}, journal, client, time.Second)
	require.NoError(t, e)
	executor := newAgentTestExecutor(t)
	a, e := NewTransactionalAgent(cp2aInstallationID, c, journal, executor)
	require.NoError(t, e)
	runCtx := WithCommunicationPermit(ctx, p)
	require.NoError(t, p.NextUnit(runCtx))
	worked, e := a.RunOnce(runCtx)
	require.NoError(t, e)
	require.True(t, worked)
	require.Len(t, calls, 2)
	var terminals, acked int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE result_canonical IS NOT NULL),COUNT(*) FILTER(WHERE acknowledged_at IS NOT NULL) FROM xinsight_connector_phase_operations WHERE installation_id=$1`, cp2aInstallationID).Scan(&terminals, &acked))
	require.Equal(t, 1, terminals)
	require.Equal(t, 1, acked)
	_, _, e = c.Poll(runCtx)
	require.ErrorIs(t, e, ErrCommunicationPaused)
	require.Len(t, calls, 2, "no new Poll after pause")
	require.ErrorIs(t, p.NextUnit(runCtx), ErrCommunicationPaused)
	require.NoError(t, p.Finish(ctx, "partial", true))
	replay, receipt, e := communicationClaim(t, s, org, project, actor, 1, "poll-pause")
	require.NoError(t, e)
	require.Nil(t, replay)
	require.Equal(t, "partial", receipt.State)
	require.Equal(t, 2, receipt.Requests)
	require.Equal(t, 1, executor.calls[ControlPhasePreparePaused])
}

func TestCommunicationPauseAfterCredentialEffectPersistsAndConfirmsIntegration(t *testing.T) {
	journal, clock := setupCredentialLifecycle(t)
	s, org, project, actor := communicationFixture(t, journal)
	ctx := context.Background()
	journal.controlBearer = func() (string, error) { return "xib_B" + cp2aControlToken[5:], nil }
	p, _, e := communicationClaim(t, s, org, project, actor, 1, "credential-pause")
	require.NoError(t, e)
	calls := 0
	client := &http.Client{Transport: roundTripConnectorFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		op, e := journal.PendingCredential(ctx, cp2aInstallationID)
		require.NoError(t, e)
		if r.Method == http.MethodGet {
			return connectorHTTPResponse(r, 401, "application/json", []byte(`{}`)), nil
		}
		state := "prepared"
		if strings.HasSuffix(r.URL.Path, "/confirm") {
			state = "committed"
		} else {
			communicationUpdate(t, s, org, project, actor, 1, false)
		}
		receipt := credentialLifecycleReceipt(op, clock.Add(48*time.Hour), state)
		raw, e := json.Marshal(receipt)
		require.NoError(t, e)
		return connectorHTTPResponse(r, 200, "application/json", raw), nil
	})}
	c, e := NewHTTPControlCounterpart("https://control.example.test", RuntimeInstallationDescriptor{InstallationID: cp2aInstallationID, XDeploymentID: "xid_ZZZZZZZZZZZZZZZZZZZZZZ", ControlProjectID: cp2aControlProject}, journal, client, time.Second)
	require.NoError(t, e)
	runCtx := WithCommunicationPermit(ctx, p)
	require.NoError(t, p.NextUnit(runCtx))
	require.NoError(t, c.RunCredential(runCtx))
	require.Equal(t, 3, calls)
	status, e := journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, e)
	require.Equal(t, "healthy", status.State)
	require.ErrorIs(t, c.RunCredential(runCtx), ErrConflict) // Not due; no new remote request.
	require.Equal(t, 3, calls)
	require.NoError(t, p.Finish(ctx, "partial", true))
}

func TestCommunicationMigrationPreservesLegacyAndFencesOldWritersIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.CommercialLicenseRetirementHead)
	journal, _ := setupRedeemInstallation(t, pool)
	ctx := context.Background()
	var org, project int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT org_id,project_id FROM xinsight_connector_installations WHERE installation_id=$1`, cp2aInstallationID).Scan(&org, &project))
	var before, after string
	fingerprint := `SELECT encode(sha256(convert_to(jsonb_agg(b ORDER BY installation_id)::text,'UTF8')),'hex') FROM xinsight_connector_control_bindings b`
	require.NoError(t, pool.QueryRow(ctx, fingerprint).Scan(&before))
	ledger, e := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, e)
	oldHead := slices.Index(ledger, schemaauthority.CommercialLicenseRetirementHead)
	communicationHead := slices.Index(ledger, schemaauthority.XInsightCommunicationHead)
	require.GreaterOrEqual(t, oldHead, 1)
	require.Equal(t, oldHead+1, communicationHead)
	oldLedger := ledger[:oldHead+1]
	communicationLedger := ledger[:communicationHead+1]
	heads := schemaauthority.SupportedSchemaHeads()
	oldSupportHead := slices.Index(heads, schemaauthority.CommercialLicenseRetirementHead)
	require.GreaterOrEqual(t, oldSupportHead, 1)
	owner := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("a", 64)}
	_, _, e = runtimegeneration.Renew(ctx, pool, owner, runtimegeneration.CurrentManifest(heads[:oldSupportHead+1]), oldLedger, false)
	require.NoError(t, e)
	require.Error(t, migrate.RunExistingThrough(ctx, pool, "../../migrations", schemaauthority.XInsightCommunicationHead), "live legacy writer prevents M127")
	var present bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('xinsight_communication_policies') IS NOT NULL`).Scan(&present))
	require.False(t, present)
	// Synthetic stopped-process proof. Production uses the existing retirement owner.
	_, e = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped',ingress_state='absent' WHERE instance_id=$1`, owner.Instance)
	require.NoError(t, e)
	require.NoError(t, migrate.RunExistingThrough(ctx, pool, "../../migrations", schemaauthority.XInsightCommunicationHead))
	var actualLedger []string
	require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(version ORDER BY version) FROM schema_migrations`).Scan(&actualLedger))
	require.Equal(t, communicationLedger, actualLedger, "the historical migration stops at M127")
	require.NoError(t, pool.QueryRow(ctx, fingerprint).Scan(&after))
	require.Equal(t, before, after, "credential custody is unchanged")
	store := NewCommunicationStore(journal.db, 1750, "", "")
	st, e := store.Read(ctx, org, project)
	require.NoError(t, e)
	require.True(t, st.AutomaticEnabled)
	require.Equal(t, 1750, st.CadenceMS)
	require.Error(t, schemaauthority.VerifyRuntimeLedger(oldLedger, communicationLedger), "old binary cannot honor M127 manual policy")
	require.NoError(t, migrate.RunExistingThrough(ctx, pool, "../../migrations", schemaauthority.XInsightCommunicationHead), "idempotent explicit migration")
}

func TestCommunicationKnownProcessExitRecoveryAndUnitBudgetIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, _ := setupRedeemInstallation(t, pool)
	s, org, project, actor := communicationFixture(t, journal)
	ctx := context.Background()
	abandoned, _, e := communicationClaim(t, s, org, project, actor, 1, "abandoned")
	require.NoError(t, e)
	_, e = pool.Exec(ctx, `UPDATE xinsight_communication_policies SET dispatch_deadline=clock_timestamp()-interval '1 second' WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, e)
	_, _, e = communicationClaim(t, s, org, project, actor, 1, "no-timeout-steal")
	require.ErrorIs(t, e, ErrCommunicationBusy)
	// A restart cannot infer death from timeout. Only authoritative stopped and
	// ingress-absent proof permits the local safety update to reconcile the slot.
	_, e = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped',ingress_state='absent' WHERE instance_id=$1`, s.ownerInstance)
	require.NoError(t, e)
	owner := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("8", 32), Artifact: s.ownerArtifact}
	ledger, e := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, e)
	_, _, e = runtimegeneration.Renew(ctx, pool, owner, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
	require.NoError(t, e)
	next := NewCommunicationStore(pool, 1250, owner.Instance, owner.Artifact)
	communicationUpdate(t, next, org, project, actor, 1, false)
	st, e := next.Read(ctx, org, project)
	require.NoError(t, e)
	require.Equal(t, "paused", st.Effective)
	require.Equal(t, "interrupted", st.LastRun.State)
	require.Error(t, abandoned.Finish(ctx, "completed", true), "old owner cannot overwrite interrupted receipt")
	p, _, e := communicationClaim(t, next, org, project, actor, 2, "new-explicit-request")
	require.NoError(t, e)
	for n := 0; n < CommunicationMaxUnits; n++ {
		require.NoError(t, p.NextUnit(ctx))
	}
	require.ErrorIs(t, p.NextUnit(ctx), ErrCommunicationPaused)
	require.NoError(t, p.Finish(ctx, "partial", false))
	communicationUpdate(t, next, org, project, actor, 2, true)
	_, _, e = communicationClaim(t, next, org, project, actor, 3, "automatic-on-reject")
	require.ErrorIs(t, e, ErrConflict)
	replay, receipt, e := communicationClaim(t, next, org, project, actor, 2, "new-explicit-request")
	require.NoError(t, e)
	require.Nil(t, replay)
	require.Equal(t, CommunicationMaxUnits, receipt.Units)
	next.SetRuntimeEnabled(false)
	st, e = next.Read(ctx, org, project)
	require.NoError(t, e)
	require.Equal(t, "runtime_disabled", st.Effective)
}
