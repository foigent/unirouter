package accessgrantboundary

import "testing"

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
