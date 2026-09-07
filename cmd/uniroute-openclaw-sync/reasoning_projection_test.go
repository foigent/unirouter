package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func reasoningFixture(id string) catalogModel {
	controls := modelprofile.DescribeParameterControls(modelprofile.Profile{ReasoningProfile: id})
	return catalogModel{ID: id, Execution: catalogExecutionProfile{Status: "verified", Source: "stable_release",
		ContextWindow: 131072, MaxOutputTokens: 65536, RequestDefaultOutputTokens: 8192,
		Capabilities: []string{"reasoning", "tools"}, ParameterControls: &controls}}
}

func TestReasoningProjectionNativeToggleAndRequiredBoundaries(t *testing.T) {
	for _, tc := range []struct {
		id, mode string
		levels   []string
		mapped   map[string]string
	}{
		{"reasoning.qwen3.7-hybrid.v1", "toggle", []string{"off", "low"}, map[string]string{"low": "enabled"}},
		{"reasoning.qwen3.8-max-hybrid.v2", "effort", []string{"off", "minimal", "low", "medium", "high", "xhigh"}, map[string]string{"high": "xhigh", "minimal": "low"}},
		{"reasoning.deepseek-v4-pro-thinking-toggle.v5", "effort", []string{"low", "medium", "high", "xhigh"}, map[string]string{"medium": "high"}},
		{"reasoning.minimax-m27-required-embedded.v2", "provider_default_only", nil, nil},
		{"reasoning.glm-5-family-thinking-toggle.v1", "provider_default_only", nil, nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			models, report, err := projectOpenClawModels("unirouter", []catalogModel{reasoningFixture(tc.id)}, nil)
			require.NoError(t, err)
			require.Equal(t, tc.mode, report.Controls[0].Mode)
			require.Equal(t, tc.levels, report.Controls[0].Client)
			require.Equal(t, "host_selected_not_measured_by_sync", report.Controls[0].ClientDefaultStatus)
			for key, value := range tc.mapped {
				require.Equal(t, value, report.Controls[0].Mapping[key])
			}
			require.Nil(t, models[0]["thinkingLevelMap"].(map[string]any)["max"])
			repeated, _, err := projectOpenClawModels("unirouter", []catalogModel{reasoningFixture(tc.id)}, models)
			require.NoError(t, err)
			a, _ := json.Marshal(models)
			b, _ := json.Marshal(repeated)
			require.JSONEq(t, string(a), string(b))
		})
	}
}

func TestReasoningProjectionRejectsDriftAndExplicitConflictsBeforeAnyConfigWrite(t *testing.T) {
	fixture := reasoningFixture("reasoning.qwen3.7-hybrid.v1")
	projected, _, err := projectOpenClawModels("unirouter", []catalogModel{fixture}, nil)
	require.NoError(t, err)
	before, _ := json.Marshal(projected)
	for _, status := range []string{"invalid", "unverified", "verified"} {
		t.Run(status, func(t *testing.T) {
			changed := fixture
			changed.Execution.Status = status
			if status == "verified" {
				changed.Execution.ParameterControls = nil
			}
			_, _, err := projectOpenClawModels("unirouter", []catalogModel{changed}, projected)
			require.Error(t, err)
			after, _ := json.Marshal(projected)
			require.Equal(t, string(before), string(after), "input remains unchanged")
		})
	}
	for _, extra := range []map[string]any{
		{"thinkingLevelMap": map[string]any{"high": "high"}},
		{"compat": map[string]any{"thinkingFormat": "deepseek"}},
		{"params": map[string]any{"thinking_budget": 262144}},
		{"params": map[string]any{"extra_body": map[string]any{"enable_thinking": false}}},
		{"params": map[string]any{"extraBody": map[string]any{"max_completion_tokens": 200000}}},
		{"params": map[string]any{"extra_body": "unverifiable"}},
	} {
		extra["id"] = fixture.ID
		_, _, err := projectOpenClawModels("unirouter", []catalogModel{fixture}, []map[string]any{extra})
		require.Error(t, err)
	}
	fixture.Execution.ParameterControls.Version = "unknown"
	_, _, err = projectOpenClawModels("unirouter", []catalogModel{fixture}, nil)
	require.ErrorContains(t, err, "unsupported parameter-controls")
}

func TestReasoningProjectionKeepsUnrelatedBodyAndReportsProviderDefault(t *testing.T) {
	fixture := reasoningFixture("reasoning.qwen3.7-hybrid.v1")
	models, report, err := projectOpenClawModels("unirouter", []catalogModel{fixture}, []map[string]any{
		{"id": fixture.ID, "params": map[string]any{"extra_body": map[string]any{"user": "local-user"}}},
	})
	require.NoError(t, err)
	require.Equal(t, "enabled", report.Controls[0].ProviderDefaults["enable_thinking"])
	require.Equal(t, "local-user", models[0]["params"].(map[string]any)["extra_body"].(map[string]any)["user"])
}

func TestClientNativeWrapperConstraintIsNotModelCapabilityAuthority(t *testing.T) {
	for _, id := range []string{"deepseek-v4-pro", "deepseek-v4-flash", "proxy/deepseek-v4-pro:variant"} {
		fixture := reasoningFixture("reasoning.deepseek-v4-pro-thinking-toggle.v5")
		fixture.ID = id
		models, report, err := projectOpenClawModels("unirouter", []catalogModel{fixture}, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"off", "high"}, report.Controls[0].Client)
		require.Equal(t, "deepseek", models[0]["compat"].(map[string]any)["thinkingFormat"])
		fixture.Execution.ParameterControls = reasoningFixture("reasoning.qwen3.7-hybrid.v1").Execution.ParameterControls
		_, _, err = projectOpenClawModels("unirouter", []catalogModel{fixture}, nil)
		require.ErrorContains(t, err, "does not match the exact typed controls")
	}
	alias := reasoningFixture("reasoning.deepseek-v4-pro-thinking-toggle.v5")
	alias.ID = "custom-stable-alias"
	models, report, err := projectOpenClawModels("unirouter", []catalogModel{alias}, nil)
	require.NoError(t, err)
	require.NotContains(t, report.Controls[0].Client, "off", "do not infer an id-triggered wrapper for an arbitrary Alias")
	require.Equal(t, "openai", models[0]["compat"].(map[string]any)["thinkingFormat"])
}
