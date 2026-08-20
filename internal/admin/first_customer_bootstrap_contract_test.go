package admin

import "testing"

func TestFreshBootstrapReleaseEvidenceEnvironmentAliases(t *testing.T) {
	tests := []struct {
		environment string
		wantRelease bool
		wantError   bool
	}{
		{environment: "", wantRelease: false},
		{environment: "local", wantRelease: false},
		{environment: "dev", wantRelease: false},
		{environment: "development", wantRelease: false},
		{environment: "test", wantRelease: false},
		{environment: "staging", wantRelease: true},
		{environment: "prod", wantRelease: true},
		{environment: "production", wantRelease: true},
		{environment: "unknown", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.environment, func(t *testing.T) {
			got, err := freshBootstrapRequiresRecordedRelease(test.environment)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError=%v", err, test.wantError)
			}
			if got != test.wantRelease {
				t.Fatalf("recorded release = %v, want %v", got, test.wantRelease)
			}
		})
	}
}
