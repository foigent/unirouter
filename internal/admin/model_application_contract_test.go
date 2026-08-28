package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalModelApplicationViewerProjectionRemovesCredentialIdentities(t *testing.T) {
	bindingID, testedBy := int64(71), int64(81)
	fixture := func() canonicalModelApplicationProjection {
		return canonicalModelApplicationProjection{Applications: []canonicalModelApplication{{
			Credential: canonicalCredentialAxis{
				State: "ready", BoundCount: 2, EligibleCount: 1,
				Bindings: []canonicalCredentialBindingRef{{ID: bindingID}},
			},
			Operations: []canonicalOperationAxis{{LatestProbe: &canonicalOperationProbeView{
				ProbeID: 91, CredentialBindingID: &bindingID, TestedBy: &testedBy,
			}}},
		}}}
	}

	viewer := fixture()
	viewerContext := context.WithValue(context.Background(), middleware.CtxOrganizationRole, auth.RoleOrgViewer)
	redactCanonicalModelApplicationViewerProjection(viewerContext, &viewer)
	if len(viewer.Applications[0].Credential.Bindings) != 0 ||
		viewer.Applications[0].Credential.BoundCount != 2 ||
		viewer.Applications[0].Credential.EligibleCount != 1 {
		t.Fatalf("viewer credential aggregate/redaction mismatch: %+v", viewer.Applications[0].Credential)
	}
	probe := viewer.Applications[0].Operations[0].LatestProbe
	if probe == nil || probe.ProbeID != 91 || probe.CredentialBindingID != nil || probe.TestedBy != nil {
		t.Fatalf("viewer probe identity redaction mismatch: %+v", probe)
	}

	admin := fixture()
	adminContext := context.WithValue(context.Background(), middleware.CtxOrganizationRole, auth.RoleOrgAdmin)
	redactCanonicalModelApplicationViewerProjection(adminContext, &admin)
	if len(admin.Applications[0].Credential.Bindings) != 1 ||
		admin.Applications[0].Operations[0].LatestProbe.CredentialBindingID == nil ||
		admin.Applications[0].Operations[0].LatestProbe.TestedBy == nil {
		t.Fatalf("admin projection was unexpectedly redacted: %+v", admin.Applications[0])
	}
}

func TestNormalizeCanonicalApplicationCreatePinsReviewedProfileDefaults(t *testing.T) {
	t.Parallel()
	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "DeepSeek official", Provider: "deepseek", BaseURL: "https://api.deepseek.com",
			AdapterType: adapters.TypeOpenAICompatible,
		},
		PublicModel: canonicalPublicModelInput{
			ModelName: "deepseek-v4-flash", ExecutionProfileID: "deepseek.v4-flash.api", ExecutionProfileVersion: "1.2.0",
		},
		Deployment: canonicalDeploymentInput{UpstreamModel: "deepseek-v4-flash"},
		Credential: canonicalCredentialInput{APIKey: "profile-secret"},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}

	normalized, err := normalizeCanonicalApplicationCreate(request)
	if err != nil {
		t.Fatalf("normalize reviewed profile: %v", err)
	}
	if normalized.PublicModel.ContextLength == nil || *normalized.PublicModel.ContextLength != 1_000_000 ||
		normalized.PublicModel.OutputLimit == nil || *normalized.PublicModel.OutputLimit != 384_000 ||
		normalized.Deployment.MaxContext == nil || *normalized.Deployment.MaxContext != 1_000_000 ||
		normalized.Deployment.DefaultMaxOutput == nil || *normalized.Deployment.DefaultMaxOutput != 32_768 {
		t.Fatalf("reviewed defaults were not applied: %+v", normalized.PublicModel)
	}

	grok := request
	grok.Connection = canonicalConnectionInput{
		Name: "xAI official", Provider: "xai", BaseURL: "https://api.x.ai/v1",
		AdapterType:   adapters.TypeOpenAICompatible,
		AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
	}
	grok.PublicModel = canonicalPublicModelInput{
		ModelName: "grok-4.6", ExecutionProfileID: "xai.grok-4.6.api", ExecutionProfileVersion: "1.1.0",
	}
	grok.Deployment = canonicalDeploymentInput{UpstreamModel: "grok-4.6"}
	normalizedGrok, err := normalizeCanonicalApplicationCreate(grok)
	if err != nil {
		t.Fatalf("normalize Grok reviewed profile: %v", err)
	}
	if normalizedGrok.PublicModel.ContextLength == nil || *normalizedGrok.PublicModel.ContextLength != 500_000 ||
		normalizedGrok.PublicModel.OutputLimit == nil || *normalizedGrok.PublicModel.OutputLimit != 131_072 ||
		normalizedGrok.Deployment.MaxContext == nil || *normalizedGrok.Deployment.MaxContext != 500_000 ||
		normalizedGrok.Deployment.DefaultMaxOutput == nil || *normalizedGrok.Deployment.DefaultMaxOutput != 32_768 {
		t.Fatalf("Grok public maximum/request default authority drifted: public=%+v deployment=%+v", normalizedGrok.PublicModel, normalizedGrok.Deployment)
	}

	narrowedGrok := grok
	narrowedGrok.PublicModel.ContextLength = canonicalApplicationIntPointer(200_000)
	narrowedGrok.PublicModel.OutputLimit = canonicalApplicationIntPointer(16_384)
	normalizedNarrowed, err := normalizeCanonicalApplicationCreate(narrowedGrok)
	if err != nil {
		t.Fatalf("normalize customer-narrowed Grok profile: %v", err)
	}
	if normalizedNarrowed.Deployment.MaxContext == nil || *normalizedNarrowed.Deployment.MaxContext != 200_000 ||
		normalizedNarrowed.Deployment.DefaultMaxOutput == nil || *normalizedNarrowed.Deployment.DefaultMaxOutput != 16_384 {
		t.Fatalf("deployment defaults escaped customer public limits: public=%+v deployment=%+v", normalizedNarrowed.PublicModel, normalizedNarrowed.Deployment)
	}

	invalidDeployment := narrowedGrok
	invalidDeployment.Deployment.MaxContext = canonicalApplicationIntPointer(200_001)
	if _, err := normalizeCanonicalApplicationCreate(invalidDeployment); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("deployment context above public cap error=%v want invalid", err)
	}
	invalidDeployment = narrowedGrok
	invalidDeployment.Deployment.DefaultMaxOutput = canonicalApplicationIntPointer(16_385)
	if _, err := normalizeCanonicalApplicationCreate(invalidDeployment); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("deployment output default above public cap error=%v want invalid", err)
	}
	misleadingDeploymentDefault := grok
	misleadingDeploymentDefault.Deployment.DefaultMaxOutput = canonicalApplicationIntPointer(8_192)
	if _, err := normalizeCanonicalApplicationCreate(misleadingDeploymentDefault); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("inert per-deployment output default error=%v want invalid", err)
	}

	unknown := request
	unknown.PublicModel.ExecutionProfileVersion = "9.9.9"
	if _, err := normalizeCanonicalApplicationCreate(unknown); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("unknown profile error=%v want invalid", err)
	}

	overLimit := request
	overLimit.PublicModel.ContextLength = canonicalApplicationIntPointer(1_000_001)
	overLimit.PublicModel.OutputLimit = canonicalApplicationIntPointer(32_768)
	if _, err := normalizeCanonicalApplicationCreate(overLimit); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("over-limit profile error=%v want invalid", err)
	}

	unverified := request
	unverified.PublicModel.ExecutionProfileID = ""
	unverified.PublicModel.ExecutionProfileVersion = ""
	unverified.PublicModel.ContextLength = nil
	unverified.PublicModel.OutputLimit = nil
	normalized, err = normalizeCanonicalApplicationCreate(unverified)
	if err != nil {
		t.Fatalf("normalize unverified profile: %v", err)
	}
	if normalized.PublicModel.ExecutionProfileID != modelprofile.UnverifiedProfileID ||
		normalized.PublicModel.ExecutionProfileVersion != modelprofile.UnverifiedProfileVersion {
		t.Fatalf("unverified profile not pinned: %+v", normalized.PublicModel)
	}

	for _, modelName := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		ambiguous := unverified
		ambiguous.PublicModel.ModelName = modelName
		if _, err := normalizeCanonicalApplicationCreate(ambiguous); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("ambiguous surface omission for %s error=%v want invalid", modelName, err)
		}
	}
	ambiguous := unverified
	ambiguous.PublicModel.ModelName = "gpt-5.6-sol"
	explicitEscapeHatch := ambiguous
	explicitEscapeHatch.PublicModel.ExecutionProfileID = modelprofile.UnverifiedProfileID
	explicitEscapeHatch.PublicModel.ExecutionProfileVersion = modelprofile.UnverifiedProfileVersion
	if _, err := normalizeCanonicalApplicationCreate(explicitEscapeHatch); err != nil {
		t.Fatalf("explicit unverified escape hatch rejected: %v", err)
	}

	responses := request
	responses.Connection = canonicalConnectionInput{
		Name: "OpenAI Responses", Provider: "openai", BaseURL: "https://api.openai.com",
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
	}
	responses.PublicModel = canonicalPublicModelInput{
		ModelName: "gpt-5.6-sol", ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.3.0",
	}
	responses.Deployment = canonicalDeploymentInput{UpstreamModel: "gpt-5.6-sol"}
	responses.Operation = canonicalOperationInput{OperationID: string(modeloperation.IDResponses)}
	normalizedResponses, err := normalizeCanonicalApplicationCreate(responses)
	if err != nil {
		t.Fatalf("normalize Responses profile: %v", err)
	}
	if normalizedResponses.PublicModel.ContextLength == nil || *normalizedResponses.PublicModel.ContextLength != 272_000 ||
		normalizedResponses.Deployment.MaxContext == nil || *normalizedResponses.Deployment.MaxContext != 272_000 {
		t.Fatalf("Responses executable context cap drifted: public=%+v deployment=%+v", normalizedResponses.PublicModel, normalizedResponses.Deployment)
	}
}

func TestCanonicalProfileProductBindingRejectsAPIAndExistingConnectionBypasses(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 41, ExecutionProfileID: "qwen.3.8-max.api", ExecutionProfileVersion: "1.7.0",
	}
	operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
	valid := canonicalConnectionIdentity{
		ID: 51, Provider: "dashscope",
		BaseURL:     "https://workspace-123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		AdapterType: adapters.TypeOpenAICompatible,
	}
	if err := validateCanonicalProfileProductBinding(publicModel, valid, "qwen3.8-max", operation); err != nil {
		t.Fatalf("valid Qwen product binding rejected: %v", err)
	}

	for name, mutate := range map[string]func(*canonicalConnectionIdentity, *string, *canonicalOperationInput){
		"Singapore endpoint": func(connection *canonicalConnectionIdentity, _ *string, _ *canonicalOperationInput) {
			connection.BaseURL = "https://workspace.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"
		},
		"trial endpoint": func(connection *canonicalConnectionIdentity, _ *string, _ *canonicalOperationInput) {
			connection.BaseURL = "https://trial.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
		},
		"token plan endpoint": func(connection *canonicalConnectionIdentity, _ *string, _ *canonicalOperationInput) {
			connection.BaseURL = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
		},
		"forged provider": func(connection *canonicalConnectionIdentity, _ *string, _ *canonicalOperationInput) {
			connection.Provider = "openrouter"
		},
		"wrong upstream model": func(_ *canonicalConnectionIdentity, upstream *string, _ *canonicalOperationInput) {
			*upstream = "qwen3.7-plus"
		},
		"wrong operation": func(_ *canonicalConnectionIdentity, _ *string, selected *canonicalOperationInput) {
			selected.OperationID = modelprofile.OperationOpenAIResponses
		},
	} {
		t.Run(name, func(t *testing.T) {
			connection, selected := valid, operation
			upstream := "qwen3.8-max"
			mutate(&connection, &upstream, &selected)
			if err := validateCanonicalProfileProductBinding(publicModel, connection, upstream, selected); !errors.Is(err, errCanonicalApplicationInvalid) {
				t.Fatalf("product identity bypass error=%v want invalid", err)
			}
		})
	}
}

func TestCanonicalProfileProductBindingRejectsForgedOpenAIEndpoint(t *testing.T) {
	t.Parallel()
	operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
	valid := canonicalConnectionIdentity{
		ID: 52, Provider: "openai", BaseURL: "https://api.openai.com/v1",
		AdapterType: adapters.TypeOpenAICompatible,
	}
	for _, model := range []struct {
		id       int64
		profile  string
		upstream string
	}{
		{42, "openai.gpt-5.6-sol.chat", "gpt-5.6-sol"},
		{43, "openai.gpt-5.6-terra.chat", "gpt-5.6-terra"},
		{44, "openai.gpt-5.6-luna.chat", "gpt-5.6-luna"},
	} {
		publicModel := canonicalPublicModelIdentity{
			ID: model.id, ExecutionProfileID: model.profile, ExecutionProfileVersion: "1.0.0",
		}
		if err := validateCanonicalProfileProductBinding(publicModel, valid, model.upstream, operation); err != nil {
			t.Fatalf("valid OpenAI Chat product binding rejected for %s: %v", model.profile, err)
		}
		forged := valid
		forged.BaseURL = "https://arbitrary.example/v1"
		if err := validateCanonicalProfileProductBinding(publicModel, forged, model.upstream, operation); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("forged OpenAI Chat endpoint for %s error=%v want invalid", model.profile, err)
		}
	}
	responsesOperation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIResponses}
	valid.BaseURL = "https://api.openai.com"
	for _, model := range []struct {
		id       int64
		profile  string
		upstream string
	}{
		{45, "openai.gpt-5.6-sol.api", "gpt-5.6-sol"},
		{46, "openai.gpt-5.6-terra.responses", "gpt-5.6-terra"},
		{47, "openai.gpt-5.6-luna.responses", "gpt-5.6-luna"},
	} {
		responsesModel := canonicalPublicModelIdentity{
			ID: model.id, ExecutionProfileID: model.profile, ExecutionProfileVersion: "1.0.0",
		}
		if err := validateCanonicalProfileProductBinding(responsesModel, valid, model.upstream, responsesOperation); err != nil {
			t.Fatalf("valid OpenAI Responses origin rejected for %s: %v", model.profile, err)
		}
		pathful := valid
		pathful.BaseURL = "https://api.openai.com/v1"
		if err := validateCanonicalProfileProductBinding(responsesModel, pathful, model.upstream, responsesOperation); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("pathful Responses endpoint for %s error=%v want invalid", model.profile, err)
		}
	}
}

func TestCanonicalProfileProductBindingRejectsForgedGeminiEndpoint(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 44, ExecutionProfileID: "gemini.3.7-flash.api", ExecutionProfileVersion: "1.6.0",
	}
	operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
	connection := canonicalConnectionIdentity{
		ID: 54, Provider: "gemini",
		BaseURL:       "https://generativelanguage.googleapis.com/v1beta/openai",
		AdapterType:   adapters.TypeOpenAICompatible,
		AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "gemini-3.7-flash", operation); err != nil {
		t.Fatalf("valid Gemini product binding rejected: %v", err)
	}
	connection.AdapterConfig = json.RawMessage(`{}`)
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "gemini-3.7-flash", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("default Gemini path mode error=%v want invalid", err)
	}
	connection.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
	connection.BaseURL = "https://arbitrary.example/v1beta/openai"
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "gemini-3.7-flash", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("forged Gemini endpoint error=%v want invalid", err)
	}
}

func TestCanonicalProfileProductBindingRejectsForgedDeepSeekEndpoint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		profileID, upstreamModel string
	}{
		{profileID: "deepseek.v4-flash.api", upstreamModel: "deepseek-v4-flash"},
		{profileID: "deepseek.v4-pro.api", upstreamModel: "deepseek-v4-pro"},
	} {
		t.Run(test.profileID, func(t *testing.T) {
			publicModel := canonicalPublicModelIdentity{
				ID: 45, ExecutionProfileID: test.profileID, ExecutionProfileVersion: "1.2.0",
			}
			operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
			connection := canonicalConnectionIdentity{
				ID: 55, Provider: "deepseek", BaseURL: "https://api.deepseek.com",
				AdapterType:   adapters.TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
			}
			if err := validateCanonicalProfileProductBinding(publicModel, connection, test.upstreamModel, operation); err != nil {
				t.Fatalf("valid DeepSeek product binding rejected: %v", err)
			}
			connection.AdapterConfig = json.RawMessage(`{}`)
			if err := validateCanonicalProfileProductBinding(publicModel, connection, test.upstreamModel, operation); !errors.Is(err, errCanonicalApplicationInvalid) {
				t.Fatalf("default path mode error=%v want invalid", err)
			}
			connection.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
			connection.BaseURL = "https://arbitrary.example"
			if err := validateCanonicalProfileProductBinding(publicModel, connection, test.upstreamModel, operation); !errors.Is(err, errCanonicalApplicationInvalid) {
				t.Fatalf("forged DeepSeek endpoint error=%v want invalid", err)
			}
		})
	}
}

func TestCanonicalTypedOperationProfileUsesExactOverrideAndPropagatesInvalidAuthority(t *testing.T) {
	t.Parallel()
	base, err := resolveCanonicalOperationProfile(
		canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat},
		adapters.TypeOpenAICompatible,
	)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveCanonicalTypedOperationProfile(canonicalPublicModelIdentity{
		ID: 45, ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
	}, base)
	if err != nil {
		t.Fatalf("resolve DeepSeek Pro typed operation: %v", err)
	}
	if resolved.ProtocolProfileID != "protocol.deepseek-openai-chat.v4" ||
		resolved.ProtocolProfileVersion != modelprofile.ChatOperationProtocolBindingVersionV1 {
		t.Fatalf("resolved operation=%+v", resolved)
	}

	unmapped, err := resolveCanonicalTypedOperationProfile(canonicalPublicModelIdentity{
		ID: 46, ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
	}, base)
	if err != nil {
		t.Fatalf("unmapped Kimi base operation rejected: %v", err)
	}
	if unmapped != base {
		t.Fatalf("NoAuthority changed canonical base: got=%+v want=%+v", unmapped, base)
	}

	_, err = resolveCanonicalTypedOperationProfile(canonicalPublicModelIdentity{
		ID: 47, ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
	}, canonicalOperationInput{OperationID: modelprofile.OperationOpenAIResponses})
	if !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("operation mismatch error=%v want invalid", err)
	}

	_, err = resolveCanonicalTypedOperationProfile(canonicalPublicModelIdentity{
		ID: 48, ExecutionProfileID: "missing.profile", ExecutionProfileVersion: "1.0.0",
	}, base)
	if !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("unknown typed identity error=%v want invalid", err)
	}
}

func TestCanonicalProfileProductBindingSeparatesKimiPlatformFromCodingSubscription(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		profileID, version, upstreamModel string
	}{
		{profileID: "kimi.k2.6.api", version: "1.6.0", upstreamModel: "kimi-k2.6"},
		{profileID: "kimi.k2.7-code.api", version: "1.2.0", upstreamModel: "kimi-k2.7-code"},
		{profileID: "kimi.k3.api", version: "1.6.0", upstreamModel: "kimi-k3"},
	} {
		t.Run(test.profileID, func(t *testing.T) {
			publicModel := canonicalPublicModelIdentity{
				ID: 46, ExecutionProfileID: test.profileID, ExecutionProfileVersion: test.version,
			}
			operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
			connection := canonicalConnectionIdentity{
				ID: 56, Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1",
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
			}
			if err := validateCanonicalProfileProductBinding(publicModel, connection, test.upstreamModel, operation); err != nil {
				t.Fatalf("valid Kimi platform product binding rejected: %v", err)
			}
			connection.BaseURL = "https://api.kimi.com/coding/v1"
			if err := validateCanonicalProfileProductBinding(publicModel, connection, test.upstreamModel, operation); !errors.Is(err, errCanonicalApplicationInvalid) {
				t.Fatalf("Kimi coding endpoint error=%v want invalid", err)
			}
			connection.BaseURL = "https://api.moonshot.cn/v1"
			if err := validateCanonicalProfileProductBinding(publicModel, connection, "kimi-for-coding", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
				t.Fatalf("Kimi coding model ID error=%v want invalid", err)
			}
		})
	}
}

func TestCanonicalProfileProductBindingRejectsForgedMiniMaxIdentity(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 47, ExecutionProfileID: "minimax.m2.7.token-plan", ExecutionProfileVersion: "1.2.0",
	}
	operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
	connection := canonicalConnectionIdentity{
		ID: 57, Provider: "minimax", BaseURL: "https://api.minimaxi.com/v1",
		AdapterType:   adapters.TypeOpenAICompatible,
		AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "MiniMax-M2.7", operation); err != nil {
		t.Fatalf("valid MiniMax product binding rejected: %v", err)
	}
	connection.BaseURL = "https://arbitrary.example/v1"
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "MiniMax-M2.7", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("forged MiniMax endpoint error=%v want invalid", err)
	}
	connection.BaseURL = "https://api.minimaxi.com/v1"
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "MiniMax-M2.7-highspeed", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("unselected MiniMax highspeed identity error=%v want invalid", err)
	}
}

func TestCanonicalProfileProductBindingRejectsForgedGrok46Identity(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 58, ExecutionProfileID: "xai.grok-4.6.api", ExecutionProfileVersion: "1.1.0",
	}
	operation := canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat}
	connection := canonicalConnectionIdentity{
		ID: 68, Provider: "xai", BaseURL: "https://api.x.ai/v1",
		AdapterType:   adapters.TypeOpenAICompatible,
		AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`),
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "grok-4.6", operation); err != nil {
		t.Fatalf("valid xAI product binding rejected: %v", err)
	}
	connection.BaseURL = "https://arbitrary.example/v1"
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "grok-4.6", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("forged xAI endpoint error=%v want invalid", err)
	}
	connection.BaseURL = "https://api.x.ai/v1"
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "grok-4.6-fast", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("unselected xAI model identity error=%v want invalid", err)
	}
}

func TestCanonicalProfileProductBindingRequiresExactClaudeSonnet5MessagesOrigin(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 59, ExecutionProfileID: "anthropic.claude-sonnet-5.api", ExecutionProfileVersion: "1.2.0",
	}
	connection := canonicalConnectionIdentity{
		ID: 69, Provider: "anthropic", BaseURL: "https://api.anthropic.com",
		AdapterType:   adapters.TypeAnthropicNative,
		AdapterConfig: json.RawMessage(`{"anthropic_version":"2023-06-01"}`),
	}
	operation, err := resolveCanonicalOperationProfile(
		canonicalOperationInput{OperationID: modelprofile.OperationAnthropicMessage},
		connection.AdapterType,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "claude-sonnet-5", operation); err != nil {
		t.Fatalf("valid Claude Sonnet 5 Messages binding rejected: %v", err)
	}
	for _, endpoint := range []string{"https://provider.example", "https://api.anthropic.com/v1"} {
		forged := connection
		forged.BaseURL = endpoint
		if err := validateCanonicalProfileProductBinding(publicModel, forged, "claude-sonnet-5", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("forged Anthropic endpoint %s error=%v want invalid", endpoint, err)
		}
	}
	wrongModel := connection
	if err := validateCanonicalProfileProductBinding(publicModel, wrongModel, "claude-opus-5", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("forged Anthropic model error=%v want invalid", err)
	}
}

func TestCanonicalProfileProductBindingRequiresExactClaudeOpus5MessagesOrigin(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 60, ExecutionProfileID: "anthropic.claude-opus-5.api", ExecutionProfileVersion: "1.1.0",
	}
	connection := canonicalConnectionIdentity{
		ID: 70, Provider: "anthropic", BaseURL: "https://api.anthropic.com",
		AdapterType:   adapters.TypeAnthropicNative,
		AdapterConfig: json.RawMessage(`{"anthropic_version":"2023-06-01"}`),
	}
	operation, err := resolveCanonicalOperationProfile(
		canonicalOperationInput{OperationID: modelprofile.OperationAnthropicMessage},
		connection.AdapterType,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "claude-opus-5", operation); err != nil {
		t.Fatalf("valid Claude Opus 5 Messages binding rejected: %v", err)
	}
	for _, endpoint := range []string{"https://provider.example", "https://api.anthropic.com/v1"} {
		forged := connection
		forged.BaseURL = endpoint
		if err := validateCanonicalProfileProductBinding(publicModel, forged, "claude-opus-5", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("forged Anthropic endpoint %s error=%v want invalid", endpoint, err)
		}
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "claude-sonnet-5", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("forged Anthropic model error=%v want invalid", err)
	}
}

func TestCanonicalProfileProductBindingRequiresExactClaudeHaiku45MessagesOrigin(t *testing.T) {
	t.Parallel()
	publicModel := canonicalPublicModelIdentity{
		ID: 61, ExecutionProfileID: "anthropic.claude-haiku-4.5.api", ExecutionProfileVersion: "1.1.0",
	}
	connection := canonicalConnectionIdentity{
		ID: 71, Provider: "anthropic", BaseURL: "https://api.anthropic.com",
		AdapterType:   adapters.TypeAnthropicNative,
		AdapterConfig: json.RawMessage(`{"anthropic_version":"2023-06-01"}`),
	}
	operation, err := resolveCanonicalOperationProfile(
		canonicalOperationInput{OperationID: modelprofile.OperationAnthropicMessage},
		connection.AdapterType,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "claude-haiku-4-5-20251001", operation); err != nil {
		t.Fatalf("valid Claude Haiku 4.5 Messages binding rejected: %v", err)
	}
	for _, endpoint := range []string{"https://provider.example", "https://api.anthropic.com/v1"} {
		forged := connection
		forged.BaseURL = endpoint
		if err := validateCanonicalProfileProductBinding(publicModel, forged, "claude-haiku-4-5-20251001", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("forged Anthropic endpoint %s error=%v want invalid", endpoint, err)
		}
	}
	if err := validateCanonicalProfileProductBinding(publicModel, connection, "claude-haiku-4-5", operation); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("unpinned Anthropic alias error=%v want invalid", err)
	}
}

func TestCanonicalNewDeploymentRejectsHistoricalProfileButAllowsExactReuse(t *testing.T) {
	t.Parallel()

	for _, current := range []canonicalPublicModelIdentity{
		{ID: 47, ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0"},
		{ID: 48, ExecutionProfileID: "kimi.k2.7-code.api", ExecutionProfileVersion: "1.3.0"},
		{ID: 49, ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.8.0"},
		{ID: 50, ExecutionProfileID: "minimax.m2.7.token-plan", ExecutionProfileVersion: "1.5.0"},
		{ID: 51, ExecutionProfileID: "minimax.m2.5-highspeed.token-plan", ExecutionProfileVersion: "1.5.0"},
		{ID: 52, ExecutionProfileID: "minimax.m3.token-plan", ExecutionProfileVersion: "1.7.0"},
		{ID: 53, ExecutionProfileID: modelprofile.UnverifiedProfileID, ExecutionProfileVersion: modelprofile.UnverifiedProfileVersion},
	} {
		if err := validateCanonicalNewDeploymentProfile(current, false); err != nil {
			t.Fatalf("current profile %+v rejected: %v", current, err)
		}
	}

	for _, historical := range []canonicalPublicModelIdentity{
		{ID: 51, ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.5.0"},
		{ID: 52, ExecutionProfileID: "kimi.k2.7-code.api", ExecutionProfileVersion: "1.1.0"},
		{ID: 53, ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.5.0"},
	} {
		if err := validateCanonicalNewDeploymentProfile(historical, false); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("new historical deployment error=%v want invalid for %+v", err, historical)
		}
		if err := validateCanonicalNewDeploymentProfile(historical, true); err != nil {
			t.Fatalf("existing historical deployment reuse rejected for %+v: %v", historical, err)
		}
	}
}

func TestCanonicalQualificationOriginGateUsesEffectiveConnectionBeforeWrites(t *testing.T) {
	t.Parallel()
	baseRequest := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Qualification", Provider: "openai", BaseURL: "https://api.example.com/v1",
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "qualification-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "qualification-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "qualification-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDResponses)},
	}
	normalized, err := normalizeCanonicalApplicationCreate(baseRequest)
	if err != nil {
		t.Fatalf("normalize qualification request: %v", err)
	}
	queryCalls := 0
	tx := &testutil.MockTx{QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
		queryCalls++
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*(dest[0].(*string)) = "https://saved.example.com/provider/v1"
			return nil
		}}
	}}
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, normalized); !errors.Is(err, errCanonicalApplicationQualificationOriginInvalid) {
		t.Fatalf("new pathful Responses origin error=%v want invalid", err)
	}
	if queryCalls != 0 {
		t.Fatalf("new connection origin gate performed %d database reads", queryCalls)
	}

	existing := normalized
	existing.Connection = canonicalConnectionInput{ID: 91}
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, existing); !errors.Is(err, errCanonicalApplicationQualificationOriginConflict) {
		t.Fatalf("existing pathful Responses origin error=%v want conflict", err)
	}
	if queryCalls != 1 {
		t.Fatalf("existing connection origin gate reads=%d want 1", queryCalls)
	}

	existing.Operation.OperationID = string(modeloperation.IDMessages)
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, existing); !errors.Is(err, errCanonicalApplicationQualificationOriginConflict) {
		t.Fatalf("existing pathful Messages origin error=%v want conflict", err)
	}
	if queryCalls != 2 {
		t.Fatalf("Messages connection origin gate reads=%d want 2", queryCalls)
	}

	pathFree := normalized
	pathFree.Connection.BaseURL = " https://API.Example.com:443/ "
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, pathFree); err != nil {
		t.Fatalf("path-free Responses origin rejected: %v", err)
	}
	if queryCalls != 2 {
		t.Fatalf("new path-free connection origin gate performed %d database reads", queryCalls)
	}

	chat := normalized
	chat.Operation.OperationID = string(modeloperation.IDChatCompletions)
	if err := validateCanonicalQualificationOrigin(context.Background(), tx, 11, chat); err != nil {
		t.Fatalf("legacy path-owning Chat base URL rejected: %v", err)
	}
	if queryCalls != 2 {
		t.Fatalf("Chat qualification bypass performed %d database reads", queryCalls)
	}
}

func TestCanonicalPublicModelNameIsImmutableFromCreation(t *testing.T) {
	t.Parallel()
	if err := validateCanonicalPublicModelRename("stable-model", "stable-model"); err != nil {
		t.Fatalf("unchanged identity rejected: %v", err)
	}
	if err := validateCanonicalPublicModelRename("stable-model", " stable-model "); err != nil {
		t.Fatalf("trim-equivalent identity rejected: %v", err)
	}
	if err := validateCanonicalPublicModelRename("stable-model", "renamed-model"); !errors.Is(err, errCanonicalApplicationConflict) {
		t.Fatalf("rename error=%v want conflict", err)
	}
	if err := validateCanonicalPublicModelRename("stable-model", ""); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("invalid identity error=%v want invalid", err)
	}
}

func TestCanonicalModelApplicationCreateRejectsDuplicateMembersBeforeSideEffects(t *testing.T) {
	t.Parallel()
	beginCalls := 0
	database := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
		beginCalls++
		return &testutil.MockTx{}, nil
	}}
	handler := NewHandlerWithAPIKeyHashSecret(
		database, "duplicate-member-jwt", "duplicate-member-hmac",
		[]byte("0123456789abcdef01234567"), nil,
	)
	baseSuffix := `,
  "public_model":{"model_name":"duplicate-model"},
  "deployment":{"upstream_model":"duplicate-upstream"},
  "credential":{"api_key":"duplicate-secret"},
  "operation":{"operation_id":"openai.chat_completions.v1"}
}`
	for name, body := range map[string]string{
		"top-level": `{
  "connection":{"name":"a"},
  "connection":{"name":"b"}` + baseSuffix,
		"nested credential": `{
  "connection":{"name":"a","provider":"openai","base_url":"https://duplicate.example/v1","adapter_type":"openai_compatible","adapter_config":{}}` + `,
  "public_model":{"model_name":"duplicate-model"},
  "deployment":{"upstream_model":"duplicate-upstream"},
  "credential":{"api_key":"duplicate-secret-a","api_key":"duplicate-secret-b"},
  "operation":{"operation_id":"openai.chat_completions.v1"}
}`,
		"adapter config": `{
  "connection":{"name":"a","provider":"openai","base_url":"https://duplicate.example/v1","adapter_type":"openai_compatible","adapter_config":{"n":1,"n":2}}` + baseSuffix,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/model-deployments", strings.NewReader(body))
			request.Header.Set(middleware.IdempotencyKeyHeader, "duplicate-member-key")
			ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxUserID, int64(1))
			response := httptest.NewRecorder()
			middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
				http.HandlerFunc(handler.CreateCanonicalModelApplication),
			).ServeHTTP(response, request.WithContext(ctx))
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "canonical_application_invalid") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if beginCalls != 0 {
		t.Fatalf("duplicate members reached database transaction %d times", beginCalls)
	}
}

func TestNormalizeCanonicalApplicationCreateStrictlyCanonicalizesAdapterConfig(t *testing.T) {
	t.Parallel()
	if canonicalModelApplicationMaxRequestBytes <= canonicalModelApplicationMaxAdapterConfigBytes {
		t.Fatalf("request limit %d must leave envelope space around the %d-byte adapter_config contract",
			canonicalModelApplicationMaxRequestBytes, canonicalModelApplicationMaxAdapterConfigBytes)
	}
	base := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Precise config", Provider: "azure", BaseURL: "https://precise.example",
			AdapterType: adapters.TypeAzureOpenAI,
		},
		PublicModel: canonicalPublicModelInput{ModelName: "precise-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "precise-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "precise-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	left := base
	left.Connection.AdapterConfig = json.RawMessage(`{"deployment":"precise-upstream","auth_mode":"api_key","api_version":"2024-10-21"}`)
	right := base
	right.Connection.AdapterConfig = json.RawMessage(` { "api_version" : "2024-10-21", "deployment" : "precise-upstream", "auth_mode" : "api_key" } `)
	normalizedLeft, err := normalizeCanonicalApplicationCreate(left)
	if err != nil {
		t.Fatalf("normalize left: %v", err)
	}
	normalizedRight, err := normalizeCanonicalApplicationCreate(right)
	if err != nil {
		t.Fatalf("normalize right: %v", err)
	}
	if string(normalizedLeft.Connection.AdapterConfig) != string(normalizedRight.Connection.AdapterConfig) {
		t.Fatalf("canonical configs differ: %s != %s", normalizedLeft.Connection.AdapterConfig, normalizedRight.Connection.AdapterConfig)
	}
	leftFingerprint, err := canonicalModelApplicationFingerprint(normalizedLeft, "canonical-config-secret")
	if err != nil {
		t.Fatalf("fingerprint left: %v", err)
	}
	rightFingerprint, err := canonicalModelApplicationFingerprint(normalizedRight, "canonical-config-secret")
	if err != nil {
		t.Fatalf("fingerprint right: %v", err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("object ordering changed fingerprint: %s != %s", leftFingerprint, rightFingerprint)
	}

	for _, raw := range []string{
		`{"a":1,"a":2}`,
		`{"nested":{"a":1,"a":2}}`,
		`[]`,
		`null`,
		`42`,
		`"config"`,
	} {
		candidate := base
		candidate.Connection.AdapterConfig = json.RawMessage(raw)
		if _, err := normalizeCanonicalApplicationCreate(candidate); !errors.Is(err, errCanonicalApplicationInvalid) {
			t.Fatalf("adapter_config %s error=%v want invalid", raw, err)
		}
	}
	oversized := base
	oversized.Connection.AdapterConfig = json.RawMessage(`{"padding":"` + strings.Repeat("a", canonicalModelApplicationMaxAdapterConfigBytes) + `"}`)
	if _, err := normalizeCanonicalApplicationCreate(oversized); !errors.Is(err, errCanonicalApplicationInvalid) {
		t.Fatalf("oversized adapter_config error=%v want invalid", err)
	}
}

func TestCanonicalJSONEqualPreservesNumberPrecisionAndObjectSemantics(t *testing.T) {
	t.Parallel()
	if !canonicalJSONEqual(
		[]byte(`{"z":900719925474099312345678901234567890,"nested":{"b":2,"a":1}}`),
		[]byte(` { "nested": { "a": 1, "b": 2 }, "z": 900719925474099312345678901234567890 } `),
	) {
		t.Fatal("semantically equal objects with precise integers were rejected")
	}
	if canonicalJSONEqual(
		[]byte(`{"value":9007199254740992}`),
		[]byte(`{"value":9007199254740993}`),
	) {
		t.Fatal("distinct integers above float64 precision were treated as equal")
	}
	if canonicalJSONEqual([]byte(`{"a":1,"a":1}`), []byte(`{"a":1}`)) {
		t.Fatal("duplicate-member JSON was treated as an equal canonical object")
	}
}

func TestCanonicalModelApplicationFingerprintIncludesSecretWithoutExposingIt(t *testing.T) {
	t.Parallel()
	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Fingerprint", Provider: "openai", BaseURL: "https://fingerprint.example/v1",
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "fingerprint-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "fingerprint-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "provider-secret-a"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	normalized, err := normalizeCanonicalApplicationCreate(request)
	if err != nil {
		t.Fatal(err)
	}
	first, err := canonicalModelApplicationFingerprint(normalized, "fingerprint-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	explicitDefaults := normalized
	explicitDefaults.PublicModel.ProviderFamily = "openai"
	explicitDefaults.PublicModel.ContextLength = canonicalApplicationIntPointer(0)
	explicitDefaults.PublicModel.OutputLimit = canonicalApplicationIntPointer(0)
	explicitDefaults.PublicModel.ApprovedCatalog = canonicalApplicationBoolPointer(false)
	explicitDefaults.Deployment.Priority = canonicalApplicationIntPointer(100)
	explicitDefaults.Deployment.RoutingWeight = canonicalApplicationIntPointer(100)
	explicitDefaults.Deployment.MaxContext = canonicalApplicationIntPointer(0)
	explicitDefaults.Deployment.DefaultMaxOutput = canonicalApplicationIntPointer(0)
	explicitDefaults.Deployment.DegradationThreshold = canonicalApplicationIntPointer(10)
	explicitFingerprint, err := canonicalModelApplicationFingerprint(explicitDefaults, "fingerprint-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	if first != explicitFingerprint {
		t.Fatalf("fixed create defaults changed fingerprint: %s != %s", first, explicitFingerprint)
	}
	normalized.Credential.APIKey = "provider-secret-b"
	second, err := canonicalModelApplicationFingerprint(normalized, "fingerprint-hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different provider secrets produced the same request fingerprint")
	}
	if strings.Contains(first, "provider-secret") || !strings.HasPrefix(first, "hmac-sha256:") || len(first) != 76 {
		t.Fatalf("unsafe or malformed fingerprint %q", first)
	}
}

func TestCanonicalModelApplicationCreateRetriesOnlyOneFreshSerializableTransaction(t *testing.T) {
	t.Parallel()
	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Retry", Provider: "openai", BaseURL: "https://retry.example/v1",
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "retry-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "retry-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "retry-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	for _, test := range []struct {
		name       string
		code       string
		wantBegins int
	}{
		{name: "serialization failure", code: "40001", wantBegins: 2},
		{name: "deadlock", code: "40P01", wantBegins: 2},
		{name: "non retryable database error", code: "23505", wantBegins: 1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			beginCalls := 0
			database := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
				beginCalls++
				return nil, &pgconn.PgError{Code: test.code, Message: "injected transaction begin failure"}
			}}
			handler := NewHandlerWithAPIKeyHashSecret(
				database, "retry-jwt", "retry-hmac", []byte("0123456789abcdef01234567"), nil,
			)
			raw := httptest.NewRequest(http.MethodPost, "/api/v1/model-deployments", nil)
			raw.Header.Set(middleware.IdempotencyKeyHeader, "retry-key")
			ctx := context.WithValue(raw.Context(), middleware.CtxOrgID, int64(17))
			ctx = context.WithValue(ctx, middleware.CtxUserID, int64(23))
			var identified *http.Request
			middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(http.HandlerFunc(
				func(_ http.ResponseWriter, request *http.Request) { identified = request },
			)).ServeHTTP(httptest.NewRecorder(), raw.WithContext(ctx))
			if identified == nil {
				t.Fatal("operation identity middleware did not forward request")
			}
			if _, err := handler.createCanonicalModelApplication(
				identified.Context(), 17, request, requestAuditMeta(identified, 17, 0),
			); err == nil {
				t.Fatal("injected transaction failure unexpectedly succeeded")
			}
			if beginCalls != test.wantBegins {
				t.Fatalf("Begin calls=%d want %d", beginCalls, test.wantBegins)
			}
		})
	}
	if isCanonicalCreateTransactionRetryable(errCanonicalApplicationIdempotencyConflict) {
		t.Fatal("typed idempotency conflict must never be transaction-retried")
	}
}

func TestCanonicalProfileProductBindingRequiresExactGPT56ResponsesEndpointContract(t *testing.T) {
	t.Parallel()
	for index, model := range []string{"sol", "terra", "luna"} {
		index := index
		model := model
		t.Run(model, func(t *testing.T) {
			t.Parallel()
			publicModel := canonicalPublicModelIdentity{
				ID: int64(42 + index), ExecutionProfileID: "openai.gpt-5.6-" + model + ".responses", ExecutionProfileVersion: "1.1.0",
			}
			connection := canonicalConnectionIdentity{
				ID: int64(52 + index), Provider: "openai", BaseURL: "https://api.openai.com",
				AdapterType: adapters.TypeOpenAICompatible,
			}
			operation, err := resolveCanonicalOperationProfile(
				canonicalOperationInput{OperationID: modelprofile.OperationOpenAIResponses},
				connection.AdapterType,
			)
			if err != nil {
				t.Fatal(err)
			}
			upstreamModel := "gpt-5.6-" + model
			if err := validateCanonicalProfileProductBinding(publicModel, connection, upstreamModel, operation); err != nil {
				t.Fatalf("exact %s Responses endpoint contract rejected: %v", model, err)
			}
			operation.ProtocolProfileVersion = "9.9.9"
			if err := validateCanonicalProfileProductBinding(publicModel, connection, upstreamModel, operation); err == nil {
				t.Fatalf("mismatched %s Responses endpoint contract was accepted", model)
			}
		})
	}
}

func TestResolveCanonicalOperationProfileUsesOnlyServerRegisteredTemplates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		operation   modeloperation.ID
		adapter     string
		wantID      string
		wantVersion string
		wantError   bool
	}{
		{name: "openai chat", operation: modeloperation.IDChatCompletions, adapter: adapters.TypeOpenAICompatible, wantID: "openai.chat_completions", wantVersion: "1.0.0"},
		{name: "azure chat", operation: modeloperation.IDChatCompletions, adapter: adapters.TypeAzureOpenAI, wantID: "openai.chat_completions", wantVersion: "1.0.0"},
		{name: "openai embeddings", operation: modeloperation.IDEmbeddings, adapter: adapters.TypeOpenAICompatible, wantID: "openai.embeddings", wantVersion: "1.0.0"},
		{name: "responses exact registry profile", operation: modeloperation.IDResponses, adapter: adapters.TypeOpenAICompatible, wantID: endpointprotocol.ProfileOpenAIResponsesText, wantVersion: "1.0.0"},
		{name: "messages exact registry profile", operation: modeloperation.IDMessages, adapter: adapters.TypeAnthropicNative, wantID: endpointprotocol.ProfileAnthropicMessagesText, wantVersion: "1.0.0"},
		{name: "anthropic adapter cannot claim openai chat", operation: modeloperation.IDChatCompletions, adapter: adapters.TypeAnthropicNative, wantError: true},
		{name: "openai adapter cannot claim anthropic messages", operation: modeloperation.IDMessages, adapter: adapters.TypeOpenAICompatible, wantError: true},
		{name: "azure responses template not registered", operation: modeloperation.IDResponses, adapter: adapters.TypeAzureOpenAI, wantError: true},
		{name: "models list is not a provider deployment", operation: modeloperation.IDModelsList, adapter: adapters.TypeOpenAICompatible, wantError: true},
		{name: "media jobs need their own provider template", operation: modeloperation.IDMediaJobs, adapter: adapters.TypeOpenAICompatible, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := resolveCanonicalOperationProfile(canonicalOperationInput{OperationID: string(test.operation)}, test.adapter)
			if test.wantError {
				if err == nil {
					t.Fatalf("expected unsupported template to fail, got %+v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve template: %v", err)
			}
			if result.ProtocolProfileID != test.wantID || result.ProtocolProfileVersion != test.wantVersion {
				t.Fatalf("profile=%s/%s want %s/%s", result.ProtocolProfileID, result.ProtocolProfileVersion, test.wantID, test.wantVersion)
			}
		})
	}
}
