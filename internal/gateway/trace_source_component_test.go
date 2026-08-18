package gateway

import "testing"

func TestSourceComponentForTraceEventUsesEventAuthority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType string
		stage     string
		want      string
	}{
		{name: "limit policy admission", eventType: "limit_policy.admission", stage: "admission", want: "limit_policy"},
		{name: "future limit policy event", eventType: "limit_policy.queue", stage: "admission", want: "limit_policy"},
		{name: "known stage fallback", eventType: "billing.freeze.succeeded", stage: "billing", want: "billing"},
		{name: "unknown admission is not mislabeled", eventType: "future.admission", stage: "admission", want: "unknown"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sourceComponentForTraceEvent(test.eventType, test.stage); got != test.want {
				t.Fatalf("sourceComponentForTraceEvent(%q, %q) = %q, want %q", test.eventType, test.stage, got, test.want)
			}
		})
	}
}
