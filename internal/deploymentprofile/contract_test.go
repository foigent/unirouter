package deploymentprofile

import "testing"

func TestNormalizeEndpointEnforcesPublicHTTPSAndNoSecretMaterial(t *testing.T) {
	valid := EndpointDraft{EndpointRef: "gwe_ecs_poc", DisplayName: "ECS POC", BaseURL: "https://gateway.example.com", Visibility: "public", TLSMode: "external_termination", CertificateRef: "secret://tls/gateway", EndpointProfile: EndpointOpenAIChat, Status: "active"}
	if _, err := NormalizeEndpoint(valid); err != nil {
		t.Fatalf("valid endpoint: %v", err)
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
}

func TestResponsesEndpointRemainsContractOnly(t *testing.T) {
	profiles := EndpointProfiles()
	if len(profiles) != 2 || profiles[1].ProfileID != EndpointOpenAIResponses || profiles[1].RuntimeState != "contract_only" || profiles[1].EvidenceLevel != "unsupported" {
		t.Fatalf("responses support claim drifted: %#v", profiles)
	}
}
