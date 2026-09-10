//go:build integration

package gateway_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil/sourcereceiver"
)

const sourceGatewaySyntheticSecret = "source-gateway-synthetic-hmac-only"

type sourceTerminalFailure struct {
	source billing.TerminalMaterializer
	fail   bool
}

func (m *sourceTerminalFailure) MaterializeTerminalTx(ctx context.Context, tx pgx.Tx, record billing.TerminalRecord) error {
	if err := m.source.MaterializeTerminalTx(ctx, tx, record); err != nil {
		return err
	}
	if m.fail {
		return errors.New("injected source terminal failure after writes")
	}
	return nil
}

func TestSourceFactsGatewayFailureRecoveryKeepsOriginalFinancialCommitIntegration(t *testing.T) {
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, journal, raw := sourceGatewayAttachSource(t, f)
	v1, err := completedfeed.NewService(f.Pool, sourceGatewaySyntheticSecret, source.SourceInstance(), true)
	require.NoError(t, err)
	failure := &sourceTerminalFailure{source: source, fail: true}
	f.Billing.SetTerminalMaterializer(billing.TerminalMaterializers{v1, failure})
	handler := sourceGatewayHandler(t, f, f.Relay, source)
	invoke := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
		r.Header.Set("Authorization", "Bearer "+raw)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	// Inject a real source INSERT failure before dispatch in this temporary DB.
	_, err = f.Pool.Exec(t.Context(), `CREATE FUNCTION source_test_refuse_auth() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.stage='authenticated' THEN RAISE EXCEPTION 'synthetic capture unavailable'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER source_test_refuse_auth BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION source_test_refuse_auth()`)
	require.NoError(t, err)
	refused := invoke()
	require.Equal(t, http.StatusServiceUnavailable, refused.Code)
	require.Zero(t, f.ProviderCalls())
	var count int
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM freeze_idempotency`).Scan(&count))
	require.Zero(t, count)
	_, err = f.Pool.Exec(t.Context(), `DROP TRIGGER source_test_refuse_auth ON execution_source_facts; DROP FUNCTION source_test_refuse_auth()`)
	require.NoError(t, err)
	w := invoke()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.EqualValues(t, 1, f.ProviderCalls())
	request := w.Header().Get(middleware.OperationIDHeader)
	_, err = journal.DrainOnce(t.Context())
	require.ErrorContains(t, err, "injected source terminal failure")
	var state string
	var facts, i01, journals int
	read := func() {
		require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&state))
		require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts WHERE request_id=$1),(SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1),(SELECT count(*) FROM usage_finalization_journal WHERE request_id=$1 AND event_type='settled')`, request).Scan(&facts, &i01, &journals))
	}
	read()
	require.Equal(t, "settled", state)
	require.Equal(t, 4, facts)
	require.Zero(t, i01)
	require.Zero(t, journals)
	var event outbox.Event
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT id,event_key,event_type,priority,org_id,request_id,payload,occurred_at FROM internal_outbox_events WHERE request_id=$1 AND event_type=$2`, request, outbox.EventBillingSettled).Scan(&event.ID, &event.Key, &event.Type, &event.Priority, &event.OrgID, &event.RequestID, &event.Payload, &event.OccurredAt))
	failure.fail = false
	_, err = f.Pool.Exec(t.Context(), `UPDATE internal_outbox_events SET available_at=NOW() WHERE request_id=$1`, request)
	require.NoError(t, err)
	_, err = journal.DrainOnce(t.Context())
	require.NoError(t, err)
	read()
	require.Equal(t, "settled", state)
	require.Equal(t, 7, facts)
	require.Equal(t, 1, i01)
	require.Equal(t, 1, journals)
	require.EqualValues(t, 1, f.ProviderCalls())
	_, err = source.DeleteExpired(t.Context(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, f.Billing.ProcessUsageJournalEvent(t.Context(), event))
	read()
	require.Zero(t, facts)
	require.Equal(t, 1, i01)
	require.Equal(t, 1, journals)
	require.EqualValues(t, 1, f.ProviderCalls())
}

func sourceGatewayPersonalAccess(t *testing.T, f gateway.SourceFactsGatewayFixture, project int64) {
	t.Helper()
	ctx := t.Context()
	_, err := f.Pool.Exec(ctx, `INSERT INTO project_members(project_id,user_id,role) VALUES($1,$2,'project_admin')`, project, f.UserID)
	require.NoError(t, err)
	var request, grant, credential int64
	require.NoError(t, f.Pool.QueryRow(ctx, `INSERT INTO access_requests(
	 request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_user_id,
	 request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,
	 environment,submission_mode,status,decision_mode,policy_reason_codes,decision_reason_codes,
	 policy_version,submitted_by,decided_by,decided_at)
	 VALUES('req_source_synthetic_001','source-synthetic',repeat('a',64),$1,$2,'user',$3,
	 'human_personal','project_admin','explicit',jsonb_build_array($4::text),jsonb_build_array($5::text),
	 'personal','direct_grant','approved','direct',ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
	 'unirouter.enterprise-access-policy.v1',$3,$3,NOW()) RETURNING id`, f.OrgID, project, f.UserID, f.Model, f.Path).Scan(&request))
	require.NoError(t, f.Pool.QueryRow(ctx, `INSERT INTO access_grants(
	 grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,project_role_snapshot,
	 scope_mode,allowed_models,allowed_endpoints,environment,status,valid_from,decision_mode,
	 policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,created_by,updated_by)
	 VALUES('grant_source_synthetic_001',$1,$2,$3,'user',$4,'project_admin','explicit',jsonb_build_array($5::text),jsonb_build_array($6::text),
	 'personal','active',NOW(),'direct',ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
	 'unirouter.enterprise-access-policy.v1',repeat('a',64),$4,$4) RETURNING id`, request, f.OrgID, project, f.UserID, f.Model, f.Path).Scan(&grant))
	// Copy the physical normal-model key's attenuation into its logical identity;
	// this is a synthetic fixture on the existing access-grant contract.
	require.NoError(t, f.Pool.QueryRow(ctx, `INSERT INTO access_credentials(
	 credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,allowed_endpoints,
	 policy_labels,rpm_limit,rps_limit,concurrency_limit,legacy_budget_limit_cents,created_by)
	 SELECT 'cred_source_synthetic_001',$1,org_id,$2,name,allowed_ips,allowed_models,allowed_endpoints,
	 policy_labels,rpm_limit,qps_limit,concurrency_limit,budget_limit,$3 FROM api_keys WHERE id=$4 RETURNING id`, grant, project, f.UserID, f.KeyID).Scan(&credential))
	_, err = f.Pool.Exec(ctx, `UPDATE api_keys SET key_type='personal',access_grant_id=$1,access_credential_id=$2,credential_ref='cred_source_synthetic_001' WHERE id=$3`, grant, credential, f.KeyID)
	require.NoError(t, err)
}

type sourceCancelWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
	once   sync.Once
}

func (w *sourceCancelWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.once.Do(w.cancel)
	return n, err
}

func TestSourceFactsPublicGatewayStreamsAndHistoricalRejectionIntegration(t *testing.T) {
	for _, tc := range []struct {
		name, version, mode string
		operation           modeloperation.ID
	}{
		{"responses_success", "1.3.0", "complete", modeloperation.IDResponses},
		{"messages_v2_success", "1.3.0", "complete", modeloperation.IDMessages},
		{"messages_v2_interrupted", "1.3.0", "interrupted", modeloperation.IDMessages},
		{"messages_v2_client_cancel", "1.3.0", "cancel", modeloperation.IDMessages},
		{"messages_v1_rejected", "1.2.0", "reject_v1", modeloperation.IDMessages},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := gateway.NewSourceFactsGatewayStreamFixture(t, tc.operation, tc.version, tc.mode)
			source, journal, raw := sourceGatewayAttachSource(t, f)
			defer sourceGatewayObserveReceiver(t, f, source)()
			handler := sourceGatewayHandler(t, f, f.Relay, source)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body))).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			if tc.operation == modeloperation.IDMessages {
				r.Header.Set("x-api-key", raw)
				r.Header.Set("anthropic-version", "2023-06-01")
			} else {
				r.Header.Set("Authorization", "Bearer "+raw)
			}
			w := httptest.NewRecorder()
			if tc.mode == "cancel" {
				handler.ServeHTTP(&sourceCancelWriter{ResponseRecorder: w, cancel: cancel}, r)
			} else {
				handler.ServeHTTP(w, r)
			}
			request := w.Header().Get(middleware.OperationIDHeader)
			_, err := journal.DrainOnce(t.Context())
			require.NoError(t, err)
			var facts, reservations, i01 int
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts WHERE request_id=$1),(SELECT count(*) FROM freeze_idempotency WHERE request_id=$1),(SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1)`, request).Scan(&facts, &reservations, &i01))
			if tc.mode == "reject_v1" {
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				require.Zero(t, f.ProviderCalls())
				require.Zero(t, reservations)
				require.Zero(t, i01)
				require.Equal(t, 2, facts)
				return
			}
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.EqualValues(t, 1, f.ProviderCalls())
			require.Equal(t, 1, reservations)
			var state string
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&state))
			if tc.mode == "complete" {
				require.Equal(t, "settled", state)
				require.Equal(t, 7, facts)
				require.Equal(t, 1, i01)
			} else {
				require.Equal(t, "settlement_pending", state)
				require.Equal(t, 4, facts)
				require.Zero(t, i01)
			}
		})
	}
}

// This fixture runs the actual two-pass key authentication, retained body,
// capture, idempotency, public relay, execution coordinator and billing writers.
// Its model authorization decision and Provider transport are synthetic; no
// source receiver credential or external service is created.
func sourceGatewayHandler(t *testing.T, fixture gateway.SourceFactsGatewayFixture, relay *gateway.Relay, capture middleware.ExecutionContextCapturer) http.Handler {
	return sourceGatewayHandlerWithAuth(t, fixture, relay, capture, middleware.GatewayAuthWithAPIKeyHashSecret(fixture.Pool, sourceGatewaySyntheticSecret))
}

func sourceGatewayHandlerWithAuth(t *testing.T, fixture gateway.SourceFactsGatewayFixture, relay *gateway.Relay, capture middleware.ExecutionContextCapturer, authenticate func(http.Handler) http.Handler) http.Handler {
	t.Helper()
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.CtxModelAlias, fixture.Model)
		ctx = modelauthz.WithDecision(ctx, modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true,
			Endpoint: fixture.Path, RequestedModel: fixture.Model, ResolvedModel: fixture.Model, RuntimeMutation: "none"})
		relay.HandlePublicNonStream(w, r.WithContext(ctx), fixture.Operation)
	})
	guarded := middleware.IdempotencyGuard(middleware.NewPostgresIdempotencyStore(fixture.Pool), sourceGatewaySyntheticSecret, final)
	retained := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext)
	return middleware.OperationIdentityWithSecret(sourceGatewaySyntheticSecret)(middleware.GatewayAuthAcrossBufferedBody(
		authenticate, retained)(
		middleware.CaptureExecutionContext(capture)(middleware.StrictJSONIngress(guarded))))
}

func sourceGatewayAttachSource(t *testing.T, fixture gateway.SourceFactsGatewayFixture) (*sourcefacts.Service, *outbox.Service, string) {
	return sourceGatewayAttachProfiles(t, fixture, false)
}

func sourceGatewayAttachProfiles(t *testing.T, fixture gateway.SourceFactsGatewayFixture, observations bool) (*sourcefacts.Service, *outbox.Service, string) {
	t.Helper()
	pool := fixture.Pool
	var project int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Source','source','active','none') RETURNING id`, fixture.OrgID).Scan(&project))
	// New public operations require an explicit live personal grant.
	raw := "ur-source-test-" + strings.Repeat("a", 32)
	_, err := pool.Exec(t.Context(), `UPDATE api_keys SET project_id=$1,owner_user_id=$2,key_type='shared',key_hash=$3,allowed_endpoints=$5 WHERE id=$4`, project, fixture.UserID, auth.HashAPIKey(raw, sourceGatewaySyntheticSecret), fixture.KeyID, fixture.Path)
	require.NoError(t, err)
	sourceGatewayPersonalAccess(t, fixture, project)
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: observations, SourceInstance: "synthetic-gateway", CursorSecret: sourceGatewaySyntheticSecret, I01Enabled: true})
	require.NoError(t, err)
	require.NoError(t, source.CheckReady(t.Context()))
	v1, err := completedfeed.NewService(pool, sourceGatewaySyntheticSecret, source.SourceInstance(), true)
	require.NoError(t, err)
	fixture.Billing.WithMode(billing.BillingModeCostObservation)
	fixture.Billing.SetReservationMaterializer(source)
	if observations {
		fixture.Billing.SetCommittedObservationSink(source)
		fixture.SetDispatchMaterializer(t, source)
	}
	fixture.Billing.SetTerminalMaterializer(billing.TerminalMaterializers{v1, source})
	journal, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	fixture.Billing.SetOutbox(journal)
	require.NoError(t, journal.RegisterHandler(outbox.EventSourceDispatch, source.ProcessDispatchEvent))
	for _, typ := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled} {
		require.NoError(t, journal.RegisterHandler(typ, fixture.Billing.ProcessUsageJournalEvent))
	}
	return source, journal, raw
}

func TestSourceFactsPublicGatewayRejectsUnsupportedAuthenticationIntegration(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		t.Run(string(operation), func(t *testing.T) {
			f := gateway.NewSourceFactsGatewayFixture(t, operation)
			source, _, validGrant := sourceGatewayAttachSource(t, f)
			handler := sourceGatewayHandler(t, f, f.Relay, source)
			invoke := func(token string, mutate func(http.Header), status int) {
				r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
				r.Header.Set("Content-Type", "application/json")
				if operation == modeloperation.IDMessages {
					r.Header.Set("x-api-key", token)
					r.Header.Set("anthropic-version", "2023-06-01")
				} else {
					r.Header.Set("Authorization", "Bearer "+token)
				}
				if mutate != nil {
					mutate(r.Header)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				require.Equal(t, status, w.Code, w.Body.String())
				if operation == modeloperation.IDMessages {
					require.Contains(t, w.Body.String(), `"type":"error"`)
				}
			}
			for n, endpoint := range []string{"*", f.Path} {
				// Even an exact endpoint string does not upgrade a legacy key
				// into the live explicit grant required by these operations.
				raw := "ur-source-legacy-" + strings.ReplaceAll(endpoint, "/", "_")
				_, err := f.Pool.Exec(t.Context(), `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,allowed_models,allowed_endpoints,status)
				 SELECT org_id,project_id,owner_user_id,'personal','synthetic legacy source',$2,$4,allowed_models,$3,'active' FROM api_keys WHERE id=$1`, f.KeyID, auth.HashAPIKey(raw, sourceGatewaySyntheticSecret), endpoint, fmt.Sprintf("legacy-src-%d", n))
				require.NoError(t, err)
				invoke(raw, nil, http.StatusForbidden)
			}
			if operation == modeloperation.IDMessages {
				invoke(validGrant, func(h http.Header) { h.Del("anthropic-version") }, http.StatusBadRequest)
				invoke(validGrant, func(h http.Header) { h.Set("Authorization", "Bearer synthetic-conflict") }, http.StatusUnauthorized)
			}
			require.Zero(t, f.ProviderCalls())
			var facts, reservations, economics int
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts),(SELECT count(*) FROM freeze_idempotency),(SELECT count(*) FROM completed_execution_feed_items)`).Scan(&facts, &reservations, &economics))
			require.Zero(t, facts, "rejection precedes the second successful authentication/capture")
			require.Zero(t, reservations)
			require.Zero(t, economics)
		})
	}
}

func TestSourceFactsPublicGatewayExplicitGrantAgentIdentityIntegration(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		t.Run(string(operation), func(t *testing.T) {
			f := gateway.NewSourceFactsGatewayFixture(t, operation)
			source, journal, _ := sourceGatewayAttachSource(t, f)
			defer sourceGatewayObserveReceiver(t, f, source)()
			require.NotEmpty(t, os.Getenv("REDIS_ADDR"), "select this task's isolated Redis explicitly")
			rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD")})
			t.Cleanup(func() { _ = rdb.Close() })
			capabilities, err := agentcap.NewService(f.Pool, rdb, sourceGatewaySyntheticSecret)
			require.NoError(t, err)
			limits := agentcap.Limits{MaxDepth: 1, MaxChildren: 1, MaxDescendants: 1, CostBudgetCents: 1000, TokenBudget: 100000, RPMLimit: 60, RPSLimit: 10, ConcurrencyLimit: 1, AllowedModels: []string{f.Model}, AllowedEndpoints: []string{f.Path}}
			_, err = capabilities.Exchange(t.Context(), &models.ApiKey{ID: f.KeyID, OrgID: f.OrgID}, agentcap.ExchangeRequest{RootAgentID: "agent:public-source-root", WorkflowTTLSeconds: 3600, Limits: limits})
			require.ErrorIs(t, err, agentcap.ErrManagementForbidden, "a personal grant cannot mint Agent capabilities")
			f.KeyID = sourceGatewayAgentWorkload(t, f)
			root, err := capabilities.Exchange(t.Context(), &models.ApiKey{ID: f.KeyID, OrgID: f.OrgID}, agentcap.ExchangeRequest{RootAgentID: "agent:public-source-root", WorkflowTTLSeconds: 3600, Limits: limits})
			require.NoError(t, err)
			parent, err := capabilities.Authenticate(t.Context(), root.CapabilityToken)
			require.NoError(t, err)
			limits.MaxChildren, limits.MaxDescendants = 0, 0
			child, err := capabilities.Delegate(t.Context(), parent.Claims, agentcap.DelegateRequest{ChildAgentID: "agent:public-source-child", Limits: limits})
			require.NoError(t, err)
			// Exercise the existing ancestor-budget decorator as well as auth.
			// This fixture uses its synchronous terminal path; durable hook
			// recovery is separately tested by the main HTTP/Agent matrix.
			f.SetBilling(capbilling.New(f.Billing, capabilities))
			handler := sourceGatewayHandlerWithAuth(t, f, f.Relay, source, middleware.GatewayAuthWithAgentCapabilities(f.Pool, sourceGatewaySyntheticSecret, capabilities))
			r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
			r.Header.Set("Content-Type", "application/json")
			if operation == modeloperation.IDMessages {
				r.Header.Set("x-api-key", child.CapabilityToken)
				r.Header.Set("anthropic-version", "2023-06-01")
			} else {
				r.Header.Set("Authorization", "Bearer "+child.CapabilityToken)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			_, err = journal.DrainOnce(t.Context())
			require.NoError(t, err)
			request := w.Header().Get(middleware.OperationIDHeader)
			var wire []byte
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.IdentityProfile).Scan(&wire))
			doc, err := sourcefacts.Decode(wire)
			require.NoError(t, err)
			require.Equal(t, root.WorkflowID, doc.Identity.WorkflowID)
			require.Equal(t, []string{"agent:public-source-root", "agent:public-source-child"}, doc.Identity.Lineage)
			require.Empty(t, doc.Identity.OwnerUserID)
			require.Equal(t, executioncontext.ID(f.UserID), doc.Identity.SponsorUserID)
			require.NotEmpty(t, doc.Identity.GrantID)
			require.Equal(t, "unknown", doc.Identity.ActualHuman)
			var facts, terminals int
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts WHERE request_id=$1),(SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1)`, request).Scan(&facts, &terminals))
			require.Equal(t, 7, facts)
			require.Equal(t, 1, terminals)
			require.EqualValues(t, 1, f.ProviderCalls())
		})
	}
}

func sourceGatewayAgentWorkload(t *testing.T, f gateway.SourceFactsGatewayFixture) int64 {
	t.Helper()
	var project, app, request, grant, credential, key int64
	ctx, pool := t.Context(), f.Pool
	require.NoError(t, pool.QueryRow(ctx, `SELECT project_id FROM api_keys WHERE id=$1`, f.KeyID).Scan(&project))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO workload_applications(contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,display_name,kind,environment,identity_mode,owner_team,status,environment_locked_at,created_by,updated_by)
	 VALUES('unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,'app_public_source_agent','workload_public_source_agent','Synthetic Agent','agent','development','service_credential','test','active',NOW(),$3,$3) RETURNING id`, f.OrgID, project, f.UserID).Scan(&app))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO access_requests(request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_workload_application_id,request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,environment,requested_valid_until,submission_mode,sponsor_user_id,status,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,submitted_by,decided_by,decided_at)
	 VALUES('req_public_agent_0001','public-agent',repeat('b',64),$1,$2,'workload_application',$3,'workload_service','project_user','explicit',jsonb_build_array($5::text),jsonb_build_array($6::text),'development',NOW()+INTERVAL '1 hour','direct_grant',$4,'approved','direct',ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',$4,$4,NOW()) RETURNING id`, f.OrgID, project, app, f.UserID, f.Model, f.Path).Scan(&request))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO access_grants(grant_ref,access_request_id,org_id,project_id,subject_type,subject_workload_application_id,sponsor_user_id,project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,environment,status,valid_from,valid_until,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,created_by,updated_by)
	 VALUES('grant_public_agent_0001',$1,$2,$3,'workload_application',$4,$5,'project_user','explicit',jsonb_build_array($6::text),jsonb_build_array($7::text),'development','active',NOW()-INTERVAL '1 hour',(SELECT requested_valid_until FROM access_requests WHERE id=$1),'direct',ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',repeat('b',64),$5,$5) RETURNING id`, request, f.OrgID, project, app, f.UserID, f.Model, f.Path).Scan(&grant))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO access_credentials(credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,allowed_endpoints,policy_labels,rpm_limit,rps_limit,created_by)
	 VALUES('cred_public_agent_0001',$1,$2,$3,'synthetic-public-agent','',$5,$6,'',60,10,$4) RETURNING id`, grant, f.OrgID, project, f.UserID, f.Model, f.Path).Scan(&credential))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_ips,allowed_models,allowed_endpoints,purpose,workload_application_id,access_grant_id,access_credential_id,credential_ref)
	 VALUES($1,$2,'service','synthetic-public-agent',$3,'ur-public-agent','active','',$7,$8,'',$4,$5,$6,'cred_public_agent_0001') RETURNING id`, f.OrgID, project, auth.HashAPIKey("synthetic-public-agent-root-only", sourceGatewaySyntheticSecret), app, grant, credential, f.Model, f.Path).Scan(&key))
	return key
}

func TestSourceFactsPublicGatewayPreservesAuthenticatedOwnerAndOriginalEconomicsIntegration(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		t.Run(string(operation), func(t *testing.T) {
			fixture := gateway.NewSourceFactsGatewayFixture(t, operation)
			pool := fixture.Pool
			source, journal, raw := sourceGatewayAttachSource(t, fixture)
			handler := sourceGatewayHandler(t, fixture, fixture.Relay, source)
			var requests []string
			for range 2 {
				r := httptest.NewRequest(http.MethodPost, fixture.Path, strings.NewReader(string(fixture.Body)))
				if operation == modeloperation.IDMessages {
					r.Header.Set("x-api-key", raw)
					r.Header.Set("anthropic-version", "2023-06-01")
				} else {
					r.Header.Set("Authorization", "Bearer "+raw)
				}
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set(middleware.ClientRequestIDHeader, "same-client-correlation")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				requests = append(requests, w.Header().Get(middleware.OperationIDHeader))
			}
			require.NotEqual(t, requests[0], requests[1])
			require.EqualValues(t, 2, fixture.ProviderCalls())
			// Change the live owner before journal materialization. The terminal
			// must retain the originally authenticated owner and billing mode.
			_, err := pool.Exec(t.Context(), `UPDATE api_keys SET owner_user_id=NULL,key_type='shared' WHERE id=$1`, fixture.KeyID)
			require.NoError(t, err)
			fixture.Billing.WithMode(billing.BillingModePrepaidEnforced)
			_, err = journal.DrainOnce(t.Context())
			require.NoError(t, err)
			for _, request := range requests {
				var wire []byte
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.IdentityProfile).Scan(&wire))
				identity, err := sourcefacts.Decode(wire)
				require.NoError(t, err)
				require.Equal(t, executioncontext.ID(fixture.UserID), identity.Identity.OwnerUserID)
				require.Equal(t, "unknown", identity.Identity.ActualHuman)
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.CostProfile).Scan(&wire))
				cost, err := sourcefacts.Decode(wire)
				require.NoError(t, err)
				require.Equal(t, "cost_observation", cost.Cost.Basis.BillingMode)
				require.Equal(t, "cost_observation", cost.Cost.JournalBillingMode)
				require.Equal(t, "journal_payload", cost.Cost.JournalModeSource)
				var i01Hash string
				var charged string
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT snapshot_sha256,convert_from(snapshot,'UTF8')::jsonb->'money'->>'charged_cost_micros' FROM completed_execution_feed_items WHERE request_id=$1`, request).Scan(&i01Hash, &charged))
				require.Equal(t, i01Hash, cost.Cost.Reference.SnapshotSHA256)
				require.Equal(t, "0", charged)
				var facts int
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1`, request).Scan(&facts))
				require.Equal(t, 7, facts)
			}
			// Retaining the terminal data is independent of the source page TTL.
			_, err = source.DeleteExpired(t.Context(), time.Now().Add(time.Hour))
			require.NoError(t, err)
			var journals int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM usage_finalization_journal WHERE org_id=$1 AND event_type='settled'`, fixture.OrgID).Scan(&journals))
			require.Equal(t, 2, journals)
		})
	}
}

func sourceGatewayObserveReceiver(t *testing.T, f gateway.SourceFactsGatewayFixture, source *sourcefacts.Service) func() {
	t.Helper()
	var project int64
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT project_id FROM api_keys WHERE id=$1`, f.KeyID).Scan(&project))
	return sourcereceiver.Observe(t, f.Pool, source, sourceGatewaySyntheticSecret, sourceGatewaySyntheticSecret, f.OrgID, project, nil)
}
