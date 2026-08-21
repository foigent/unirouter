package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
