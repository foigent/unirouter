package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/providercogs"
)

func TestProviderCOGSOfflineContractIsExplicitlyNonPersistent(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider-cogs/offline-contract", nil)
	response := httptest.NewRecorder()

	handler.ProviderCOGSOfflineContract(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"mode":"offline_preview"`,
		`"persistent":false`,
		`"provider_network_access":false`,
		`"credentials_accepted":false`,
		`"customer_ledger_mutation":"none"`,
		`"aggregate_auto_attribution":false`,
		`"sha256":"sha256:`,
		`"golden_report":`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("contract missing %s: %s", expected, response.Body.String())
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", response.Header().Get("Cache-Control"))
	}
}

func TestPreviewProviderCOGSOfflineRejectsOversizedBodiesBeforeEvaluation(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/provider-cogs/offline-preview",
		strings.NewReader(strings.Repeat("x", providercogs.MaxImportBytes+1)),
	)
	response := httptest.NewRecorder()

	handler.PreviewProviderCOGSOffline(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"import_size_invalid"`) {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestPreviewProviderCOGSOfflineFailsClosedWithoutEchoingEvidence(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/provider-cogs/offline-preview",
		strings.NewReader(`{"secret_supplier_line":"must-not-echo"}`),
	)
	response := httptest.NewRecorder()

	handler.PreviewProviderCOGSOffline(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "must-not-echo") {
		t.Fatalf("response leaked input evidence: %s", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", response.Header().Get("Cache-Control"))
	}
}
