package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/agenttrust"
)

func TestAgentTrustOfflineContractIsNoStoreAndNotRuntimeTrusted(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent-trust/offline-contract", nil)
	recorder := httptest.NewRecorder()
	(&Handler{}).AgentTrustOfflineContract(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", recorder.Header().Get("Cache-Control"))
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		agenttrust.ContractVersion,
		`"fixture_public_key_runtime_trust":false`,
		`"runtime_enforcement_authorized":false`,
		`"runtime_mutation":"none"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	for _, forbidden := range []string{`"private_key"`, `"root_secret"`, `"capability_token"`, `"credential_value"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("contract leaked %q: %s", forbidden, body)
		}
	}
}

func TestPreviewAgentTrustReturnsVerifiedOfflineReport(t *testing.T) {
	body, err := agenttrust.SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-trust/offline-preview", strings.NewReader(string(body)))
	recorder := httptest.NewRecorder()
	(&Handler{}).PreviewAgentTrust(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var report agenttrust.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.ReceiptVerification.CryptographicallyValid || report.ReceiptVerification.RuntimeTrusted ||
		report.RuntimeEnforcementAuthorized || report.RuntimeMutation != agenttrust.RuntimeMutationNone {
		t.Fatalf("unexpected report=%#v", report)
	}
}

func TestPreviewAgentTrustRejectsInvalidAndOversizedInputs(t *testing.T) {
	for name, test := range map[string]struct {
		body     string
		expected int
	}{
		"invalid":   {body: `{}`, expected: http.StatusBadRequest},
		"oversized": {body: strings.Repeat("x", agenttrust.MaxInputBytes+1), expected: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-trust/offline-preview", strings.NewReader(test.body))
			recorder := httptest.NewRecorder()
			(&Handler{}).PreviewAgentTrust(recorder, request)
			if recorder.Code != test.expected {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
