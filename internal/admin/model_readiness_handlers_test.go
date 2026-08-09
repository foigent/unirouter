package admin

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/modelreadiness"
)

func TestModelReadinessRejectsNonCanonicalOrUnboundedQueriesBeforeLookup(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"missing project", "model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"model too long", "project_id=1&model=" + strings.Repeat("m", maxReadinessModelLength+1) + "&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"model control", "project_id=1&model=m%0Aother&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self"},
		{"dynamic endpoint", "project_id=1&model=m&endpoint=%2Fv1%2Fmedia%2Fjobs%2F42&caller=self"},
		{"unsupported endpoint", "project_id=1&model=m&endpoint=%2Fv1%2Fresponses&caller=self"},
		{"unknown caller", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=other"},
		{"self application", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self&application_ref=app_one"},
		{"workload application missing", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=workload_application"},
		{"workload application too long", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=workload_application&application_ref=app_" + strings.Repeat("a", maxReadinessApplicationRefLength)},
		{"workload application control", "project_id=1&model=m&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=workload_application&application_ref=app_one%0Aother"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &Handler{}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness?"+test.query, nil)
			response := httptest.NewRecorder()
			handler.GetModelReadiness(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestReadinessProjectAndBudgetAxesPreserveIndependentSemantics(t *testing.T) {
	tests := []struct {
		name   string
		axis   modelreadiness.Axis
		state  modelreadiness.State
		reason string
	}{
		{"active project", readinessProjectAxis("active", "/projects/1"), modelreadiness.StateReady, "project_active"},
		{"inactive project", readinessProjectAxis("archived", "/projects/1"), modelreadiness.StateBlocked, "project_inactive"},
		{"budget absent", readinessProjectBudgetAxis(sql.NullInt64{}, "", "/projects/1?tab=budget"), modelreadiness.StateNotApplicable, "project_budget_not_configured_monitoring_only"},
		{"budget monitoring", readinessProjectBudgetAxis(sql.NullInt64{Int64: 100, Valid: true}, "monthly", "/projects/1?tab=budget"), modelreadiness.StateWarning, "project_budget_monitoring_only"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.axis.State != test.state || test.axis.ReasonCode != test.reason {
				t.Fatalf("axis=%#v", test.axis)
			}
		})
	}
}

func TestReadinessGrantScopeUsesCanonicalProjectDefaultAndExplicitIntersection(t *testing.T) {
	tests := []struct {
		name      string
		grant     readinessGrantEvidence
		requested string
		resolved  string
		endpoint  string
		allowed   bool
	}{
		{"project default chat", readinessGrantEvidence{ScopeMode: accessscope.ModeProjectDefault}, "alias", "model", "/v1/chat/completions", true},
		{"project default media excluded", readinessGrantEvidence{ScopeMode: accessscope.ModeProjectDefault}, "model", "model", "/v1/media/jobs", false},
		{"explicit requested model", readinessGrantEvidence{ScopeMode: accessscope.ModeExplicit, AllowedModels: []string{"alias"}, AllowedEndpoints: []string{"/v1/embeddings"}}, "alias", "model", "/v1/embeddings", true},
		{"explicit resolved model", readinessGrantEvidence{ScopeMode: accessscope.ModeExplicit, AllowedModels: []string{"model"}, AllowedEndpoints: []string{"/v1/embeddings"}}, "alias", "model", "/v1/embeddings", true},
		{"explicit endpoint excluded", readinessGrantEvidence{ScopeMode: accessscope.ModeExplicit, AllowedModels: []string{"model"}, AllowedEndpoints: []string{"/v1/chat/completions"}}, "model", "model", "/v1/embeddings", false},
		{"unknown mode", readinessGrantEvidence{ScopeMode: "unknown", AllowedModels: []string{"model"}, AllowedEndpoints: []string{"/v1/chat/completions"}}, "model", "model", "/v1/chat/completions", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := readinessGrantScopeAllows(test.grant, test.requested, test.resolved, test.endpoint); got != test.allowed {
				t.Fatalf("allowed=%v want=%v", got, test.allowed)
			}
		})
	}
}
