package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/governancesim"
)

func TestGovernanceSimulationOfflineContractIsNoStoreAndCredentialFree(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/governance-simulation/offline-contract", nil)
	recorder := httptest.NewRecorder()
	(&Handler{}).GovernanceSimulationOfflineContract(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", recorder.Header().Get("Cache-Control"))
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		governancesim.ContractVersion,
		governancesim.ReleaseVersion,
		`"runtime_promotion_authorized":false`,
		`"runtime_mutation":"none"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	for _, forbidden := range []string{`"credential_value"`, `"base_url"`, `"key_enc"`, `"secret"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("contract leaked forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestPreviewGovernanceSimulationReturnsDeterministicNoMutationReport(t *testing.T) {
	body, err := governancesim.SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/governance-simulation/offline-preview", strings.NewReader(string(body)))
	recorder := httptest.NewRecorder()
	(&Handler{}).PreviewGovernanceSimulation(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var report governancesim.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.RuntimeMutation != governancesim.RuntimeMutationNone ||
		report.Release.RuntimePromotionAuthorized ||
		report.OverallRisk != governancesim.RiskReviewRequired {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestPreviewGovernanceSimulationRejectsInvalidAndOversizedInputs(t *testing.T) {
	for name, test := range map[string]struct {
		body     string
		expected int
	}{
		"invalid":   {body: `{}`, expected: http.StatusBadRequest},
		"oversized": {body: strings.Repeat("x", governancesim.MaxInputBytes+1), expected: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/governance-simulation/offline-preview", strings.NewReader(test.body))
			recorder := httptest.NewRecorder()
			(&Handler{}).PreviewGovernanceSimulation(recorder, request)
			if recorder.Code != test.expected {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
