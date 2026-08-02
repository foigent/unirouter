package clientcompat

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/uniroute/unirouter/internal/workloadapp"
)

//go:embed testdata/client-compatibility.v1.json
var compatibilityBundle []byte

func TestCompatibilityBundleMatchesAuthoritativeClientProfiles(t *testing.T) {
	var bundle struct {
		ContractVersion string `json:"contract_version"`
		Cases           []struct {
			CaseID               string `json:"case_id"`
			ProfileID            string `json:"profile_id"`
			Endpoint             string `json:"endpoint"`
			ExpectedRuntimeState string `json:"expected_runtime_state"`
			ExpectedEvidence     string `json:"expected_evidence_level"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(compatibilityBundle, &bundle); err != nil {
		t.Fatalf("parse compatibility bundle: %v", err)
	}
	if bundle.ContractVersion != "unirouter.client-compatibility.v1" || len(bundle.Cases) != len(workloadapp.ClientProfiles()) {
		t.Fatalf("bundle coverage drifted: version=%s cases=%d profiles=%d", bundle.ContractVersion, len(bundle.Cases), len(workloadapp.ClientProfiles()))
	}
	seen := make(map[string]bool, len(bundle.Cases))
	for _, testCase := range bundle.Cases {
		if testCase.CaseID == "" || seen[testCase.ProfileID] {
			t.Fatalf("duplicate or unnamed case: %#v", testCase)
		}
		seen[testCase.ProfileID] = true
		profile, ok := workloadapp.LookupClientProfile(testCase.ProfileID)
		if !ok {
			t.Fatalf("unknown profile %s", testCase.ProfileID)
		}
		if profile.RuntimeState != testCase.ExpectedRuntimeState || profile.EvidenceLevel != testCase.ExpectedEvidence {
			t.Fatalf("evidence claim drift for %s: %#v", testCase.ProfileID, profile)
		}
		endpointFound := false
		for _, endpoint := range profile.SupportedEndpoints {
			if endpoint == testCase.Endpoint {
				endpointFound = true
			}
		}
		if !endpointFound {
			t.Fatalf("endpoint %s missing from %s", testCase.Endpoint, testCase.ProfileID)
		}
	}
}

func TestCompatibilityBundleCannotClaimResponsesOrAdaptersAsReady(t *testing.T) {
	responses, _ := workloadapp.LookupClientProfile("client.openai-agents.responses.v1")
	if responses.RuntimeState != workloadapp.RuntimeContractOnly || responses.EvidenceLevel != workloadapp.EvidenceUnsupported || responses.RecommendedForPOC {
		t.Fatalf("Responses claim widened: %#v", responses)
	}
	for _, profileID := range []string{"client.feishu-bot.adapter.v1", "client.feishu-aily.adapter.v1"} {
		profile, _ := workloadapp.LookupClientProfile(profileID)
		if profile.RuntimeState != workloadapp.RuntimeAdapterRequired || profile.EvidenceLevel != workloadapp.EvidenceUnsupported {
			t.Fatalf("adapter claim widened: %#v", profile)
		}
	}
}
