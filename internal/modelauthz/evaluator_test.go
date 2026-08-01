package modelauthz

import (
	"testing"
	"time"
)

func TestNormalizeDraftRejectsWideningOrAmbiguousShapes(t *testing.T) {
	valid := Draft{
		AuthorizationRef: "mca_project_chat",
		SubjectType:      SubjectProject, SubjectRef: "42", Environment: "production",
		Effect: EffectAllow, AllowedModels: []string{"company-chat", "company-chat"},
		AllowedEndpoints: []string{"/v1/chat/completions"}, Reason: "approved enterprise assistant",
	}
	normalized, err := NormalizeDraft(valid)
	if err != nil {
		t.Fatalf("NormalizeDraft() error = %v", err)
	}
	if len(normalized.AllowedModels) != 1 || normalized.AllowedModels[0] != "company-chat" {
		t.Fatalf("models not normalized: %#v", normalized.AllowedModels)
	}
	for _, mutate := range []func(*Draft){
		func(value *Draft) { value.AuthorizationRef = "bad" },
		func(value *Draft) { value.SubjectRef = "042" },
		func(value *Draft) { value.SubjectType = "tool" },
		func(value *Draft) { value.AllowedModels = nil },
		func(value *Draft) { value.AllowedEndpoints = []string{"/v1/tool/call"} },
		func(value *Draft) { value.Effect = "grant" },
		func(value *Draft) { value.Environment = "Prod US" },
		func(value *Draft) { value.Reason = "" },
	} {
		candidate := valid
		mutate(&candidate)
		if _, err := NormalizeDraft(candidate); err == nil {
			t.Fatalf("invalid draft accepted: %#v", candidate)
		}
	}
}

func TestEvaluatePoliciesIntersectsAllowScopesAndDenyWins(t *testing.T) {
	input := Input{
		OrgID: 7, APIKeyID: 11, ProjectID: 22, OwnerUserID: 33,
		AgentID: "agent:research", Environment: "production",
		Endpoint: "/v1/chat/completions", RequestedModel: "company-chat", ResolvedModel: "gpt-4o",
		Now: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
	policy := func(ref, subjectType, subjectRef, effect string, models ...string) Policy {
		return Policy{
			AuthorizationRef: ref, SubjectType: subjectType, SubjectRef: subjectRef,
			Effect: effect, AllowedModels: models,
			AllowedEndpoints: []string{input.Endpoint}, Status: StatusActive,
		}
	}

	if decision := EvaluatePolicies(input, nil); !decision.Allowed || decision.Reason != "legacy_authority_unchanged" {
		t.Fatalf("default compatibility changed: %#v", decision)
	}
	decision := EvaluatePolicies(input, []Policy{
		policy("mca_key_allow", SubjectAPIKey, "11", EffectAllow, "company-chat"),
		policy("mca_project_allow", SubjectProject, "22", EffectAllow, "gpt-4o"),
	})
	if !decision.Allowed || decision.Reason != "explicit_allow_intersection" || len(decision.PolicyRefs) != 2 {
		t.Fatalf("intersection should allow: %#v", decision)
	}
	decision = EvaluatePolicies(input, []Policy{
		policy("mca_key_allow", SubjectAPIKey, "11", EffectAllow, "company-chat"),
		policy("mca_project_other", SubjectProject, "22", EffectAllow, "other-model"),
	})
	if decision.Allowed || decision.Reason != "allow_scope_excludes_model" {
		t.Fatalf("restrictive group did not deny: %#v", decision)
	}
	decision = EvaluatePolicies(input, []Policy{
		policy("mca_allow", SubjectAPIKey, "11", EffectAllow, "*"),
		policy("mca_deny", SubjectAgent, "agent:research", EffectDeny, "gpt-4o"),
	})
	if decision.Allowed || decision.Reason != "explicit_deny" || len(decision.PolicyRefs) != 1 || decision.PolicyRefs[0] != "mca_deny" {
		t.Fatalf("deny did not win: %#v", decision)
	}
}

func TestEvaluatePoliciesOnlyAppliesEndpointSpecificRules(t *testing.T) {
	input := Input{Environment: "production", Endpoint: "/v1/embeddings", RequestedModel: "embed-v1"}
	decision := EvaluatePolicies(input, []Policy{{
		AuthorizationRef: "mca_chat_only", SubjectType: SubjectProject, SubjectRef: "2",
		Effect: EffectAllow, AllowedModels: []string{"chat-v1"},
		AllowedEndpoints: []string{"/v1/chat/completions"}, Status: StatusActive,
	}})
	if !decision.Allowed || len(decision.PolicyRefs) != 0 {
		t.Fatalf("unrelated endpoint changed authority: %#v", decision)
	}
}

func TestEvaluatePoliciesAllowRulesCannotGrantPastExplicitDeny(t *testing.T) {
	input := Input{Environment: "production", Endpoint: "/v1/models", RequestedModel: "model-a", ResolvedModel: "model-a"}
	policies := []Policy{
		{AuthorizationRef: "mca_project_allow", SubjectType: SubjectProject, SubjectRef: "2", Effect: EffectAllow, AllowedModels: []string{"*"}, AllowedEndpoints: []string{"/v1/models"}, Status: StatusActive},
		{AuthorizationRef: "mca_key_deny", SubjectType: SubjectAPIKey, SubjectRef: "3", Effect: EffectDeny, AllowedModels: []string{"model-a"}, AllowedEndpoints: []string{"/v1/models"}, Status: StatusActive},
	}
	decision := EvaluatePolicies(input, policies)
	if decision.Allowed || decision.Reason != "explicit_deny" {
		t.Fatalf("deny was widened by an allow rule: %#v", decision)
	}
}
