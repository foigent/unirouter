package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/riskcontrol"
)

func TestRiskControlOfflineContractIsNoStoreAndRuntimeIsolated(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/risk-control/offline-contract",
		nil,
	)
	recorder := httptest.NewRecorder()
	(&Handler{}).RiskControlOfflineContract(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", recorder.Header().Get("Cache-Control"))
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		riskcontrol.ContractVersion,
		`"remote_risk_dependency":false`,
		`"provider_or_shadow_traffic":false`,
		`"runtime_policy_read":false`,
		`"runtime_enforcement_authorized":false`,
		`"runtime_promotion_authorized":false`,
		`"external_decision_gate":"P7-D04"`,
		`"runtime_mutation":"none"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	for _, forbidden := range []string{
		`"private_key"`, `"credential_value"`, `"raw_prompt"`, `"raw_response"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("contract leaked %q", forbidden)
		}
	}
}

func TestPreviewRiskControlReturnsBoundedOfflineReport(t *testing.T) {
	body, err := riskcontrol.SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/risk-control/offline-preview",
		strings.NewReader(string(body)),
	)
	recorder := httptest.NewRecorder()
	(&Handler{}).PreviewRiskControl(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var report riskcontrol.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Advice != riskcontrol.AdviceShadowOnly ||
		report.Proposal.State != riskcontrol.ProposalSimulatedReviewRequired ||
		report.Proposal.RuntimePromotionAuthorized ||
		report.RuntimeBoundary.RuntimeMutation != riskcontrol.RuntimeMutationNone {
		t.Fatalf("unexpected report=%#v", report)
	}
}

func TestPreviewRiskControlRejectsInvalidAndOversizedInputs(t *testing.T) {
	for name, test := range map[string]struct {
		body     string
		expected int
	}{
		"invalid":   {body: `{}`, expected: http.StatusBadRequest},
		"oversized": {body: strings.Repeat("x", riskcontrol.MaxInputBytes+1), expected: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/risk-control/offline-preview",
				strings.NewReader(test.body),
			)
			recorder := httptest.NewRecorder()
			(&Handler{}).PreviewRiskControl(recorder, request)
			if recorder.Code != test.expected {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
