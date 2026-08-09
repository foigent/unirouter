package accessgrantboundary

import (
	"testing"

	"github.com/uniroute/unirouter/internal/models"
)

func TestExecutionResolverAcceptsOnlyExactMediaDispatchEndpoint(t *testing.T) {
	if !validMediaDispatchEndpoint("/v1/media/jobs") {
		t.Fatal("exact media submission endpoint must be accepted")
	}
	jobID := "mediajob_0123456789abcdef0123456789abcdef"
	for _, endpoint := range []string{
		"/v1/media/jobs/" + jobID,
		"/v1/media/jobs/" + jobID + "/cancel",
		"/v1/media/jobs/" + jobID + "/content",
		"/v1/media/jobs/not-a-job",
		"/v1/media/jobs/" + jobID + "/delete",
		"/v1/media/jobs/",
		"/v1/media/jobs-extra",
	} {
		if validMediaDispatchEndpoint(endpoint) {
			t.Fatalf("execution resolver accepted non-dispatch endpoint %q", endpoint)
		}
	}
}

func TestSnapshotAllowsExecutionUsesEffectiveCredentialAndGrantIntersection(t *testing.T) {
	tests := []struct {
		name      string
		snapshot  Snapshot
		requested string
		resolved  string
		endpoint  string
		want      bool
	}{
		{
			name:      "requested model",
			snapshot:  Snapshot{APIKey: models.ApiKey{AllowedModels: "alias", AllowedEndpoints: "/v1/chat/completions"}},
			requested: "alias", resolved: "model", endpoint: "/v1/chat/completions", want: true,
		},
		{
			name:      "resolved model",
			snapshot:  Snapshot{APIKey: models.ApiKey{AllowedModels: "model", AllowedEndpoints: "/v1/chat/completions"}},
			requested: "alias", resolved: "model", endpoint: "/v1/chat/completions", want: true,
		},
		{
			name:      "model excluded",
			snapshot:  Snapshot{APIKey: models.ApiKey{AllowedModels: "other", AllowedEndpoints: "/v1/chat/completions"}},
			requested: "alias", resolved: "model", endpoint: "/v1/chat/completions", want: false,
		},
		{
			name:      "endpoint excluded",
			snapshot:  Snapshot{APIKey: models.ApiKey{AllowedModels: "*", AllowedEndpoints: "/v1/embeddings"}},
			requested: "model", resolved: "model", endpoint: "/v1/chat/completions", want: false,
		},
		{
			name:      "materialized empty intersection",
			snapshot:  Snapshot{APIKey: models.ApiKey{AllowedModels: "*", AllowedEndpoints: "/v1/chat/completions", ModelScopeDenied: true}},
			requested: "model", resolved: "model", endpoint: "/v1/chat/completions", want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.snapshot.AllowsExecution(test.requested, test.resolved, test.endpoint); got != test.want {
				t.Fatalf("allowed=%v want=%v", got, test.want)
			}
		})
	}
}
