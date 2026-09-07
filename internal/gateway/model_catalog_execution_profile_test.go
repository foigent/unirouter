package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRequestIncludesModelExecutionProfiles(t *testing.T) {
	require.True(t, requestIncludesModelExecutionProfiles([]string{"price, execution_profile"}))
	require.False(t, requestIncludesModelExecutionProfiles([]string{"price", "provider_claims"}))
}

func TestHandleModelsExecutionProfileProjectionIsOptInAndExecutableOnly(t *testing.T) {
	queryCount := 0
	database := &testutil.MockDB{
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			queryCount++
			switch queryCount {
			case 1:
				require.Contains(t, query, "WITH candidates AS")
				return &testutil.MockRows{Rows: []func(dest ...any) error{
					func(dest ...any) error {
						return setCallableModelIdentity(dest, "Kimi K2.6")
					},
				}}, nil
			case 2:
				require.Contains(t, query, "registry.execution_profile_id")
				require.Contains(t, query, "LEFT JOIN model_aliases")
				require.Equal(t, int64(7), args[0])
				require.Equal(t, []string{"Kimi K2.6"}, args[1])
				return &testutil.MockRows{Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "Kimi K2.6"
						*dest[1].(*string) = "Kimi K2.6"
						*dest[2].(*bool) = true
						*dest[3].(*int) = 262_144
						*dest[4].(*int) = 32_768
						*dest[5].(*string) = "kimi.k2.6.api"
						*dest[6].(*string) = "1.5.0"
						*dest[7].(*string) = "registry"
						*dest[8].(*bool) = true
						return nil
					},
				}}, nil
			default:
				t.Fatalf("unexpected query %d: %s", queryCount, query)
				return nil, nil
			}
		},
	}
	relay := newTestRelay(database, &mockBilling{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models?include=execution_profile", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{
		ID: 11, OrgID: 7, AllowedModels: "Kimi K2.6",
	})
	recorder := httptest.NewRecorder()

	relay.HandleModels(recorder, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 2, queryCount)
	var response struct {
		Data []struct {
			ID        string                      `json:"id"`
			Execution publicModelExecutionProfile `json:"x_unirouter_execution"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	metadata := response.Data[0].Execution
	require.Equal(t, "Kimi K2.6", response.Data[0].ID)
	require.Equal(t, "verified", metadata.Status)
	require.Equal(t, "registry", metadata.Source)
	require.Zero(t, metadata.RequestDefaultOutputTokens)
	require.Equal(t, 262_144, metadata.ContextWindow)
	require.Equal(t, 32_768, metadata.MaxOutputTokens)
	require.Equal(t, "protocol.kimi-openai-chat.v3", metadata.ProtocolProfile)
	require.Contains(t, metadata.Capabilities, "vision")
	require.Contains(t, metadata.Capabilities, "video")
	require.Contains(t, metadata.Capabilities, "builtin_web_search")
	require.Equal(t, "toggle", metadata.ReasoningMode)
	require.Empty(t, metadata.ReasoningEffortValues)
}

func TestHandleModelsExecutionProfileProjectionUsesAuthorizedSuccessorIdentity(t *testing.T) {
	queryCount := 0
	database := &testutil.MockDB{
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			queryCount++
			switch queryCount {
			case 1:
				return &testutil.MockRows{Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "glm-5.2-20260902"
						*dest[1].(*string) = "glm-5.2"
						*dest[2].(*string) = "glm-5.2-20260902"
						return nil
					},
				}}, nil
			case 2:
				require.Contains(t, query, "registry.execution_profile_id")
				require.Equal(t, int64(7), args[0])
				require.Equal(t, []string{"glm-5.2-20260902"}, args[1])
				return &testutil.MockRows{Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "glm-5.2-20260902"
						*dest[1].(*string) = "GLM-5.2"
						*dest[2].(*bool) = true
						*dest[3].(*int) = 128_000
						*dest[4].(*int) = 65_536
						*dest[5].(*string) = "glm.5.2.api"
						*dest[6].(*string) = "1.8.0"
						*dest[7].(*string) = "stable_release"
						*dest[8].(*bool) = true
						return nil
					},
				}}, nil
			default:
				t.Fatalf("unexpected query %d: %s", queryCount, query)
				return nil, nil
			}
		},
	}
	relay := newTestRelay(database, &mockBilling{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models?include=execution_profile", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{
		ID: 11, OrgID: 7, AllowedModels: "glm-5.2-20260902",
	})
	recorder := httptest.NewRecorder()

	relay.HandleModels(recorder, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 2, queryCount)
	var response struct {
		Data []struct {
			ID        string                      `json:"id"`
			Execution publicModelExecutionProfile `json:"x_unirouter_execution"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	require.Equal(t, "glm-5.2", response.Data[0].ID)
	require.Equal(t, "verified", response.Data[0].Execution.Status)
	require.Equal(t, "GLM-5.2", response.Data[0].Execution.DisplayName)
	require.NotContains(t, recorder.Body.String(), "glm-5.2-20260902")
}

func TestHandleModelsWithoutIncludeKeepsOpenAICompatibleShapeAndQueryCost(t *testing.T) {
	queryCount := 0
	database := &testutil.MockDB{
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			queryCount++
			require.True(t, strings.Contains(query, "WITH candidates AS"))
			require.NotContains(t, query, "registry.execution_profile_id")
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					return setCallableModelIdentity(dest, "Kimi K2.6")
				},
			}}, nil
		},
	}
	relay := newTestRelay(database, &mockBilling{})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{
		ID: 11, OrgID: 7, AllowedModels: "Kimi K2.6",
	})
	recorder := httptest.NewRecorder()

	relay.HandleModels(recorder, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, queryCount)
	require.NotContains(t, recorder.Body.String(), "x_unirouter_execution")
}

// An isolated SQL fixture exercises the projection against mutable registry
// drift and malformed retained slots, without weakening the production guards.
func TestModelCatalogUsesStableReleasePinsAndRejectsMalformedSource(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
CREATE TABLE model_registry (id bigint, org_id bigint, model_name text, display_name text, context_length integer, output_limit integer, execution_profile_id text, execution_profile_version text);
CREATE TABLE model_aliases (org_id bigint, alias text, target_model text, status text);
CREATE TABLE runtime_change_slots (model_name text, public_model_id bigint, stable_release_id bigint);
CREATE TABLE runtime_change_releases (id bigint, public_model_id bigint, model_name text, candidate_version text, stage text, route_items_sealed_at timestamptz, route_items_hash text, route_items_count integer, execution_profile_id text, execution_profile_version text, public_context_limit integer, public_output_limit integer);
INSERT INTO model_registry VALUES (1,7,'glm-canonical','GLM',32768,8192,'glm.5.3.api','1.8.0');
INSERT INTO model_aliases VALUES (7,'glm-stable','glm-canonical','active');
INSERT INTO runtime_change_slots VALUES ('glm-canonical',1,11);
INSERT INTO runtime_change_releases VALUES (11,1,'glm-canonical','unirouter.runtime-change-candidate.v2','active',NOW(),'sealed',1,'glm.5.3.api','1.8.0',128000,65536);`)
	require.NoError(t, err)
	relay := NewRelay(pool, nil, nil, &mockBilling{}, nil, nil)
	check := func() publicModelExecutionProfile {
		t.Helper()
		items, err := relay.loadPublicModelExecutionProfiles(ctx, 7, []string{"glm-stable"})
		require.NoError(t, err)
		return items["glm-stable"]
	}
	metadata := check()
	require.Equal(t, "verified", metadata.Status)
	require.Equal(t, "stable_release", metadata.Source)
	require.Equal(t, 128000, metadata.ContextWindow)
	require.Equal(t, 65536, metadata.MaxOutputTokens)
	require.Equal(t, 65536, metadata.RequestDefaultOutputTokens)
	require.NotNil(t, metadata.ParameterControls)
	pinnedControls := metadata.ParameterControls
	require.Equal(t, "utf8_byte_upper_bound", metadata.StreamOutputAccounting)
	_, err = pool.Exec(ctx, `UPDATE model_registry SET execution_profile_id='qwen.3.8-max.api', execution_profile_version='missing'`)
	require.NoError(t, err)
	require.Equal(t, pinnedControls, check().ParameterControls, "mutable registry cannot reinterpret exact Release controls")
	_, err = pool.Exec(ctx, `UPDATE model_registry SET execution_profile_id='glm.5.3.api', execution_profile_version='1.8.0'`)
	require.NoError(t, err)
	reset := func() {
		t.Helper()
		_, err := pool.Exec(ctx, `UPDATE runtime_change_slots SET public_model_id=1,stable_release_id=11;
UPDATE runtime_change_releases SET stage='active',public_model_id=1,model_name='glm-canonical',route_items_sealed_at=NOW(),candidate_version='unirouter.runtime-change-candidate.v2',execution_profile_version='1.8.0',public_output_limit=65536;`)
		require.NoError(t, err)
	}
	for _, statement := range []string{
		`UPDATE runtime_change_releases SET stage='sealed'`,
		`UPDATE runtime_change_releases SET public_model_id=2`,
		`UPDATE runtime_change_slots SET public_model_id=2`,
		`UPDATE runtime_change_releases SET model_name='wrong-model'`,
		`UPDATE runtime_change_releases SET route_items_sealed_at=NULL`,
		`UPDATE runtime_change_releases SET candidate_version='unirouter.runtime-change-candidate.v1'`,
		`UPDATE runtime_change_releases SET execution_profile_version='missing'`,
		`UPDATE runtime_change_releases SET public_output_limit=204800`,
		`UPDATE runtime_change_slots SET stable_release_id=999`,
	} {
		reset()
		_, err = pool.Exec(ctx, statement)
		require.NoError(t, err)
		metadata = check()
		require.Equal(t, "invalid", metadata.Status, statement)
		require.Equal(t, "stable_release", metadata.Source)
		require.Zero(t, metadata.RequestDefaultOutputTokens)
		require.Nil(t, metadata.ParameterControls, "invalid pins must not fall back to registry controls")
	}
	reset()
	_, err = pool.Exec(ctx, `UPDATE runtime_change_slots SET stable_release_id=NULL`)
	require.NoError(t, err)
	metadata = check()
	require.Equal(t, "registry", metadata.Source)
	require.Equal(t, 8192, metadata.MaxOutputTokens)
	require.Zero(t, metadata.RequestDefaultOutputTokens, "registry must not impersonate an active Release default")
	items, err := relay.loadPublicModelExecutionProfiles(ctx, 99, []string{"glm-stable"})
	require.NoError(t, err)
	require.Equal(t, "unregistered", items["glm-stable"].Status)
}
