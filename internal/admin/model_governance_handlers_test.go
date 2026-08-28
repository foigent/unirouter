package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelExecutionProfileCatalogAndExactAliasResolution(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	recorder := httptest.NewRecorder()
	h.ListModelExecutionProfiles(recorder, httptest.NewRequest(http.MethodGet, "/model-execution-profiles", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		ContractVersion string `json:"contract_version"`
		Data            []struct {
			ID                     string   `json:"id"`
			Version                string   `json:"version"`
			Current                bool     `json:"current"`
			Provider               string   `json:"provider"`
			Region                 string   `json:"region"`
			APISurface             string   `json:"api_surface"`
			Operations             []string `json:"operations"`
			DefaultOutputAuthority string   `json:"default_output_authority"`
			CatalogEvidenceState   string   `json:"catalog_evidence_state"`
			ProviderCapabilities   []string `json:"provider_capabilities"`
			Capabilities           []string `json:"capabilities"`
			AlwaysRequired         []string `json:"always_required_capabilities"`
			Aliases                []string `json:"aliases"`
			EvidenceRefs           []string `json:"evidence_refs"`
			UpstreamModelFamily    string   `json:"upstream_model_family"`
			UpstreamVersionPolicy  string   `json:"upstream_version_policy"`
			CacheUsageProfile      string   `json:"cache_usage_profile"`
			ProtocolProfile        string   `json:"protocol_profile"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "unirouter.model-execution-profiles.v1", response.ContractVersion)
	require.NotEmpty(t, response.Data)
	require.Equal(t, "unverified.chat", response.Data[0].ID)
	require.True(t, response.Data[0].Current)
	require.NotNil(t, response.Data[0].ProviderCapabilities)
	require.NotNil(t, response.Data[0].Capabilities)
	require.NotNil(t, response.Data[0].AlwaysRequired)
	require.NotNil(t, response.Data[0].Aliases)
	require.NotNil(t, response.Data[0].EvidenceRefs)
	var qwenRegion string
	currentGPTSurfaces := map[string]string{}
	legacyGPTCurrent := false
	var evolvingHistorical, evolvingCandidate, evolvingCore, evolvingCurrent *struct {
		ID                     string   `json:"id"`
		Version                string   `json:"version"`
		Current                bool     `json:"current"`
		Provider               string   `json:"provider"`
		Region                 string   `json:"region"`
		APISurface             string   `json:"api_surface"`
		Operations             []string `json:"operations"`
		DefaultOutputAuthority string   `json:"default_output_authority"`
		CatalogEvidenceState   string   `json:"catalog_evidence_state"`
		ProviderCapabilities   []string `json:"provider_capabilities"`
		Capabilities           []string `json:"capabilities"`
		AlwaysRequired         []string `json:"always_required_capabilities"`
		Aliases                []string `json:"aliases"`
		EvidenceRefs           []string `json:"evidence_refs"`
		UpstreamModelFamily    string   `json:"upstream_model_family"`
		UpstreamVersionPolicy  string   `json:"upstream_version_policy"`
		CacheUsageProfile      string   `json:"cache_usage_profile"`
		ProtocolProfile        string   `json:"protocol_profile"`
	}
	for _, profile := range response.Data {
		if profile.ID == "qwen.3.8-max.api" && profile.Current {
			require.Equal(t, "dashscope", profile.Provider)
			require.Equal(t, "openai_chat_completions", profile.APISurface)
			require.Equal(t, []string{"openai.chat_completions.v1"}, profile.Operations)
			require.Equal(t, "provider_documented", profile.DefaultOutputAuthority)
			require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
			qwenRegion = profile.Region
		}
		if profile.Current && strings.HasPrefix(profile.ID, "openai.gpt-5.6-") {
			currentGPTSurfaces[profile.ID] = profile.APISurface
		}
		if strings.HasPrefix(profile.ID, "openai.gpt-5.6-") && strings.HasSuffix(profile.ID, ".api") && profile.Current {
			legacyGPTCurrent = true
		}
		if profile.ID == "doubao.seed-evolving.api" && profile.Version == "1.0.0" {
			copy := profile
			evolvingHistorical = &copy
		}
		if profile.ID == "doubao.seed-evolving.api" && profile.Version == "1.1.0" {
			copy := profile
			evolvingCandidate = &copy
		}
		if profile.ID == "doubao.seed-evolving.api" && profile.Version == "1.2.0" {
			copy := profile
			evolvingCore = &copy
		}
		if profile.ID == "doubao.seed-evolving.api" && profile.Version == "1.8.0" {
			copy := profile
			evolvingCurrent = &copy
		}
	}
	require.Equal(t, "cn-beijing", qwenRegion)
	require.Equal(t, map[string]string{
		"openai.gpt-5.6-sol.chat":        "openai_chat_completions",
		"openai.gpt-5.6-sol.responses":   "openai_responses",
		"openai.gpt-5.6-terra.chat":      "openai_chat_completions",
		"openai.gpt-5.6-terra.responses": "openai_responses",
		"openai.gpt-5.6-luna.chat":       "openai_chat_completions",
		"openai.gpt-5.6-luna.responses":  "openai_responses",
	}, currentGPTSurfaces)
	require.False(t, legacyGPTCurrent)
	require.NotNil(t, evolvingHistorical)
	require.False(t, evolvingHistorical.Current)
	require.Empty(t, evolvingHistorical.UpstreamModelFamily)
	require.Empty(t, evolvingHistorical.UpstreamVersionPolicy)
	require.NotNil(t, evolvingCandidate)
	require.False(t, evolvingCandidate.Current)
	require.Equal(t, "doubao-seed-evolving", evolvingCandidate.UpstreamModelFamily)
	require.Equal(t, "floating", evolvingCandidate.UpstreamVersionPolicy)
	require.NotNil(t, evolvingCore)
	require.False(t, evolvingCore.Current)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v1", evolvingCore.ProtocolProfile)
	require.NotNil(t, evolvingCurrent)
	require.True(t, evolvingCurrent.Current)
	require.Equal(t, "doubao-seed-evolving", evolvingCurrent.UpstreamModelFamily)
	require.Equal(t, "floating", evolvingCurrent.UpstreamVersionPolicy)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v6", evolvingCurrent.ProtocolProfile)
	require.Equal(t, "cache-usage.doubao-ark-chat.v1", evolvingCurrent.CacheUsageProfile)
	require.ElementsMatch(t, []string{
		"streaming", "reasoning", "reasoning_effort", "tools", "function_tools",
		"tool_choice_auto", "tool_choice_named", "tool_choice_required", "parallel_tool_calls",
		"strict_json_schema", "vision", "prompt_cache", "cache_read_usage",
	}, evolvingCurrent.Capabilities)
	require.Equal(t, []string{"prompt_cache", "cache_read_usage"}, evolvingCurrent.AlwaysRequired)
	require.Equal(t, "locally_conformant", evolvingCurrent.CatalogEvidenceState)

	profile, err := resolveModelExecutionProfile(" MiniMax_M2.7 ", "", "")
	require.NoError(t, err)
	require.Equal(t, "minimax.m2.7.token-plan", profile.ID)
	require.Equal(t, 204_800, profile.DefaultContextWindow)
	require.Equal(t, 65_536, profile.DefaultOutputLimit)
	require.Contains(t, profile.ProviderCapabilities, "prompt_cache")
	require.NotContains(t, profile.Capabilities, "vision")

	unknown, err := resolveModelExecutionProfile("fake-MiniMax-M2.7-preview", "", "")
	require.NoError(t, err)
	require.Equal(t, "unverified.chat", unknown.ID)
	_, err = resolveModelExecutionProfile("gpt-5.6-sol", "", "")
	require.ErrorContains(t, err, "multiple API surfaces")
	_, err = resolveModelExecutionProfile("gpt-5.6-terra", "", "")
	require.ErrorContains(t, err, "multiple API surfaces")
	_, err = resolveModelExecutionProfile("gpt-5.6-luna", "", "")
	require.ErrorContains(t, err, "multiple API surfaces")
	chat, err := resolveModelExecutionProfile("gpt-5.6-sol", "openai.gpt-5.6-sol.chat", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "openai_chat_completions", chat.APISurface)
	_, err = resolveModelExecutionProfile("MiniMax-M2.7", "minimax.m2.7.token-plan", "9.9.9")
	require.Error(t, err)
}

func TestCreateModelRegistry_InvalidLifecycle(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/model-registry", strings.NewReader(`{"org_id":1,"model_name":"gpt-4o","lifecycle":"live"}`))
	req.Header.Set("Content-Type", "application/json")
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.CreateModelRegistry(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateModelRegistryRejectsHistoricalExecutionProfileBeforeDatabase(t *testing.T) {
	beginCalled := false
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
		beginCalled = true
		return nil, errors.New("database must not be reached")
	}})
	req := httptest.NewRequest(http.MethodPost, "/model-registry", strings.NewReader(`{
		"org_id":1,
		"model_name":"historical-kimi-coding",
		"execution_profile_id":"kimi.k2.7-code.api",
		"execution_profile_version":"1.0.0"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = platformModelGovernanceRequest(req)
	recorder := httptest.NewRecorder()

	h.CreateModelRegistry(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.False(t, beginCalled)
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, profileAttachmentErrorCode, response["code"])
}

func TestUpdateModelRegistryRejectsProfileIdentityMutationAfterDeployment(t *testing.T) {
	existsRead := false
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FOR UPDATE"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*string) = "deployed-model"
					*dest[2].(*string) = "Deployed model"
					*dest[3].(*string) = "custom"
					*dest[4].(*string) = "ga"
					*dest[5].(*int) = 128000
					*dest[6].(*int) = 8192
					*dest[7].(*string) = ""
					*dest[8].(*string) = ""
					*dest[9].(*string) = "deployed-model"
					*dest[10].(*bool) = false
					*dest[11].(*string) = ""
					*dest[12].(*string) = "unverified.chat"
					*dest[13].(*string) = "1.0.0"
					return nil
				}}
			case strings.Contains(sql, "SELECT EXISTS"):
				existsRead = true
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*bool) = true
					return nil
				}}
			default:
				t.Fatalf("unexpected query: %s", sql)
				return &testutil.MockRow{}
			}
		},
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			t.Fatalf("profile identity conflict must not write: %s", sql)
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := httptest.NewRequest(http.MethodPut, "/model-registry/10", strings.NewReader(`{
		"execution_profile_id":"kimi.k3.api",
		"execution_profile_version":"1.8.0"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "10")
	req = platformModelGovernanceRequest(req)
	recorder := httptest.NewRecorder()

	h.UpdateModelRegistry(recorder, req)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.True(t, existsRead)
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, deployedProfileIdentityImmutableErrorCode, response["code"])
}

func TestContextLengthInputJSONContract(t *testing.T) {
	valid := []struct {
		json string
		want int
	}{
		{json: `0`, want: 0},
		{json: `131072`, want: 131072},
		{json: `2147483647`, want: 2147483647},
	}
	for _, testCase := range valid {
		t.Run("accepts_"+testCase.json, func(t *testing.T) {
			var value contextLengthInput
			if err := json.Unmarshal([]byte(testCase.json), &value); err != nil {
				t.Fatalf("expected exact integer to be accepted: %v", err)
			}
			if int(value) != testCase.want {
				t.Fatalf("expected %d, got %d", testCase.want, value)
			}
		})
	}

	for _, input := range []string{`2147483648`, `1.5`, `1e3`, `-1`, `"128K"`, `"131072"`, `null`} {
		t.Run("rejects_"+input, func(t *testing.T) {
			var value contextLengthInput
			if err := json.Unmarshal([]byte(input), &value); !errors.Is(err, errInvalidContextLength) {
				t.Fatalf("expected stable context-length error for %s, got %v", input, err)
			}
		})
	}
}

func TestModelRegistryContextLengthValidationIsStableAndPreDatabase(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "create overflow", method: http.MethodPost, target: "/model-registry", body: `{"org_id":1,"model_name":"overflow","context_length":2147483648}`},
		{name: "create fractional", method: http.MethodPost, target: "/model-registry", body: `{"org_id":1,"model_name":"fraction","context_length":1.5}`},
		{name: "create string shorthand", method: http.MethodPost, target: "/model-registry", body: `{"org_id":1,"model_name":"shorthand","context_length":"128K"}`},
		{name: "update scientific notation", method: http.MethodPut, target: "/model-registry/10", body: `{"context_length":1e3}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			beginCalled := false
			db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
				beginCalled = true
				return nil, errors.New("database must not be reached")
			}}
			h := newTestHandler(db)
			req := httptest.NewRequest(testCase.method, testCase.target, strings.NewReader(testCase.body))
			req.Header.Set("Content-Type", "application/json")
			if testCase.method == http.MethodPut {
				req.SetPathValue("id", "10")
			}
			req = platformModelGovernanceRequest(req)
			rec := httptest.NewRecorder()

			if testCase.method == http.MethodPost {
				h.CreateModelRegistry(rec, req)
			} else {
				h.UpdateModelRegistry(rec, req)
			}

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if beginCalled {
				t.Fatal("invalid context_length must be rejected before PostgreSQL")
			}
			var response map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response["code"] != contextLengthErrorCode || response["field"] != "context_length" || response["reason_code"] != contextLengthReasonCode {
				t.Fatalf("unexpected stable validation response: %#v", response)
			}
		})
	}
}

func TestCreateModelRegistryAcceptsMaximumContextLengthBeforeDatabase(t *testing.T) {
	beginCalled := false
	db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
		beginCalled = true
		return nil, errors.New("deliberate database stop")
	}}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/model-registry", strings.NewReader(`{"org_id":1,"model_name":"max-context","context_length":2147483647}`))
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.CreateModelRegistry(rec, req)

	if !beginCalled {
		t.Fatal("maximum PostgreSQL INTEGER token count should reach the mutation transaction")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected deliberate database stop after validation, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateModelRegistry_LifecycleAuditBeforeAfter(t *testing.T) {
	var auditArgs []any
	lockedBeforeRead := false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			lockedBefore := strings.Contains(sql, "FOR UPDATE")
			lockedBeforeRead = lockedBeforeRead || lockedBefore
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 1
				*dest[1].(*string) = "gpt-4o"
				*dest[2].(*string) = "GPT-4o"
				*dest[3].(*string) = "openai"
				*dest[4].(*string) = "ga"
				*dest[5].(*int) = 128000
				*dest[6].(*int) = 16384
				*dest[7].(*string) = "chat,vision"
				*dest[8].(*string) = "global"
				*dest[9].(*string) = "gpt-4o"
				*dest[10].(*bool) = true
				*dest[11].(*string) = ""
				*dest[12].(*string) = "unverified.chat"
				*dest[13].(*string) = "1.0.0"
				if !lockedBefore {
					*dest[4].(*string) = "deprecated"
					*dest[11].(*string) = "Use company-chat-v2"
				}
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditArgs = append([]any(nil), args...)
				return pgconn.CommandTag{}, nil
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/model-registry/10", strings.NewReader(`{"lifecycle":"deprecated","migration_recommendation":"Use company-chat-v2"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-test")
	req.SetPathValue("id", "10")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.UpdateModelRegistry(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(auditArgs) != 14 {
		t.Fatalf("expected 14 audit args, got %#v", auditArgs)
	}
	if !lockedBeforeRead {
		t.Fatal("model registry before state must be locked in the mutation transaction")
	}
	if auditArgs[1] != int64(7) || auditArgs[9] != "admin_console" {
		t.Fatalf("expected actor/source in audit args, got %#v", auditArgs)
	}
	if !strings.Contains(auditArgs[11].(string), `"lifecycle":"ga"`) {
		t.Fatalf("expected before lifecycle ga, got %v", auditArgs[11])
	}
	if !strings.Contains(auditArgs[12].(string), `"lifecycle":"deprecated"`) {
		t.Fatalf("expected after lifecycle deprecated, got %v", auditArgs[12])
	}
}

func TestListApprovedModelCatalog_FiltersApprovedLifecycle(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "approved_catalog = TRUE") || !strings.Contains(sql, "mr.lifecycle IN ('ga', 'deprecated')") {
				t.Fatalf("catalog count query missing approved lifecycle filters: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "approved_catalog = TRUE") || !strings.Contains(sql, "mr.lifecycle IN ('ga', 'deprecated')") {
				t.Fatalf("catalog query missing approved lifecycle filters: %s", sql)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 10
						*dest[1].(*int64) = 1
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*string) = "GPT-4o"
						*dest[4].(*string) = "openai"
						*dest[5].(*string) = "ga"
						*dest[6].(*int) = 128000
						*dest[7].(*int) = 4096
						*dest[8].(*string) = "chat,vision"
						*dest[9].(*string) = "global"
						*dest[10].(*string) = "gpt-4o"
						*dest[11].(*bool) = true
						*dest[12].(*string) = ""
						*dest[13].(*string) = "unverified.chat"
						*dest[14].(*string) = "1.0.0"
						*dest[15].(*time.Time) = now
						*dest[16].(*time.Time) = now
						*dest[17].(*string) = "company-chat-fast"
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/model-catalog?org_id=1", nil)
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.ListApprovedModelCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["total"].(float64) != 1 {
		t.Fatalf("expected total=1, got %v", resp["total"])
	}
}

func TestListApprovedModelCatalog_RoutableOnlyRequiresStableActiveRelease(t *testing.T) {
	const assignmentProjection = "FROM runtime_channel_assignments_v1 assignment"
	checkCanonicalProjection := func(sql string) {
		t.Helper()
		for _, predicate := range []string{
			"JOIN runtime_route_revision_items item ON item.release_id = release.id",
			"release.id = slot.stable_release_id", "slot.public_model_id = mr.id",
			"release.public_model_id = mr.id", "deployment.public_model_id = mr.id",
			"deployment.customer_org_id = mr.org_id", "release.stage = 'active'",
			"release.candidate_version = 'unirouter.runtime-change-candidate.v2'",
			"release.route_items_sealed_at IS NOT NULL", "item.admission = 'allow'",
			"item.source_candidate_version = release.candidate_version",
			"legacy.candidate_version = 'unirouter.runtime-change-candidate.v1'",
		} {
			if !strings.Contains(sql, predicate) {
				t.Fatalf("catalog query lost exact manifest predicate %q: %s", predicate, sql)
			}
		}
	}
	countChecked := false
	listChecked := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			checkCanonicalProjection(sql)
			if !strings.Contains(sql, assignmentProjection) || !strings.Contains(sql, "assignment.model_name = mr.model_name") ||
				!strings.Contains(sql, "assignment.assignment_kind = 'stable'") || !strings.Contains(sql, "assignment.release_stage = 'active'") {
				t.Fatalf("routable catalog count query missing canonical stable assignment filter: %s", sql)
			}
			countChecked = true
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			checkCanonicalProjection(sql)
			if !strings.Contains(sql, assignmentProjection) || !strings.Contains(sql, "assignment.model_name = mr.model_name") ||
				!strings.Contains(sql, "assignment.assignment_kind = 'stable'") || !strings.Contains(sql, "assignment.release_stage = 'active'") {
				t.Fatalf("routable catalog list query missing canonical stable assignment filter: %s", sql)
			}
			listChecked = true
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/model-catalog?org_id=1&routable_only=true", nil)
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.ListApprovedModelCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !countChecked || !listChecked {
		t.Fatalf("expected routable filter in both count and list queries: count=%v list=%v", countChecked, listChecked)
	}
}

func TestReplaceProjectModelSets_DeveloperForbidden(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPut, "/projects/42/model-sets", strings.NewReader(`{"model_set_ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "42")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ReplaceProjectModelSets(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateModelSet_InvalidItem(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/model-sets", strings.NewReader(`{"org_id":1,"name":"Chat","items":[{"item_type":"provider","item_name":"gpt-4o"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.CreateModelSet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListModelSetItems_ReturnsOrgScopedItems(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "JOIN model_sets ms") || !strings.Contains(sql, "ms.org_id = $2") {
				t.Fatalf("expected org-scoped model set item query, got %s", sql)
			}
			if args[0] != int64(10) || args[1] != int64(1) {
				t.Fatalf("unexpected args: %#v", args)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "alias"
						*dest[1].(*string) = "company-chat"
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/model-sets/10/items?org_id=1", nil)
	req.SetPathValue("id", "10")
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.ListModelSetItems(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	items := resp["data"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["item_name"] != "company-chat" {
		t.Fatalf("unexpected items: %#v", resp["data"])
	}
}

func platformModelGovernanceRequest(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	return req.WithContext(ctx)
}
