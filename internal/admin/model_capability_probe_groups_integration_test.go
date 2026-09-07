//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSharedCapabilityProbeGroupAtomicFactsAndIndependentApprovalsIntegration(t *testing.T) {
	for _, mode := range []string{"success", "data-definition", "midflight-drift", "transport-unknown", "invalid-200", "atomic-rollback", "commit-unknown", "not-started"} {
		t.Run(mode, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			var org, actor int64
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('shared-probe','active','customer') RETURNING id`).Scan(&org))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'shared-probe','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
			database := &canonicalAmbiguousCommitDB{Pool: pool}
			h := NewHandlerWithAPIKeyHashSecret(database, "fixture", "fixture-hash", []byte("0123456789abcdef01234567"), nil)
			authorized := canonicalApplicationAuthorizedRequest(org, actor)
			application := createCanonicalApplicationForTest(t, h, authorized, canonicalModelApplicationCreateRequest{
				Connection:  canonicalConnectionInput{Name: "Shared GLM", Provider: "glm", BaseURL: "https://open.bigmodel.cn/api/paas/v4", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
				PublicModel: canonicalPublicModelInput{ModelName: "shared-glm", ExecutionProfileID: "glm.5.3.api", ExecutionProfileVersion: "1.9.0"},
				Deployment:  canonicalDeploymentInput{UpstreamModel: "glm-5.3"}, Credential: canonicalCredentialInput{APIKey: "fixture-only"}, Operation: canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
			}, http.StatusCreated).Application

			if mode == "data-definition" {
				profile, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
				require.True(t, ok)
				profile.Version = "99.0.0"
				definition, e := modelprofile.NewDefinition(profile, "glm/glm-5.3", "")
				require.NoError(t, e)
				tx, e := pool.Begin(ctx)
				require.NoError(t, e)
				defer tx.Rollback(ctx)
				wire := []byte(`{"fixture":"shared observation data profile"}`)
				_, e = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,kind,legacy_through,legacy_projection_digest,expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,materializer_contract,target_identity,publisher_identity) VALUES('shared-data-fixture','legacy_import','000113_glm_stream_function_tools_qualification',$1,$1,$1,$2,$3,'fixture','{}','{}')`, strings.Repeat("0", 64), fmt.Sprintf("%x", sha256.Sum256(wire)), wire)
				require.NoError(t, e)
				_, e = tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id','shared-data-fixture',true)`)
				require.NoError(t, e)
				_, e = tx.Exec(ctx, `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,schema_version,canonical_bytes,publication_id) VALUES($1,$2,$3,$4,$5,$6,'shared-data-fixture')`, definition.Digest(), profile.ID, profile.Version, definition.ProductLineageID(), modelprofile.DefinitionSchemaV1, definition.CanonicalBytes())
				require.NoError(t, e)
				_, e = tx.Exec(ctx, `INSERT INTO model_capability_probe_contract_bindings(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,introduced_publication_id,profile_definition_digest) SELECT operation_id,capability_id,execution_profile_id,'99.0.0',protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,'shared-data-fixture',$1 FROM model_capability_probe_contract_bindings WHERE execution_profile_id='glm.5.3.api' AND execution_profile_version='1.9.0'`, definition.Digest())
				require.NoError(t, e)
				_, e = tx.Exec(ctx, `INSERT INTO model_capability_current_probe_contracts(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version) SELECT operation_id,capability_id,execution_profile_id,'99.0.0',protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version FROM model_capability_current_probe_contracts WHERE execution_profile_id='glm.5.3.api' AND execution_profile_version='1.9.0'`)
				require.NoError(t, e)
				require.NoError(t, tx.Commit(ctx))
				application = createCanonicalApplicationForTest(t, h, authorized, canonicalModelApplicationCreateRequest{Connection: canonicalConnectionInput{ID: application.Connection.ID}, PublicModel: canonicalPublicModelInput{ID: application.PublicModel.ID}, Deployment: canonicalDeploymentInput{UpstreamModel: "glm-5.3", ProfileDefinitionDigest: definition.Digest(), MaxContext: canonicalTestPtr(128000), DefaultMaxOutput: canonicalTestPtr(4096)}, Credential: canonicalCredentialInput{BindingID: application.Credential.Bindings[0].ID}, Operation: canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)}}, http.StatusCreated).Application
			}
			dep, binding, op := application.Deployment.ID, application.Credential.Bindings[0].ID, string(modeloperation.IDChatCompletions)
			fixture, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
			require.NoError(t, err)
			if mode == "not-started" {
				// Wrong local decryptor: the persisted identity remains current,
				// but no transport dispatch is possible.
				h = NewHandlerWithAPIKeyHashSecret(database, "fixture", "fixture-hash", []byte("different-fixture-key-123"), nil)
			}
			expectedCalls := 1
			if mode == "not-started" {
				expectedCalls = 0
			}
			calls := 0
			h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "commit-unknown" {
					database.mu.Lock()
					database.inject = true
					database.mu.Unlock()
				}
				if mode == "atomic-rollback" {
					_, e := pool.Exec(ctx, `CREATE FUNCTION fail_shared_fact_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.capability_id='streaming' THEN RAISE EXCEPTION 'fixture partial group failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_shared_fact_fixture BEFORE INSERT ON model_capability_probes FOR EACH ROW EXECUTE FUNCTION fail_shared_fact_fixture()`)
					require.NoError(t, e)
				}
				if mode == "transport-unknown" {
					return nil, errors.New("fixture transport result unknown")
				}
				if mode == "midflight-drift" {
					_, e := pool.Exec(context.Background(), `UPDATE model_deployment_capabilities SET support_state='unsupported',revision=revision+1 WHERE deployment_id=$1 AND capability_id='streaming'`, dep)
					require.NoError(t, e)
				}
				wire := string(fixture)
				if mode == "invalid-200" {
					wire = strings.ReplaceAll(wire, "UNIROUTER_TOOL_PROBE_OK", "INVALID")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			}))
			_, deniedActor := h.loadCapabilityProbePlan(ctx, org, actor+999999, dep, op, binding)
			require.Error(t, deniedActor)
			require.Zero(t, calls)
			plan, err := h.loadCapabilityProbePlan(ctx, org, actor, dep, op, binding)
			require.NoError(t, err)
			require.Zero(t, calls)
			require.Len(t, plan.Groups, 1)
			g := plan.Groups[0]
			require.Len(t, g.CapabilityIDs, 5)
			audit := requestAuditMeta(authorized(http.MethodPost, "/", ""), org, 0)
			_, err = h.probeCapabilityGroup(ctx, org, actor, dep, op, binding, strings.Repeat("f", 64), g.CapabilityIDs, audit)
			require.Error(t, err)
			require.Zero(t, calls)
			result, err := h.probeCapabilityGroup(ctx, org, actor, dep, op, binding, g.PlanDigest, g.CapabilityIDs, audit)
			if mode == "atomic-rollback" || mode == "commit-unknown" {
				require.Error(t, err)
				require.Equal(t, expectedCalls, calls)
				require.NotEmpty(t, result.Observation.SourceRef)
				require.Empty(t, result.Results)
				var count int
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_capability_probes WHERE deployment_id=$1`, dep).Scan(&count))
				readback, e := h.loadCapabilityProbeGroup(ctx, org, dep, op, result.Observation.SourceRef)
				if mode == "atomic-rollback" {
					require.Zero(t, count)
					require.Error(t, e)
				} else {
					require.Equal(t, 5, count)
					require.NoError(t, e)
					require.Len(t, readback.Facts, 5)
					require.Equal(t, result.Observation, readback.Observation)
					for _, v := range readback.CurrentQualifications {
						require.False(t, v.Qualified)
					}
				}
				require.Equal(t, expectedCalls, calls, "readback never dispatches again")
				return
			}
			require.NoError(t, err)
			require.Equal(t, expectedCalls, calls)
			require.Len(t, result.Results, 5)
			var rows, refs, approved int
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT source_ref) FROM model_capability_probes WHERE deployment_id=$1`, dep).Scan(&rows, &refs))
			require.Equal(t, 5, rows)
			require.Equal(t, 1, refs)
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_capability_routing_approvals WHERE deployment_id=$1`, dep).Scan(&approved))
			require.Zero(t, approved)
			readback, err := h.loadCapabilityProbeGroup(ctx, org, dep, op, result.Observation.SourceRef)
			require.NoError(t, err)
			require.Len(t, readback.Facts, 5)
			require.Equal(t, result.Observation, readback.Observation)
			require.Equal(t, expectedCalls, calls)
			for _, v := range readback.CurrentQualifications {
				require.False(t, v.Qualified)
			}
			for _, r := range result.Results {
				require.Equal(t, (mode == "success" || mode == "data-definition"), r.Success)
				if mode == "midflight-drift" {
					require.False(t, r.Current)
					require.Equal(t, "configuration_changed_during_probe", r.ReasonCode)
				}
			}
			if mode == "transport-unknown" {
				require.Equal(t, "started", result.Observation.DispatchState)
				require.True(t, result.Observation.OutcomeAmbiguous)
			} else if mode == "not-started" {
				require.Equal(t, "not_started", result.Observation.DispatchState)
				require.False(t, result.Observation.OutcomeAmbiguous)
			} else {
				require.Equal(t, "response_received", result.Observation.DispatchState)
			}
			if mode == "success" || mode == "data-definition" {
				// A normal org administrator still cannot grant platform approval.
				_, denied := h.approveCanonicalModelCapabilityRouting(ctx, org, actor, org, dep, op, result.Results[0].CapabilityID, modelCapabilityApprovalRequest{CapabilityProbeID: result.Results[0].CapabilityProbeID, Reason: "not a platform approver"}, audit)
				require.Error(t, denied)
				var operatorOrg, approver int64
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('shared-approvals','active','platform') RETURNING id`).Scan(&operatorOrg))
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'shared-approver','fixture','platform_admin','platform_admin','active') RETURNING id`, operatorOrg).Scan(&approver))
				approvalAudit := requestAuditMeta(canonicalOperationProbeAuthorizedRequest(operatorOrg, org, approver, auth.RolePlatformAdmin)(http.MethodPost, "/", ""), org, 0)
				for _, r := range result.Results {
					a, e := h.approveCanonicalModelCapabilityRouting(ctx, org, approver, operatorOrg, dep, op, r.CapabilityID, modelCapabilityApprovalRequest{CapabilityProbeID: r.CapabilityProbeID, Reason: "reviewed exact shared fixture"}, approvalAudit)
					require.NoError(t, e)
					require.True(t, a.Qualified)
				}
				readback, err = h.loadCapabilityProbeGroup(ctx, org, dep, op, result.Observation.SourceRef)
				require.NoError(t, err)
				for _, v := range readback.CurrentQualifications {
					require.True(t, v.Qualified)
				}
				require.Equal(t, expectedCalls, calls)
			}
		})
	}
}
