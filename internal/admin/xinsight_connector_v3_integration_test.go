//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type adoptionResolver struct{ calls int }

func (r *adoptionResolver) ResolveXInsightReceiverBinding(context.Context, string, string, string) (XInsightReceiverBinding, error) {
	r.calls++
	return XInsightReceiverBinding{}, xinsightconnector.ErrConflict
}
func loadAdminV3(t *testing.T, name string) xinsightconnector.ControlOperationV3 {
	t.Helper()
	raw, e := os.ReadFile("../xinsightconnector/testdata/control-v3/valid/" + name + ".json")
	require.NoError(t, e)
	o, e := xinsightconnector.DecodeControlOperationV3(raw)
	require.NoError(t, e)
	return o
}

type adoptionFixture struct {
	sourceControlFixture
	journal      *xinsightconnector.PostgresPhaseJournal
	executor     *XInsightConnectorActivationExecutor
	counterpart  *cp2bCounterpart
	service      *completedfeed.Service
	resolver     *adoptionResolver
	installation xinsightconnector.Installation
	bind         xinsightconnector.ControlOperationV3
	prepared     xinsightconnector.PreparedInvitationRedeem
	rawFeed      string
}

func newAdoptionFixture(t *testing.T) adoptionFixture {
	t.Helper()
	ctx := context.Background()
	f := adoptionFixture{sourceControlFixture: newSourceControlFixture(t), counterpart: &cp2bCounterpart{}, resolver: &adoptionResolver{}}
	var e error
	f.service, e = completedfeed.NewService(f.pool, syntheticSourceControlSecret, f.source.SourceInstance(), true)
	require.NoError(t, e)
	f.handler.SetCompletedFeedController(f.service)
	human := func(action, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
		r := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v1/data-exchange/xinsight/connections/"+stringInt64(f.projectID)+"/"+action, body, f.actorID, f.orgID, auth.RolePlatformAdmin, "adoption-"+action)
		r.SetPathValue("project_id", stringInt64(f.projectID))
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	reg, raw := testXInsightFeedRegistration(t, "adoption-original")
	f.rawFeed = raw
	created := human("credentials", xInsightFeedCreateBody(t, "adoption-original", "127.0.0.1/32"), f.handler.CreateXInsightFeedCredential)
	require.Equal(t, 201, created.Code, created.Body.String())
	var response struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &response))
	f.activeKeyID = response.ID
	activated := human("credentials/activate", fmt.Sprintf(`{"staged_credential_id":%d}`, f.activeKeyID), f.handler.ActivateInitialXInsightFeedCredential)
	require.Equal(t, 200, activated.Code, activated.Body.String())
	resumed := human("resume", "", f.handler.ResumeXInsightFeedConnection)
	require.Equal(t, 200, resumed.Code, resumed.Body.String())
	_, e = f.pool.Exec(ctx, `UPDATE completed_execution_feed_connections SET acknowledged_sequence=7 WHERE project_id=$1`, f.projectID)
	require.NoError(t, e)
	manager, e := xinsightconnector.NewManager(f.pool, xinsightconnector.CommitAuthorizerFunc(func(context.Context, pgx.Tx, int64, int64, int64, string) error { return nil }), f.service)
	require.NoError(t, e)
	f.installation, _, e = manager.CreateInstallation(ctx, xinsightconnector.Consent{ReceiverMode: xinsightconnector.ReceiverModeAdoptExisting, InstallationID: cp2bInstallationID, XDeploymentID: cp2bDeploymentID, InvitationSHA256: cp2bSHA(cp2bInvitation), OrgID: f.orgID, ProjectID: f.projectID, EventScope: xinsightconnector.EventScopeModelCompleted, IdempotencyKey: "adoption-consent", ConsentedByUserID: f.actorID})
	require.NoError(t, e)
	f.journal, e = xinsightconnector.NewPostgresPhaseJournal(f.pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, e)
	require.NoError(t, f.journal.StageInvitation(ctx, cp2bInstallationID, cp2bInvitation))
	f.prepared, e = f.journal.PrepareRedeem(ctx, cp2bInstallationID)
	require.NoError(t, e)
	var request struct {
		SchemaVersion string                                      `json:"schema_version"`
		Bootstrap     xinsightconnector.ReceiverAdoptionBootstrap `json:"receiver_adoption_bootstrap"`
		SHA           string                                      `json:"receiver_adoption_bootstrap_sha256"`
	}
	require.NoError(t, json.Unmarshal(f.prepared.CanonicalRequest, &request))
	require.Equal(t, xinsightconnector.RedeemAdoptionRequestSchema, request.SchemaVersion)
	require.Equal(t, stringInt64(f.activeKeyID), request.Bootstrap.FeedKeyID)
	require.Equal(t, f.installation.SourceBindingSHA256, request.Bootstrap.SourceBindingSHA256)
	ack := struct {
		SchemaVersion   string    `json:"schema_version"`
		InstallationID  string    `json:"installation_id"`
		DeploymentID    string    `json:"deployment_id"`
		ProjectID       string    `json:"project_id"`
		FinalEventScope []string  `json:"final_event_scope"`
		ConsentSHA256   string    `json:"consent_sha256"`
		ConfirmedAt     time.Time `json:"confirmed_at"`
	}{"xinsight.connector-installation-acknowledgement.v1", cp2bInstallationID, cp2bDeploymentID, f.prepared.ControlProjectID, []string{xinsightconnector.EventScopeModelCompleted}, f.installation.SourceBindingSHA256, f.installation.ConsentedAt.UTC()}
	result, e := json.Marshal(map[string]any{"schema_version": xinsightconnector.RedeemAdoptionResultSchema, "installation": map[string]any{"schema_version": "xinsight.connector-installation.v1", "installation_id": cp2bInstallationID, "deployment_id": cp2bDeploymentID, "connector_type": "unirouter", "requested_event_scope": []string{xinsightconnector.EventScopeModelCompleted}, "final_event_scope": []string{xinsightconnector.EventScopeModelCompleted}, "project_id": f.prepared.ControlProjectID, "installation_acknowledgement_sha256": xinsightconnector.PublicCanonicalSHA256(ack), "status": "pending", "desired_state": "active", "observed_state": "agent_registered", "created_at": f.installation.ConsentedAt, "updated_at": f.installation.ConsentedAt, "version": 1}, "receiver_adoption_bootstrap_sha256": request.SHA})
	require.NoError(t, e)
	// Pending preparation is byte stable; neither key drift nor process restart
	// may silently regenerate its public bootstrap or control authority.
	again, e := f.journal.PrepareRedeem(ctx, cp2bInstallationID)
	require.NoError(t, e)
	require.Equal(t, f.prepared.CanonicalRequest, again.CanonicalRequest)
	tampered := strings.Replace(string(result), request.SHA, "sha256:"+strings.Repeat("0", 64), 1)
	require.Error(t, f.journal.FinalizeRedeem(ctx, f.prepared, []byte(tampered)))
	require.NoError(t, f.journal.FinalizeRedeem(ctx, f.prepared, result))
	require.NoError(t, f.journal.FinalizeRedeem(ctx, f.prepared, result))
	f.executor, e = NewXInsightConnectorActivationExecutor(f.handler, f.resolver)
	require.NoError(t, e)
	f.bind = loadAdminV3(t, "01-bind-existing-operation")
	f.bind.ProjectID = f.prepared.ControlProjectID
	q := f.bind.Request.(xinsightconnector.BindExistingReceiverRequest)
	q.Candidate.SourceInstance = f.source.SourceInstance()
	q.Candidate.SourceBindingSHA256 = f.installation.SourceBindingSHA256
	q.Candidate.FeedKeyID = stringInt64(f.activeKeyID)
	q.Candidate.AllowedIP = "127.0.0.1/32"
	q.Candidate.ReceiverRegistration = xinsightconnector.ReceiverRegistrationV1{SchemaVersion: reg.SchemaVersion, TokenFormat: reg.TokenFormat, VerifierScheme: reg.VerifierScheme, KeyPrefix: reg.KeyPrefix, VerifierSHA256: reg.VerifierSHA256, OperationID: reg.OperationID}
	q.Candidate.WorkerConfig.SourceInstance = f.source.SourceInstance()
	q.Candidate.WorkerConfig.ReceiverRegistrationSHA256 = xinsightconnector.PublicCanonicalSHA256(q.Candidate.ReceiverRegistration)
	q.CandidateSHA256 = xinsightconnector.PublicCanonicalSHA256(q.Candidate)
	f.bind.Request = q
	return f
}
func (f adoptionFixture) run(t *testing.T, o xinsightconnector.ControlOperationV3) (xinsightconnector.ControlResultV3, error) {
	t.Helper()
	raw, e := o.CanonicalJSON()
	require.NoError(t, e)
	f.counterpart.operation = raw
	agent, e := xinsightconnector.NewTransactionalAgent(cp2bInstallationID, f.counterpart, f.journal, f.executor)
	require.NoError(t, e)
	processed, e := agent.RunOnce(context.Background())
	require.True(t, processed)
	if e != nil {
		return xinsightconnector.ControlResultV3{}, e
	}
	result, e := xinsightconnector.DecodeControlResultV3(f.counterpart.acks[len(f.counterpart.acks)-1])
	require.NoError(t, e)
	return result, nil
}
func (f adoptionFixture) confirmation(t *testing.T, b xinsightconnector.ControlResultV3) xinsightconnector.ControlOperationV3 {
	t.Helper()
	o := loadAdminV3(t, "03-confirm-existing-operation")
	o.ProjectID = f.prepared.ControlProjectID
	o.PredecessorOperationID = f.bind.OperationID
	o.PredecessorResultSHA256 = xinsightconnector.PublicCanonicalSHA256(b)
	q := o.Request.(xinsightconnector.ConfirmExistingReceiverRequest)
	c := f.bind.Request.(xinsightconnector.BindExistingReceiverRequest).Candidate
	q.AdoptionBindingSHA256 = b.Result.(xinsightconnector.BindExistingReceiverResult).AdoptionBindingSHA256
	q.XIAdoptionReceipt.BindResultSHA256 = o.PredecessorResultSHA256
	q.XIAdoptionReceipt.CandidateSHA256 = xinsightconnector.PublicCanonicalSHA256(c)
	q.XIAdoptionReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(q.XIAdoptionReceipt)
	q.FeedReadReceipt.FeedKeyID = c.FeedKeyID
	q.FeedReadReceipt.ReceiverRegistrationSHA256 = xinsightconnector.PublicCanonicalSHA256(c.ReceiverRegistration)
	q.FeedReadReceipt.WorkerConfigSHA256 = xinsightconnector.PublicCanonicalSHA256(c.WorkerConfig)
	q.FeedReadReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(q.FeedReadReceipt)
	o.Request = q
	return o
}
func TestConnectorV3AdoptionPairingAtomicIntegration(t *testing.T) {
	f := newAdoptionFixture(t)
	ctx := context.Background()
	// Active nonempty prior feed is preserved; ACK loss must replay one terminal.
	f.counterpart.ackError = errors.New("synthetic ACK response loss")
	_, e := f.run(t, f.bind)
	require.ErrorContains(t, e, "synthetic ACK")
	first := append([]byte(nil), f.counterpart.acks[0]...)
	bind, e := f.run(t, f.bind)
	require.NoError(t, e)
	require.Equal(t, first, f.counterpart.acks[1])
	requireCP2BAuditCount(t, f.pool, f.projectID, "data_exchange.xinsight.receiver.bind", 1)
	confirm := f.confirmation(t, bind)
	_, e = f.run(t, confirm)
	require.ErrorIs(t, e, xinsightconnector.ErrConflict, "no authenticated original-key pull after bind")
	// Exercise the existing authenticated handler; an invalid page request records
	// authentication but cannot substitute for XI's successful CommitPage receipt.
	resolver, e := middleware.NewClientIPResolver(nil)
	require.NoError(t, e)
	feed := middleware.CompletedExecutionFeedAuth(f.pool, syntheticSourceControlSecret, resolver)(f.service.Handler())
	r := httptest.NewRequest("GET", "/api/v1/integrations/xinsight/completed-executions", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Authorization", "Bearer "+f.rawFeed)
	w := httptest.NewRecorder()
	feed.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	bad := confirm
	q := bad.Request.(xinsightconnector.ConfirmExistingReceiverRequest)
	q.FeedReadReceipt.PagesCommitted = q.XIAdoptionReceipt.PagesCommittedBefore
	q.FeedReadReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(q.FeedReadReceipt)
	bad.Request = q
	_, e = bad.CanonicalJSON()
	require.Error(t, e)
	drift := confirm
	dq := drift.Request.(xinsightconnector.ConfirmExistingReceiverRequest)
	dq.XIAdoptionReceipt.CommittedCursorSHA256 = "sha256:" + strings.Repeat("a", 64)
	dq.XIAdoptionReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(dq.XIAdoptionReceipt)
	drift.Request = dq
	_, e = f.run(t, drift)
	require.ErrorIs(t, e, xinsightconnector.ErrConflict)
	confirmed, e := f.run(t, confirm)
	require.NoError(t, e)
	require.Equal(t, "existing_receiver_adopted", confirmed.Result.(xinsightconnector.ConfirmExistingReceiverResult).Readiness)
	requireCP2BFeedState(t, f.pool, f.projectID, "active", 1, 0, "connected")
	var cursor int64
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT acknowledged_sequence FROM completed_execution_feed_connections WHERE project_id=$1`, f.projectID).Scan(&cursor))
	require.Equal(t, int64(7), cursor)
	require.Zero(t, f.resolver.calls)
	for _, name := range []string{"05-policy-pair-operation", "07-source-enroll-pair-operation"} {
		o := loadAdminV3(t, name)
		o.ProjectID = f.prepared.ControlProjectID
		o.PredecessorOperationID = confirm.OperationID
		o.PredecessorResultSHA256 = xinsightconnector.PublicCanonicalSHA256(confirmed)
		q := o.Request.(xinsightconnector.DeliverMachineRequest)
		q.SourceInstance = f.source.SourceInstance()
		q.SourceBindingSHA256 = f.installation.SourceBindingSHA256
		q.Registration.AllowedIP = "127.0.0.1/32"
		q.RegistrationSHA256 = xinsightconnector.PublicCanonicalSHA256(q.Registration)
		q.CustodyReceipt.RegistrationSHA256 = q.RegistrationSHA256
		q.CustodyReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(q.CustodyReceipt)
		o.Request = q
		result, e := f.run(t, o)
		require.NoError(t, e)
		require.Equal(t, "not_asserted", result.Result.(xinsightconnector.DeliverMachineResult).Activation)
	}
	var count int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_machine_credentials`).Scan(&count))
	require.Zero(t, count, "delivery is not registration or activation")
	detail := summarizeXInsightConnectorInstallation(f.installation)
	human := apiKeyIntegrationRequest(t, f.pool, "GET", "/", "", f.actorID, f.orgID, auth.RolePlatformAdmin, "adoption-readback")
	require.NoError(t, f.handler.readConnectorV3PublicDetails(human.Context(), detail, f.orgID, f.projectID))
	require.Len(t, detail.DeliveredMachineCandidates, 2)
	require.Equal(t, "confirmed", detail.ReceiverAdoption.Status)
	public, e := json.Marshal(detail)
	require.NoError(t, e)
	require.NotContains(t, string(public), f.rawFeed)
	require.NotContains(t, string(public), "cursor_sha256")
	for _, candidate := range detail.DeliveredMachineCandidates {
		body, _ := json.Marshal(candidate.Registration)
		r := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v2/organizations/1/xinsight-policy-credentials", string(body), f.actorID, f.orgID, auth.RolePlatformAdmin, "human-machine-register-"+candidate.Purpose)
		r.SetPathValue("org_id", stringInt64(f.orgID))
		w := httptest.NewRecorder()
		f.handler.RegisterXInsightMachineCredential(w, r)
		require.Equal(t, 201, w.Code, w.Body.String())
		var c struct {
			KeyID string `json:"key_id"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &c))
		require.NotEmpty(t, c.KeyID)
		activation := apiKeyIntegrationRequest(t, f.pool, "POST", "/activate", "{}", f.actorID, f.orgID, auth.RolePlatformAdmin, "human-machine-activate-"+candidate.Purpose)
		activation.SetPathValue("org_id", strconv.FormatInt(f.orgID, 10))
		activation.SetPathValue("key_id", c.KeyID)
		aw := httptest.NewRecorder()
		f.handler.ActivateXInsightMachineCredential(aw, activation)
		require.Equal(t, 200, aw.Code, aw.Body.String())
	}
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE purpose IN ('xinsight.policy.manage','xinsight.source.enroll') AND status='feed_active'`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM xinsight_policy_delegations)+(SELECT count(*) FROM xinsight_source_enrollments)`).Scan(&count))
	require.Zero(t, count, "human credential activation does not create grants")
	// Revoked saved authority blocks new work but never rewrites a terminal replay.
	_, e = f.pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.actorID)
	require.NoError(t, e)
	_, e = f.run(t, f.bind)
	require.NoError(t, e)
	conflict := f.bind
	conflict.OperationID = "xco_FFFFFFFFFFFFFFFFFFFFFF"
	_, e = f.run(t, conflict)
	require.ErrorIs(t, e, xinsightconnector.ErrUnauthorized)
}
func TestConnectorV3BootstrapDriftAndAuditRollbackIntegration(t *testing.T) {
	t.Run("original key drift", func(t *testing.T) {
		f := newAdoptionFixture(t)
		ctx := context.Background()
		_, e := f.pool.Exec(ctx, `UPDATE api_keys SET allowed_ips='127.0.0.2/32' WHERE id=$1`, f.activeKeyID)
		require.NoError(t, e)
		_, e = f.run(t, f.bind)
		require.ErrorIs(t, e, xinsightconnector.ErrConflict)
		var count int
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_receiver_adoptions`).Scan(&count))
		require.Zero(t, count)
	})
	t.Run("audit failure rolls back claim and binding", func(t *testing.T) {
		f := newAdoptionFixture(t)
		ctx := context.Background()
		_, e := f.pool.Exec(ctx, `CREATE FUNCTION reject_adoption_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='data_exchange.xinsight.receiver.bind' THEN RAISE EXCEPTION 'synthetic adoption audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_adoption_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_adoption_audit()`)
		require.NoError(t, e)
		_, e = f.run(t, f.bind)
		require.Error(t, e)
		var count int
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM xinsight_receiver_adoptions)+(SELECT count(*) FROM xinsight_connector_phase_operations)`).Scan(&count))
		require.Zero(t, count)
		_, e = f.pool.Exec(ctx, `DROP TRIGGER reject_adoption_audit ON audit_logs; DROP FUNCTION reject_adoption_audit()`)
		require.NoError(t, e)
		_, e = f.run(t, f.bind)
		require.NoError(t, e)
	})
}

func TestConnectorV2OnM123BridgePreservesLifecycleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightPolicyExpandHead)
	ctx := t.Context()
	journal, executor, counterpart, project, _ := setupCP2BActivation(t, pool)
	agent, e := xinsightconnector.NewTransactionalAgent(cp2bInstallationID, counterpart, journal, executor)
	require.NoError(t, e)
	prepare := cp2bPrepareOperation()
	counterpart.operation = canonicalCP2BOperation(t, prepare)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	prepared := decodeCP2BResult(t, counterpart.acks[0])
	priorSHA, e := prepared.CanonicalSHA256()
	require.NoError(t, e)
	registration := cp2bReceiverRegistration(t, auth.ReceiverFeedTokenPrefix+strings.Repeat("ab", 32))
	register := cp2bRegisterOperation(prepare, priorSHA, registration)
	counterpart.operation = canonicalCP2BOperation(t, register)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	registered := decodeCP2BResult(t, counterpart.acks[1])
	priorSHA, e = registered.CanonicalSHA256()
	require.NoError(t, e)
	resume := cp2bResumeOperation(t, register, registered, priorSHA)
	counterpart.operation = canonicalCP2BOperation(t, resume)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	requireCP2BFeedState(t, pool, project, "active", 1, 0, "connected")
	var i xInsightConnectorInstallationSummary
	i.InstallationID = cp2bInstallationID
	i.ControlFence = 1
	var org, actor int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT org_id,consented_by FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&org, &actor))
	human := apiKeyIntegrationRequest(t, pool, "GET", "/", "", actor, org, auth.RolePlatformAdmin, "v2-bridge-read")
	require.NoError(t, executor.handler.readConnectorV3PublicDetails(human.Context(), &i, org, project))
	require.Empty(t, i.DeliveredMachineCandidates)
	require.Nil(t, i.ReceiverAdoption)
}

func TestConnectorAdoptionConsentRejectsIneligibleOriginalBeforePersistenceIntegration(t *testing.T) {
	f := newXInsightFeedAuthorizationFixture(t, true)
	service, e := completedfeed.NewService(f.pool, syntheticSourceControlSecret, "synthetic-source-control", true)
	require.NoError(t, e)
	manager, e := xinsightconnector.NewManager(f.pool, xinsightconnector.CommitAuthorizerFunc(func(context.Context, pgx.Tx, int64, int64, int64, string) error { return nil }), service)
	require.NoError(t, e)
	_, _, e = manager.CreateInstallation(t.Context(), xinsightconnector.Consent{ReceiverMode: xinsightconnector.ReceiverModeAdoptExisting, InstallationID: cp2bInstallationID, XDeploymentID: cp2bDeploymentID, InvitationSHA256: cp2bSHA(cp2bInvitation), OrgID: f.orgID, ProjectID: f.projectID, EventScope: xinsightconnector.EventScopeModelCompleted, IdempotencyKey: "ineligible-adoption", ConsentedByUserID: f.actorID})
	require.ErrorIs(t, e, xinsightconnector.ErrConflict)
	var count int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_connector_installations`).Scan(&count))
	require.Zero(t, count)
	var status string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT status FROM completed_execution_feed_connections WHERE project_id=$1`, f.projectID).Scan(&status))
	require.Equal(t, "active", status)
}

func TestConnectorV3ConcurrentReplayAndConflictingBindIntegration(t *testing.T) {
	f := newAdoptionFixture(t)
	ctx := t.Context()
	start := make(chan struct{})
	var wg sync.WaitGroup
	type outcome struct {
		raw []byte
		sha string
		err error
	}
	results := make(chan outcome, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			raw, sha, e := f.journal.ExecuteV3(ctx, f.bind, f.executor)
			results <- outcome{raw, sha, e}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var first outcome
	for r := range results {
		require.NoError(t, r.err)
		if first.raw == nil {
			first = r
		} else {
			require.Equal(t, first.raw, r.raw)
			require.Equal(t, first.sha, r.sha)
		}
	}
	requireCP2BAuditCount(t, f.pool, f.projectID, "data_exchange.xinsight.receiver.bind", 1)
	other := f.bind
	other.OperationID = "xco_FFFFFFFFFFFFFFFFFFFFFF"
	_, _, e := f.journal.ExecuteV3(ctx, other, f.executor)
	require.ErrorIs(t, e, xinsightconnector.ErrConflict)
	var count int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_connector_phase_operations`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestConnectorV3PairsAfterOriginalV2ResumeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	journal, executor, counterpart, project, _ := setupCP2BActivation(t, pool)
	agent, e := xinsightconnector.NewTransactionalAgent(cp2bInstallationID, counterpart, journal, executor)
	require.NoError(t, e)
	prepare := cp2bPrepareOperation()
	counterpart.operation = canonicalCP2BOperation(t, prepare)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	prepared := decodeCP2BResult(t, counterpart.acks[0])
	priorSHA, e := prepared.CanonicalSHA256()
	require.NoError(t, e)
	registration := cp2bReceiverRegistration(t, auth.ReceiverFeedTokenPrefix+strings.Repeat("ab", 32))
	register := cp2bRegisterOperation(prepare, priorSHA, registration)
	counterpart.operation = canonicalCP2BOperation(t, register)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	registered := decodeCP2BResult(t, counterpart.acks[1])
	priorSHA, e = registered.CanonicalSHA256()
	require.NoError(t, e)
	resume := cp2bResumeOperation(t, register, registered, priorSHA)
	counterpart.operation = canonicalCP2BOperation(t, resume)
	_, e = agent.RunOnce(ctx)
	require.NoError(t, e)
	resumed := decodeCP2BResult(t, counterpart.acks[2])
	priorSHA, e = resumed.CanonicalSHA256()
	require.NoError(t, e)
	requireCP2BFeedState(t, pool, project, "active", 1, 0, "connected")
	var org, actor int64
	var source, binding, mode string
	require.NoError(t, pool.QueryRow(ctx, `SELECT org_id,consented_by,source_instance,source_binding_sha256,receiver_mode
		FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&org, &actor, &source, &binding, &mode))
	require.Equal(t, xinsightconnector.ReceiverModeNewInstall, mode)
	for _, name := range []string{"05-policy-pair-operation", "07-source-enroll-pair-operation"} {
		o := loadAdminV3(t, name)
		o.ProjectID = cp2bProjectRef
		if name == "05-policy-pair-operation" {
			o.OperationID = "xco_EEEEEEEEEEEEEEEEEEEEEE"
		} else {
			o.OperationID = "xco_FFFFFFFFFFFFFFFFFFFFFF"
		}
		o.PredecessorOperationID = resume.OperationID
		o.PredecessorResultSHA256 = priorSHA
		q := o.Request.(xinsightconnector.DeliverMachineRequest)
		q.SourceInstance, q.SourceBindingSHA256 = source, binding
		q.Registration.AllowedIP = "127.0.0.1/32"
		q.RegistrationSHA256 = xinsightconnector.PublicCanonicalSHA256(q.Registration)
		q.CustodyReceipt.RegistrationSHA256 = q.RegistrationSHA256
		q.CustodyReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(q.CustodyReceipt)
		o.Request = q
		raw, e := o.CanonicalJSON()
		require.NoError(t, e)
		counterpart.operation = raw
		_, e = agent.RunOnce(ctx)
		require.NoError(t, e)
		terminal := append([]byte(nil), counterpart.acks[len(counterpart.acks)-1]...)
		_, e = agent.RunOnce(ctx)
		require.NoError(t, e)
		require.Equal(t, terminal, counterpart.acks[len(counterpart.acks)-1])
		result, e := xinsightconnector.DecodeControlResultV3(terminal)
		require.NoError(t, e)
		require.Equal(t, "not_asserted", result.Result.(xinsightconnector.DeliverMachineResult).Activation)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_receiver_adoptions`).Scan(&count))
	require.Zero(t, count, "new install must not fabricate an adoption binding")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_machine_credentials`).Scan(&count))
	require.Zero(t, count, "candidate delivery does not activate machine credentials")
	i := &xInsightConnectorInstallationSummary{InstallationID: cp2bInstallationID, ControlFence: 1, SourceInstance: source, SourceBindingSHA256: binding}
	human := apiKeyIntegrationRequest(t, pool, "GET", "/", "", actor, org, auth.RolePlatformAdmin, "v2-pair-read")
	require.NoError(t, executor.handler.readConnectorV3PublicDetails(human.Context(), i, org, project))
	require.Len(t, i.DeliveredMachineCandidates, 2)
	require.Nil(t, i.ReceiverAdoption)
	requireCP2BAuditCount(t, pool, project, "data_exchange.xinsight.machine_candidate.deliver", 2)
}
