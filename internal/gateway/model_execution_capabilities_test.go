package gateway

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/responsecache"
)

func TestRequiredCapabilitiesForChat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "plain", body: `{"model":"m","messages":[{"role":"user","content":"hello"}]}`, want: []string{}},
		{name: "empty and null controls", body: `{"model":"m","messages":[{"role":"assistant","content":null,"tool_calls":[],"function_call":null}],"tools":[],"functions":[],"tool_choice":null,"function_call":null,"modalities":["text"],"audio":null,"stream":false,"stream_options":null}`, want: []string{}},
		{name: "explicit no tool controls", body: `{"model":"m","messages":[],"tool_choice":"none","function_call":"none"}`, want: []string{}},
		{name: "nonempty tools", body: `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}]}`, want: []string{"tools"}},
		{name: "required tool choice", body: `{"model":"m","messages":[],"tool_choice":"required"}`, want: []string{"tools"}},
		{name: "object tool choice", body: `{"model":"m","messages":[],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`, want: []string{"tools"}},
		{name: "tools and tool history", body: `{"model":"m","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"1"}]},{"role":"tool","content":"ok"}],"tools":[],"tool_choice":"auto"}`, want: []string{"tools"}},
		{name: "legacy functions", body: `{"model":"m","messages":[],"functions":[{"name":"lookup"}],"function_call":"auto"}`, want: []string{"tools"}},
		{name: "legacy function history", body: `{"model":"m","messages":[{"role":"assistant","content":null,"function_call":{"name":"lookup","arguments":"{}"}},{"role":"function","name":"lookup","content":"ok"}]}`, want: []string{"tools"}},
		{name: "parallel tools", body: `{"model":"m","messages":[],"parallel_tool_calls":true}`, want: []string{"parallel_tool_calls", "tools"}},
		{name: "json object", body: `{"model":"m","messages":[],"response_format":{"type":"json_object"}}`, want: []string{"json_object"}},
		{name: "json schema", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{}}}}`, want: []string{"json_schema"}},
		{name: "vision audio and text", body: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"x"},{"type":"image_url","image_url":{"url":"x"}},{"type":"input_audio","input_audio":{"data":"x","format":"wav"}}]}]}`, want: []string{"audio", "vision"}},
		{name: "audio output modality", body: `{"model":"m","messages":[],"modalities":["text","audio"],"audio":{"voice":"alloy","format":"wav"}}`, want: []string{"audio"}},
		{name: "reasoning and stream", body: `{"model":"m","messages":[],"reasoning_effort":"high","stream":true,"stream_options":{"include_usage":true}}`, want: []string{"reasoning", "streaming"}},
		{name: "Kimi thinking toggle", body: `{"model":"m","messages":[],"thinking":{"type":"enabled"}}`, want: []string{"reasoning"}},
		{name: "prompt cache key", body: `{"model":"m","messages":[],"prompt_cache_key":"session-1"}`, want: []string{"prompt_cache"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := requiredCapabilitiesForChat([]byte(test.body))
			if err != nil {
				t.Fatalf("derive capabilities: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("capabilities=%v want=%v", got, test.want)
			}
		})
	}
}

func TestRequiredCapabilitiesForChatRejectsKnownUnsupportedSemantics(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"model":"m","messages":[],"response_format":{"type":"future_binary"}}`,
		`{"model":"m","messages":[{"role":"user","content":[{"type":"file","file_id":"x"}]}]}`,
		`{"model":"m","messages":[],"parallel_tool_calls":"yes"}`,
		`{"model":"m","messages":[],"modalities":["text","video"]}`,
		`{"model":"m","messages":[],"audio":"wav"}`,
		`{"model":"m","messages":[],"functions":{}}`,
		`{"model":"m","messages":[],"function_call":[]}`,
		`{"model":"m","messages":[{"role":"assistant","content":null,"function_call":[]}]}`,
		`{"model":"m","messages":[],"stream":false,"stream_options":{"include_usage":true}}`,
	} {
		if got, err := requiredCapabilitiesForChat([]byte(body)); err == nil {
			t.Fatalf("expected semantic rejection, got %v for %s", got, body)
		}
	}
}

func TestRequiredCapabilitiesForChatDoesNotInferUnknownExtension(t *testing.T) {
	t.Parallel()
	got, err := requiredCapabilitiesForChat([]byte(`{"model":"m","messages":[],"provider_future_option":{"mode":"x"}}`))
	if err != nil {
		t.Fatalf("derive capabilities: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unknown extension must not invent a capability: %v", got)
	}
}

func TestCanonicalRouteCannotInventLegacyIdentityForResponseCache(t *testing.T) {
	t.Parallel()
	relay := &Relay{invocationOptimization: &invocationopt.Manager{}}
	decision := relay.evaluateResponseCache(
		t.Context(), http.Header{}, "request", "public-model", &models.ApiKey{ID: 1},
		invocationOptimizationState{found: true},
		&RouteResult{
			SnapshotSource: "canonical",
			Channel:        models.Channel{ID: 91},
			ChannelKey:     models.ChannelKey{ID: 92},
		},
		&http.Request{}, "price", nil,
	)
	if decision.reason != responsecache.ReasonInvalidConfiguration || decision.hit != nil || decision.miss != nil {
		t.Fatalf("canonical cache identity must safely bypass: %#v", decision)
	}
}
