package accessscope

import (
	"reflect"
	"testing"
)

func TestProjectDefaultEndpointContract(t *testing.T) {
	want := []string{"/v1/chat/completions", "/v1/embeddings", "/v1/models"}
	if got := ProjectDefaultEndpoints(); !reflect.DeepEqual(got, want) {
		t.Fatalf("project default endpoints = %v, want %v", got, want)
	}
	for _, denied := range []string{
		"/v1/agent-capabilities/exchange",
		"/v1/agent-capabilities/delegate",
		"/v1/agent-capabilities/refresh",
		"/v1/agent-capabilities/revoke",
	} {
		if IsSupportedEndpoint(denied) {
			t.Fatalf("unsupported endpoint %q entered grant vocabulary", denied)
		}
	}
	if !IsSupportedEndpoint("/v1/media/jobs") || IsProjectDefaultEndpoint("/v1/media/jobs") {
		t.Fatal("media must be supported only as an explicitly reviewed endpoint family")
	}
}

func TestProjectDefaultRateContract(t *testing.T) {
	if ProjectDefaultRPMLimit != 60 || ProjectDefaultRPSLimit != 10 {
		t.Fatalf("project default rates = %d RPM/%d RPS", ProjectDefaultRPMLimit, ProjectDefaultRPSLimit)
	}
}

func TestValidateRequiresExplicitSupportedScope(t *testing.T) {
	if !Validate(ModeProjectDefault, nil, nil) {
		t.Fatal("empty project-default scope must be valid")
	}
	if Validate(ModeProjectDefault, []string{"model"}, nil) {
		t.Fatal("project-default scope must not persist an implicit model list")
	}
	if !Validate(ModeExplicit, []string{"model"}, []string{"/v1/chat/completions"}) {
		t.Fatal("explicit supported scope must be valid")
	}
	if !Validate(ModeExplicit, []string{"model"}, []string{"/v1/media/jobs"}) {
		t.Fatal("explicit media endpoint family must be supported")
	}
	for _, endpoint := range []string{"/v1/agent-capabilities/exchange", "/unknown"} {
		if Validate(ModeExplicit, []string{"model"}, []string{endpoint}) {
			t.Fatalf("explicit unsupported endpoint %q must fail closed", endpoint)
		}
	}
}

func TestCanonicalExecutionEndpointStrictMediaFamily(t *testing.T) {
	jobID := "mediajob_0123456789abcdef0123456789abcdef"
	for _, path := range []string{
		"/v1/media/jobs",
		"/v1/media/jobs/" + jobID,
		"/v1/media/jobs/" + jobID + "/cancel",
		"/v1/media/jobs/" + jobID + "/content",
	} {
		if got := CanonicalExecutionEndpoint(path); got != "/v1/media/jobs" {
			t.Fatalf("CanonicalExecutionEndpoint(%q) = %q", path, got)
		}
	}
	for _, path := range []string{
		"/v1/media/jobs/not-a-job",
		"/v1/media/jobs/" + jobID + "/delete",
		"/v1/media/jobs/" + jobID + "/content/extra",
		"/v1/media/jobs/" + jobID + "/",
		"/v1/media/jobs-extra/" + jobID,
	} {
		if got := CanonicalExecutionEndpoint(path); got != path {
			t.Fatalf("invalid path %q canonicalized to %q", path, got)
		}
	}
}
