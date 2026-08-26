package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func anthropicToolsTestContract() PublicRequestContract {
	contract := sonnet5ControlsPublicMessagesContract()
	contract.ExecutionProfileVersion = "1.4.0"
	contract.AnthropicMessages.AllowedResponseStops = []string{"end_turn", "max_tokens", "refusal", "stop_sequence", "tool_use"}
	contract.AnthropicMessages.FunctionTools = &PublicAnthropicFunctionToolsContract{
		MaxTools: 64, ToolNamePattern: `^[a-zA-Z0-9_-]{1,64}$`,
		ToolChoices: []string{"any", "auto", "none", "tool"}, ProviderDefaultChoice: "auto",
		NamedChoice: true, ParallelToolUse: true, ParallelProviderDefault: true,
		ExactHistory: true, ExactResponse: true, RequiresNonStream: true,
		ToolInputIdentityGuarded: true, ThinkingHistoryIdentityOnly: true,
	}
	return contract
}

func TestPublicAnthropicMessagesToolsV3PreparesOneTypedInvocationAuthority(t *testing.T) {
	profile := mustPublicProfile(t, anthropicMessagesV3PublicIdentity())
	prepared, err := profile.PrepareRequest([]byte(`{
		"model":"public-claude","max_tokens":1024,
		"tools":[{"name":"weather","description":"Look up a city","input_schema":{"type":"object","properties":{"city":{"type":"string","description":"City name"}},"required":["city"]}}],
		"tool_choice":{"type":"any","disable_parallel_tool_use":true},
		"messages":[{"role":"user","content":"Weather in Paris?"}]
	}`), "public-claude", anthropicToolsTestContract())
	if err != nil {
		t.Fatal(err)
	}
	wantSemantics := PublicInvocationSemantics{
		ReasoningControlEffective: "adaptive", ReasoningEffortEffective: "high",
		FunctionToolsRequested: true, DeclaredToolNames: []string{"weather"},
		ToolChoiceExplicit: true, ToolChoiceEffective: "required",
		ParallelToolUseExplicit: true,
	}
	if !reflect.DeepEqual(prepared.InvocationSemantics(), wantSemantics) {
		t.Fatalf("semantics=%+v want=%+v", prepared.InvocationSemantics(), wantSemantics)
	}
	texts := prepared.Admission().Texts
	if len(texts) != 6 || texts[0].Path != "tools[0].description" ||
		texts[1].Path != "tools[0].input_schema.properties.city.description" ||
		texts[2].Path != "tools[0].input_schema.properties.city.type" ||
		texts[5].Path != "messages[0].content" {
		t.Fatalf("texts=%+v", texts)
	}
	invalid := make([]string, len(texts))
	for index := range texts {
		invalid[index] = texts[index].Text
	}
	invalid[0] = "rewrite"
	if _, err := prepared.WithTexts(invalid); !errors.Is(err, ErrPublicTextReplacement) {
		t.Fatalf("tool definition rewrite err=%v", err)
	}
	replacements := make([]string, len(texts))
	for index := range texts {
		replacements[index] = texts[index].Text
	}
	replacements[len(replacements)-1] = "Safe weather question"
	guarded, err := prepared.WithTexts(replacements)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(guarded.InvocationSemantics(), prepared.InvocationSemantics()) {
		t.Fatal("guard replacement changed typed invocation semantics")
	}
	upstream, err := profile.BuildUpstream(context.Background(), guarded, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := upstream.NewHTTPRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wire := readPublicBody(t, request)
	choice := wire["tool_choice"].(map[string]any)
	if wire["model"] != "claude-sonnet-5" || choice["type"] != "any" ||
		choice["disable_parallel_tool_use"] != true ||
		wire["messages"].([]any)[0].(map[string]any)["content"] != "Safe weather question" {
		t.Fatalf("wire=%+v", wire)
	}
}

func TestPublicAnthropicMessagesToolsV3PreservesExactHistoryAndToolResponse(t *testing.T) {
	profile := mustPublicProfile(t, anthropicMessagesV3PublicIdentity())
	prepared, err := profile.PrepareRequest([]byte(`{
		"model":"public-claude","max_tokens":1024,
		"tools":[{"name":"weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
		"tool_choice":{"type":"tool","name":"weather","disable_parallel_tool_use":true},
		"messages":[
			{"role":"user","content":"Weather?"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"use the weather tool","signature":"opaque-signature"},
				{"type":"tool_use","id":"toolu_1","name":"weather","input":{"city":"Paris"}}
			]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Sunny"}]}
		]
	}`), "public-claude", anthropicToolsTestContract())
	if err != nil {
		t.Fatal(err)
	}
	texts := prepared.Admission().Texts
	if len(texts) != 6 {
		t.Fatalf("texts=%+v", texts)
	}
	for index, text := range texts {
		replacement := text.Text
		if text.Path == "messages[2].content[0].content" {
			replacement = "Safe result"
		}
		texts[index].Text = replacement
	}
	replacements := make([]string, len(texts))
	for index := range texts {
		replacements[index] = texts[index].Text
	}
	guarded, err := prepared.WithTexts(replacements)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), guarded, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, []byte(`{
		"id":"msg_tool","type":"message","role":"assistant","model":"claude-sonnet-5",
		"content":[
			{"type":"thinking","thinking":"","signature":"opaque-response-signature"},
			{"type":"tool_use","id":"toolu_2","name":"weather","input":{"city":"London"}}
		],
		"stop_reason":"tool_use","stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":4,"output_tokens_details":{"thinking_tokens":1}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if delivery.ProviderObject() != "msg_tool" || delivery.CustomerUsage() != (PublicCustomerUsage{InputTokens: 10, OutputTokens: 4}) {
		t.Fatalf("delivery=%+v usage=%+v", delivery, delivery.CustomerUsage())
	}
	guardTexts := delivery.Texts()
	if len(guardTexts) != 1 || guardTexts[0].Path != "content[1].input.city" || guardTexts[0].Text != "London" {
		t.Fatalf("delivery texts=%+v", guardTexts)
	}
	if _, err := delivery.WithTexts([]string{"rewrite"}); !errors.Is(err, ErrPublicTextReplacement) {
		t.Fatalf("tool input rewrite err=%v", err)
	}
	guardedDelivery, err := delivery.WithTexts([]string{"London"})
	if err != nil {
		t.Fatal(err)
	}
	var customer map[string]any
	if json.Unmarshal(guardedDelivery.Body(), &customer) != nil || customer["model"] != "public-claude" {
		t.Fatalf("customer body=%s", guardedDelivery.Body())
	}
}

func TestPublicAnthropicMessagesToolsV3RejectsChoiceAndResponseDrift(t *testing.T) {
	profile := mustPublicProfile(t, anthropicMessagesV3PublicIdentity())
	base := `"model":"public-claude","max_tokens":64,"tools":[{"name":"weather","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]`
	for name, body := range map[string]string{
		"unknown tool field": `{` + base + `,"tools":[{"name":"weather","input_schema":{"type":"object"},"strict":true}]}`,
		"undeclared named":   `{` + base + `,"tool_choice":{"type":"tool","name":"other"}}`,
		"stream tools":       `{` + base + `,"stream":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.PrepareRequest([]byte(body), "public-claude", anthropicToolsTestContract()); err == nil {
				t.Fatalf("invalid request accepted: %s", body)
			}
		})
	}

	prepare := func(t *testing.T, choice string) *PublicUpstreamRequest {
		t.Helper()
		prepared, err := profile.PrepareRequest([]byte(`{`+base+choice+`}`), "public-claude", anthropicToolsTestContract())
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
	response := func(content, stop string) []byte {
		return []byte(`{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-5","content":` + content + `,"stop_reason":"` + stop + `","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1,"output_tokens_details":{"thinking_tokens":0}}}`)
	}
	for name, test := range map[string]struct {
		choice  string
		content string
		stop    string
	}{
		"none returned tool": {`,"tool_choice":{"type":"none"}`, `[{"type":"tool_use","id":"a","name":"weather","input":{}}]`, "tool_use"},
		"named mismatch":     {`,"tool_choice":{"type":"tool","name":"weather"}`, `[{"type":"tool_use","id":"a","name":"other","input":{}}]`, "tool_use"},
		"parallel disabled":  {`,"tool_choice":{"type":"auto","disable_parallel_tool_use":true}`, `[{"type":"tool_use","id":"a","name":"weather","input":{}},{"type":"tool_use","id":"b","name":"weather","input":{}}]`, "tool_use"},
		"duplicate ids":      {``, `[{"type":"tool_use","id":"a","name":"weather","input":{}},{"type":"tool_use","id":"a","name":"weather","input":{}}]`, "tool_use"},
		"stop without tool":  {``, `[{"type":"text","text":"answer"}]`, "tool_use"},
		"tool without stop":  {``, `[{"type":"tool_use","id":"a","name":"weather","input":{}}]`, "end_turn"},
		"any without tool":   {`,"tool_choice":{"type":"any"}`, `[{"type":"text","text":"answer"}]`, "end_turn"},
		"named without tool": {`,"tool_choice":{"type":"tool","name":"weather"}`, `[{"type":"text","text":"answer"}]`, "end_turn"},
	} {
		t.Run(name, func(t *testing.T) {
			upstream := prepare(t, test.choice)
			if delivery, err := upstream.ParseDelivery(context.Background(), http.StatusOK, response(test.content, test.stop)); err == nil || delivery.Body() != nil {
				t.Fatalf("invalid response accepted: delivery=%s err=%v", delivery.Body(), err)
			}
		})
	}
}

func TestAnthropicMessagesV3FixtureProfileUsesTheTypedToolsCodec(t *testing.T) {
	profile, err := NewRegistry().Resolve(ProfileAnthropicMessagesTextV3, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	request, binding, err := profile.BuildRequest(context.Background(), BuildInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", RequestedModel: "public-claude",
		MappedModel: "claude-sonnet-5", Body: []byte(`{"model":"public-claude","max_tokens":64,"tools":[{"name":"weather","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer request.Body.Close()
	if _, err := io.ReadAll(request.Body); err != nil || binding.profileID != ProfileAnthropicMessagesTextV3 {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, []byte(`{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"a","name":"weather","input":{}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`))
	if err != nil || outcome.ProfileID != ProfileAnthropicMessagesTextV3 || outcome.ProviderObject != "msg" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}
