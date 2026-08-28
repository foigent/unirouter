package deploymentprofile

import (
	"reflect"
	"testing"
)

func TestNormalizeEndpointEnforcesPublicHTTPSAndNoSecretMaterial(t *testing.T) {
	valid := EndpointDraft{EndpointRef: "gwe_ecs_poc", DisplayName: "ECS POC", BaseURL: "https://gateway.example.com", Visibility: "public", TLSMode: "external_termination", CertificateRef: "secret://tls/gateway", EndpointProfile: EndpointOpenAIChat, Status: "active"}
	if _, err := NormalizeEndpoint(valid); err != nil {
		t.Fatalf("valid endpoint: %v", err)
	}
	for _, profileID := range []string{EndpointOpenAIChat, EndpointOpenAIResponses, EndpointAnthropicMessages} {
		candidate := valid
		candidate.EndpointProfile = profileID
		if _, err := NormalizeEndpoint(candidate); err != nil {
			t.Fatalf("supported endpoint profile %q: %v", profileID, err)
		}
	}
	invalid := valid
	invalid.BaseURL = "http://gateway.example.com"
	if _, err := NormalizeEndpoint(invalid); err == nil {
		t.Fatal("public HTTP endpoint must fail closed")
	}
	invalid = valid
	invalid.CertificateRef = "-----BEGIN PRIVATE KEY-----"
	if _, err := NormalizeEndpoint(invalid); err == nil {
		t.Fatal("secret material must not enter endpoint metadata")
	}
	invalid = valid
	invalid.EndpointProfile = "endpoint.unknown.v1"
	if _, err := NormalizeEndpoint(invalid); err == nil {
		t.Fatal("unknown endpoint profile must fail closed")
	}
}

func TestEndpointProfilesAreStableAndDefensivelyCopied(t *testing.T) {
	first := EndpointProfiles()
	second := EndpointProfiles()
	if len(first) != 3 || !reflect.DeepEqual(first, second) {
		t.Fatalf("endpoint profile catalog is not stable: first=%#v second=%#v", first, second)
	}
	want := []EndpointProfile{
		{ProfileID: EndpointOpenAIChat, DisplayName: "OpenAI Chat Completions", Paths: []string{"/v1/chat/completions", "/v1/models"}, RuntimeState: "runtime_supported", EvidenceLevel: "verified", Streaming: true},
		{ProfileID: EndpointOpenAIResponses, DisplayName: "OpenAI Responses (strict text; exact stream subset)", Paths: []string{"/v1/responses"}, RuntimeState: "runtime_supported", EvidenceLevel: EvidenceRepositoryRuntimeVerified, Streaming: true},
		{ProfileID: EndpointAnthropicMessages, DisplayName: "Anthropic Messages (strict non-stream text)", Paths: []string{"/v1/messages"}, RuntimeState: "runtime_supported", EvidenceLevel: EvidenceRepositoryRuntimeVerified, Streaming: false},
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("endpoint profile catalog drifted: got=%#v want=%#v", first, want)
	}
	first[1].Paths[0] = "/tampered"
	if third := EndpointProfiles(); third[1].Paths[0] != "/v1/responses" {
		t.Fatalf("endpoint profile catalog leaked mutable path storage: %#v", third)
	}
}
