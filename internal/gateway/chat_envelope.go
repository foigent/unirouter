package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/tokenutil"
)

// ChatCompletionRequest is the routing and admission envelope for an OpenAI
// Chat Completions request. The gateway deliberately models only fields it must
// understand. The original request body remains the provider adapter input so
// fields are not silently lost through this projection; the versioned
// admission contract separately rejects any field whose forwarding semantics
// cannot be bounded before planning and dispatch.
type ChatCompletionRequest struct {
	Model               string    `json:"model"`
	Messages            []Message `json:"messages"`
	Stream              bool      `json:"stream"`
	MaxTokens           *int      `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int      `json:"max_completion_tokens,omitempty"`
}

// completionTokenLimit returns the effective caller-supplied output ceiling.
// max_completion_tokens is OpenAI's current field and therefore takes
// precedence after validation. A compatibility client may send both fields
// only when their values agree.
func (request ChatCompletionRequest) completionTokenLimit() *int {
	if request.MaxCompletionTokens != nil {
		return request.MaxCompletionTokens
	}
	return request.MaxTokens
}

func (request ChatCompletionRequest) hasCompletionTokenLimit() bool {
	return request.MaxCompletionTokens != nil || request.MaxTokens != nil
}

// Message keeps a compatibility text projection for existing policy and
// metering code while retaining the exact JSON content value and all unknown
// message fields. content may be a string, an array of content parts, or null
// for assistant tool-call messages.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"-"`

	rawContent       json.RawMessage
	extraFields      map[string]json.RawMessage
	contentTextParts []messageTextPart
	nonTextParts     int
	contentPresent   bool
}

type messageTextPart struct {
	Path    string
	Content string
}

func (message *Message) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("chat message must be a JSON object")
	}

	*message = Message{extraFields: make(map[string]json.RawMessage)}
	if raw, ok := fields["role"]; ok {
		if err := json.Unmarshal(raw, &message.Role); err != nil {
			return fmt.Errorf("chat message role: %w", err)
		}
	}
	if raw, ok := fields["content"]; ok {
		message.contentPresent = true
		message.rawContent = append(json.RawMessage(nil), raw...)
		parts, nonTextParts, err := parseMessageContent(raw)
		if err != nil {
			return err
		}
		message.contentTextParts = parts
		message.nonTextParts = nonTextParts
		message.Content = joinMessageTextParts(parts)
	}
	for key, raw := range fields {
		if key == "role" || key == "content" {
			continue
		}
		message.extraFields[key] = append(json.RawMessage(nil), raw...)
	}
	return nil
}

func (message Message) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(message.extraFields)+2)
	for key, raw := range message.extraFields {
		fields[key] = append(json.RawMessage(nil), raw...)
	}
	role, err := json.Marshal(message.Role)
	if err != nil {
		return nil, err
	}
	fields["role"] = role
	if len(bytes.TrimSpace(message.rawContent)) > 0 {
		fields["content"] = append(json.RawMessage(nil), message.rawContent...)
	} else if message.contentPresent || message.extraFields == nil || message.Content != "" {
		content, err := json.Marshal(message.Content)
		if err != nil {
			return nil, err
		}
		fields["content"] = content
	}
	return json.Marshal(fields)
}

func parseMessageContent(raw json.RawMessage) ([]messageTextPart, int, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, 0, nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, 0, fmt.Errorf("chat message content: %w", err)
		}
		return []messageTextPart{{Path: "content", Content: text}}, 0, nil
	}
	if trimmed[0] != '[' {
		return nil, 0, fmt.Errorf("chat message content must be a string, array, or null")
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawParts); err != nil {
		return nil, 0, fmt.Errorf("chat message content parts: %w", err)
	}
	texts := make([]messageTextPart, 0, len(rawParts))
	nonTextParts := 0
	for index, rawPart := range rawParts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(rawPart, &part); err != nil || part == nil {
			nonTextParts++
			continue
		}
		rawText, hasText := part["text"]
		if !hasText {
			nonTextParts++
			continue
		}
		var text string
		if err := json.Unmarshal(rawText, &text); err != nil {
			return nil, 0, fmt.Errorf("chat message content[%d].text: %w", index, err)
		}
		texts = append(texts, messageTextPart{Path: fmt.Sprintf("content[%d].text", index), Content: text})
	}
	return texts, nonTextParts, nil
}

func joinMessageTextParts(parts []messageTextPart) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, part.Content)
	}
	return strings.Join(values, "\n")
}

func (message Message) textParts() []messageTextPart {
	if len(message.contentTextParts) > 0 {
		return message.contentTextParts
	}
	if message.Content != "" || len(bytes.TrimSpace(message.rawContent)) == 0 {
		return []messageTextPart{{Path: "content", Content: message.Content}}
	}
	return nil
}

func (message Message) estimatedInputTokens() int {
	estimated := 0
	for _, part := range message.textParts() {
		estimated += tokenutil.EstimateTokens(part.Content)
	}
	// Vision/audio token accounting is provider-specific. This compatibility
	// floor keeps admission from treating a non-text part as free while final
	// billing uses authoritative upstream usage. It is not an upper bound;
	// operation-specific admission must fail closed until a provider profile can
	// supply a defensible reservation contract.
	estimated += message.nonTextParts * 256
	return estimated
}
