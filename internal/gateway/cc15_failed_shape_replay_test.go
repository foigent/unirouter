package gateway

import (
	"reflect"
	"testing"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

// Replays the capability-bearing fields read from all eight failed C request bodies.
// Message text and tool schemas do not affect these two capability selectors.
func TestCC15FailedOpenClawShapeCapabilityReplay(t *testing.T) {
	body := []byte(`{"model":"company-chat","messages":[{"role":"system","content":"synthetic"},{"role":"user","content":"synthetic"}],"stream":true,"stream_options":{"include_usage":true},"tools":[{"type":"function","function":{"name":"edit"}},{"type":"function","function":{"name":"read"}}],"tool_choice":"auto","max_tokens":1024}`)
	got, err := requiredCapabilitiesForChat(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"streaming", "tools"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("raw requirements = %v, want %v", got, want)
	}
	profile, ok := modelprofile.Lookup(modelprofile.UnverifiedProfileID, modelprofile.UnverifiedProfileVersion)
	if !ok {
		t.Fatal("unverified profile missing")
	}
	requirements, err := invocationCapabilityRequirementsForChat(profile, modelExecutionProfileEvidence{}, body)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		ids = append(ids, requirement.CapabilityID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("invocation requirements = %v, want %v", ids, want)
	}
}
