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
						*dest[0].(*string) = "Kimi K2.6"
						return nil
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
						*dest[6].(*string) = "1.1.0"
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
	require.Equal(t, 262_144, metadata.ContextWindow)
	require.Equal(t, 32_768, metadata.MaxOutputTokens)
	require.Equal(t, "protocol.kimi-openai-chat.v2", metadata.ProtocolProfile)
	require.Contains(t, metadata.Capabilities, "vision")
	require.Contains(t, metadata.Capabilities, "video")
	require.Equal(t, "toggle", metadata.ReasoningMode)
	require.Empty(t, metadata.ReasoningEffortValues)
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
					*dest[0].(*string) = "Kimi K2.6"
					return nil
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
