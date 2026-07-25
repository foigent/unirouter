package admin

import (
	"testing"

	"github.com/uniroute/unirouter/internal/middleware"
)

func TestValidateCompletedFeedKeyShape(t *testing.T) {
	tests := []struct {
		name          string
		purpose       string
		keyType       string
		projectID     int64
		allowedIPs    string
		allowedModels string
		valid         bool
	}{
		{
			name: "exact IPv4 service binding", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "service", projectID: 7, allowedIPs: "203.0.113.10/32", valid: true,
		},
		{
			name: "exact IPv6 service binding", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "service", projectID: 7, allowedIPs: "2001:db8::10/128", valid: true,
		},
		{
			name: "ordinary key unchanged", purpose: "", keyType: "personal",
			projectID: 7, allowedModels: "model-a", valid: true,
		},
		{
			name: "personal purpose key", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "personal", projectID: 7, allowedIPs: "203.0.113.10/32",
		},
		{
			name: "org scoped", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "service", allowedIPs: "203.0.113.10/32",
		},
		{
			name: "no network binding", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "service", projectID: 7,
		},
		{
			name: "global network binding", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "service", projectID: 7, allowedIPs: "0.0.0.0/0",
		},
		{
			name: "model allowance", purpose: middleware.APIKeyPurposeXInsightFeedRead,
			keyType: "service", projectID: 7, allowedIPs: "203.0.113.10/32", allowedModels: "model-a",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errText := validateCompletedFeedKeyShape(
				test.purpose,
				test.keyType,
				test.projectID,
				test.allowedIPs,
				test.allowedModels,
			)
			if test.valid && errText != "" {
				t.Fatalf("expected valid feed credential shape, got %q", errText)
			}
			if !test.valid && errText == "" {
				t.Fatal("expected invalid feed credential shape to fail")
			}
		})
	}
}

func TestFeedActiveCompatibilityStatusIsExternallyActiveOnlyForFeedPurpose(t *testing.T) {
	if got := apiKeyDisplayStatus(
		middleware.APIKeyStatusFeedActive,
		middleware.APIKeyPurposeXInsightFeedRead,
	); got != "active" {
		t.Fatalf("feed purpose status=%q, want active", got)
	}
	if got := apiKeyDisplayStatus(middleware.APIKeyStatusFeedActive, ""); got != middleware.APIKeyStatusFeedActive {
		t.Fatalf("ordinary key unexpectedly remapped compatibility status: %q", got)
	}
}
