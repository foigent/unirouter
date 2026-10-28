//go:build integration

package admin

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestUpstreamObservationSharedFactsApprovalAuthoringAndActiveExecutionIntegration(t *testing.T) {
	testPublishedDeepSeekFlashHTTPFlow(t, "deepseek-flash-meaningful-stream-probe.json", true)
}

func exerciseScopedProbeEpochs(t *testing.T, pool *pgxpool.Pool, h *Handler, actor func(string, string, string) *http.Request, app canonicalModelApplication, expectedCalls *int, dispatchHook *func()) upstreamrelease.State {
	t.Helper()
	ctx := t.Context()
	dep := app.Deployment.ID
	binding := app.Credential.Bindings[0].ID
	put := func(state upstreamrelease.State, label string) upstreamrelease.State {
		in := upstreamInput(state, label)
		in.ExpectedConnectionRevision = app.Connection.ConfigRevision
		in.ExpectedDeploymentRevision = app.Deployment.ConfigRevision
		return serveUpstreamObservation(t, h, actor, dep, http.MethodPut, in, 200)
	}
	state := put(upstreamrelease.State{}, "A")
	authRequest := actor(http.MethodGet, "/", "")
	org, e := resolveActorOrganization(authRequest.Context(), 0)
	require.NoError(t, e)
	// Actor ownership is already fixed by the authenticated fixture request.
	var actorID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM users WHERE org_id=$1 AND organization_role='org_admin' ORDER BY id LIMIT 1`, org).Scan(&actorID))
	load := func() capabilityProbePlan {
		p, e := h.loadCapabilityProbePlan(ctx, org, actorID, dep, modelprofile.OperationOpenAIChat, binding)
		require.NoError(t, e)
		require.Len(t, p.Groups, 1)
		return p
	}
	plan := load()
	same := put(state, "A")
	require.Equal(t, state.Observation.Identity, same.Observation.Identity)
	require.Equal(t, plan.Groups[0].PlanDigest, load().Groups[0].PlanDigest)
	invoke := func(group capabilityProbeGroup, expected int) capabilityProbeGroupResult {
		wire, _ := json.Marshal(capabilityProbeGroupRequest{CredentialBindingID: binding, PlanDigest: group.PlanDigest, CapabilityIDs: group.CapabilityIDs})
		r := actor(http.MethodPost, "/", string(wire))
		r.SetPathValue("deploymentID", strconv.FormatInt(dep, 10))
		r.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
		w := httptest.NewRecorder()
		h.ProbeCanonicalCapabilityGroup(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
		var result capabilityProbeGroupResult
		if expected == 200 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		}
		return result
	}
	state = put(same, "B")
	invoke(plan.Groups[0], 409)
	state = put(state, "A")
	require.NotEqual(t, plan.Groups[0].PlanDigest, load().Groups[0].PlanDigest, "A->B->A cannot revive an old plan")
	_, err := h.probeCanonicalModelCapability(ctx, org, actorID, dep, modelprofile.OperationOpenAIChat, "function_tools", modelCapabilityProbeRequest{CredentialBindingID: binding}, requestAuditMeta(actor(http.MethodPost, "/", ""), org, 0))
	require.ErrorIs(t, err, errModelCapabilityConflict)
	proof, err := h.probeCanonicalModelCapability(ctx, org, actorID, dep, modelprofile.OperationOpenAIChat, "function_tools", modelCapabilityProbeRequest{CredentialBindingID: binding, ExpectedUpstreamObservationIdentity: state.Observation.Identity}, requestAuditMeta(actor(http.MethodPost, "/", ""), org, 0))
	require.NoError(t, err)
	require.True(t, proof.Success)
	*expectedCalls++
	captured, err := upstreamrelease.Probe(ctx, pool, true, org, dep, proof.CapabilityProbeID)
	require.NoError(t, err)
	require.True(t, upstreamrelease.Matches(state.Observation, captured))
	// The real post-dispatch transaction must retain all six facts after a
	// declaration change. No model retry or partial qualification is allowed.
	group := load().Groups[0]
	before := state
	*dispatchHook = func() { state = put(state, "B") }
	stale := invoke(group, 200)
	*expectedCalls++
	require.Len(t, stale.Results, 6)
	for _, fact := range stale.Results {
		require.False(t, fact.Success)
		require.False(t, fact.Current)
		require.True(t, fact.ProviderSuccess)
		captured, e := upstreamrelease.Probe(ctx, pool, true, org, dep, fact.CapabilityProbeID)
		require.NoError(t, e)
		require.Equal(t, before.Observation.Identity, captured.Identity)
	}
	state = put(state, "A")
	return state
}

func exerciseScopedApprovedHistory(t *testing.T, pool *pgxpool.Pool, h *Handler, actor func(string, string, string) *http.Request, app canonicalModelApplication, state upstreamrelease.State, operatorOrg, approver int64, facts []modelCapabilityProbeResult) {
	t.Helper()
	ctx := t.Context()
	dep := app.Deployment.ID
	var before string
	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY capability_probe_id)::text FROM model_capability_probes p WHERE deployment_id=$1`, dep).Scan(&before))
	in := upstreamInput(state, "B")
	in.ExpectedConnectionRevision = app.Connection.ConfigRevision
	in.ExpectedDeploymentRevision = app.Deployment.ConfigRevision
	changed := serveUpstreamObservation(t, h, actor, dep, http.MethodPut, in, 200)
	require.NotEqual(t, state.Observation.Identity, changed.Observation.Identity)
	org, e := resolveActorOrganization(actor(http.MethodGet, "/", "").Context(), 0)
	require.NoError(t, e)
	fact := facts[0]
	view, e := h.loadModelCapabilityQualificationView(ctx, org, dep, fact.OperationID, fact.CapabilityID)
	require.NoError(t, e)
	require.True(t, view.Qualified, "recording metadata must not revoke active execution qualification")
	require.Equal(t, "stale", view.UpstreamScope.State)
	// An exact old approval is a historical readback, not a new approval of B.
	result, e := h.approveCanonicalModelCapabilityRouting(ctx, org, approver, operatorOrg, dep, fact.OperationID, fact.CapabilityID, modelCapabilityApprovalRequest{CapabilityProbeID: fact.CapabilityProbeID, Reason: "exact shared Flash fixture", ExpectedUpstreamObservationIdentity: state.Observation.Identity}, requestAuditMeta(actor(http.MethodPost, "/", ""), org, 0))
	require.NoError(t, e)
	require.True(t, result.IdempotencyReplay)
	require.Equal(t, "stale", result.UpstreamScope.State)
	// Fresh authoring must validate both the client epoch and retained approvals.
	options, e := h.deploymentLifecycleManager.ListDeploymentReleaseAuthoringOptions(ctx, org)
	require.NoError(t, e)
	var baseline *runtimechange.DeploymentReleaseBaseline
	for _, option := range options.Models {
		if option.PublicModel.ID == app.PublicModel.ID {
			baseline = option.Baseline
		}
	}
	require.NotNil(t, baseline)
	var approvalIDs []int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(approval_id ORDER BY approval_id) FROM model_capability_routing_approvals WHERE deployment_id=$1`, dep).Scan(&approvalIDs))
	input := canonicalDeploymentReleaseAuthoringRequest{PublicModelID: app.PublicModel.ID, Baseline: *baseline, Selections: []runtimechange.DeploymentReleaseSelection{{DeploymentID: dep, OperationID: modelprofile.OperationOpenAIChat, ExpectedUpstreamObservationIdentity: state.Observation.Identity}}, ProfileDefinitionDigest: app.Operations[0].ProfileDefinitionDigest, PublicContextLimit: 131072, PublicOutputLimit: 2048, CapabilityApprovalIDs: approvalIDs, Reason: "scoped stale release fixture"}
	serveCanonicalDeploymentReleaseAuthoring(t, h, actor, input, "upstream-stale-client", http.StatusConflict)
	input.Selections[0].ExpectedUpstreamObservationIdentity = changed.Observation.Identity
	serveCanonicalDeploymentReleaseAuthoring(t, h, actor, input, "upstream-stale-proof", http.StatusConflict)
	var after string
	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY capability_probe_id)::text FROM model_capability_probes p WHERE deployment_id=$1`, dep).Scan(&after))
	require.Equal(t, before, after, "stored summaries/fingerprints stay immutable")
}
