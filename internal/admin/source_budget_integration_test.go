//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

const sourceBudgetPath = "/api/v1/integrations/xinsight/source/v2/budgets"

func enableSourceBudgetFixture(t *testing.T, f *sourceControlFixture) *billing.Billing {
	t.Helper()
	events, err := outbox.New(f.pool, outbox.DefaultOptions())
	require.NoError(t, err)
	financial := billing.NewBilling(f.pool, nil)
	financial.SetOutbox(events)
	f.handler.SetOutbox(events)
	f.source, err = sourcefacts.New(f.pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: true, BudgetSnapshotsEnabled: true, FinancialJournalReady: financial.FinancialJournalReady, SourceInstance: "synthetic-source-control", CursorSecret: syntheticSourceControlSecret})
	require.NoError(t, err)
	require.NoError(t, f.source.CheckReady(t.Context()))
	f.handler.SetSourceFactsController(f.source)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	f.reader = f.source.BudgetHandler(resolver)
	return financial
}

func TestSourceBudgetAddingConnectionProfilesNeverGrantsExistingCredentialIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	reg, token := f.registration(t, "A-stays-A", []string{sourcefacts.ContextProfile})
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	before := sourcePage(t, f.read(t, token, sourceReadPath+"?profile="+sourcefacts.ContextProfile, "127.0.0.1:2000", 200))
	enableSourceBudgetFixture(t, &f)
	profiles := append([]string{sourcefacts.ContextProfile}, sourcefacts.BudgetProfiles()...)
	slices.Sort(profiles)
	f.control(t, "profiles", map[string]any{"profiles": profiles}, 200)
	f.read(t, token, sourceBudgetPath, "127.0.0.1:2000", 403)
	var registered []string
	var generation int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT sc.profiles,c.scope_generation FROM execution_source_credentials sc JOIN execution_source_connections c USING(org_id,project_id) WHERE sc.api_key_id=$1`, key["credential_id"]).Scan(&registered, &generation))
	require.Equal(t, []string{sourcefacts.ContextProfile}, registered)
	require.EqualValues(t, 1, generation)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	f.reader = f.source.Handler(resolver)
	f.capture(t, "D-not-auto-granted")
	require.Len(t, sourcePage(t, f.read(t, token, sourceReadPath+"?profile="+sourcefacts.ContextProfile+"&cursor="+url.QueryEscape(before.NextCursor), "127.0.0.1:2000", 200)).Events, 1)
}

func TestSourceBudgetBindingChecksCurrentRoleSessionScopeAndAuditAtomicityIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	enableSourceBudgetFixture(t, &f)
	ctx := t.Context()
	body := sourceBudgetBindingRequest{OrgID: f.orgID, ProjectID: f.projectID, ScopeKind: "project", ScopeRef: "explicit-synthetic-scope", ExpectedRevision: "0", Status: "active"}
	makeRequest := func(actor int64, role string, b sourceBudgetBindingRequest) *http.Request {
		wire, err := json.Marshal(b)
		require.NoError(t, err)
		return apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v1/enterprise-finance/source-budget-bindings", string(wire), actor, f.orgID, role, "D-binding-authorization")
	}
	invoke := func(r *http.Request, status int) {
		w := httptest.NewRecorder()
		f.handler.SetSourceBudgetBinding(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	var orgAdmin, billingAdmin int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,status) VALUES($1,'D-org-admin','synthetic','org_admin','active') RETURNING id`, f.orgID).Scan(&orgAdmin))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,status) VALUES($1,'D-billing-admin','synthetic','billing_admin','active') RETURNING id`, f.orgID).Scan(&billingAdmin))
	invoke(makeRequest(orgAdmin, auth.RoleOrgAdmin, body), 403)
	invalid := body
	invalid.ScopeKind = "cost_center"
	invoke(makeRequest(billingAdmin, auth.RoleBillingAdmin, invalid), 400)
	stale := makeRequest(billingAdmin, auth.RoleBillingAdmin, body)
	_, err := f.pool.Exec(ctx, `UPDATE users SET role='org_admin' WHERE id=$1`, billingAdmin)
	require.NoError(t, err)
	invoke(stale, 403)
	_, err = f.pool.Exec(ctx, `UPDATE users SET role='billing_admin' WHERE id=$1`, billingAdmin)
	require.NoError(t, err)
	staleSession := makeRequest(billingAdmin, auth.RoleBillingAdmin, body)
	_, err = f.pool.Exec(ctx, `UPDATE admin_sessions SET revoked_at=clock_timestamp() WHERE user_id=$1 AND sid_hash=$2`, billingAdmin, middleware.GetAdminSessionHashFromContext(staleSession.Context()))
	require.NoError(t, err)
	invoke(staleSession, 403)
	var foreignOrg, foreignProject int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('D-foreign','active','standard') RETURNING id`).Scan(&foreignOrg))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status) VALUES($1,'D-foreign','D-foreign','active') RETURNING id`, foreignOrg).Scan(&foreignProject))
	foreign := body
	foreign.ProjectID = foreignProject
	invoke(makeRequest(billingAdmin, auth.RoleBillingAdmin, foreign), 404)
	inactive := makeRequest(billingAdmin, auth.RoleBillingAdmin, body)
	_, err = f.pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, f.projectID)
	require.NoError(t, err)
	invoke(inactive, 404)
	_, err = f.pool.Exec(ctx, `UPDATE projects SET status='active' WHERE id=$1`, f.projectID)
	require.NoError(t, err)
	// Fail only the original audit insert: no binding/history can commit alone.
	_, err = f.pool.Exec(ctx, `CREATE FUNCTION reject_synthetic_budget_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='evidence.audit.v1' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_synthetic_budget_audit BEFORE INSERT ON internal_outbox_events FOR EACH ROW EXECUTE FUNCTION reject_synthetic_budget_audit()`)
	require.NoError(t, err)
	invoke(makeRequest(billingAdmin, auth.RoleBillingAdmin, body), 503)
	var count int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_budget_bindings WHERE org_id=$1`, f.orgID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_budget_binding_history WHERE org_id=$1`, f.orgID).Scan(&count))
	require.Zero(t, count)
}

type sourceBudgetCommitGateDB struct {
	*pgxpool.Pool
	reached, release chan struct{}
	pid              chan uint32
}
type sourceBudgetCommitGateTx struct {
	pgx.Tx
	reached, release chan struct{}
}

func (d sourceBudgetCommitGateDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	d.pid <- tx.Conn().PgConn().PID()
	return sourceBudgetCommitGateTx{tx, d.reached, d.release}, nil
}
func (t sourceBudgetCommitGateTx) Commit(ctx context.Context) error {
	close(t.reached)
	select {
	case <-t.release:
		return t.Tx.Commit(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestSourceBudgetReaderFailsClosedAcrossCommittedBindingChangeIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	enableSourceBudgetFixture(t, &f)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	profiles := sourcefacts.BudgetProfiles()
	slices.Sort(profiles)
	reg, token := f.registration(t, "D-concurrent", profiles)
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	reached, release := make(chan struct{}), make(chan struct{})
	pid := make(chan uint32, 1)
	gated := NewHandler(sourceBudgetCommitGateDB{f.pool, reached, release, pid}, syntheticSourceControlSecret, nil, nil)
	events, err := outbox.New(f.pool, outbox.DefaultOptions())
	require.NoError(t, err)
	gated.SetOutbox(events)
	body := sourceBudgetBindingRequest{OrgID: f.orgID, ProjectID: f.projectID, ScopeKind: "project", ScopeRef: "D-concurrent-scope", ExpectedRevision: "0", Status: "active"}
	wire, err := json.Marshal(body)
	require.NoError(t, err)
	// Retain the authenticated context while adding a bounded cancellation.
	request := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v1/enterprise-finance/source-budget-bindings", string(wire), f.actorID, f.orgID, auth.RolePlatformAdmin, "D-concurrent-write")
	writerCtx, writerCancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer writerCancel()
	request = request.WithContext(writerCtx)
	written := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); gated.SetSourceBudgetBinding(w, request); written <- w }()
	var writerPID uint32
	select {
	case writerPID = <-pid:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-reached:
	case w := <-written:
		t.Fatalf("writer failed before commit: %d %s", w.Code, w.Body.String())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	reader := f.source.BudgetHandler(resolver)
	readDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("GET", sourceBudgetPath, nil).WithContext(ctx)
		r.RemoteAddr = "127.0.0.1:2000"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		reader.ServeHTTP(w, r)
		readDone <- w
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		e := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, writerPID).Scan(&blocked)
		return e == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	close(release)
	select {
	case w := <-written:
		require.Equal(t, 200, w.Code, w.Body.String())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case w := <-readDone:
		require.Equal(t, 503, w.Code, w.Body.String())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	f.read(t, token, sourceBudgetPath, "127.0.0.1:2000", 200)
}

func sourceBudgetBody(t *testing.T, response *httptest.ResponseRecorder, profile string) map[string]any {
	t.Helper()
	wire := response.Body.Bytes()
	var reading sourcefacts.BudgetReading
	require.NoError(t, json.Unmarshal(wire, &reading))
	require.NotEmpty(t, reading.ReadingID)
	exportSourceBudgetSample(t, wire)
	for _, doc := range reading.Documents {
		require.Equal(t, doc.SourceSHA256, eventconnector.HashSourceBytes(doc.Document))
		var value map[string]any
		require.NoError(t, json.Unmarshal(doc.Document, &value))
		canonical, canonicalErr := eventconnector.CanonicalJSON(value)
		require.Nil(t, canonicalErr)
		require.JSONEq(t, string(canonical), string(doc.Document))
		require.Equal(t, canonical, []byte(doc.Document))
		require.Equal(t, reading.ReadingID, value["reading_id"])
		require.Equal(t, reading.ReadingCutoff, value["reading_cutoff"])
		require.Equal(t, reading.ObservedAt, value["observed_at"])
		if doc.Profile == profile {
			return value["body"].(map[string]any)
		}
	}
	t.Fatalf("missing authorized budget profile %s", profile)
	return nil
}

func TestSourceBudgetOriginalEnterpriseFlowAndDRevocationPreserveABIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	ctx := t.Context()
	empty := map[string]any{}
	reg, _ := f.registration(t, "D-disabled", []string{sourcefacts.BudgetDefinitionProfile})
	f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 503)
	enableSourceBudgetFixture(t, &f)
	profiles := append([]string{sourcefacts.ContextProfile, sourcefacts.RuntimeProfile}, sourcefacts.BudgetProfiles()...)
	slices.Sort(profiles)
	reg, token := f.registration(t, "D-explicit", profiles)
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", empty, 200)
	post := func(handler http.HandlerFunc, body any, actor int64, role string, pathValues map[string]string, status int) []byte {
		t.Helper()
		wire, err := json.Marshal(body)
		require.NoError(t, err)
		r := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v1/enterprise-finance/synthetic", string(wire), actor, f.orgID, role, "D-original-operation")
		for k, v := range pathValues {
			r.SetPathValue(k, v)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.Bytes()
	}
	post(f.handler.UpdateProjectBudget, map[string]any{"budget_cents": 50000, "budget_period": "monthly"}, f.actorID, auth.RolePlatformAdmin, map[string]string{"id": stringInt64(f.projectID)}, 200)
	var otherProject, checker int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'other synthetic project','other-d-project','active','none') RETURNING id`, f.orgID).Scan(&otherProject))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,status) VALUES($1,'D-billing-checker','synthetic-hash','billing_admin','active') RETURNING id`, f.orgID).Scan(&checker))
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	post(f.handler.CreateEnterpriseFiscalPeriod, map[string]any{"org_id": f.orgID, "period_ref": "D-period", "start_at": start, "end_at": end}, f.actorID, auth.RolePlatformAdmin, nil, 201)
	post(f.handler.CreateEnterpriseBudgetDraft, map[string]any{"org_id": f.orgID, "budget_ref": "D-project-budget", "scope": map[string]any{"kind": "project", "ref": "synthetic-project-scope"}, "fiscal_period_ref": "D-period", "amount": map[string]any{"value_minor": "1000000000", "currency": "CNY", "scale": 1000000}}, f.actorID, auth.RolePlatformAdmin, nil, 201)
	post(f.handler.ApproveEnterpriseBudget, map[string]any{"org_id": f.orgID, "expected_version": 1}, checker, auth.RoleBillingAdmin, map[string]string{"budget_ref": "D-project-budget"}, 200)
	// The original allocation authority validates these synthetic Source rows.
	_, err := f.pool.Exec(ctx, `INSERT INTO call_logs(request_id,org_id,project_id,cost_micros,created_at) VALUES('D-source-600',$1,$2,600000000,$4),('D-other-900',$1,$3,900000000,$4)`, f.orgID, f.projectID, otherProject, now)
	require.NoError(t, err)
	post(f.handler.CreateEnterpriseAllocationRun, map[string]any{"org_id": f.orgID, "allocation_run_ref": "D-original-run", "fiscal_period_ref": "D-period", "source_revision": "synthetic-source-revision", "rule_set_ref": "D-rules", "rule_set_revision": "1", "results": []any{
		map[string]any{"request_ref": "D-source-600", "target": map[string]any{"kind": "project", "ref": "synthetic-project-scope"}, "state": "direct", "amount": map[string]any{"value_minor": "600000000", "currency": "CNY", "scale": 1000000}},
		map[string]any{"request_ref": "D-other-900", "target": map[string]any{"kind": "cost_center", "ref": "private-other-cost-center"}, "state": "direct", "amount": map[string]any{"value_minor": "900000000", "currency": "CNY", "scale": 1000000}},
	}}, checker, auth.RoleBillingAdmin, nil, 201)
	binding := map[string]any{"org_id": f.orgID, "project_id": f.projectID, "scope_kind": "project", "scope_ref": "synthetic-project-scope", "expected_revision": "0", "status": "active"}
	post(f.handler.SetSourceBudgetBinding, binding, checker, auth.RoleBillingAdmin, nil, 200)
	post(f.handler.SetSourceBudgetBinding, binding, checker, auth.RoleBillingAdmin, nil, 409)
	var auditRef string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT audit_ref FROM execution_source_budget_bindings WHERE org_id=$1`, f.orgID).Scan(&auditRef))
	var auditCount int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM internal_outbox_events WHERE event_key=$1 AND event_type='evidence.audit.v1'`, auditRef).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	enterpriseTarget := sourceBudgetPath + "/current?budget_ref=enterprise-budget:D-project-budget"
	enterpriseWire := f.read(t, token, enterpriseTarget, "127.0.0.1:2000", 200)
	summary := sourceBudgetBody(t, enterpriseWire, sourcefacts.BudgetSummaryProfile)
	require.Equal(t, "exact", summary["coverage"].(map[string]any)["state"])
	require.Equal(t, "600000000", summary["known_subtotal"].(map[string]any)["value_minor"])
	require.Equal(t, "400000000", summary["comparison"].(map[string]any)["remaining"].(map[string]any)["value_minor"])
	for _, secret := range []string{"D-other-900", "900000000", "1500000000", "private-other-cost-center", "source_record_count", "approved_by"} {
		require.NotContains(t, enterpriseWire.Body.String(), secret)
	}
	gatewayTarget := sourceBudgetPath + "/current?budget_ref=gateway-project:" + stringInt64(f.projectID)
	gatewayWire := f.read(t, token, gatewayTarget, "127.0.0.1:2000", 200)
	gateway := sourceBudgetBody(t, gatewayWire, sourcefacts.BudgetSummaryProfile)
	require.Equal(t, "0", gateway["known_subtotal"].(map[string]any)["value_minor"], "enterprise/call-log source is not gateway journal money")
	require.Equal(t, "coverage_gap", gateway["comparison"].(map[string]any)["state"])
	notification := billing.NewEventService(f.pool, "")
	emitted, err := notification.CheckProjectBudget(ctx, f.orgID, f.projectID)
	require.NoError(t, err)
	require.NotNil(t, emitted)
	alertsWire := f.read(t, token, gatewayTarget, "127.0.0.1:2000", 200)
	alerts := sourceBudgetBody(t, alertsWire, sourcefacts.BudgetAlertProfile)
	require.Len(t, alerts["entries"], 1)
	alert := alerts["entries"].([]any)[0].(map[string]any)
	require.Equal(t, "original_event_amounts", alert["trigger_evidence"])
	require.Equal(t, "version_unknown", alert["budget_version_state"])
	require.Equal(t, "not_observed", alert["human_read"])
	// Handling an original reminder does not rewrite its trigger amounts or
	// imply a human read. Subsequent budget edits never mutate an old reading.
	var notificationID int64
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT id FROM notification_events WHERE org_id=$1 AND project_id=$2`, f.orgID, f.projectID).Scan(&notificationID))
	post(f.handler.AcknowledgeNotificationEvent, map[string]any{}, f.actorID, auth.RolePlatformAdmin, map[string]string{"id": stringInt64(notificationID)}, 200)
	handled := sourceBudgetBody(t, f.read(t, token, gatewayTarget, "127.0.0.1:2000", 200), sourcefacts.BudgetAlertProfile)
	handledAlert := handled["entries"].([]any)[0].(map[string]any)
	require.Equal(t, "acknowledged", handledAlert["status"])
	require.Equal(t, "not_observed", handledAlert["human_read"])
	require.Equal(t, "600000000", handledAlert["spent_at_trigger"].(map[string]any)["value_minor"])
	require.Equal(t, "500000000", handledAlert["budget_at_trigger"].(map[string]any)["value_minor"])
	post(f.handler.UpdateProjectBudget, map[string]any{"budget_cents": 100000, "budget_period": "monthly"}, f.actorID, auth.RolePlatformAdmin, map[string]string{"id": stringInt64(f.projectID)}, 200)
	reading1000 := f.read(t, token, gatewayTarget, "127.0.0.1:2000", 200)
	oldBytes := append([]byte(nil), reading1000.Body.Bytes()...)
	definition1000 := sourceBudgetBody(t, reading1000, sourcefacts.BudgetDefinitionProfile)
	require.Equal(t, "1000000000", definition1000["amount"].(map[string]any)["value_minor"])
	post(f.handler.UpdateProjectBudget, map[string]any{"budget_cents": 150000, "budget_period": "monthly"}, f.actorID, auth.RolePlatformAdmin, map[string]string{"id": stringInt64(f.projectID)}, 200)
	definition1500 := sourceBudgetBody(t, f.read(t, token, gatewayTarget, "127.0.0.1:2000", 200), sourcefacts.BudgetDefinitionProfile)
	require.Equal(t, "1500000000", definition1500["amount"].(map[string]any)["value_minor"])
	require.NotEqual(t, definition1000["revision"], definition1500["revision"])
	require.Equal(t, oldBytes, reading1000.Body.Bytes())
	// Capture an unconsumed page in each original stream before changing D.
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	aReader, bReader, dReader := f.source.Handler(resolver), f.source.ObservationHandler(resolver), f.source.BudgetHandler(resolver)
	aTarget := sourceReadPath + "?profile=" + sourcefacts.ContextProfile
	bTarget := "/api/v1/integrations/xinsight/source/v2/events?profile=" + sourcefacts.RuntimeProfile
	f.reader = aReader
	aBoot := sourcePage(t, f.read(t, token, aTarget, "127.0.0.1:2000", 200))
	f.reader = bReader
	bBoot := sourcePage(t, f.read(t, token, bTarget, "127.0.0.1:2000", 200))
	f.capture(t, "D-preserve-one")
	f.capture(t, "D-preserve-two")
	f.reader = dReader
	var catalog map[string]any
	require.NoError(t, json.Unmarshal(f.read(t, token, sourceBudgetPath+"?limit=1", "127.0.0.1:2000", 200).Body.Bytes(), &catalog))
	catalogWire, err := json.Marshal(catalog)
	require.NoError(t, err)
	exportSourceBudgetSample(t, catalogWire)
	oldCursor := catalog["next_cursor"].(string)
	require.NotEmpty(t, oldCursor)
	var generation int64
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT scope_generation FROM execution_source_connections WHERE project_id=$1`, f.projectID).Scan(&generation))
	f.control(t, "profiles", map[string]any{"profiles": []string{sourcefacts.ContextProfile, sourcefacts.RuntimeProfile}}, 200)
	f.read(t, token, enterpriseTarget, "127.0.0.1:2000", 403)
	f.reader = aReader
	aPage := sourcePage(t, f.read(t, token, aTarget+"&limit=1&cursor="+url.QueryEscape(aBoot.NextCursor), "127.0.0.1:2000", 200))
	require.Len(t, aPage.Events, 1)
	require.True(t, aPage.HasMore)
	f.reader = bReader
	bPage := sourcePage(t, f.read(t, token, bTarget+"&limit=1&cursor="+url.QueryEscape(bBoot.NextCursor), "127.0.0.1:2000", 200))
	require.Len(t, bPage.Events, 1)
	require.True(t, bPage.HasMore)
	f.control(t, "profiles", map[string]any{"profiles": profiles}, 200)
	f.reader = dReader
	f.read(t, token, sourceBudgetPath+"?cursor="+url.QueryEscape(oldCursor), "127.0.0.1:2000", 400)
	f.read(t, token, enterpriseTarget, "127.0.0.1:2000", 200)
	binding["expected_revision"] = "1"
	binding["status"] = "revoked"
	post(f.handler.SetSourceBudgetBinding, binding, checker, auth.RoleBillingAdmin, nil, 200)
	f.read(t, token, enterpriseTarget, "127.0.0.1:2000", 404)
	f.reader = aReader
	require.Len(t, sourcePage(t, f.read(t, token, aTarget+"&cursor="+url.QueryEscape(aPage.NextCursor), "127.0.0.1:2000", 200)).Events, 1)
	f.reader = bReader
	require.Len(t, sourcePage(t, f.read(t, token, bTarget+"&cursor="+url.QueryEscape(bPage.NextCursor), "127.0.0.1:2000", 200)).Events, 1)
	var afterGeneration int64
	var registered []string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT scope_generation FROM execution_source_connections WHERE project_id=$1`, f.projectID).Scan(&afterGeneration))
	require.Equal(t, generation, afterGeneration)
	keyID, _ := strconv.ParseInt(key["credential_id"].(string), 10, 64)
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT profiles FROM execution_source_credentials WHERE api_key_id=$1`, keyID).Scan(&registered))
	require.Equal(t, profiles, registered)
	var charges int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM usage_finalization_journal WHERE org_id=$1`, f.orgID).Scan(&charges))
	require.Zero(t, charges, "D never creates a second financial charge")
}

func exportSourceBudgetSample(t *testing.T, wire []byte) {
	t.Helper()
	directory := os.Getenv("SOURCE_BUDGET_EXPORT_DIR")
	if directory == "" {
		return
	}
	require.True(t, filepath.IsAbs(directory))
	require.NoError(t, os.MkdirAll(directory, 0700))
	digest := sha256.Sum256(wire)
	name := filepath.Join(directory, fmt.Sprintf("budget-%x.json", digest[:12]))
	require.NoError(t, os.WriteFile(name, wire, 0600))
}
