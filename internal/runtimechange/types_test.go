package runtimechange

import (
	"reflect"
	"testing"

	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func intPointer(value int) *int { return &value }

func TestNormalizeDraftAndMergeOverrides(t *testing.T) {
	request, err := NormalizeDraft(DraftRequest{
		ReleaseRef: " RCR_Model_Canary ", ModelName: "company-chat", Reason: "bounded rollout",
		Overrides: []routingpolicy.ChannelOverride{{
			ChannelID: 9, ExpectedRoutingEpoch: 4, Admission: "ALLOW", Weight: intPointer(25),
		}},
	})
	if err != nil {
		t.Fatalf("NormalizeDraft() error = %v", err)
	}
	if request.ReleaseRef != "rcr_model_canary" || request.Overrides[0].Admission != routingpolicy.AdmissionAllow {
		t.Fatalf("unexpected normalized request: %#v", request)
	}
	baseline := []routingpolicy.ChannelOverride{
		{ChannelID: 3, ExpectedRoutingEpoch: 1, Admission: routingpolicy.AdmissionDeny},
		{ChannelID: 9, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow},
	}
	merged := mergeOverrides(baseline, request.Overrides)
	if len(merged) != 2 || merged[0].ChannelID != 3 || merged[1].ExpectedRoutingEpoch != 4 || merged[1].Weight == nil {
		t.Fatalf("unexpected merged override set: %#v", merged)
	}
	if reflect.DeepEqual(merged, baseline) {
		t.Fatal("patch did not replace matching channel")
	}
}

func TestCanarySelectionIsDeterministicAndBounded(t *testing.T) {
	first := canarySelected("company-chat", "request-42", "rcr_release", 2500)
	for index := 0; index < 50; index++ {
		if got := canarySelected("company-chat", "request-42", "rcr_release", 2500); got != first {
			t.Fatal("canary selection changed for the same server selection key")
		}
	}
	if canarySelected("company-chat", "", "rcr_release", 5000) {
		t.Fatal("empty selection key must not enter canary")
	}
	selected := 0
	for index := 0; index < 10000; index++ {
		if canarySelected("company-chat", string(rune(index+1)), "rcr_release", 1000) {
			selected++
		}
	}
	if selected < 800 || selected > 1200 {
		t.Fatalf("10%% canary distribution outside deterministic tolerance: %d", selected)
	}
}
