package workloadapp

import (
	"reflect"
	"strings"
	"testing"
)

func TestClientProfilesAreStableAndDefensivelyCopied(t *testing.T) {
	first := ClientProfiles()
	second := ClientProfiles()
	if len(first) != 9 || !reflect.DeepEqual(first, second) {
		t.Fatalf("client profile catalog is not stable: first=%d second=%d", len(first), len(second))
	}
	first[0].SupportedEndpoints[0] = "/tampered"
	third := ClientProfiles()
	if third[0].SupportedEndpoints[0] != "/v1/chat/completions" {
		t.Fatal("client profile catalog leaked mutable endpoint storage")
	}
	responses, ok := LookupClientProfile("client.openai-agents.responses.v1")
	if !ok || responses.RuntimeState != RuntimeContractOnly || responses.EvidenceLevel != EvidenceUnsupported {
		t.Fatalf("Responses profile overclaims runtime support: %+v", responses)
	}
}

func TestLegacyWorkloadIdentityAdapterIsDeterministicAndRejectsAmbiguity(t *testing.T) {
	short, err := DeriveV2LogicalRef("app_customer_service_agent")
	if err != nil || short != "workload_customer_service_agent" {
		t.Fatalf("short derived ref=%q err=%v", short, err)
	}
	longRef := "app_" + strings.Repeat("a", 60)
	first, err := DeriveV2LogicalRef(longRef)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeriveV2LogicalRef(longRef)
	if err != nil || first != second || len(first) > 64 {
		t.Fatalf("long derived refs=%q %q err=%v", first, second, err)
	}
	for input, want := range map[string]string{
		"batch_job": KindWorkflow, "agent_runtime": KindAgent,
		"service_integration": KindIntegrationAdapter,
	} {
		got, adapted, err := NormalizeCompatibleV2Kind(input)
		if err != nil || !adapted || got != want {
			t.Fatalf("kind %q => %q adapted=%v err=%v", input, got, adapted, err)
		}
	}
	for _, input := range []string{KindInteractiveClient, IdentityPersonalDevelopment, "unknown"} {
		if _, _, err := NormalizeCompatibleV2Kind(input); err == nil {
			t.Fatalf("ambiguous legacy kind %q accepted", input)
		}
	}
}

func TestNormalizeDraft(t *testing.T) {
	draft, err := NormalizeDraft(Draft{
		ApplicationRef: " APP_CLAIMS_AGENT ", DisplayName: " Claims Agent ", Kind: KindAgent,
		Environment: " POC_2 ", IdentityMode: IdentityServiceCredential,
		ClientProfile: " CLIENT.LANGCHAIN-OPENAI.CHAT.V1 ", OwnerTeam: "AI Platform",
	})
	if err != nil {
		t.Fatalf("NormalizeDraft failed: %v", err)
	}
	if draft.ApplicationRef != "app_claims_agent" || draft.Environment != "poc_2" || draft.Status != StatusActive {
		t.Fatalf("unexpected normalized draft: %+v", draft)
	}
}

func TestNormalizeDraftRejectsAuthorityAndProfileConflicts(t *testing.T) {
	tests := []Draft{
		{ApplicationRef: "app_x", DisplayName: "X", Kind: KindAgent, Environment: "poc", IdentityMode: "tool_authority", ClientProfile: "client.openai-sdk.chat.v1"},
		{ApplicationRef: "app_x", DisplayName: "X", Kind: KindIntegrationAdapter, Environment: "poc", IdentityMode: IdentityPersonalDevelopment, ClientProfile: "client.feishu-bot.adapter.v1"},
		{ApplicationRef: "app_x", DisplayName: "X", Kind: KindAgent, Environment: "poc", IdentityMode: IdentityServiceCredential, ClientProfile: "client.unknown.v1"},
	}
	for _, test := range tests {
		if _, err := NormalizeDraft(test); err == nil {
			t.Fatalf("expected invalid draft: %+v", test)
		}
	}
}

func TestNormalizeDraftKeepsApplicationIdentityWithinAuditResourceBoundary(t *testing.T) {
	base := Draft{
		DisplayName: "Boundary", Kind: KindAgent, Environment: "poc",
		IdentityMode: IdentityServiceCredential, ClientProfile: "client.openai-sdk.chat.v1",
	}
	base.ApplicationRef = "app_" + strings.Repeat("a", 60)
	if _, err := NormalizeDraft(base); err != nil {
		t.Fatalf("64-byte application_ref should be valid: %v", err)
	}
	base.ApplicationRef = "app_" + strings.Repeat("a", 61)
	if _, err := NormalizeDraft(base); err == nil {
		t.Fatal("application_ref longer than audit resource boundary should be rejected")
	}
}

func TestResolveReadinessExplainsCredentialChannelContradiction(t *testing.T) {
	profile, _ := LookupClientProfile("client.openai-sdk.chat.v1")
	result := ResolveReadiness(ReadinessInput{
		ApplicationStatus: StatusActive, ClientProfile: profile,
		Endpoint: "/v1/chat/completions", Model: "kimi-k2.6", ProjectActive: true,
		ActiveCredentialCount: 1, CredentialAllowsModel: true,
		AuthorizationState: "legacy_authority", LimitState: "builtin", ModelGovernanceState: "ready",
		RoutingPreviewAvailable: true, RoutingCandidateCount: 0,
	})
	if result.Overall != OverallBlocked {
		t.Fatalf("overall=%s want blocked", result.Overall)
	}
	axis := findAxis(t, result, "upstream_route")
	if axis.State != AxisBlocked || axis.ReasonCode != "no_eligible_upstream_route" {
		t.Fatalf("routing axis=%+v", axis)
	}
}

func TestResolveReadinessRequiresModelContextWithoutInventingFailure(t *testing.T) {
	profile, _ := LookupClientProfile("client.openai-sdk.chat.v1")
	result := ResolveReadiness(ReadinessInput{
		ApplicationStatus: StatusActive, ClientProfile: profile,
		Endpoint: "/v1/chat/completions", ProjectActive: true, ActiveCredentialCount: 1,
	})
	if result.Overall != OverallNeedsInput {
		t.Fatalf("overall=%s want needs_input", result.Overall)
	}
	if findAxis(t, result, "upstream_route").State != AxisContextRequired {
		t.Fatal("missing model was incorrectly presented as an upstream failure")
	}
}

func TestResolveReadinessBlocksContractOnlyResponses(t *testing.T) {
	profile, _ := LookupClientProfile("client.openai-agents.responses.v1")
	result := ResolveReadiness(ReadinessInput{
		ApplicationStatus: StatusActive, ClientProfile: profile,
		Endpoint: "/v1/responses", Model: "company-chat", ProjectActive: true,
		ActiveCredentialCount: 1, CredentialAllowsModel: true,
		AuthorizationState: "legacy_authority", LimitState: "builtin", ModelGovernanceState: "ready",
		RoutingPreviewAvailable: true, RoutingCandidateCount: 1,
	})
	axis := findAxis(t, result, "client_profile")
	if result.Overall != OverallBlocked || axis.ReasonCode != "endpoint_contract_only" {
		t.Fatalf("Responses runtime was overclaimed: %+v", result)
	}
}

func TestResolveReadinessTreatsMissingV2ClientProfileAsOptionalContext(t *testing.T) {
	optional := ResolveReadiness(ReadinessInput{
		ApplicationStatus: StatusActive, ClientProfileOptional: true,
		ProjectActive: true, ActiveCredentialCount: 1,
	})
	axis := findAxis(t, optional, "client_profile")
	if axis.State != AxisNotApplicable || axis.ReasonCode != "client_profile_not_configured_optional" {
		t.Fatalf("optional profile axis=%+v", axis)
	}

	selection := ResolveReadiness(ReadinessInput{
		ApplicationStatus: StatusActive, ClientProfileOptional: true, ClientProfileSelectionRequired: true,
		ProjectActive: true, ActiveCredentialCount: 1,
	})
	axis = findAxis(t, selection, "client_profile")
	if axis.State != AxisContextRequired || axis.ReasonCode != "client_profile_selection_optional" {
		t.Fatalf("multi-profile context axis=%+v", axis)
	}

	legacy := ResolveReadiness(ReadinessInput{ApplicationStatus: StatusActive, ProjectActive: true})
	axis = findAxis(t, legacy, "client_profile")
	if axis.State != AxisBlocked || axis.ReasonCode != "client_profile_unknown" {
		t.Fatalf("legacy missing profile axis=%+v", axis)
	}
}

func findAxis(t *testing.T, result ReadinessResult, key string) Axis {
	t.Helper()
	for _, axis := range result.Axes {
		if axis.Key == key {
			return axis
		}
	}
	t.Fatalf("axis %s not found", key)
	return Axis{}
}
