package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestAnthropicMessagesV2FreezesDualTransportWithoutUpgradingV1(t *testing.T) {
	profile, err := NewRegistry().Resolve(ProfileAnthropicMessagesTextV2, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	base := `"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]`
	for _, test := range []struct {
		name      string
		streamRaw string
		streaming bool
	}{
		{name: "omitted", streamRaw: ""},
		{name: "false", streamRaw: `,"stream":false`},
		{name: "true", streamRaw: `,"stream":true`, streaming: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, binding, err := profile.BuildRequest(context.Background(), BuildInput{
				BaseURL: "https://api.anthropic.example", APIKey: "secret",
				RequestedModel: "public-claude", MappedModel: "provider-private-claude",
				Body: []byte("{" + base + test.streamRaw + "}"),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer request.Body.Close()
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if json.Unmarshal(body, &wire) != nil || wire["stream"] != test.streaming ||
				binding.streaming != test.streaming || binding.profileID != ProfileAnthropicMessagesTextV2 {
				t.Fatalf("wire=%+v binding=%+v", wire, binding)
			}
			response := []byte(`{
				"id":"msg_1","type":"message","role":"assistant","model":"provider-private-claude",
				"content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","stop_sequence":null,
				"usage":{"input_tokens":2,"output_tokens":1}
			}`)
			outcome, err := profile.ParseResponse(context.Background(), binding, http.StatusOK, response)
			if test.streaming {
				if !errors.Is(err, ErrInvalidConfiguration) {
					t.Fatalf("stream response parser err=%v", err)
				}
				return
			}
			if err != nil || outcome.ProfileID != ProfileAnthropicMessagesTextV2 || outcome.ProviderObject != "msg_1" {
				t.Fatalf("outcome=%+v err=%v", outcome, err)
			}
		})
	}

	historical, err := NewRegistry().Resolve(ProfileAnthropicMessagesText, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := historical.BuildRequest(context.Background(), BuildInput{
		BaseURL: "https://api.anthropic.example", APIKey: "secret",
		RequestedModel: "public-claude", MappedModel: "provider-private-claude",
		Body: []byte("{" + base + `,"stream":true}`),
	}); !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("historical v1 accepted streaming: %v", err)
	}
}
