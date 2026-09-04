package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uniroute/unirouter/internal/strictjson"
)

// FunctionToolStreamFacts contains only mechanical facts. The caller owns
// envelope delivery policy, authoritative usage and capability assertions.
type FunctionToolStreamFacts struct {
	Model    string
	Content  string
	Choice   bool
	Finish   string
	HasUsage bool
	Tools    []FunctionToolDelta
}

type FunctionToolDelta struct {
	Index                     int
	ID, Type, Name, Arguments *string
}

func ParseFunctionToolStreamFacts(data []byte) (FunctionToolStreamFacts, error) {
	var facts FunctionToolStreamFacts
	if err := strictjson.Validate(data); err != nil {
		return facts, err
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return facts, fmt.Errorf("invalid stream envelope")
	}
	keys := func(object map[string]json.RawMessage, allowed ...string) bool {
		for key := range object {
			found := false
			for _, name := range allowed {
				if key == name {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if !keys(raw, "id", "object", "created", "model", "choices", "usage") {
		return facts, fmt.Errorf("unmodeled stream envelope field")
	}
	var object string
	if json.Unmarshal(raw["object"], &object) != nil || object != "chat.completion.chunk" {
		return facts, fmt.Errorf("invalid stream object")
	}
	var id string
	var created int64
	if json.Unmarshal(raw["id"], &id) != nil || id == "" || bytes.Equal(bytes.TrimSpace(raw["created"]), []byte("null")) || json.Unmarshal(raw["created"], &created) != nil || created < 0 {
		return facts, fmt.Errorf("invalid stream metadata")
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(raw["choices"], &choices) != nil || choices == nil {
		return facts, fmt.Errorf("invalid stream choices")
	}
	for _, choice := range choices {
		if !keys(choice, "index", "delta", "finish_reason") {
			return facts, fmt.Errorf("unmodeled stream choice field")
		}
		var delta map[string]json.RawMessage
		if json.Unmarshal(choice["delta"], &delta) != nil || delta == nil || !keys(delta, "role", "content", "tool_calls") {
			return facts, fmt.Errorf("invalid stream delta")
		}
		if value, present := delta["role"]; present {
			var role string
			if json.Unmarshal(value, &role) != nil || role != "assistant" {
				return facts, fmt.Errorf("invalid stream role")
			}
		}
		if value, present := delta["tool_calls"]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return facts, fmt.Errorf("null stream tools")
		}
	}
	var envelope struct {
		Model   string `json:"model"`
		Choices []struct {
			Index  *int    `json:"index"`
			Finish *string `json:"finish_reason"`
			Delta  struct {
				Content *string                      `json:"content"`
				Tools   []map[string]json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return facts, err
	}
	facts.Model = envelope.Model
	facts.HasUsage = len(envelope.Usage) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.Usage), []byte("null"))
	if len(envelope.Choices) > 1 {
		return facts, fmt.Errorf("function-tool stream requires one choice")
	}
	if len(envelope.Choices) == 0 {
		return facts, nil
	}
	choice := envelope.Choices[0]
	if choice.Index == nil || *choice.Index != 0 {
		return facts, fmt.Errorf("function-tool stream choice index must be zero")
	}
	facts.Choice = true
	if choice.Delta.Content != nil {
		facts.Content = *choice.Delta.Content
	}
	if choice.Finish != nil {
		facts.Finish = *choice.Finish
	}
	for _, tool := range choice.Delta.Tools {
		for key := range tool {
			if key != "index" && key != "id" && key != "type" && key != "function" {
				return facts, fmt.Errorf("unmodeled tool delta field")
			}
		}
		var index int
		if len(tool["index"]) == 0 || bytes.Equal(bytes.TrimSpace(tool["index"]), []byte("null")) || json.Unmarshal(tool["index"], &index) != nil || index < 0 || index >= 128 {
			return facts, fmt.Errorf("invalid tool delta index")
		}
		delta := FunctionToolDelta{Index: index}
		readString := func(raw json.RawMessage, target **string) error {
			if len(raw) == 0 {
				return nil
			}
			var value string
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
				return fmt.Errorf("tool delta value must be a string")
			}
			*target = &value
			return nil
		}
		if err := readString(tool["id"], &delta.ID); err != nil {
			return facts, err
		}
		if err := readString(tool["type"], &delta.Type); err != nil {
			return facts, err
		}
		if raw, ok := tool["function"]; ok {
			var function map[string]json.RawMessage
			if json.Unmarshal(raw, &function) != nil || function == nil {
				return facts, fmt.Errorf("invalid function delta")
			}
			for key := range function {
				if key != "name" && key != "arguments" {
					return facts, fmt.Errorf("unmodeled function delta field")
				}
			}
			if err := readString(function["name"], &delta.Name); err != nil {
				return facts, err
			}
			if err := readString(function["arguments"], &delta.Arguments); err != nil {
				return facts, err
			}
		}
		facts.Tools = append(facts.Tools, delta)
	}
	return facts, nil
}

type StreamFunctionCall struct{ ID, Name, Arguments string }

// FunctionToolStreamState is shared by new opt-in Runtime and Probe paths.
// Historical provider state machines do not select it. A failed state is not
// reusable; callers terminate on the first error while retaining prior usage.
type FunctionToolStreamState struct {
	expectedModel string
	tools         map[int]StreamFunctionCall
	ids           map[string]int
	retainedBytes int
	finish        string
	usage         bool
}

func NewFunctionToolStreamState(expectedModel string) *FunctionToolStreamState {
	return &FunctionToolStreamState{expectedModel: expectedModel, tools: make(map[int]StreamFunctionCall), ids: make(map[string]int)}
}

var functionToolStreamName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (s *FunctionToolStreamState) Accept(f FunctionToolStreamFacts, authoritativeUsage bool) error {
	if s == nil || s.expectedModel == "" || f.Model != s.expectedModel {
		return fmt.Errorf("stream model identity mismatch")
	}
	if s.usage {
		return fmt.Errorf("data after terminal stream usage")
	}
	if s.finish != "" && f.Choice {
		return fmt.Errorf("model data after stream finish")
	}
	for _, delta := range f.Tools {
		call, exists := s.tools[delta.Index]
		if !exists {
			if len(s.tools) >= 128 || delta.ID == nil || strings.TrimSpace(*delta.ID) == "" || delta.Type == nil || *delta.Type != "function" || delta.Name == nil || !functionToolStreamName.MatchString(*delta.Name) {
				return fmt.Errorf("tool delta lacks initial identity")
			}
			if _, duplicate := s.ids[*delta.ID]; duplicate {
				return fmt.Errorf("duplicate stream tool id")
			}
			call.ID, call.Name = *delta.ID, *delta.Name
			identityBytes := len(call.ID) + len(call.Name)
			if identityBytes > (1<<20)-s.retainedBytes {
				return fmt.Errorf("stream tool identities exceed bound")
			}
			s.retainedBytes += identityBytes
			s.ids[call.ID] = delta.Index
		} else if (delta.ID != nil && *delta.ID != call.ID) || (delta.Name != nil && *delta.Name != call.Name) || (delta.Type != nil && *delta.Type != "function") {
			return fmt.Errorf("stream tool identity changed")
		}
		if delta.Arguments != nil {
			if len(*delta.Arguments) > (1<<20)-s.retainedBytes {
				return fmt.Errorf("stream tool arguments exceed bound")
			}
			s.retainedBytes += len(*delta.Arguments)
			call.Arguments += *delta.Arguments
		}
		s.tools[delta.Index] = call
	}
	if f.Finish != "" {
		switch f.Finish {
		case "tool_calls":
			if len(s.tools) == 0 {
				return fmt.Errorf("tool finish without tools")
			}
			for i := 0; i < len(s.tools); i++ {
				call, ok := s.tools[i]
				if !ok {
					return fmt.Errorf("noncontiguous stream tool indexes")
				}
				var arguments map[string]json.RawMessage
				if strictjson.Validate([]byte(call.Arguments)) != nil || json.Unmarshal([]byte(call.Arguments), &arguments) != nil || arguments == nil {
					return fmt.Errorf("invalid completed tool arguments")
				}
			}
		case "stop":
			if len(s.tools) > 0 {
				return fmt.Errorf("tool stream stopped without tool finish")
			}
		case "length":
			// A truncated tool call is not executable. Fail closed rather than
			// let the client treat an incomplete argument object as a tool result.
			if len(s.tools) > 0 {
				return fmt.Errorf("truncated stream tool arguments")
			}
		default:
			return fmt.Errorf("unsupported stream finish")
		}
		s.finish = f.Finish
	}
	if f.HasUsage {
		if !authoritativeUsage || s.finish == "" {
			return fmt.Errorf("stream usage before finish or without authority")
		}
		s.usage = true
	} else if !f.Choice {
		return fmt.Errorf("empty stream frame without usage")
	}
	return nil
}

func (s *FunctionToolStreamState) Finish() error {
	if s == nil || s.finish == "" || !s.usage {
		return fmt.Errorf("stream requires finish and terminal usage")
	}
	return nil
}

func (s *FunctionToolStreamState) Calls() []StreamFunctionCall {
	result := make([]StreamFunctionCall, 0, len(s.tools))
	for i := 0; i < len(s.tools); i++ {
		result = append(result, s.tools[i])
	}
	return result
}
