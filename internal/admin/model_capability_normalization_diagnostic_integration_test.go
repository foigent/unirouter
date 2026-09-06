//go:build integration

package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCapabilityNormalizationFailurePersistsOnlySafeEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var org, actor int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('diagnostic-fixture','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'diagnostic-fixture','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	h := NewHandler(pool, "fixture", []byte("0123456789abcdef01234567"), nil)
	authorized := canonicalApplicationAuthorizedRequest(org, actor)
	application := createCanonicalApplicationForTest(t, h, authorized, canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{Name: "diagnostic fixture", Provider: "minimax", BaseURL: "https://api.minimax.cn/v1", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`)},
		PublicModel: canonicalPublicModelInput{ModelName: "diagnostic-minimax", ExecutionProfileID: "minimax.m2.7.payg", ExecutionProfileVersion: "1.3.0"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "MiniMax-M2.7"}, Credential: canonicalCredentialInput{APIKey: diagnosticPrivateMarker},
		Operation: canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat},
	}, http.StatusCreated)
	id := application.Application.Deployment.ID
	path := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/cache_read_usage/probes", id, modelprofile.OperationOpenAIChat)
	calls := 0
	for _, valid := range []bool{false, true} {
		h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(*http.Request) (*http.Response, error) {
			calls++
			body := diagnosticM27Response
			if !valid {
				body = strings.Replace(body, `"cached_tokens":0`, `"cached_tokens":null`, 1)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}))
		req := authorized(http.MethodPost, path, fmt.Sprintf(`{"credential_binding_id":%d}`, application.Application.Credential.Bindings[0].ID))
		req.SetPathValue("deploymentID", strconv.FormatInt(id, 10))
		req.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
		req.SetPathValue("capabilityID", modelprofile.CapabilityCacheReadUsage)
		rec := httptest.NewRecorder()
		h.ProbeCanonicalModelCapability(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var result modelCapabilityProbeResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		require.Equal(t, valid, result.Success)
		var wire []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT response_summary FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&wire))
		require.NotContains(t, string(wire), diagnosticPrivateMarker)
		var summary map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(wire, &summary))
		if valid {
			require.NotContains(t, summary, "normalization_diagnostic")
		} else {
			var diagnostic modelCapabilityNormalizationDiagnostic
			require.NoError(t, json.Unmarshal(summary["normalization_diagnostic"], &diagnostic))
			require.Equal(t, "adapter_usage", diagnostic.Stage)
			require.True(t, diagnostic.Full)
			require.Positive(t, diagnostic.Bytes)
			require.Len(t, diagnostic.BodySHA256, 64)
			require.Zero(t, result.TotalTokens)
		}
	}
	require.Equal(t, 2, calls)
	var approvals int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_capability_routing_approvals WHERE deployment_id=$1`, id).Scan(&approvals))
	require.Zero(t, approvals, "neither diagnostic evidence nor Probe success creates an Approval")
}
