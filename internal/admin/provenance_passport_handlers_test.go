package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/provenancepassport"
)

func TestProvenancePassportOfflineContractIsNoStoreAndRuntimeIsolated(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provenance-passport/offline-contract", nil)
	recorder := httptest.NewRecorder()
	(&Handler{}).ProvenancePassportOfflineContract(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", recorder.Header().Get("Cache-Control"))
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		provenancepassport.ContractVersion,
		`"second_connector_registered":false`,
		`"second_connector_runtime_owned":false`,
		`"credentials_accepted":false`,
		`"cursor_or_checkpoint_accepted":false`,
		`"runtime_mutation":"none"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	for _, forbidden := range []string{`"api_key"`, `"credential_value"`, `"cursor":`, `"checkpoint":`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("contract leaked %q: %s", forbidden, body)
		}
	}
}

func TestPreviewProvenancePassportReturnsExactOfflineReport(t *testing.T) {
	body, err := provenancepassport.SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/provenance-passport/offline-preview",
		strings.NewReader(string(body)),
	)
	recorder := httptest.NewRecorder()
	(&Handler{}).PreviewProvenancePassport(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var report provenancepassport.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Source.ExactBytesPreserved ||
		report.SecondConnector.RuntimeRegistered ||
		report.RuntimeBoundary.RuntimeMutation != provenancepassport.RuntimeMutationNone {
		t.Fatalf("unexpected report=%#v", report)
	}
}

func TestPreviewProvenancePassportRejectsInvalidAndOversizedInputs(t *testing.T) {
	for name, test := range map[string]struct {
		body     string
		expected int
	}{
		"invalid":   {body: `{}`, expected: http.StatusBadRequest},
		"oversized": {body: strings.Repeat("x", provenancepassport.MaxInputBytes+1), expected: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/provenance-passport/offline-preview",
				strings.NewReader(test.body),
			)
			recorder := httptest.NewRecorder()
			(&Handler{}).PreviewProvenancePassport(recorder, request)
			if recorder.Code != test.expected {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
