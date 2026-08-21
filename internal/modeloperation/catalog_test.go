package modeloperation

import (
	"net/http"
	"reflect"
	"testing"
)

func TestCatalogContracts(t *testing.T) {
	tests := []struct {
		id                    ID
		method                string
		path                  string
		paid                  bool
		projectDefault        bool
		requiresExplicitScope bool
		legacyUnboundAllowed  bool
		agentDelegable        bool
		errorEnvelope         ErrorEnvelope
	}{
		{IDChatCompletions, http.MethodPost, PathChatCompletions, true, true, false, true, true, ErrorEnvelopeOpenAIV1},
		{IDEmbeddings, http.MethodPost, PathEmbeddings, true, true, false, true, true, ErrorEnvelopeOpenAIV1},
		{IDResponses, http.MethodPost, PathResponses, true, false, true, false, true, ErrorEnvelopeOpenAIV1},
		{IDMessages, http.MethodPost, PathMessages, true, false, true, false, true, ErrorEnvelopeAnthropicV1},
		{IDModelsList, http.MethodGet, PathModels, false, true, false, true, true, ErrorEnvelopeOpenAIV1},
		{IDMediaJobs, http.MethodPost, PathMediaJobs, true, false, true, true, false, ErrorEnvelopeOpenAIV1},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		definition, ok := Lookup(test.path)
		if !ok {
			t.Fatalf("operation %q missing", test.path)
		}
		if _, duplicate := seen[definition.Path]; duplicate {
			t.Fatalf("duplicate operation path %q", definition.Path)
		}
		seen[definition.Path] = struct{}{}
		if definition.ID != test.id || definition.Method != test.method ||
			definition.ProtocolFamily == "" || definition.ProtocolVersion == "" ||
			definition.Paid != test.paid || definition.ProjectDefault != test.projectDefault ||
			definition.RequiresExplicitScope != test.requiresExplicitScope ||
			definition.LegacyUnboundAllowed != test.legacyUnboundAllowed ||
			definition.AgentDelegable != test.agentDelegable ||
			definition.ErrorEnvelope != test.errorEnvelope {
			t.Fatalf("operation %q = %#v", test.path, definition)
		}
		if byID, ok := LookupID(test.id); !ok || byID.Path != test.path {
			t.Fatalf("operation id %q did not resolve: %#v", test.id, byID)
		}
		if got := IsPaidSubmission(test.method, test.path); got != test.paid {
			t.Fatalf("paid(%q) = %v, want %v", test.path, got, test.paid)
		}
		if IsPaidSubmission(http.MethodDelete, test.path) {
			t.Fatalf("wrong method became paid for %q", test.path)
		}
	}
	if len(All()) != len(tests) {
		t.Fatalf("catalog contains %d definitions, want %d", len(All()), len(tests))
	}
	if _, ok := Lookup("/v1/unknown"); ok || IsPaidSubmission(http.MethodPost, "/v1/unknown") {
		t.Fatal("unknown operation entered the catalog")
	}
}

func TestAgentCapabilityPathsAreDerivedAndDefensive(t *testing.T) {
	want := []string{
		PathChatCompletions,
		PathEmbeddings,
		PathResponses,
		PathMessages,
		PathModels,
	}
	got := AgentCapabilityPaths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Agent capability paths = %v, want %v", got, want)
	}
	got[0] = "/mutated"
	if next := AgentCapabilityPaths(); !reflect.DeepEqual(next, want) {
		t.Fatalf("caller mutated Agent capability paths: %v", next)
	}
	if definition, _ := Lookup(PathMediaJobs); definition.AgentDelegable {
		t.Fatal("asynchronous media unexpectedly became Agent-delegable")
	}
}

func TestProjectDefaultPathsAreDerivedAndDefensive(t *testing.T) {
	want := []string{PathChatCompletions, PathEmbeddings, PathModels}
	got := ProjectDefaultPaths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project defaults = %v, want %v", got, want)
	}
	got[0] = "/mutated"
	if next := ProjectDefaultPaths(); !reflect.DeepEqual(next, want) {
		t.Fatalf("caller mutated catalog defaults: %v", next)
	}

	all := All()
	all[0].Path = "/mutated"
	if definition, _ := Lookup(PathChatCompletions); definition.Path != PathChatCompletions {
		t.Fatal("caller mutated catalog definitions")
	}
}

func TestCanonicalPathKeepsDynamicMediaAuthorizationStrict(t *testing.T) {
	jobID := "mediajob_0123456789abcdef0123456789abcdef"
	for _, path := range []string{
		PathMediaJobs,
		PathMediaJobs + "/" + jobID,
		PathMediaJobs + "/" + jobID + "/cancel",
		PathMediaJobs + "/" + jobID + "/content",
	} {
		if got := CanonicalPath(path); got != PathMediaJobs {
			t.Fatalf("CanonicalPath(%q) = %q", path, got)
		}
	}
	for _, path := range []string{
		PathMediaJobs + "/not-a-job",
		PathMediaJobs + "/" + jobID + "/delete",
		PathMediaJobs + "/" + jobID + "/content/extra",
		PathMediaJobs + "/" + jobID + "/",
		"/v1/media/jobs-extra/" + jobID,
	} {
		if got := CanonicalPath(path); got != path {
			t.Fatalf("invalid path %q canonicalized to %q", path, got)
		}
	}
	if IsPaidSubmission(http.MethodGet, PathMediaJobs+"/"+jobID) {
		t.Fatal("dynamic media read became a paid submission")
	}
}
