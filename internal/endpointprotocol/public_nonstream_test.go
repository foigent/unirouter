package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

func TestPublicRegistryRequiresExactIndependentExecutionIdentity(t *testing.T) {
	historical, err := NewRegistry().Resolve(ProfileOpenAIResponsesText, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if descriptor := historical.Descriptor(); descriptor.RuntimeState != RuntimeContractOnly || descriptor.CustomerChargeState != ChargeObservedOnly {
		t.Fatalf("historical descriptor was upgraded in place: %+v", descriptor)
	}

	identity := publicIdentity(modeloperation.IDResponses, modeloperation.PathResponses, ProfileOpenAIResponsesText, "1.0.0")
	profile, err := NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := profile.Descriptor()
	if descriptor.Identity != identity || descriptor.CachePolicy != PublicCachePolicyBypass || descriptor.Streaming || descriptor.Modality != "text" {
		t.Fatalf("public descriptor=%+v", descriptor)
	}

	for _, mutate := range []func(*PublicContractIdentity){
		func(value *PublicContractIdentity) { value.ModelOperationID = string(modeloperation.IDMessages) },
		func(value *PublicContractIdentity) { value.PublicPath = modeloperation.PathMessages },
		func(value *PublicContractIdentity) { value.ExecutorContractVersion = "other" },
		func(value *PublicContractIdentity) { value.ProfileID = ProfileAnthropicMessagesText },
		func(value *PublicContractIdentity) { value.DeliveryContractVersion = "other" },
		func(value *PublicContractIdentity) { value.ChargeContractVersion = "other" },
		func(value *PublicContractIdentity) { value.IdempotencyContractVersion = "other" },
		func(value *PublicContractIdentity) { value.ProfileVersion = "1.0.1" },
	} {
		candidate := identity
		mutate(&candidate)
		if _, err := NewPublicNonStreamRegistry().Resolve(candidate); !errors.Is(err, ErrPublicContractMismatch) {
			t.Fatalf("identity %+v error=%v", candidate, err)
		}
	}
}

func TestPublicRegistryKeepsResponsesV1AndV2ExactIdentitiesAddressable(t *testing.T) {
	v1 := publicIdentity(
		modeloperation.IDResponses, modeloperation.PathResponses,
		ProfileOpenAIResponsesText, "1.0.0",
	)
	v2 := PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDResponses),
		PublicPath:                 modeloperation.PathResponses,
		ExecutorContractVersion:    "unirouter.endpoint-profile-responses-dual-executor.v1",
		ProfileID:                  ProfileOpenAIResponsesTextV2,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    PublicResponsesDeliveryContractV2,
		ChargeContractVersion:      PublicCustomerChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	registry := NewPublicNonStreamRegistry()
	for _, identity := range []PublicContractIdentity{v1, v2} {
		profile, err := registry.Resolve(identity)
		if err != nil {
			t.Fatalf("resolve %+v: %v", identity, err)
		}
		if descriptor := profile.Descriptor(); descriptor.Identity != identity || descriptor.Streaming {
			t.Fatalf("descriptor=%+v want identity=%+v nonstream", descriptor, identity)
		}
	}
}

func TestPublicMessagesV2FreezesExactNonStreamAndStreamRequestModes(t *testing.T) {
	identity := PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDMessages),
		PublicPath:                 modeloperation.PathMessages,
		ExecutorContractVersion:    "unirouter.endpoint-profile-messages-dual-executor.v1",
		ProfileID:                  ProfileAnthropicMessagesTextV2,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    PublicAnthropicMessagesDeliveryContractV2,
		ChargeContractVersion:      PublicCustomerChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	contract := sonnet5ControlsPublicMessagesContract()
	nonstream, err := NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewPublicStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor := nonstream.Descriptor(); descriptor.Identity != identity || descriptor.Streaming {
		t.Fatalf("nonstream descriptor=%+v", descriptor)
	}
	if descriptor := stream.Descriptor(); descriptor.Identity != identity || !descriptor.Streaming {
		t.Fatalf("stream descriptor=%+v", descriptor)
	}

	base := `"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]`
	for _, body := range []string{"{" + base + "}", "{" + base + `,"stream":false}`} {
		prepared, err := nonstream.PrepareRequest([]byte(body), "public-claude", contract)
		if err != nil {
			t.Fatalf("nonstream body=%s err=%v", body, err)
		}
		if prepared.InvocationSemantics().Streaming {
			t.Fatalf("nonstream semantics=%+v", prepared.InvocationSemantics())
		}
		upstream, err := nonstream.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
			BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "provider-private-claude",
		})
		if err != nil {
			t.Fatal(err)
		}
		if upstream.binding.base.streaming {
			t.Fatal("nonstream request binding was marked streaming")
		}
	}
	if _, err := nonstream.PrepareRequest([]byte("{"+base+`,"stream":true}`), "public-claude", contract); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nonstream accepted stream=true: %v", err)
	}

	streamBody := []byte("{" + base + `,"stream":true}`)
	prepared, err := stream.PrepareRequest(streamBody, "public-claude", contract)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.InvocationSemantics().Streaming {
		t.Fatalf("stream semantics=%+v", prepared.InvocationSemantics())
	}
	upstream, err := stream.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "provider-private-claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := readPublicBody(t, upstream.request)
	if body["stream"] != true || body["model"] != "provider-private-claude" {
		t.Fatalf("upstream body=%+v", body)
	}
	if !upstream.binding.base.streaming {
		t.Fatal("stream request binding was not marked streaming")
	}
	for _, body := range []string{"{" + base + "}", "{" + base + `,"stream":false}`} {
		if _, err := stream.PrepareRequest([]byte(body), "public-claude", contract); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("stream profile accepted body=%s err=%v", body, err)
		}
	}
	for _, invalid := range []string{`"true"`, `null`, `1`, `{}`} {
		body := []byte("{" + base + `,"stream":` + invalid + "}")
		if _, err := stream.PrepareRequest(body, "public-claude", contract); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("stream profile accepted stream=%s err=%v", invalid, err)
		}
	}

	historical := mustPublicProfile(t, publicIdentity(
		modeloperation.IDMessages, modeloperation.PathMessages,
		ProfileAnthropicMessagesText, "1.0.0",
	))
	if _, err := historical.PrepareRequest(streamBody, "public-claude", contract); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("historical messages profile was upgraded in place: %v", err)
	}
}

func TestPublicResponsesExactCaseSchemaRejectsAliasesAtEveryResponseLayer(t *testing.T) {
	_, upstream := buildPublicResponses(t)
	base := `{
		"id":"resp_case","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null,
		"output":[{"id":"msg_case","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],
		"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":14}
	}`
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "root alias", old: `"id":"resp_case"`, new: `"ID":"resp_case"`},
		{name: "root semantic duplicate", old: `"id":"resp_case"`, new: `"id":"resp_case","ID":"other"`},
		{name: "output alias", old: `"type":"message"`, new: `"TYPE":"message"`},
		{name: "output semantic duplicate", old: `"type":"message"`, new: `"type":"message","TYPE":"message"`},
		{name: "content alias", old: `"text":"answer"`, new: `"TEXT":"answer"`},
		{name: "content semantic duplicate", old: `"text":"answer"`, new: `"text":"answer","TEXT":"other"`},
		{name: "usage alias", old: `"input_tokens":10`, new: `"INPUT_TOKENS":10`},
		{name: "usage semantic duplicate", old: `"input_tokens":10`, new: `"input_tokens":10,"INPUT_TOKENS":10`},
		{name: "input detail alias", old: `"cached_tokens":3`, new: `"CACHED_TOKENS":3`},
		{name: "input detail semantic duplicate", old: `"cached_tokens":3`, new: `"cached_tokens":3,"CACHED_TOKENS":3`},
		{name: "output detail alias", old: `"reasoning_tokens":1`, new: `"REASONING_TOKENS":1`},
		{name: "output detail semantic duplicate", old: `"reasoning_tokens":1`, new: `"reasoning_tokens":1,"REASONING_TOKENS":1`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(base, test.old, test.new, 1)
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(body)); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("case alias crossed exact schema: error=%v body=%s", err, body)
			}
		})
	}
}

func TestPublicAnthropicRequestExactCaseSchemaRejectsMAXTokensAndMessageAliases(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	base := `{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`
	tests := []struct {
		name string
		old  string
		new  string
		kind error
	}{
		{name: "MAX_TOKENS alias", old: `"max_tokens":64`, new: `"MAX_TOKENS":64`, kind: ErrInvalidRequest},
		{name: "MAX_TOKENS semantic duplicate", old: `"max_tokens":64`, new: `"max_tokens":64,"MAX_TOKENS":1`, kind: ErrInvalidRequest},
		{name: "message alias", old: `"role":"user"`, new: `"ROLE":"user"`, kind: ErrInvalidRequest},
		{name: "message semantic duplicate", old: `"role":"user"`, new: `"role":"user","ROLE":"assistant"`, kind: ErrInvalidRequest},
		{name: "exact duplicate remains rejected", old: `"max_tokens":64`, new: `"max_tokens":64,"max_tokens":1`, kind: ErrInvalidJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(base, test.old, test.new, 1)
			if _, err := profile.PrepareRequest([]byte(body), "public-claude"); !errors.Is(err, test.kind) {
				t.Fatalf("case alias crossed exact schema: error=%v body=%s", err, body)
			}
		})
	}
}

func TestPublicAnthropicResponseExactCaseSchemaRejectsAliasesAtEveryLayer(t *testing.T) {
	_, upstream := buildPublicMessages(t)
	base := `{
		"id":"msg_case","type":"message","role":"assistant","model":"provider-private-claude",
		"content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":80,"output_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}
	}`
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "root alias", old: `"id":"msg_case"`, new: `"ID":"msg_case"`},
		{name: "root semantic duplicate", old: `"id":"msg_case"`, new: `"id":"msg_case","ID":"other"`},
		{name: "content alias", old: `"text":"answer"`, new: `"TEXT":"answer"`},
		{name: "content semantic duplicate", old: `"text":"answer"`, new: `"text":"answer","TEXT":"other"`},
		{name: "usage alias", old: `"input_tokens":20`, new: `"INPUT_TOKENS":20`},
		{name: "usage semantic duplicate", old: `"input_tokens":20`, new: `"input_tokens":20,"INPUT_TOKENS":20`},
		{name: "cache detail alias", old: `"ephemeral_5m_input_tokens":10`, new: `"EPHEMERAL_5M_INPUT_TOKENS":10`},
		{name: "cache detail semantic duplicate", old: `"ephemeral_5m_input_tokens":10`, new: `"ephemeral_5m_input_tokens":10,"EPHEMERAL_5M_INPUT_TOKENS":10`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(base, test.old, test.new, 1)
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(body)); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("case alias crossed exact schema: error=%v body=%s", err, body)
			}
		})
	}
}

func TestPublicResponsesPrepareAdmissionTransformAndBuild(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDResponses, modeloperation.PathResponses, ProfileOpenAIResponsesText, "1.0.0"))
	if _, err := profile.PrepareRequest(
		[]byte(`{"model":"public-text","input":"hello"}`),
		"public-text",
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing explicit max was accepted: %v", err)
	}

	prepared, err := profile.PrepareRequest([]byte(`{
		"model":"public-text",
		"input":"hello",
		"instructions":"be brief",
		"max_output_tokens":64
	}`), "public-text")
	if err != nil {
		t.Fatal(err)
	}
	admission := prepared.Admission()
	if admission.EndpointKind != EndpointResponses || admission.RequestedModel != "public-text" ||
		admission.ExplicitMaxOutputTokens != 64 || admission.CachePolicy != PublicCachePolicyBypass ||
		admission.InputTextBytes != len("hello")+len("be brief") || len(admission.Texts) != 2 ||
		admission.Texts[0].Path != "input" || admission.Texts[1].Path != "instructions" {
		t.Fatalf("admission=%+v", admission)
	}
	transformed, err := prepared.WithTexts([]string{"redacted input", "redacted instructions"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Admission().Texts[0].Text != "hello" {
		t.Fatal("prepared request mutated in place")
	}
	upstream, err := profile.BuildUpstream(context.Background(), transformed, PublicUpstreamInput{
		BaseURL:     "https://api.openai.com",
		APIKey:      "secret",
		RequestID:   "req-public",
		MappedModel: "provider-private-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := upstream.NewHTTPRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if httpRequest.URL.String() != "https://api.openai.com/v1/responses" ||
		httpRequest.Header.Get("Authorization") != "Bearer secret" ||
		httpRequest.Header.Get("X-Client-Request-Id") != "req-public" {
		t.Fatalf("request=%s headers=%v", httpRequest.URL, httpRequest.Header)
	}
	wire := readPublicBody(t, httpRequest)
	if wire["model"] != "provider-private-model" || wire["input"] != "redacted input" ||
		wire["instructions"] != "redacted instructions" || wire["stream"] != false || wire["store"] != false {
		t.Fatalf("wire=%v", wire)
	}
}

func TestPublicResponsesReasoningEffortRequiresExactComposedContract(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(
		modeloperation.IDResponses, modeloperation.PathResponses,
		ProfileOpenAIResponsesText, "1.0.0",
	))
	body := []byte(`{
		"model":"public-text","input":"hello","max_output_tokens":64,
		"reasoning":{"effort":"max"}
	}`)
	if _, err := profile.PrepareRequest(body, "public-text"); !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("core contract accepted reasoning: %v", err)
	}
	contract := PublicRequestContract{
		ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.2.0",
		Reasoning: &PublicReasoningRequestContract{
			RequestObjectField: "reasoning", EffortField: "effort",
			AllowedEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"},
		},
	}
	prepared, err := profile.PrepareRequest(body, "public-text", contract)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(t.Context(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.openai.com", APIKey: "secret", MappedModel: "provider-private-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := upstream.NewHTTPRequest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wire := readPublicBody(t, httpRequest)
	reasoning, ok := wire["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "max" {
		t.Fatalf("reasoning control was not preserved: %v", wire)
	}
	omitted, err := profile.PrepareRequest(
		[]byte(`{"model":"public-text","input":"hello","max_output_tokens":64}`),
		"public-text",
		contract,
	)
	if err != nil {
		t.Fatal(err)
	}
	omittedUpstream, err := profile.BuildUpstream(t.Context(), omitted, PublicUpstreamInput{
		BaseURL: "https://api.openai.com", APIKey: "secret", MappedModel: "provider-private-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	omittedRequest, err := omittedUpstream.NewHTTPRequest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if omittedWire := readPublicBody(t, omittedRequest); omittedWire["reasoning"] != nil {
		t.Fatalf("omitted reasoning acquired an injected default: %v", omittedWire)
	}
	providerBody := []byte(`{
		"id":"resp_reasoning","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null,"store":false,
		"output":[
			{"id":"rs_reasoning","type":"reasoning","status":"completed","summary":[],"content":[],"encrypted_content":"opaque-provider-state"},
			{"id":"msg_reasoning","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		],
		"usage":{"input_tokens":10,"output_tokens":4,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":14}
	}`)
	delivery, err := upstream.ParseDelivery(t.Context(), http.StatusOK, providerBody)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(delivery.Body()), "opaque-provider-state") ||
		delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, OutputTokens: 4}) {
		t.Fatalf("opaque state leaked or usage drifted: body=%s usage=%+v", delivery.Body(), delivery.CustomerUsage())
	}
	for name, replacement := range map[string]string{
		"nonempty summary": `"summary":[{"type":"summary_text","text":"secret"}]`,
		"nonempty content": `"content":[{"type":"reasoning_text","text":"secret"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			mutated := string(providerBody)
			if strings.Contains(name, "summary") {
				mutated = strings.Replace(mutated, `"summary":[]`, replacement, 1)
			} else {
				mutated = strings.Replace(mutated, `"content":[]`, replacement, 1)
			}
			if _, err := upstream.ParseDelivery(t.Context(), http.StatusOK, []byte(mutated)); err == nil {
				t.Fatalf("nonempty reasoning payload was accepted: %s", mutated)
			}
		})
	}

	for name, requestBody := range map[string]string{
		"unknown effort": `{"model":"public-text","input":"x","max_output_tokens":16,"reasoning":{"effort":"ultra"}}`,
		"summary":        `{"model":"public-text","input":"x","max_output_tokens":16,"reasoning":{"effort":"low","summary":"auto"}}`,
		"future field":   `{"model":"public-text","input":"x","max_output_tokens":16,"reasoning":{"effort":"low","future":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.PrepareRequest([]byte(requestBody), "public-text", contract); err == nil {
				t.Fatalf("unsupported reasoning request accepted: %s", requestBody)
			}
		})
	}
}

func TestPublicMessagesPrepareAdmissionTransformAndBuild(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepared, err := profile.PrepareRequest([]byte(`{
		"model":"public-claude",
		"max_tokens":80,
		"system":"be brief",
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi"}
		]
	}`), "public-claude")
	if err != nil {
		t.Fatal(err)
	}
	admission := prepared.Admission()
	if admission.EndpointKind != EndpointMessages || admission.ExplicitMaxOutputTokens != 80 ||
		admission.CachePolicy != PublicCachePolicyBypass || len(admission.Texts) != 3 ||
		admission.Texts[0].Path != "system" || admission.Texts[2].Path != "messages[1].content" {
		t.Fatalf("admission=%+v", admission)
	}
	transformed, err := prepared.WithTexts([]string{"safe system", "safe user", "safe assistant"})
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), transformed, PublicUpstreamInput{
		BaseURL:     "https://api.anthropic.com",
		APIKey:      "secret",
		MappedModel: "provider-private-claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := upstream.NewHTTPRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if httpRequest.URL.String() != "https://api.anthropic.com/v1/messages" ||
		httpRequest.Header.Get("x-api-key") != "secret" ||
		httpRequest.Header.Get("anthropic-version") != anthropicMessagesAPIVersion ||
		httpRequest.Header.Get("Authorization") != "" {
		t.Fatalf("request=%s headers=%v", httpRequest.URL, httpRequest.Header)
	}
	wire := readPublicBody(t, httpRequest)
	messages := wire["messages"].([]any)
	if wire["model"] != "provider-private-claude" || wire["system"] != "safe system" ||
		messages[0].(map[string]any)["content"] != "safe user" || wire["stream"] != false {
		t.Fatalf("wire=%v", wire)
	}
}

func TestPublicMessagesSonnet5ExactContractRejectsUnsupportedRequestSemantics(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	contract := sonnet5PublicMessagesContract()
	valid := []byte(`{"model":"public-claude","max_tokens":128000,"messages":[{"role":"user","content":"hello"}]}`)
	prepared, err := profile.PrepareRequest(valid, "public-claude", contract)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.RequestContract().ExecutionProfileVersion != "1.1.0" {
		t.Fatalf("contract=%+v", prepared.RequestContract())
	}

	for name, body := range map[string]string{
		"temperature":       `{"model":"public-claude","max_tokens":64,"temperature":0.5,"messages":[{"role":"user","content":"hello"}]}`,
		"top p":             `{"model":"public-claude","max_tokens":64,"top_p":0.5,"messages":[{"role":"user","content":"hello"}]}`,
		"manual thinking":   `{"model":"public-claude","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":1000},"messages":[{"role":"user","content":"hello"}]}`,
		"assistant prefill": `{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"The answer is"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.PrepareRequest([]byte(body), "public-claude", contract); err == nil {
				t.Fatalf("unsupported Sonnet 5 request accepted: %s", body)
			}
		})
	}
}

func TestPublicMessagesSonnet5ControlsAreTypedPreservedAndGuardStable(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	contract := sonnet5ControlsPublicMessagesContract()
	body := []byte(`{
		"model":"public-claude","max_tokens":128000,
		"thinking":{"type":"disabled"},"output_config":{"effort":"max"},
		"messages":[{"role":"user","content":"hello"}]
	}`)
	prepared, err := profile.PrepareRequest(body, "public-claude", contract)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.InvocationSemantics(), PublicInvocationSemantics{
		ReasoningControlExplicit: true, ReasoningControlEffective: "disabled",
		ReasoningEffortExplicit: true, ReasoningEffortEffective: "max",
	}) {
		t.Fatalf("semantics=%+v", prepared.InvocationSemantics())
	}
	replaced, err := prepared.WithTexts([]string{"safe user"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replaced.InvocationSemantics(), prepared.InvocationSemantics()) {
		t.Fatalf("guard replacement changed semantics: before=%+v after=%+v", prepared.InvocationSemantics(), replaced.InvocationSemantics())
	}
	upstream, err := profile.BuildUpstream(context.Background(), replaced, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := upstream.NewHTTPRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wire := readPublicBody(t, httpRequest)
	thinking, _ := wire["thinking"].(map[string]any)
	outputConfig, _ := wire["output_config"].(map[string]any)
	messages, _ := wire["messages"].([]any)
	if wire["model"] != "claude-sonnet-5" || thinking["type"] != "disabled" ||
		outputConfig["effort"] != "max" || messages[0].(map[string]any)["content"] != "safe user" {
		t.Fatalf("wire=%v", wire)
	}
}

func TestPublicMessagesOpus5DisabledThinkingRequiresHighOrLowerEffort(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	contract := opus5ControlsPublicMessagesContract()
	for name, body := range map[string]string{
		"provider defaults":     `{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`,
		"disabled default high": `{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hello"}]}`,
		"disabled low":          `{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"output_config":{"effort":"low"},"messages":[{"role":"user","content":"hello"}]}`,
		"adaptive max":          `{"model":"public-claude","max_tokens":64,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"messages":[{"role":"user","content":"hello"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.PrepareRequest([]byte(body), "public-claude", contract); err != nil {
				t.Fatalf("valid Opus controls rejected: %v", err)
			}
		})
	}
	for _, effort := range []string{"xhigh", "max"} {
		t.Run("disabled "+effort, func(t *testing.T) {
			body := `{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"output_config":{"effort":"` + effort + `"},"messages":[{"role":"user","content":"hello"}]}`
			if _, err := profile.PrepareRequest([]byte(body), "public-claude", contract); err == nil {
				t.Fatalf("incompatible Opus controls accepted: %s", body)
			}
		})
	}
}

func TestPublicMessagesSonnet5ControlsRejectUnknownNullAndLegacyShapes(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	contract := sonnet5ControlsPublicMessagesContract()
	for name, body := range map[string]string{
		"null thinking":      `{"model":"public-claude","max_tokens":64,"thinking":null,"messages":[{"role":"user","content":"hello"}]}`,
		"empty thinking":     `{"model":"public-claude","max_tokens":64,"thinking":{},"messages":[{"role":"user","content":"hello"}]}`,
		"manual thinking":    `{"model":"public-claude","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`,
		"thinking display":   `{"model":"public-claude","max_tokens":64,"thinking":{"type":"adaptive","display":"summarized"},"messages":[{"role":"user","content":"hello"}]}`,
		"null output config": `{"model":"public-claude","max_tokens":64,"output_config":null,"messages":[{"role":"user","content":"hello"}]}`,
		"invalid effort":     `{"model":"public-claude","max_tokens":64,"output_config":{"effort":"ultra"},"messages":[{"role":"user","content":"hello"}]}`,
		"structured output":  `{"model":"public-claude","max_tokens":64,"output_config":{"effort":"high","format":{"type":"json_schema"}},"messages":[{"role":"user","content":"hello"}]}`,
		"sampling override":  `{"model":"public-claude","max_tokens":64,"top_p":0.9,"messages":[{"role":"user","content":"hello"}]}`,
		"assistant prefill":  `{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"prefill"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.PrepareRequest([]byte(body), "public-claude", contract); err == nil {
				t.Fatalf("invalid Sonnet controls accepted: %s", body)
			}
		})
	}
}

func TestPublicMessagesSonnet5OmittedThinkingEvidenceIsTypedAndNotGuardText(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", sonnet5ControlsPublicMessagesContract(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.InvocationSemantics(), PublicInvocationSemantics{
		ReasoningControlEffective: "adaptive", ReasoningEffortEffective: "high",
	}) {
		t.Fatalf("omitted controls lost provider defaults: %+v", prepared.InvocationSemantics())
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(`{
		"id":"msg_thinking","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[
			{"type":"thinking","thinking":"","signature":"opaque-signature"},
			{"type":"text","text":"answer"}
		],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":3}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, OutputTokens: 7}) {
		t.Fatalf("usage=%+v", delivery.CustomerUsage())
	}
	if texts := delivery.Texts(); len(texts) != 1 || texts[0] != (PublicText{Path: "content[1].text", Text: "answer"}) {
		t.Fatalf("guard texts=%+v", texts)
	}
	replaced, err := delivery.WithTexts([]string{"safe answer"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replaced.Body()), `"signature":"opaque-signature"`) ||
		!strings.Contains(string(replaced.Body()), `"text":"safe answer"`) ||
		!strings.Contains(string(delivery.Body()), `"text":"answer"`) {
		t.Fatalf("thinking evidence or guard replacement drifted: original=%s replaced=%s", delivery.Body(), replaced.Body())
	}
	historicalPrepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", sonnet5PublicMessagesContract(),
	)
	if err != nil {
		t.Fatal(err)
	}
	historicalUpstream, err := profile.BuildUpstream(context.Background(), historicalPrepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := historicalUpstream.ParseDelivery(context.Background(), http.StatusOK, []byte(`{
		"id":"msg_thinking","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[{"type":"thinking","thinking":"","signature":"opaque-signature"},{"type":"text","text":"answer"}],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":7}
	}`)); err == nil {
		t.Fatal("historical response contract accepted adaptive-thinking evidence")
	}
	if _, err := historicalUpstream.ParseDelivery(context.Background(), http.StatusOK, []byte(`{
		"id":"msg_redacted","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[{"type":"redacted_thinking","data":"opaque-redacted-data"},{"type":"text","text":"answer"}],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":7}
	}`)); err == nil {
		t.Fatal("historical response contract accepted redacted-thinking evidence")
	}
}

func TestPublicMessagesSonnet5DisabledThinkingRejectsContradictoryResponseEvidence(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", sonnet5ControlsPublicMessagesContract(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.InvocationSemantics().ReasoningControlEffective != "disabled" {
		t.Fatalf("semantics=%+v", prepared.InvocationSemantics())
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := `{
		"id":"msg_disabled","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[{"type":"text","text":"answer"}],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":0}}
	}`
	if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(base)); err != nil {
		t.Fatalf("disabled response without thinking evidence rejected: %v", err)
	}

	for name, body := range map[string]string{
		"thinking block": strings.Replace(
			base,
			`"content":[{"type":"text","text":"answer"}]`,
			`"content":[{"type":"thinking","thinking":"","signature":"opaque"},{"type":"text","text":"answer"}]`,
			1,
		),
		"redacted thinking block": strings.Replace(
			base,
			`"content":[{"type":"text","text":"answer"}]`,
			`"content":[{"type":"redacted_thinking","data":"opaque-redacted-data"},{"type":"text","text":"answer"}]`,
			1,
		),
		"positive thinking tokens": strings.Replace(base, `"thinking_tokens":0`, `"thinking_tokens":3`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(body)); err == nil {
				t.Fatalf("disabled thinking accepted contradictory evidence: %s", body)
			}
		})
	}
}

func TestPublicMessagesHaiku45ProviderDefaultDisabledRejectsThinkingEvidence(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", haiku45ThinkingPublicMessagesContract(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.InvocationSemantics().ReasoningControlEffective != "disabled" {
		t.Fatalf("semantics=%+v", prepared.InvocationSemantics())
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-haiku-4-5-20251001",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := `{
		"id":"msg_disabled","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001",
		"content":[{"type":"text","text":"answer"}],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":0}}
	}`
	if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(base)); err != nil {
		t.Fatalf("default-disabled response rejected: %v", err)
	}
	for name, body := range map[string]string{
		"regular": strings.Replace(
			base,
			`"content":[{"type":"text","text":"answer"}]`,
			`"content":[{"type":"thinking","thinking":"summary","signature":"opaque"},{"type":"text","text":"answer"}]`,
			1,
		),
		"redacted": strings.Replace(
			base,
			`"content":[{"type":"text","text":"answer"}]`,
			`"content":[{"type":"redacted_thinking","data":"opaque-redacted-data"},{"type":"text","text":"answer"}]`,
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(body)); err == nil {
				t.Fatalf("default-disabled Haiku accepted thinking evidence: %s", body)
			}
		})
	}
}

func TestPublicMessagesSonnet5ResponseMetadataIsExactProfileEvidenceOnly(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepare := func(t *testing.T, contract PublicRequestContract) *PublicUpstreamRequest {
		t.Helper()
		prepared, err := profile.PrepareRequest(
			[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
			"public-claude", contract,
		)
		if err != nil {
			t.Fatal(err)
		}
		upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
			BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
		})
		if err != nil {
			t.Fatal(err)
		}
		return upstream
	}
	body := []byte(`{
		"id":"msg_metadata","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[{"type":"text","text":"answer"}],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":7,"inference_geo":"global","service_tier":"standard"}
	}`)
	current := prepare(t, sonnet5ControlsPublicMessagesContract())
	delivery, err := current.ParseDelivery(context.Background(), http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, OutputTokens: 7}) ||
		!strings.Contains(string(delivery.Body()), `"inference_geo":"global"`) ||
		!strings.Contains(string(delivery.Body()), `"service_tier":"standard"`) {
		t.Fatalf("metadata or inclusive usage lost: body=%s usage=%+v", delivery.Body(), delivery.CustomerUsage())
	}

	historical := prepare(t, sonnet5PublicMessagesContract())
	if _, err := historical.ParseDelivery(context.Background(), http.StatusOK, body); err == nil {
		t.Fatal("historical response contract accepted new response metadata")
	}
	for name, replacement := range map[string]string{
		"empty inference geo":  `"inference_geo":""`,
		"unknown service tier": `"service_tier":"future"`,
	} {
		t.Run(name, func(t *testing.T) {
			mutated := string(body)
			if strings.Contains(replacement, "inference_geo") {
				mutated = strings.Replace(mutated, `"inference_geo":"global"`, replacement, 1)
			} else {
				mutated = strings.Replace(mutated, `"service_tier":"standard"`, replacement, 1)
			}
			if _, err := current.ParseDelivery(context.Background(), http.StatusOK, []byte(mutated)); err == nil {
				t.Fatalf("invalid response metadata accepted: %s", mutated)
			}
		})
	}
}

func TestPublicMessagesHaiku45ManualThinkingBudgetIsExactAndPreserved(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	contract := haiku45ThinkingPublicMessagesContract()

	for name, body := range map[string]string{
		"provider default disabled": `{"model":"public-claude","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}`,
		"explicit disabled":         `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hello"}]}`,
		"minimum budget":            `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`,
		"profile maximum budget":    `{"model":"public-claude","max_tokens":64000,"thinking":{"type":"enabled","budget_tokens":63999},"messages":[{"role":"user","content":"hello"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			prepared, err := profile.PrepareRequest([]byte(body), "public-claude", contract)
			if err != nil {
				t.Fatalf("valid Haiku thinking request rejected: %v", err)
			}
			if name == "minimum budget" {
				semantics := prepared.InvocationSemantics()
				if !semantics.ReasoningControlExplicit || semantics.ReasoningControlEffective != "enabled" ||
					!semantics.ReasoningBudgetExplicit || semantics.ReasoningBudgetEffective != 1024 {
					t.Fatalf("semantics=%+v", semantics)
				}
				upstream, buildErr := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
					BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-haiku-4-5-20251001",
				})
				if buildErr != nil {
					t.Fatal(buildErr)
				}
				httpRequest, requestErr := upstream.NewHTTPRequest(context.Background())
				if requestErr != nil {
					t.Fatal(requestErr)
				}
				wire := readPublicBody(t, httpRequest)
				thinking, ok := wire["thinking"].(map[string]any)
				if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(1024) {
					t.Fatalf("thinking wire=%#v", wire["thinking"])
				}
			}
		})
	}

	for name, body := range map[string]string{
		"null thinking":        `{"model":"public-claude","max_tokens":4096,"thinking":null,"messages":[{"role":"user","content":"hello"}]}`,
		"adaptive thinking":    `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"hello"}]}`,
		"missing budget":       `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`,
		"below minimum":        `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1023},"messages":[{"role":"user","content":"hello"}]}`,
		"equal to max tokens":  `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":4096},"messages":[{"role":"user","content":"hello"}]}`,
		"above profile max":    `{"model":"public-claude","max_tokens":64001,"thinking":{"type":"enabled","budget_tokens":64000},"messages":[{"role":"user","content":"hello"}]}`,
		"decimal budget":       `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024.0},"messages":[{"role":"user","content":"hello"}]}`,
		"disabled with budget": `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"disabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`,
		"display not modeled":  `{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024,"display":"summarized"},"messages":[{"role":"user","content":"hello"}]}`,
		"effort not modeled":   `{"model":"public-claude","max_tokens":4096,"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hello"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.PrepareRequest([]byte(body), "public-claude", contract); err == nil {
				t.Fatalf("invalid Haiku thinking request accepted: %s", body)
			}
		})
	}
}

func TestPublicMessagesHaiku45SummarizedThinkingIsGuardedDeliveryText(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	contract := haiku45ThinkingPublicMessagesContract()
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", contract,
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-haiku-4-5-20251001",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{
		"id":"msg_haiku","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001",
		"content":[{"type":"thinking","thinking":"summary","signature":"opaque-signature"},{"type":"text","text":"answer"}],
		"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":12,"output_tokens_details":{"thinking_tokens":7},"inference_geo":"global","service_tier":"standard"}
	}`)
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, OutputTokens: 12}) {
		t.Fatalf("usage=%+v", delivery.CustomerUsage())
	}
	if texts := delivery.Texts(); !slices.Equal(texts, []PublicText{
		{Path: "content[0].thinking", Text: "summary"},
		{Path: "content[1].text", Text: "answer"},
	}) {
		t.Fatalf("guard texts=%+v", texts)
	}
	replaced, err := delivery.WithTexts([]string{"safe summary", "safe answer"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replaced.Body()), `"thinking":"safe summary"`) ||
		!strings.Contains(string(replaced.Body()), `"text":"safe answer"`) ||
		!strings.Contains(string(replaced.Body()), `"signature":"opaque-signature"`) ||
		!strings.Contains(string(delivery.Body()), `"thinking":"summary"`) {
		t.Fatalf("thinking guard replacement drifted: original=%s replaced=%s", delivery.Body(), replaced.Body())
	}
	redactedBody := []byte(strings.Replace(
		string(body),
		`{"type":"thinking","thinking":"summary","signature":"opaque-signature"}`,
		`{"type":"redacted_thinking","data":"opaque-redacted-data"}`,
		1,
	))
	redacted, err := upstream.ParseDelivery(context.Background(), http.StatusOK, redactedBody)
	if err != nil {
		t.Fatalf("valid redacted thinking evidence rejected: %v", err)
	}
	if texts := redacted.Texts(); !slices.Equal(texts, []PublicText{{Path: "content[1].text", Text: "answer"}}) ||
		!strings.Contains(string(redacted.Body()), `"data":"opaque-redacted-data"`) {
		t.Fatalf("redacted evidence crossed text guard or was lost: texts=%+v body=%s", texts, redacted.Body())
	}
	redactedReplaced, err := redacted.WithTexts([]string{"safe answer"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(redactedReplaced.Body()), `"data":"opaque-redacted-data"`) ||
		!strings.Contains(string(redactedReplaced.Body()), `"text":"safe answer"`) ||
		strings.Contains(string(redactedReplaced.Body()), `"text":"answer"`) {
		t.Fatalf("redacted opaque data or text replacement drifted: %s", redactedReplaced.Body())
	}

	for name, mutated := range map[string]string{
		"missing thinking": strings.Replace(string(body), `{"type":"thinking","thinking":"summary","signature":"opaque-signature"},`, "", 1),
		"thinking after text": strings.Replace(
			string(body),
			`{"type":"thinking","thinking":"summary","signature":"opaque-signature"},{"type":"text","text":"answer"}`,
			`{"type":"text","text":"answer"},{"type":"thinking","thinking":"summary","signature":"opaque-signature"}`,
			1,
		),
		"redacted thinking after text": strings.Replace(
			string(body),
			`{"type":"thinking","thinking":"summary","signature":"opaque-signature"},{"type":"text","text":"answer"}`,
			`{"type":"thinking","thinking":"summary","signature":"opaque-signature"},{"type":"text","text":"answer"},{"type":"redacted_thinking","data":"opaque-redacted-data"}`,
			1,
		),
		"empty summary":        strings.Replace(string(body), `"thinking":"summary"`, `"thinking":""`, 1),
		"zero thinking tokens": strings.Replace(string(body), `"thinking_tokens":7`, `"thinking_tokens":0`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(mutated)); err == nil {
				t.Fatalf("invalid Haiku thinking response accepted: %s", mutated)
			}
		})
	}
}

func TestPublicMessagesAdaptiveProfilesPreserveRedactedThinkingAsOpaqueEvidence(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	for _, test := range []struct {
		name          string
		providerModel string
		contract      PublicRequestContract
	}{
		{name: "sonnet", providerModel: "claude-sonnet-5", contract: sonnet5ControlsPublicMessagesContract()},
		{name: "opus", providerModel: "claude-opus-5", contract: opus5ControlsPublicMessagesContract()},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := profile.PrepareRequest(
				[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
				"public-claude", test.contract,
			)
			if err != nil {
				t.Fatal(err)
			}
			upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
				BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: test.providerModel,
			})
			if err != nil {
				t.Fatal(err)
			}
			body := []byte(`{
				"id":"msg_redacted","type":"message","role":"assistant","model":"` + test.providerModel + `",
				"content":[{"type":"redacted_thinking","data":"opaque-redacted-data"},{"type":"text","text":"answer"}],
				"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":3}}
			}`)
			delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body)
			if err != nil {
				t.Fatal(err)
			}
			if texts := delivery.Texts(); !slices.Equal(texts, []PublicText{{Path: "content[1].text", Text: "answer"}}) ||
				!strings.Contains(string(delivery.Body()), `"data":"opaque-redacted-data"`) {
				t.Fatalf("redacted evidence crossed text guard or was lost: texts=%+v body=%s", texts, delivery.Body())
			}
		})
	}
}

func TestPublicMessagesSonnet5RefusalIsSuccessfulTypedDelivery(t *testing.T) {
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude", sonnet5PublicMessagesContract(),
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{
		"id":"msg_refusal","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[],"stop_reason":"refusal","stop_sequence":null,
		"stop_details":{"type":"refusal","category":"cyber","explanation":"request declined"},
		"usage":{"input_tokens":10,"output_tokens":1}
	}`)
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.ProviderObject() != "msg_refusal" ||
		delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, OutputTokens: 1}) ||
		!strings.Contains(string(delivery.Body()), `"stop_reason":"refusal"`) ||
		!strings.Contains(string(delivery.Body()), `"model":"public-claude"`) {
		t.Fatalf("delivery body=%s usage=%+v", delivery.Body(), delivery.CustomerUsage())
	}
	if texts := delivery.Texts(); len(texts) != 1 || texts[0] != (PublicText{
		Path: "stop_details.explanation", Text: "request declined",
	}) {
		t.Fatalf("refusal guard texts=%+v", texts)
	}
	replaced, err := delivery.WithTexts([]string{"safe refusal explanation"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replaced.Body()), `"explanation":"safe refusal explanation"`) ||
		!strings.Contains(string(delivery.Body()), `"explanation":"request declined"`) {
		t.Fatalf("refusal replacement mutated source or was not delivered: original=%s replaced=%s", delivery.Body(), replaced.Body())
	}
	if _, err := delivery.WithTexts([]string{""}); err == nil {
		t.Fatal("empty refusal explanation replacement was accepted")
	}

	historical, historicalUpstream := buildPublicMessages(t)
	_ = historical
	if _, err := historicalUpstream.ParseDelivery(context.Background(), http.StatusOK, body); err == nil {
		t.Fatal("historical zero-value public Messages contract accepted Sonnet 5 stop_details")
	}

	for name, mutated := range map[string]string{
		"missing details": strings.Replace(string(body), `"stop_details":{"type":"refusal","category":"cyber","explanation":"request declined"},`, "", 1),
		"wrong type":      strings.Replace(string(body), `"type":"refusal","category"`, `"type":"future","category"`, 1),
		"non-null stop":   strings.Replace(string(body), `"stop_sequence":null`, `"stop_sequence":"x"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(mutated)); err == nil {
				t.Fatalf("invalid refusal accepted: %s", mutated)
			}
		})
	}
}

func TestPublicResponsesStrictDeliveryRewritesModelAndMapsUsage(t *testing.T) {
	profile, upstream := buildPublicResponses(t)
	body := []byte(`{
		"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null,
		"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],
		"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":14}
	}`)
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.EndpointKind() != EndpointResponses || delivery.ProviderObject() != "resp_1" ||
		delivery.CachePolicy() != PublicCachePolicyBypass || delivery.CustomerUsage() != (PublicCustomerUsage{
		InputTokens: 10, CacheReadTokens: 3, CacheWriteTokens: 2, OutputTokens: 4,
	}) {
		t.Fatalf("delivery usage=%+v", delivery.CustomerUsage())
	}
	if got := delivery.Texts(); len(got) != 1 || got[0].Text != "answer" {
		t.Fatalf("texts=%v", got)
	}
	assertPublicBodyModel(t, delivery.Body(), "public-text", "provider-private-model")
	replaced, err := delivery.WithTexts([]string{"safe answer"})
	if err != nil {
		t.Fatal(err)
	}
	var normalized struct {
		Model      string `json:"model"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(replaced.Body(), &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.Model != "public-text" || normalized.OutputText != "safe answer" || normalized.Output[0].Content[0].Text != "safe answer" {
		t.Fatalf("normalized=%+v", normalized)
	}

	// The historical contract intentionally tolerates future evidence fields;
	// the public delivery contract must reject the same raw provider payload.
	withUnknownRoot := strings.Replace(string(body), `"usage":`, `"future_root":true,"usage":`, 1)
	if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(withUnknownRoot)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("unknown public root accepted: %v", err)
	}
	if _, err := profile.PrepareRequest([]byte(`{"model":"public-text","input":"x","max_output_tokens":16,"future":true}`), "public-text"); !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("unknown request semantics accepted: %v", err)
	}
}

func TestPublicResponsesAcceptsDocumentedOpenAIEnvelopeButProjectsOnlyCoreText(t *testing.T) {
	_, upstream := buildPublicResponses(t)
	body := []byte(`{
		"id":"resp_openai","object":"response","created_at":1787610000,"completed_at":1787610001,
		"status":"completed","background":false,"billing":null,"error":null,"incomplete_details":null,
		"instructions":null,"max_output_tokens":64,"max_tool_calls":null,"model":"provider-private-model",
		"output":[
			{"id":"rs_openai","type":"reasoning","status":"completed","summary":[],"content":[],"encrypted_content":null},
			{"id":"msg_openai","type":"message","status":"completed","role":"assistant","phase":"final_answer",
			 "content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}
		],
		"parallel_tool_calls":true,"previous_response_id":null,
		"reasoning":{"effort":"medium","summary":null},"service_tier":"default","store":false,
		"temperature":1,"text":{"format":{"type":"text"}},"tool_choice":"auto","tools":[],
		"top_logprobs":0,"top_p":1,"truncation":"disabled","user":null,"metadata":{},
		"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},
		         "output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":14}
	}`)
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, CacheReadTokens: 3, CacheWriteTokens: 2, OutputTokens: 4}) {
		t.Fatalf("usage=%+v", delivery.CustomerUsage())
	}
	var projected struct {
		Model  string `json:"model"`
		Output []struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
		} `json:"output"`
		ServiceTier json.RawMessage `json:"service_tier"`
		CreatedAt   json.RawMessage `json:"created_at"`
	}
	if err := json.Unmarshal(delivery.Body(), &projected); err != nil {
		t.Fatal(err)
	}
	if projected.Model != "public-text" || len(projected.Output) != 1 ||
		projected.Output[0].Type != "message" || projected.Output[0].Phase != "final_answer" ||
		len(projected.ServiceTier) != 0 || len(projected.CreatedAt) != 0 {
		t.Fatalf("unsafe or incomplete core projection: %+v body=%s", projected, delivery.Body())
	}
}

func TestPublicResponsesRejectsNonCoreSemanticsInDocumentedEnvelope(t *testing.T) {
	_, upstream := buildPublicResponses(t)
	base := `{
		"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null,"store":false,"tools":[],
		"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant",
		           "content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}],
		"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}
	}`
	for name, mutated := range map[string]string{
		"stored state":        strings.Replace(base, `"store":false`, `"store":true`, 1),
		"provider tool":       strings.Replace(base, `"tools":[]`, `"tools":[{"type":"web_search"}]`, 1),
		"citation annotation": strings.Replace(base, `"annotations":[]`, `"annotations":[{"type":"url_citation"}]`, 1),
		"logprobs":            strings.Replace(base, `"logprobs":[]`, `"logprobs":[{"token":"x"}]`, 1),
		"context management":  strings.Replace(base, `"tools":[]`, `"tools":[],"context_management":[{"type":"compaction"}]`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(mutated)); !errors.Is(err, ErrUnsupportedSemantics) {
				t.Fatalf("non-core response accepted: error=%v body=%s", err, mutated)
			}
		})
	}
}

func TestPublicResponsesRejectsMalformedQuarantinedFieldsAsInvalidResponse(t *testing.T) {
	_, upstream := buildPublicResponses(t)
	base := `{
		"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null,"store":false,"tools":[],
		"output":[
			{"id":"rs_1","type":"reasoning","status":"completed","summary":[],"content":[],"encrypted_content":null},
			{"id":"msg_1","type":"message","status":"completed","role":"assistant",
			 "content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}
		],
		"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}
	}`
	for name, mutated := range map[string]string{
		"service tier object":       strings.Replace(base, `"store":false`, `"service_tier":{},"store":false`, 1),
		"created at string":         strings.Replace(base, `"store":false`, `"created_at":"bad","store":false`, 1),
		"parallel tools array":      strings.Replace(base, `"tools":[]`, `"parallel_tool_calls":[],"tools":[]`, 1),
		"reasoning string":          strings.Replace(base, `"store":false`, `"reasoning":"bad","store":false`, 1),
		"reasoning phase object":    strings.Replace(base, `"encrypted_content":null`, `"encrypted_content":null,"phase":{}`, 1),
		"context management object": strings.Replace(base, `"tools":[]`, `"tools":[],"context_management":{}`, 1),
		"store string":              strings.Replace(base, `"store":false`, `"store":"false"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(mutated)); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("malformed response field was not invalid: error=%v body=%s", err, mutated)
			}
		})
	}
}

func TestPublicResponsesDeliveryFailsClosedOnUnknownNestedUsageAndCacheOverlap(t *testing.T) {
	_, upstream := buildPublicResponses(t)
	template := `{
		"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null,
		"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer"}%s]}],
		"usage":%s
	}`
	tests := []struct {
		name      string
		itemExtra string
		usage     string
	}{
		{name: "unknown output item", itemExtra: `,"future_item":true`, usage: `{"input_tokens":10,"output_tokens":1,"total_tokens":11}`},
		{name: "unknown usage", usage: `{"input_tokens":10,"output_tokens":1,"total_tokens":11,"future_counter":1}`},
		{name: "duplicate usage", usage: `{"input_tokens":10,"input_tokens":10,"output_tokens":1,"total_tokens":11}`},
		{name: "overlapping cache exceeds input", usage: `{"input_tokens":10,"input_tokens_details":{"cached_tokens":8,"cache_write_tokens":3},"output_tokens":1,"total_tokens":11}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(strings.Replace(strings.Replace(template, "%s", test.itemExtra, 1), "%s", test.usage, 1))
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body); err == nil {
				t.Fatalf("unsafe response accepted: %s", body)
			}
		})
	}
}

func TestPublicMessagesStrictDeliveryRewritesModelAndNormalizesUsage(t *testing.T) {
	_, upstream := buildPublicMessages(t)
	body := []byte(`{
		"id":"msg_1","type":"message","role":"assistant","model":"provider-private-claude",
		"content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","stop_sequence":null,
		"usage":{"input_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":80,"output_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}
	}`)
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, body)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 130, CacheReadTokens: 80, CacheWriteTokens: 30, OutputTokens: 5}) {
		t.Fatalf("usage=%+v", delivery.CustomerUsage())
	}
	assertPublicBodyModel(t, delivery.Body(), "public-claude", "provider-private-claude")

	for name, mutated := range map[string]string{
		"unknown root":    strings.Replace(string(body), `"usage":`, `"future_root":true,"usage":`, 1),
		"unknown content": strings.Replace(string(body), `"text":"answer"`, `"text":"answer","citations":[]`, 1),
		"unknown usage":   strings.Replace(string(body), `"output_tokens":5`, `"output_tokens":5,"future_counter":1`, 1),
		"duplicate usage": strings.Replace(string(body), `"output_tokens":5`, `"output_tokens":5,"output_tokens":5`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(mutated)); err == nil {
				t.Fatalf("unsafe response accepted: %s", mutated)
			}
		})
	}
}

func TestPublicDeliveryRejectsUsageBeyondPersistedChargeContract(t *testing.T) {
	_, responses := buildPublicResponses(t)
	_, messages := buildPublicMessages(t)
	tests := []struct {
		name     string
		upstream *PublicUpstreamRequest
		body     string
	}{
		{
			name:     "responses",
			upstream: responses,
			body: `{
				"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
				"error":null,"incomplete_details":null,
				"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],
				"usage":{"input_tokens":3000000000,"output_tokens":0,"total_tokens":3000000000}
			}`,
		},
		{
			name:     "messages",
			upstream: messages,
			body: `{
				"id":"msg_1","type":"message","role":"assistant","model":"provider-private-claude",
				"content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":3000000000,"output_tokens":0}
			}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delivery, err := test.upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(test.body))
			if !errors.Is(err, ErrInvalidResponse) || delivery.Body() != nil || delivery.CustomerUsage() != (PublicCustomerUsage{}) {
				t.Fatalf("persisted charge limit crossed: delivery=%+v usage=%+v err=%v",
					delivery, delivery.CustomerUsage(), err)
			}
		})
	}
}

func mustPublicProfile(t *testing.T, identity PublicContractIdentity) PublicNonStreamProfile {
	t.Helper()
	profile, err := NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func buildPublicResponses(t *testing.T) (PublicNonStreamProfile, *PublicUpstreamRequest) {
	t.Helper()
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDResponses, modeloperation.PathResponses, ProfileOpenAIResponsesText, "1.0.0"))
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-text","input":"hello","max_output_tokens":64}`),
		"public-text",
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.openai.com", APIKey: "secret", MappedModel: "provider-private-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile, upstream
}

func buildPublicMessages(t *testing.T) (PublicNonStreamProfile, *PublicUpstreamRequest) {
	t.Helper()
	profile := mustPublicProfile(t, publicIdentity(modeloperation.IDMessages, modeloperation.PathMessages, ProfileAnthropicMessagesText, "1.0.0"))
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude",
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "provider-private-claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile, upstream
}

func sonnet5PublicMessagesContract() PublicRequestContract {
	return PublicRequestContract{
		ExecutionProfileID: "anthropic.claude-sonnet-5.api", ExecutionProfileVersion: "1.1.0",
		AnthropicMessages: &PublicAnthropicMessagesContract{
			ForbiddenRootFields:     []string{"temperature", "top_p"},
			RequireFinalUserMessage: true,
			AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
			RefusalStopDetails:      true,
		},
	}
}

func sonnet5ControlsPublicMessagesContract() PublicRequestContract {
	contract := sonnet5PublicMessagesContract()
	contract.ExecutionProfileVersion = "1.2.0"
	contract.AnthropicMessages.ResponseInferenceGeo = true
	contract.AnthropicMessages.ResponseServiceTier = true
	contract.AnthropicMessages.Reasoning = &PublicAnthropicReasoningRequestContract{
		ThinkingObjectField: "thinking", ThinkingTypeField: "type",
		AllowedThinkingTypes: []string{"adaptive", "disabled"}, ThinkingProviderDefault: "adaptive",
		DisabledThinkingTypes:   []string{"disabled"},
		OutputConfigObjectField: "output_config", EffortField: "effort",
		AllowedEfforts: []string{"low", "medium", "high", "xhigh", "max"}, EffortProviderDefault: "high",
		ResponseThinkingTextMode: "omitted",
	}
	return contract
}

func opus5ControlsPublicMessagesContract() PublicRequestContract {
	contract := sonnet5ControlsPublicMessagesContract()
	contract.ExecutionProfileID = "anthropic.claude-opus-5.api"
	contract.ExecutionProfileVersion = "1.1.0"
	contract.AnthropicMessages.Reasoning.EffortsAllowedDisabled = []string{"low", "medium", "high"}
	return contract
}

func haiku45ThinkingPublicMessagesContract() PublicRequestContract {
	contract := sonnet5PublicMessagesContract()
	contract.ExecutionProfileID = "anthropic.claude-haiku-4.5.api"
	contract.ExecutionProfileVersion = "1.1.0"
	contract.AnthropicMessages.ResponseInferenceGeo = true
	contract.AnthropicMessages.ResponseServiceTier = true
	contract.AnthropicMessages.Reasoning = &PublicAnthropicReasoningRequestContract{
		ThinkingObjectField:      "thinking",
		ThinkingTypeField:        "type",
		AllowedThinkingTypes:     []string{"enabled", "disabled"},
		DisabledThinkingTypes:    []string{"disabled"},
		ThinkingProviderDefault:  "disabled",
		BudgetField:              "budget_tokens",
		BudgetMinimum:            1_024,
		BudgetMaximum:            63_999,
		BudgetRequiredTypes:      []string{"enabled"},
		ResponseThinkingTextMode: "summarized",
	}
	return contract
}

func readPublicBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertPublicBodyModel(t *testing.T, body []byte, want, forbidden string) {
	t.Helper()
	if strings.Contains(string(body), forbidden) {
		t.Fatalf("provider model leaked in delivery: %s", body)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["model"] != want {
		t.Fatalf("model=%v want %s body=%s", envelope["model"], want, body)
	}
}
