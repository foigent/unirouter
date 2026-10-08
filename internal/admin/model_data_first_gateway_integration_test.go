//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// Continue the actual create/Probe/Approval/authoring test through formal Slot,
// preflight and real Relay/coordinator/billing owners. Only the upstream transport
// is a fixture. No raw Slot/Release write or simulated settlement is used.
func verifyFirstDataGateway(t *testing.T, h *Handler, actor func(string, string, string) *http.Request,
	release runtimechange.DeploymentRelease, application canonicalModelApplication,
	definition modelprofile.Definition, stream []byte) {
	t.Helper()
	ctx := t.Context()
	pool, ok := h.db.(*pgxpool.Pool)
	require.True(t, ok)
	org, err := resolveActorOrganization(actor(http.MethodGet, "/", "").Context(), 0)
	require.NoError(t, err)
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	wire, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	digest, err := manifest.Digest()
	require.NoError(t, err)
	identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("d", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, identity.Artifact, digest, wire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, identity.Instance, identity.Artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LatestSupportedMigration)
	require.NoError(t, err)
	_, _ = serveCanonicalLifecycleMutation(t, h, actor, release.ReleaseRef, "activate-initial", fmt.Sprintf(`{"expected_release_version":%d,"reason":"uncompiled data first activation fixture"}`, release.Version), "data-first-activate", h.ActivateInitialCanonicalDeploymentLifecycle, http.StatusOK)
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,name,key_prefix,key_hash,status) VALUES($1,'data-first-fixture','data-first',repeat('a',64),'active') RETURNING id`, org).Scan(&keyID))
	bill := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	journal, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	bill.SetOutbox(journal)
	for _, event := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingUsagePreparedV1, outbox.EventBillingSettledV1} {
		require.NoError(t, journal.RegisterHandler(event, bill.ProcessUsageJournalEvent))
	}
	planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	coordinator, err := modelexecution.New(pool, bill, modelexecution.DefaultConfig())
	require.NoError(t, err)
	relay := gateway.NewRelay(pool, nil, nil, bill, nil, h.encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
	guard, err := guardrail.NewService(pool, filter.NewDFAFilter(nil), []byte("data-first-guardrail-secret"), guardrail.DefaultOptions())
	require.NoError(t, err)
	relay.SetGuardrail(guard)
	calls := 0
	relay.SetProviderHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, strings.TrimRight(application.Connection.BaseURL, "/")+"/chat/completions", r.URL.String())
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, definition.Profile().UpstreamModelIDs[0], body["model"])
		if definition.Profile().Provider == "deepseek" {
			require.NotContains(t, body, "tool_choice")
			require.NotContains(t, body, "thinking")
			require.NotContains(t, body, "reasoning_effort")
		}
		require.Equal(t, true, body["stream"])
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(string(stream)))}, nil
	}))
	body := fmt.Sprintf(`{"model":%q,"stream":true,"stream_options":{"include_usage":true},"max_tokens":1024,"tool_choice":"auto","messages":[{"role":"user","content":"call unirouter_probe_echo"}],"tools":[{"type":"function","function":{"name":"unirouter_probe_echo","description":"echo fixture","parameters":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}}}]}`, application.PublicModel.ModelName)
	invoke := func(handle http.HandlerFunc, id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		requestCtx := runtimegeneration.WithIdentity(r.Context(), identity)
		requestCtx = context.WithValue(requestCtx, middleware.CtxOrgID, org)
		requestCtx = context.WithValue(requestCtx, middleware.CtxKeyID, keyID)
		requestCtx = context.WithValue(requestCtx, middleware.CtxAPIKey, &models.ApiKey{ID: keyID, OrgID: org, Status: "active"})
		requestCtx = context.WithValue(requestCtx, middleware.CtxModelAlias, application.PublicModel.ModelName)
		requestCtx = modelauthz.WithDecision(requestCtx, modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: "/v1/chat/completions", RequestedModel: application.PublicModel.ModelName, ResolvedModel: application.PublicModel.ModelName, RuntimeMutation: "none"})
		r = r.WithContext(requestCtx)
		r.Header.Set(middleware.IdempotencyKeyHeader, id)
		budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
		guarded := payloadpolicy.RequestMiddleware(budget, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext)(middleware.StrictJSONIngress(handle))
		w := httptest.NewRecorder()
		middleware.OperationIdentityWithSecret("data-first-operation-secret")(guarded).ServeHTTP(w, r)
		return w
	}
	preflight := invoke(relay.HandleChatPreflight, "data-first-preflight")
	require.Equal(t, http.StatusOK, preflight.Code, preflight.Body.String())
	require.Contains(t, preflight.Body.String(), `"locally_admissible":true`)
	require.Contains(t, preflight.Body.String(), definition.Profile().ID)
	require.Equal(t, 0, calls)
	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM billing_provider_attempts WHERE org_id=$1`, org).Scan(&attemptCount))
	require.Zero(t, attemptCount)
	result := invoke(relay.HandleChatCompletion, "data-first-stream")
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), "[DONE]")
	require.Equal(t, 1, calls)
	_, err = journal.DrainOnce(ctx)
	require.NoError(t, err)
	requestID := result.Header().Get("X-Request-ID")
	require.NotEmpty(t, requestID)
	var releaseID int64
	var state, instance, snapshot string
	require.NoError(t, pool.QueryRow(ctx, `SELECT route_runtime_release_id,state,runtime_instance_id,execution_snapshot_hash FROM billing_provider_attempts WHERE request_id=$1`, requestID).Scan(&releaseID, &state, &instance, &snapshot))
	require.Equal(t, release.ID, releaseID)
	require.Equal(t, "settled", state)
	require.Equal(t, identity.Instance, instance)
	require.NotEmpty(t, snapshot)
	for query, expected := range map[string]int{
		`SELECT count(*) FROM billing_provider_attempts WHERE request_id=$1`:                                  1,
		`SELECT count(*) FROM billing_observation_receipts WHERE request_id=$1 AND terminal_status='settled'`: 1,
		`SELECT count(*) FROM request_delivery_receipts WHERE request_id=$1 AND outcome='completed'`:          1,
		`SELECT count(*) FROM usage_finalization_journal WHERE request_id=$1 AND event_type='settled'`:        1,
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, query, requestID).Scan(&count))
		require.Equal(t, expected, count, query)
	}
}
