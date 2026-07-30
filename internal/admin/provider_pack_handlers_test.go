package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProviderPacksReturnsCredentialFreeFixtureClaims(t *testing.T) {
	handler := &Handler{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider-packs", nil)

	handler.ListProviderPacks(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
	var response struct {
		Data []struct {
			Manifest struct {
				PackID           string `json:"pack_id"`
				AdapterType      string `json:"adapter_type"`
				ConformanceLevel string `json:"conformance_level"`
				NetworkVerified  bool   `json:"network_verified"`
			} `json:"manifest"`
			ManifestSHA256 string `json:"manifest_sha256"`
			SchemaSHA256   string `json:"schema_sha256"`
		} `json:"data"`
		Total         int `json:"total"`
		ClaimBoundary struct {
			ConformanceLevel string `json:"conformance_level"`
			NetworkVerified  bool   `json:"network_verified"`
			LiveCalls        bool   `json:"live_calls"`
			CredentialsUsed  bool   `json:"credentials_used"`
			ReportSHA256     string `json:"report_sha256"`
		} `json:"claim_boundary"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 3 || len(response.Data) != 3 {
		t.Fatalf("unexpected catalog size: %+v", response)
	}
	if response.ClaimBoundary.ConformanceLevel != "fixture_verified" ||
		response.ClaimBoundary.NetworkVerified ||
		response.ClaimBoundary.LiveCalls ||
		response.ClaimBoundary.CredentialsUsed ||
		response.ClaimBoundary.ReportSHA256 != "sha256:803111ecad2b365508bcac0afae2d92185d7d12de3190db31653c8eb72b33488" {
		t.Fatalf("catalog overclaims evidence: %+v", response.ClaimBoundary)
	}
	for _, pack := range response.Data {
		if pack.Manifest.PackID == "" || pack.Manifest.AdapterType == "" ||
			pack.Manifest.ConformanceLevel != "fixture_verified" ||
			pack.Manifest.NetworkVerified ||
			len(pack.ManifestSHA256) != len("sha256:")+64 ||
			len(pack.SchemaSHA256) != len("sha256:")+64 {
			t.Fatalf("invalid pack response: %+v", pack)
		}
	}
}
