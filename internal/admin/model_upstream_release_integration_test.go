//go:build integration

package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

type upstreamObservationTestFixture struct {
	pool       *pgxpool.Pool
	h          *Handler
	org, actor int64
	endpoint   canonicalOperationProbeFixture
	authorized func(string, string, string) *http.Request
}

func newUpstreamObservationFixture(t *testing.T) upstreamObservationTestFixture {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var org, actor int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('upstream-fixture','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'upstream-admin','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	key := []byte("0123456789abcdef01234567")
	endpoint := insertCanonicalOperationProbeFixture(t, pool, org, key, "upstream")
	base := canonicalApplicationAuthorizedRequest(org, actor)
	authorized := func(method, target, body string) *http.Request {
		return withIntegrationCommittingActor(t, pool, base(method, target, body), actor)
	}
	return upstreamObservationTestFixture{pool, NewHandler(pool, "upstream-fixture", key, nil), org, actor, endpoint, authorized}
}
func serveUpstreamObservation(t *testing.T, h *Handler, authorized func(string, string, string) *http.Request, dep int64, method string, input any, expected int) upstreamrelease.State {
	t.Helper()
	wire, err := json.Marshal(input)
	require.NoError(t, err)
	r := authorized(method, fmt.Sprintf("/api/v1/model-deployments/%d/upstream-release-observation", dep), string(wire))
	r.SetPathValue("deploymentID", strconv.FormatInt(dep, 10))
	w := httptest.NewRecorder()
	if method == http.MethodPut {
		h.PutModelUpstreamReleaseObservation(w, r)
	} else {
		h.GetModelUpstreamReleaseObservation(w, r)
	}
	require.Equal(t, expected, w.Code, w.Body.String())
	var state upstreamrelease.State
	if expected == 200 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &state))
	}
	return state
}
func upstreamInput(state upstreamrelease.State, label string) upstreamrelease.Input {
	return upstreamrelease.Input{ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1, ExpectedRevision: state.Revision, ExpectedIdentity: upstreamrelease.Identity(state.Observation), ReleaseLabel: label, SourceURL: "https://provider.example.test/releases/current"}
}

func TestUpstreamObservationHTTPPermissionsCASAndShapeIntegration(t *testing.T) {
	f := newUpstreamObservationFixture(t)
	ctx := t.Context()
	dep := f.endpoint.DeploymentID
	old := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodGet, nil, 200)
	require.Nil(t, old.Observation)
	a := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(old, "A"), 200)
	a2 := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(a, "A"), 200)
	require.Equal(t, a.Observation.Identity, a2.Observation.Identity)
	require.EqualValues(t, 2, a2.Revision)
	serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(a, "A"), 409)
	in := upstreamInput(a2, "A")
	in.ReleaseLabel = ""
	serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, in, 400)
	// The handler reauthorizes the actual DB actor instead of trusting request role claims.
	var viewer int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'upstream-viewer','fixture','org_viewer','org_viewer','active') RETURNING id`, f.org).Scan(&viewer))
	viewerBase := canonicalApplicationAuthorizedRequest(f.org, viewer)
	viewerRequest := func(method, target, body string) *http.Request {
		return withIntegrationCommittingActor(t, f.pool, viewerBase(method, target, body), viewer)
	}
	serveUpstreamObservation(t, f.h, viewerRequest, dep, http.MethodPut, upstreamInput(a2, "B"), 403)
	serveUpstreamObservation(t, f.h, canonicalApplicationAuthorizedRequest(f.org, f.actor), dep, http.MethodPut, upstreamInput(a2, "B"), 403)
	// Exactly one concurrent CAS can win. No automatic retry, no lost update.
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for _, label := range []string{"B", "C"} {
		group.Add(1)
		go func(label string) {
			defer group.Done()
			wire, _ := json.Marshal(upstreamInput(a2, label))
			r := f.authorized(http.MethodPut, "/", string(wire))
			r.SetPathValue("deploymentID", strconv.FormatInt(dep, 10))
			w := httptest.NewRecorder()
			f.h.PutModelUpstreamReleaseObservation(w, r)
			codes <- w.Code
		}(label)
	}
	group.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	require.Equal(t, map[int]int{200: 1, 409: 1}, counts)
	current := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodGet, nil, 200)
	require.EqualValues(t, 3, current.Revision)
	require.NotEqual(t, a.Observation.Identity, current.Observation.Identity)
	var auditCount int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='model_deployment.upstream_release_observation'`).Scan(&auditCount))
	require.Equal(t, 3, auditCount)
	// Direct DB writes cannot clear, skip revisions, relabel an epoch or store malformed JSON.
	for name, mutate := range map[string]func(map[string]any){
		"missing label":            func(v map[string]any) { delete(v["claim"].(map[string]any), "release_label") },
		"string actor":             func(v map[string]any) { v["recorded_by"] = "1" },
		"null timestamp":           func(v map[string]any) { v["checked_at"] = nil },
		"multibyte label overflow": func(v map[string]any) { v["claim"].(map[string]any)["release_label"] = strings.Repeat("中", 80) },
		"invalid source host":      func(v map[string]any) { v["provenance"].(map[string]any)["source_url"] = "https://bad\\host/release" },
		"invalid source escape": func(v map[string]any) {
			v["provenance"].(map[string]any)["source_url"] = "https://provider.example.test/%zz"
		},

		"numeric revision": func(v map[string]any) { v["provenance"].(map[string]any)["source_revision"] = 2 },
		"scope relabel":    func(v map[string]any) { v["scope"].(map[string]any)["api_model"] = "other" },
		"old epoch":        func(v map[string]any) { v["claim"].(map[string]any)["release_label"] = "new" },
	} {
		t.Run(name, func(t *testing.T) {
			wire, _ := json.Marshal(current.Observation)
			var v map[string]any
			require.NoError(t, json.Unmarshal(wire, &v))
			mutate(v)
			wire, _ = json.Marshal(v)
			_, err := f.pool.Exec(ctx, `UPDATE model_deployments SET upstream_release_observation=$2,upstream_observation_revision=upstream_observation_revision+1 WHERE id=$1`, dep, wire)
			require.Error(t, err)
		})
	}
	_, err := f.pool.Exec(ctx, `UPDATE model_deployments SET upstream_release_observation=NULL,upstream_observation_revision=0 WHERE id=$1`, dep)
	require.Error(t, err)
}

func TestUpstreamObservationOperationProbeApprovalAndReplayIntegration(t *testing.T) {
	f := newUpstreamObservationFixture(t)
	ctx := t.Context()
	dep := f.endpoint.DeploymentID
	_, err := f.pool.Exec(ctx, `INSERT INTO model_deployment_operations(deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,protocol_profile_id,protocol_profile_version) VALUES($1,$2,'enabled','declared','',1,$3,'1.0.0')`, dep, modeloperation.IDResponses, endpointprotocol.ProfileOpenAIResponsesText)
	require.NoError(t, err)
	state := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(upstreamrelease.State{}, "A"), 200)
	calls := 0
	f.h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return canonicalProbeHTTPResponse(200, `{"id":"fixture","object":"response","status":"completed","model":"upstream-upstream","error":null,"incomplete_details":null,"output":[{"id":"message","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`), nil
	}))
	probeRequest := func(identity string, expected int) canonicalOperationProbeResponse {
		wire, _ := json.Marshal(canonicalOperationProbeRequest{CredentialBindingID: f.endpoint.BindingID, ExpectedUpstreamObservationIdentity: identity})
		r := f.authorized(http.MethodPost, "/", string(wire))
		r.SetPathValue("deploymentID", strconv.FormatInt(dep, 10))
		r.SetPathValue("operationID", string(modeloperation.IDResponses))
		w := httptest.NewRecorder()
		f.h.ProbeCanonicalModelOperation(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
		var result canonicalOperationProbeResponse
		if expected == 200 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		}
		return result
	}
	probeRequest("", 409)
	require.Zero(t, calls)
	probe := probeRequest(state.Observation.Identity, 200)
	require.True(t, probe.Success)
	require.Equal(t, 1, calls)
	var connection, deployment, operation int64
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT c.config_revision,d.config_revision,o.revision FROM model_deployments d JOIN provider_connections c ON c.id=d.connection_id JOIN model_deployment_operations o ON o.deployment_id=d.id WHERE d.id=$1 AND o.operation_id=$2`, dep, modeloperation.IDResponses).Scan(&connection, &deployment, &operation))
	var operatorOrg, approver int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('upstream-operator','active','platform') RETURNING id`).Scan(&operatorOrg))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'upstream-platform','fixture','platform_admin','platform_admin','active') RETURNING id`, operatorOrg).Scan(&approver))
	platform := canonicalOperationProbeAuthorizedRequest(operatorOrg, f.org, approver, "platform_admin")
	input := modelOperationProductionApprovalRequest{ProbeID: probe.ProbeID, ExpectedConnectionRevision: connection, ExpectedDeploymentRevision: deployment, ExpectedOperationRevision: operation, ExpectedExecutorContractVersion: modelqualification.ExecutorContractVersion, ExpectedUpstreamObservationIdentity: state.Observation.Identity, Reason: "scoped synthetic approval"}
	approve := func(expected int) modelOperationProductionApprovalResponse {
		wire, _ := json.Marshal(input)
		r := platform(http.MethodPost, "/", string(wire))
		r.SetPathValue("deploymentID", strconv.FormatInt(dep, 10))
		r.SetPathValue("operationID", string(modeloperation.IDResponses))
		w := httptest.NewRecorder()
		f.h.ApproveCanonicalModelOperationProduction(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
		var result modelOperationProductionApprovalResponse
		if expected < 300 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		}
		return result
	}
	expectedID := input.ExpectedUpstreamObservationIdentity
	input.ExpectedUpstreamObservationIdentity = ""
	approve(409)
	input.ExpectedUpstreamObservationIdentity = expectedID
	approved := approve(201)
	require.Equal(t, "current", approved.UpstreamScope.State)
	state = serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(state, "B"), 200)
	replay := approve(200)
	require.True(t, replay.Replayed)
	require.Equal(t, approved.ApprovalID, replay.ApprovalID)
	require.Equal(t, "stale", replay.UpstreamScope.State)
	require.Equal(t, 1, calls)
}

func TestUpstreamObservationRejectsOldEndpointDraftAndAllowsReviewedRebindIntegration(t *testing.T) {
	f := newUpstreamObservationFixture(t)
	ctx := t.Context()
	dep := f.endpoint.DeploymentID
	a := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(upstreamrelease.State{}, "A"), 200)
	_, err := f.pool.Exec(ctx, `UPDATE provider_connections SET base_url='https://replacement.example.test',config_revision=config_revision+1 WHERE id=$1`, f.endpoint.ConnectionID)
	require.NoError(t, err)
	// Same declaration revision/identity is not sufficient after an origin edit.
	serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, upstreamInput(a, "A"), 409)
	reread := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodGet, nil, 200)
	require.Equal(t, a, reread)
	input := upstreamInput(reread, "A")
	input.ExpectedConnectionRevision = 2
	rebound := serveUpstreamObservation(t, f.h, f.authorized, dep, http.MethodPut, input, 200)
	require.Equal(t, "https://replacement.example.test", rebound.Observation.Scope.BaseURL)
	require.NotEqual(t, a.Observation.Identity, rebound.Observation.Identity)
}
