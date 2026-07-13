package guardrail

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClassifierUsesFixedBoundedContract(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type")
		}
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["contract_version"] != ExternalClassifierContractVersion || request["text"] != "unsafe" {
			t.Fatalf("unexpected request: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"contract_version": ExternalClassifierContractVersion,
			"findings":         []map[string]interface{}{{"category": "fixture", "start": 0, "end": 6, "confidence_bucket": "high"}},
		})
	}))
	defer server.Close()
	classifier, err := NewHTTPClassifier(HTTPClassifierOptions{
		Name: "fixture", Version: "fixture.v1", Endpoint: server.URL,
		Client: server.Client(), AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := classifier.Detect(t.Context(), Text{Path: "message", Content: "unsafe"}, json.RawMessage(`{"threshold":"high"}`))
	if err != nil || len(findings) != 1 || findings[0].Category != "fixture" || calls != 1 {
		t.Fatalf("unexpected classifier result: findings=%+v calls=%d err=%v", findings, calls, err)
	}
}

func TestHTTPClassifierRejectsTenantLikeEndpointsAndUnsafeFindings(t *testing.T) {
	if _, err := NewHTTPClassifier(HTTPClassifierOptions{Name: "x", Version: "v1", Endpoint: "http://tenant.example/classify"}); err == nil {
		t.Fatal("plain HTTP endpoint was accepted")
	}
	if _, err := NewHTTPClassifier(HTTPClassifierOptions{Name: "x", Version: "v1", Endpoint: "https://user:pass@example.com/classify"}); err == nil {
		t.Fatal("credential-bearing endpoint was accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"contract_version": ExternalClassifierContractVersion,
			"findings":         []map[string]interface{}{{"category": "bad", "start": 0, "end": 999}},
		})
	}))
	defer server.Close()
	classifier, err := NewHTTPClassifier(HTTPClassifierOptions{Name: "fixture", Version: "v1", Endpoint: server.URL, Client: server.Client(), AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := classifier.Detect(t.Context(), Text{Content: "short"}, nil); err == nil {
		t.Fatal("out-of-range finding was accepted")
	}
}

func TestHTTPClassifierDoesNotLetUnsortedFindingsSuppressEarlierSpans(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"contract_version": ExternalClassifierContractVersion,
			"findings": []map[string]interface{}{
				{"category": "later", "start": 5, "end": 10, "confidence_bucket": "high"},
				{"category": "earlier", "start": 0, "end": 4, "confidence_bucket": "high"},
			},
		})
	}))
	defer server.Close()
	classifier, err := NewHTTPClassifier(HTTPClassifierOptions{Name: "fixture", Version: "v1", Endpoint: server.URL, Client: server.Client(), AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := classifier.Detect(t.Context(), Text{Content: "0123456789"}, nil)
	if err != nil || len(findings) != 2 {
		t.Fatalf("unsorted detector findings were prematurely discarded: findings=%+v err=%v", findings, err)
	}
}
