package modelrouting

import (
	"errors"
	"strings"
	"testing"
)

// Uses the exact stream/tools capability requirements derived by the gateway
// for the failed OpenClaw envelope and the observed empty current capability map.
func TestCC15EmptyCapabilityMapCannotRouteStreamTools(t *testing.T) {
	value := canonicalFixture()
	for index := range value.Canonical {
		value.Canonical[index].Capabilities = nil
	}
	request := requestFor(ModeCanonical, "cc15-stream-tools")
	request.InvocationCapabilities = invocationRequirements("streaming", "tools")
	plan, err := planFixture(t, value, request)
	if !errors.Is(err, ErrNoRoute) || len(plan.Routes) != 0 {
		t.Fatalf("plan=%#v error=%v", plan, err)
	}
	found := false
	for _, exclusion := range plan.Exclusions {
		if strings.HasPrefix(exclusion.Reason, "unsupported_capability:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing capability exclusion: %#v", plan.Exclusions)
	}
}
