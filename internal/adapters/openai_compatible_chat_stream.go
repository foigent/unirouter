package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

var (
	ErrChatSSEUnsupportedField = errors.New("unsupported SSE field")
	ErrChatSSEEventTooLarge    = errors.New("SSE event exceeds the configured limit")
)

const kimiChatCacheUsageProfile = "cache-usage.kimi-platform.v1"

// SplitSSELines implements every line ending admitted by the SSE wire format.
// Callers retain ownership of scanner and complete-body limits.
func SplitSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for index, value := range data {
		switch value {
		case '\n':
			lineEnd := index
			if index > 0 && data[index-1] == '\r' {
				lineEnd--
			}
			return index + 1, data[:lineEnd], nil
		case '\r':
			if index+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			if index+1 < len(data) && data[index+1] == '\n' {
				return index + 2, data[:index], nil
			}
			return index + 1, data[:index], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type ChatSSEEvent struct {
	Data []byte
}

// ChatSSEEventAssembler owns only one bounded event. It does not own the
// response-body limit or any provider/model policy.
type ChatSSEEventAssembler struct {
	maxDataBytes int
	data         []byte
	hasData      bool
}

type DeepSeekChatStreamRuntimeIdentity struct {
	ProfileDefinition       modelprofile.Definition
	OperationID             string
	ExecutionProfileID      string
	ExecutionProfileVersion string
	Provider                string
	UpstreamModel           string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	CacheUsageProfile       string
}

type DeepSeekChatStreamProbeIdentity struct {
	OperationID             string
	CapabilityID            string
	ExecutionProfileID      string
	ExecutionProfileVersion string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	ProbeContractID         string
	ProbeContractVersion    string
	ExpectedProvider        string
	ExpectedUpstreamModel   string
	PublicPath              string
}

type deepSeekChatStreamPolicy uint8

const (
	deepSeekChatStreamRuntimeCompatibility deepSeekChatStreamPolicy = iota + 1
	deepSeekChatStreamProbeSeparateUsage
	deepSeekChatStreamProbeFinalChoiceUsage
)

// DeepSeekChatStreamMechanics is exact-scoped mechanical policy. Runtime and
// Probe intentionally have different immutable acceptance policies; both use
// the same lifecycle state over role-owned validated frame facts instead of
// maintaining separate finish/usage/DONE state machines.
type DeepSeekChatStreamMechanics struct {
	policy            deepSeekChatStreamPolicy
	expectedModel     string
	cacheUsageProfile string
	valid             bool
}

func ResolveDeepSeekRuntimeChatStreamMechanics(identity DeepSeekChatStreamRuntimeIdentity) (DeepSeekChatStreamMechanics, bool, error) {
	if d := identity.ProfileDefinition; d.Digest() != "" && d.Profile().Provider == "deepseek" && d.Profile().ProtocolProfile == "protocol.deepseek-openai-chat.v4" {
		if err := validateDataMechanicsIdentity(d, identity.ExecutionProfileID, identity.ExecutionProfileVersion, identity.Provider, identity.UpstreamModel, identity.OperationID, identity.ProtocolProfileID, identity.ProtocolProfileVersion, identity.CacheUsageProfile); err != nil {
			return DeepSeekChatStreamMechanics{}, false, err
		}
		return DeepSeekChatStreamMechanics{policy: deepSeekChatStreamRuntimeCompatibility, expectedModel: identity.UpstreamModel, cacheUsageProfile: identity.CacheUsageProfile, valid: true}, true, nil
	}
	expectedModel := ""
	switch strings.TrimSpace(identity.ExecutionProfileID) + "\x00" + strings.TrimSpace(identity.ExecutionProfileVersion) {
	case "deepseek.v4-flash.api\x001.5.0":
		expectedModel = "deepseek-v4-flash"
	case "deepseek.v4-pro.api\x001.5.0":
		expectedModel = "deepseek-v4-pro"
	default:
		return DeepSeekChatStreamMechanics{}, false, nil
	}
	if modelprofile.IsImmutableExistingAttachmentOperationProtocolIdentity(
		strings.TrimSpace(identity.ExecutionProfileID),
		strings.TrimSpace(identity.ExecutionProfileVersion),
		strings.TrimSpace(identity.OperationID),
		strings.TrimSpace(identity.ProtocolProfileID),
		strings.TrimSpace(identity.ProtocolProfileVersion),
	) && strings.EqualFold(strings.TrimSpace(identity.Provider), "deepseek") &&
		strings.TrimSpace(identity.UpstreamModel) == expectedModel &&
		strings.TrimSpace(identity.CacheUsageProfile) == "cache-usage.deepseek-v4.v1" {
		// This exact Flash attachment predates the DeepSeek v4 protocol binding.
		// It must continue through the existing generic Runtime path; this
		// convergence does not turn historical compatibility into a new binding.
		return DeepSeekChatStreamMechanics{}, false, nil
	}
	if strings.TrimSpace(identity.OperationID) != modelprofile.OperationOpenAIChat ||
		!strings.EqualFold(strings.TrimSpace(identity.Provider), "deepseek") ||
		strings.TrimSpace(identity.UpstreamModel) != expectedModel ||
		strings.TrimSpace(identity.ProtocolProfileID) != "protocol.deepseek-openai-chat.v4" ||
		strings.TrimSpace(identity.ProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 ||
		strings.TrimSpace(identity.CacheUsageProfile) != "cache-usage.deepseek-v4.v1" {
		return DeepSeekChatStreamMechanics{}, false, fmt.Errorf("exact current DeepSeek Chat stream runtime identity mismatch")
	}
	return DeepSeekChatStreamMechanics{
		policy: deepSeekChatStreamRuntimeCompatibility, expectedModel: expectedModel,
		cacheUsageProfile: "cache-usage.deepseek-v4.v1", valid: true,
	}, true, nil
}

func ResolveDeepSeekProbeChatStreamMechanics(identity DeepSeekChatStreamProbeIdentity) (DeepSeekChatStreamMechanics, bool, error) {
	policy := deepSeekChatStreamPolicy(0)
	switch strings.TrimSpace(identity.ProbeContractVersion) {
	case "1.0.0":
		policy = deepSeekChatStreamProbeSeparateUsage
	case "2.0.0":
		policy = deepSeekChatStreamProbeFinalChoiceUsage
	default:
		return DeepSeekChatStreamMechanics{}, false, nil
	}
	expectedModel := strings.TrimSpace(identity.ExpectedUpstreamModel)
	profileKey := strings.TrimSpace(identity.ExecutionProfileID) + "\x00" + strings.TrimSpace(identity.ExecutionProfileVersion)
	validProfile := (profileKey == "deepseek.v4-flash.api\x001.4.0" || profileKey == "deepseek.v4-flash.api\x001.5.0") && expectedModel == "deepseek-v4-flash"
	validProfile = validProfile || profileKey == "deepseek.v4-pro.api\x001.5.0" && expectedModel == "deepseek-v4-pro"
	protocol := strings.TrimSpace(identity.ProtocolProfileID)
	validProtocol := protocol == "protocol.deepseek-openai-chat.v4" ||
		(strings.TrimSpace(identity.ExecutionProfileID) == "deepseek.v4-flash.api" && protocol == "openai.chat_completions")
	if strings.TrimSpace(identity.OperationID) != modelprofile.OperationOpenAIChat ||
		strings.TrimSpace(identity.CapabilityID) != modelprofile.CapabilityStreaming || !validProfile || !validProtocol ||
		strings.TrimSpace(identity.ProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 ||
		strings.TrimSpace(identity.ProbeContractID) != "unirouter.capability-probe.deepseek-chat-sse.v1" ||
		!strings.EqualFold(strings.TrimSpace(identity.ExpectedProvider), "deepseek") ||
		strings.TrimSpace(identity.PublicPath) != "/v1/chat/completions" {
		return DeepSeekChatStreamMechanics{}, false, fmt.Errorf("exact DeepSeek Chat stream Probe binding mismatch")
	}
	if policy == deepSeekChatStreamProbeSeparateUsage && profileKey != "deepseek.v4-flash.api\x001.4.0" {
		return DeepSeekChatStreamMechanics{}, false, fmt.Errorf("historical DeepSeek Chat stream Probe binding mismatch")
	}
	return DeepSeekChatStreamMechanics{policy: policy, expectedModel: expectedModel, valid: true}, true, nil
}

type DeepSeekChatStreamPrepared struct {
	ReportedModel     string
	ChoiceCount       int
	DeltaCount        int
	ContentDeltaCount int
	ContentInChunk    bool
	FinishReason      string
	HasUsage          bool
	PromptTokens      int64
	CompletionTokens  int64
	TotalTokens       int64
	rawUsage          json.RawMessage
}

type DeepSeekChatStreamFacts struct {
	ChoiceCount  int
	FinishReason string
	HasUsage     bool
}

type DeepSeekChatStreamIssue string

const (
	DeepSeekChatStreamIssueJSON                DeepSeekChatStreamIssue = "json"
	DeepSeekChatStreamIssueChoices             DeepSeekChatStreamIssue = "choices"
	DeepSeekChatStreamIssueDelta               DeepSeekChatStreamIssue = "delta"
	DeepSeekChatStreamIssueContent             DeepSeekChatStreamIssue = "content"
	DeepSeekChatStreamIssueFinishReason        DeepSeekChatStreamIssue = "finish_reason"
	DeepSeekChatStreamIssueFinishReasonChanged DeepSeekChatStreamIssue = "finish_reason_changed"
	DeepSeekChatStreamIssueFinalUsage          DeepSeekChatStreamIssue = "final_usage"
	DeepSeekChatStreamIssueUsageMixed          DeepSeekChatStreamIssue = "usage_mixed_with_choices"
	DeepSeekChatStreamIssueUsage               DeepSeekChatStreamIssue = "usage"
	DeepSeekChatStreamIssueUsageTotal          DeepSeekChatStreamIssue = "usage_total"
	DeepSeekChatStreamIssueTerminalOrder       DeepSeekChatStreamIssue = "terminal_order"
	DeepSeekChatStreamIssueDataAfterDone       DeepSeekChatStreamIssue = "data_after_done"
	DeepSeekChatStreamIssueDataAfterUsage      DeepSeekChatStreamIssue = "data_after_usage"
	DeepSeekChatStreamIssueIncomplete          DeepSeekChatStreamIssue = "incomplete"
)

type deepSeekChatStreamError struct {
	issue DeepSeekChatStreamIssue
	cause error
}

func (e *deepSeekChatStreamError) Error() string {
	if e.cause == nil {
		return fmt.Sprintf("invalid DeepSeek Chat stream mechanics: %s", e.issue)
	}
	return fmt.Sprintf("invalid DeepSeek Chat stream mechanics: %s: %v", e.issue, e.cause)
}

func (e *deepSeekChatStreamError) Unwrap() error { return e.cause }

func invalidDeepSeekChatStream(issue DeepSeekChatStreamIssue, cause ...error) error {
	err := &deepSeekChatStreamError{issue: issue}
	if len(cause) > 0 {
		err.cause = cause[0]
	}
	return err
}

func DeepSeekChatStreamIssueForError(err error) DeepSeekChatStreamIssue {
	var streamErr *deepSeekChatStreamError
	if errors.As(err, &streamErr) {
		return streamErr.issue
	}
	return ""
}

func PrepareDeepSeekChatStreamEvent(data []byte, mechanics DeepSeekChatStreamMechanics) (DeepSeekChatStreamPrepared, error) {
	if !mechanics.valid || mechanics.expectedModel == "" {
		return DeepSeekChatStreamPrepared{}, fmt.Errorf("invalid DeepSeek Chat stream mechanics")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var chunk struct {
		Model   string          `json:"model"`
		Choices json.RawMessage `json:"choices"`
		Usage   json.RawMessage `json:"usage"`
	}
	if err := decoder.Decode(&chunk); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return DeepSeekChatStreamPrepared{}, invalidDeepSeekChatStream(DeepSeekChatStreamIssueJSON, err)
	}
	prepared := DeepSeekChatStreamPrepared{ReportedModel: chunk.Model}
	var choices []struct {
		Delta        json.RawMessage `json:"delta"`
		FinishReason json.RawMessage `json:"finish_reason"`
	}
	if len(chunk.Choices) == 0 || json.Unmarshal(chunk.Choices, &choices) != nil {
		return prepared, invalidDeepSeekChatStream(DeepSeekChatStreamIssueChoices)
	}
	prepared.ChoiceCount = len(choices)
	for _, choice := range choices {
		var delta map[string]json.RawMessage
		if len(choice.Delta) == 0 || json.Unmarshal(choice.Delta, &delta) != nil || delta == nil {
			return prepared, invalidDeepSeekChatStream(DeepSeekChatStreamIssueDelta)
		}
		prepared.DeltaCount++
		if raw, exists := delta["content"]; exists && !isJSONNullUsage(raw) {
			var content string
			if json.Unmarshal(raw, &content) != nil {
				return prepared, invalidDeepSeekChatStream(DeepSeekChatStreamIssueContent)
			}
			if content != "" {
				prepared.ContentInChunk = true
				prepared.ContentDeltaCount++
			}
		}
		if len(choice.FinishReason) > 0 && !isJSONNullUsage(choice.FinishReason) {
			var finish string
			if json.Unmarshal(choice.FinishReason, &finish) != nil || strings.TrimSpace(finish) == "" || strings.ContainsAny(finish, "\x00\r\n") {
				return prepared, invalidDeepSeekChatStream(DeepSeekChatStreamIssueFinishReason)
			}
			if prepared.FinishReason != "" && prepared.FinishReason != finish {
				return prepared, invalidDeepSeekChatStream(DeepSeekChatStreamIssueFinishReasonChanged)
			}
			prepared.FinishReason = finish
		}
	}
	if len(chunk.Usage) > 0 && !isJSONNullUsage(chunk.Usage) {
		prepared.HasUsage = true
		prepared.rawUsage = append(json.RawMessage(nil), chunk.Usage...)
	}
	return prepared, nil
}

func ParseDeepSeekChatStreamUsage(prepared DeepSeekChatStreamPrepared) (DeepSeekChatStreamPrepared, error) {
	if !prepared.HasUsage || len(prepared.rawUsage) == 0 {
		return prepared, nil
	}
	prompt, completion, total, err := parseDeepSeekBaseUsage(prepared.rawUsage)
	if err != nil {
		return prepared, err
	}
	prepared.PromptTokens, prepared.CompletionTokens, prepared.TotalTokens = prompt, completion, total
	return prepared, nil
}

func parseDeepSeekBaseUsage(raw json.RawMessage) (int64, int64, int64, error) {
	var usage struct {
		PromptTokens     json.Number `json:"prompt_tokens"`
		CompletionTokens json.Number `json:"completion_tokens"`
		TotalTokens      json.Number `json:"total_tokens"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&usage) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return 0, 0, 0, invalidDeepSeekChatStream(DeepSeekChatStreamIssueUsage)
	}
	values := []json.Number{usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens}
	parsed := make([]int64, len(values))
	for index, value := range values {
		if value == "" || strings.ContainsAny(string(value), ".eE+") {
			return 0, 0, 0, invalidDeepSeekChatStream(DeepSeekChatStreamIssueUsage)
		}
		integer, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || integer < 0 {
			return 0, 0, 0, invalidDeepSeekChatStream(DeepSeekChatStreamIssueUsage)
		}
		parsed[index] = integer
	}
	if parsed[0]+parsed[1] != parsed[2] {
		return 0, 0, 0, invalidDeepSeekChatStream(DeepSeekChatStreamIssueUsageTotal)
	}
	return parsed[0], parsed[1], parsed[2], nil
}

type DeepSeekChatStreamState struct {
	mechanics   DeepSeekChatStreamMechanics
	finish      string
	usageSeen   bool
	done        bool
	contentSeen bool
}

func NewDeepSeekChatStreamState(mechanics DeepSeekChatStreamMechanics) (*DeepSeekChatStreamState, error) {
	if !mechanics.valid {
		return nil, fmt.Errorf("invalid DeepSeek Chat stream mechanics")
	}
	return &DeepSeekChatStreamState{mechanics: mechanics}, nil
}

func (s *DeepSeekChatStreamState) BeginDataEvent() error {
	if s == nil || !s.mechanics.valid {
		return fmt.Errorf("DeepSeek Chat stream state is not initialized")
	}
	if s.done {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueDataAfterDone)
	}
	if s.mechanics.policy == deepSeekChatStreamProbeFinalChoiceUsage && s.usageSeen {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueDataAfterUsage)
	}
	return nil
}

func (s *DeepSeekChatStreamState) Accept(prepared DeepSeekChatStreamPrepared) error {
	if err := s.BeginDataEvent(); err != nil {
		return err
	}
	if err := s.acceptFacts(DeepSeekChatStreamFacts{
		ChoiceCount: prepared.ChoiceCount, FinishReason: prepared.FinishReason, HasUsage: prepared.HasUsage,
	}, prepared.ContentInChunk); err != nil {
		return err
	}
	s.RecordContent(prepared.ContentDeltaCount)
	return nil
}

func (s *DeepSeekChatStreamState) AcceptRuntimeFacts(facts DeepSeekChatStreamFacts) error {
	if s == nil || !s.mechanics.valid || s.mechanics.policy != deepSeekChatStreamRuntimeCompatibility {
		return fmt.Errorf("DeepSeek Chat stream runtime state is not initialized")
	}
	if s.done {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueDataAfterDone)
	}
	return s.acceptFacts(facts, false)
}

func (s *DeepSeekChatStreamState) acceptFacts(facts DeepSeekChatStreamFacts, contentInChunk bool) error {
	if s.mechanics.policy == deepSeekChatStreamRuntimeCompatibility {
		if facts.ChoiceCount > 0 {
			// Runtime historically treated the most recent choice frame as the
			// clean-EOF authority. A later nonterminal choice therefore clears an
			// earlier finish; usage-only frames leave that choice fact unchanged.
			s.finish = facts.FinishReason
		}
		s.usageSeen = s.usageSeen || facts.HasUsage
		return nil
	}
	if facts.FinishReason != "" {
		if s.finish != "" && s.finish != facts.FinishReason {
			return invalidDeepSeekChatStream(DeepSeekChatStreamIssueFinishReasonChanged)
		}
		s.finish = facts.FinishReason
	}
	if s.mechanics.policy == deepSeekChatStreamProbeSeparateUsage {
		if facts.ChoiceCount == 0 {
			if s.usageSeen || !facts.HasUsage {
				return invalidDeepSeekChatStream(DeepSeekChatStreamIssueFinalUsage)
			}
			s.usageSeen = true
			return nil
		}
		if facts.HasUsage {
			return invalidDeepSeekChatStream(DeepSeekChatStreamIssueUsageMixed)
		}
		return nil
	}
	if facts.ChoiceCount == 0 {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueFinalUsage)
	}
	if facts.HasUsage {
		if s.usageSeen || facts.ChoiceCount != 1 || facts.FinishReason == "" || contentInChunk {
			return invalidDeepSeekChatStream(DeepSeekChatStreamIssueFinalUsage)
		}
		s.usageSeen = true
	}
	return nil
}

func (s *DeepSeekChatStreamState) AcceptDone() error {
	if s == nil || !s.mechanics.valid {
		return fmt.Errorf("DeepSeek Chat stream state is not initialized")
	}
	if s.done {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueTerminalOrder)
	}
	if s.mechanics.policy != deepSeekChatStreamRuntimeCompatibility && (!s.usageSeen || s.finish == "") {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueTerminalOrder)
	}
	s.done = true
	return nil
}

func (s *DeepSeekChatStreamState) RecordContent(count int) {
	if s != nil && count > 0 {
		s.contentSeen = true
	}
}

func (s *DeepSeekChatStreamState) FinishEOF() error {
	if s == nil || !s.mechanics.valid {
		return fmt.Errorf("DeepSeek Chat stream state is not initialized")
	}
	if s.mechanics.policy == deepSeekChatStreamRuntimeCompatibility {
		if s.done || s.finish != "" {
			return nil
		}
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueIncomplete)
	}
	if !s.done || !s.usageSeen || s.finish == "" || !s.contentSeen {
		return invalidDeepSeekChatStream(DeepSeekChatStreamIssueIncomplete)
	}
	return nil
}

func NewChatSSEEventAssembler(maxDataBytes int) (*ChatSSEEventAssembler, error) {
	if maxDataBytes <= 0 {
		return nil, fmt.Errorf("SSE event limit must be positive")
	}
	return &ChatSSEEventAssembler{maxDataBytes: maxDataBytes}, nil
}

func (a *ChatSSEEventAssembler) AcceptLine(line string) (ChatSSEEvent, bool, error) {
	if a == nil || a.maxDataBytes <= 0 {
		return ChatSSEEvent{}, false, fmt.Errorf("SSE event assembler is not initialized")
	}
	if line == "" {
		return a.finishEvent()
	}
	if strings.HasPrefix(line, ":") {
		return ChatSSEEvent{}, false, nil
	}
	field, value := line, ""
	if colon := strings.IndexByte(line, ':'); colon >= 0 {
		field, value = line[:colon], line[colon+1:]
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
	}
	if field != "data" {
		return ChatSSEEvent{}, false, fmt.Errorf("%w: %s", ErrChatSSEUnsupportedField, field)
	}
	additional := len(value)
	if a.hasData {
		additional++
	}
	if additional > a.maxDataBytes-len(a.data) {
		return ChatSSEEvent{}, false, ErrChatSSEEventTooLarge
	}
	if a.hasData {
		a.data = append(a.data, '\n')
	}
	a.data = append(a.data, value...)
	a.hasData = true
	return ChatSSEEvent{}, false, nil
}

func (a *ChatSSEEventAssembler) Finish() (ChatSSEEvent, bool, error) {
	if a == nil || a.maxDataBytes <= 0 {
		return ChatSSEEvent{}, false, fmt.Errorf("SSE event assembler is not initialized")
	}
	return a.finishEvent()
}

func (a *ChatSSEEventAssembler) finishEvent() (ChatSSEEvent, bool, error) {
	if !a.hasData {
		return ChatSSEEvent{}, false, nil
	}
	event := ChatSSEEvent{Data: append([]byte(nil), a.data...)}
	a.data = a.data[:0]
	a.hasData = false
	return event, true, nil
}

type KimiChatStreamRuntimeIdentity struct {
	ProfileDefinition       modelprofile.Definition
	OperationID             string
	ExecutionProfileID      string
	ExecutionProfileVersion string
	Provider                string
	UpstreamModel           string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	CacheUsageProfile       string
}

type KimiChatStreamProbeIdentity struct {
	OperationID             string
	CapabilityID            string
	ExecutionProfileID      string
	ExecutionProfileVersion string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	ProbeContractID         string
	ProbeContractVersion    string
	ExpectedProvider        string
	ExpectedUpstreamModel   string
	PublicPath              string
}

// KimiChatStreamMechanics is process-local and can only be constructed by the
// exact runtime or Probe identity resolvers below.
type KimiChatStreamMechanics struct {
	provider                string
	upstreamModel           string
	protocolProfileID       string
	cacheUsageProfile       string
	normalizeEmptyReasoning bool
	finishReasons           []string
	valid                   bool
}

func ResolveKimiRuntimeChatStreamMechanics(identity KimiChatStreamRuntimeIdentity) (KimiChatStreamMechanics, bool, error) {
	if d := identity.ProfileDefinition; d.Digest() != "" && d.Profile().Provider == "kimi" && (d.Profile().ProtocolProfile == "protocol.kimi-openai-chat.v5" || d.Profile().ProtocolProfile == "protocol.kimi-openai-chat.k3.v5") {
		if err := validateDataMechanicsIdentity(d, identity.ExecutionProfileID, identity.ExecutionProfileVersion, identity.Provider, identity.UpstreamModel, identity.OperationID, identity.ProtocolProfileID, identity.ProtocolProfileVersion, identity.CacheUsageProfile); err != nil {
			return KimiChatStreamMechanics{}, false, err
		}
		mechanics := newKimiChatStreamMechanics(identity.UpstreamModel, identity.ProtocolProfileID, []string{"stop", "length", "content_filter"})
		mechanics.normalizeEmptyReasoning = d.Profile().ReasoningProfile == "reasoning.kimi-k2.6-thinking-toggle.v2"
		return mechanics, true, nil
	}
	expectedModel, expectedProtocol := "", ""
	switch strings.TrimSpace(identity.ExecutionProfileID) + "\x00" + strings.TrimSpace(identity.ExecutionProfileVersion) {
	case "kimi.k2.6.api\x001.7.0":
		expectedModel, expectedProtocol = "kimi-k2.6", "protocol.kimi-openai-chat.v5"
	case "kimi.k2.7-code.api\x001.3.0":
		expectedModel, expectedProtocol = "kimi-k2.7-code", "protocol.kimi-openai-chat.v5"
	case "kimi.k3.api\x001.9.0":
		expectedModel, expectedProtocol = "kimi-k3", "protocol.kimi-openai-chat.k3.v5"
	default:
		return KimiChatStreamMechanics{}, false, nil
	}
	if strings.TrimSpace(identity.OperationID) != modelprofile.OperationOpenAIChat ||
		!strings.EqualFold(strings.TrimSpace(identity.Provider), "kimi") ||
		strings.TrimSpace(identity.UpstreamModel) != expectedModel ||
		strings.TrimSpace(identity.ProtocolProfileID) != expectedProtocol ||
		strings.TrimSpace(identity.ProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 ||
		strings.TrimSpace(identity.CacheUsageProfile) != kimiChatCacheUsageProfile {
		return KimiChatStreamMechanics{}, false, fmt.Errorf("exact current Kimi Chat stream runtime identity mismatch")
	}
	return newKimiChatStreamMechanics(
		expectedModel, expectedProtocol, []string{"stop", "length", "content_filter"},
	), true, nil
}

func ResolveKimiProbeChatStreamMechanics(identity KimiChatStreamProbeIdentity) (KimiChatStreamMechanics, bool, error) {
	expectedProfile, expectedVersion, expectedModel, expectedProtocol := "", "", "", ""
	switch strings.TrimSpace(identity.ProbeContractVersion) {
	case "3.0.0":
		expectedProfile, expectedVersion, expectedModel, expectedProtocol = "kimi.k2.6.api", "1.7.0", "kimi-k2.6", "protocol.kimi-openai-chat.v5"
	case "4.0.0":
		switch strings.TrimSpace(identity.ExpectedUpstreamModel) {
		case "kimi-k2.7-code":
			expectedProfile, expectedVersion, expectedModel, expectedProtocol = "kimi.k2.7-code.api", "1.3.0", "kimi-k2.7-code", "protocol.kimi-openai-chat.v5"
		case "kimi-k3":
			expectedProfile, expectedVersion, expectedModel, expectedProtocol = "kimi.k3.api", "1.9.0", "kimi-k3", "protocol.kimi-openai-chat.k3.v5"
		default:
			return KimiChatStreamMechanics{}, false, nil
		}
	default:
		return KimiChatStreamMechanics{}, false, nil
	}
	if strings.TrimSpace(identity.OperationID) != modelprofile.OperationOpenAIChat ||
		strings.TrimSpace(identity.CapabilityID) != modelprofile.CapabilityStreaming ||
		strings.TrimSpace(identity.ExecutionProfileID) != expectedProfile ||
		strings.TrimSpace(identity.ExecutionProfileVersion) != expectedVersion ||
		strings.TrimSpace(identity.ProtocolProfileID) != expectedProtocol ||
		strings.TrimSpace(identity.ProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 ||
		strings.TrimSpace(identity.ProbeContractID) != "unirouter.capability-probe.kimi-chat-sse.v1" ||
		!strings.EqualFold(strings.TrimSpace(identity.ExpectedProvider), "kimi") ||
		strings.TrimSpace(identity.ExpectedUpstreamModel) != expectedModel ||
		strings.TrimSpace(identity.PublicPath) != "/v1/chat/completions" {
		return KimiChatStreamMechanics{}, false, fmt.Errorf("exact current Kimi Chat stream Probe binding mismatch")
	}
	return newKimiChatStreamMechanics(expectedModel, expectedProtocol, []string{"stop"}), true, nil
}

func newKimiChatStreamMechanics(upstreamModel, protocolProfileID string, finishReasons []string) KimiChatStreamMechanics {
	return KimiChatStreamMechanics{
		provider: "kimi", upstreamModel: upstreamModel, protocolProfileID: protocolProfileID,
		cacheUsageProfile:       kimiChatCacheUsageProfile,
		normalizeEmptyReasoning: upstreamModel == "kimi-k2.6",
		finishReasons:           append([]string(nil), finishReasons...), valid: true,
	}
}

type KimiChatStreamPrepared struct {
	Body             []byte
	Changed          bool
	ChoiceCount      int
	TerminalChoice   bool
	UsageOnly        bool
	HasTopLevelUsage bool
	TopLevelUsage    Usage
	ChoiceUsages     []Usage
}

type KimiChatStreamIssue string

const (
	KimiChatStreamIssueDocument          KimiChatStreamIssue = "document"
	KimiChatStreamIssuePreparation       KimiChatStreamIssue = "preparation"
	KimiChatStreamIssueChoices           KimiChatStreamIssue = "choices"
	KimiChatStreamIssueFinishReason      KimiChatStreamIssue = "finish_reason"
	KimiChatStreamIssueUsage             KimiChatStreamIssue = "usage"
	KimiChatStreamIssueUsageOrder        KimiChatStreamIssue = "usage_order"
	KimiChatStreamIssueUsageConflict     KimiChatStreamIssue = "usage_conflict"
	KimiChatStreamIssueTerminalOrder     KimiChatStreamIssue = "terminal_order"
	KimiChatStreamIssueDataAfterTerminal KimiChatStreamIssue = "data_after_terminal"
)

type kimiChatStreamError struct {
	issue KimiChatStreamIssue
	cause error
}

func (e *kimiChatStreamError) Error() string {
	if e.cause == nil {
		return fmt.Sprintf("invalid current Kimi Chat stream: %s", e.issue)
	}
	return fmt.Sprintf("invalid current Kimi Chat stream: %s: %v", e.issue, e.cause)
}

func (e *kimiChatStreamError) Unwrap() error { return e.cause }

func invalidKimiChatStream(issue KimiChatStreamIssue, cause ...error) error {
	err := &kimiChatStreamError{issue: issue}
	if len(cause) > 0 {
		err.cause = cause[0]
	}
	return err
}

func KimiChatStreamIssueForError(err error) KimiChatStreamIssue {
	var streamErr *kimiChatStreamError
	if errors.As(err, &streamErr) {
		return streamErr.issue
	}
	return ""
}

func PrepareKimiChatStreamEvent(data []byte, mechanics KimiChatStreamMechanics) (KimiChatStreamPrepared, error) {
	prepared := KimiChatStreamPrepared{Body: data}
	if !mechanics.valid || mechanics.provider != "kimi" || mechanics.upstreamModel == "" ||
		mechanics.protocolProfileID == "" || mechanics.cacheUsageProfile == "" || len(mechanics.finishReasons) == 0 {
		return prepared, fmt.Errorf("invalid current Kimi Chat stream mechanics")
	}
	if mechanics.normalizeEmptyReasoning {
		normalized, changed, err := NormalizeKimiK26ChatStreamChunk(
			mechanics.provider, mechanics.upstreamModel, mechanics.protocolProfileID, data,
		)
		if err != nil {
			return prepared, invalidKimiChatStream(KimiChatStreamIssuePreparation, err)
		}
		prepared.Body, prepared.Changed = normalized, changed
	} else if err := strictjson.Validate(data); err != nil {
		return prepared, invalidKimiChatStream(KimiChatStreamIssueDocument, err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(prepared.Body, &envelope); err != nil || envelope == nil {
		return prepared, invalidKimiChatStream(KimiChatStreamIssueDocument, err)
	}
	rawChoices, exists := envelope["choices"]
	var choices []map[string]json.RawMessage
	if exists && !isJSONNullUsage(rawChoices) {
		if err := json.Unmarshal(rawChoices, &choices); err != nil {
			return prepared, invalidKimiChatStream(KimiChatStreamIssueChoices, err)
		}
	}
	prepared.ChoiceCount = len(choices)
	for _, choice := range choices {
		terminal := false
		if rawFinish, present := choice["finish_reason"]; present && !isJSONNullUsage(rawFinish) {
			var finish string
			if json.Unmarshal(rawFinish, &finish) != nil || !kimiChatStreamFinishSupported(mechanics, finish) {
				return prepared, invalidKimiChatStream(KimiChatStreamIssueFinishReason)
			}
			terminal = true
		}
		if len(choices) == 1 {
			prepared.TerminalChoice = terminal
		}
		if rawUsage, present := choice["usage"]; present && !isJSONNullUsage(rawUsage) {
			usage, err := parseKimiChatStreamUsageObject(rawUsage, mechanics.cacheUsageProfile)
			if err != nil {
				return prepared, invalidKimiChatStream(KimiChatStreamIssueUsage, err)
			}
			prepared.ChoiceUsages = append(prepared.ChoiceUsages, usage)
		}
	}
	if rawUsage, present := envelope["usage"]; present && !isJSONNullUsage(rawUsage) {
		usage, found, err := ParseOpenAICompatibleUsageWithCacheProfile(
			prepared.Body, EndpointChatCompletions, metering.UsageSourceUpstream, mechanics.cacheUsageProfile,
		)
		if err != nil || !found {
			return prepared, invalidKimiChatStream(KimiChatStreamIssueUsage, err)
		}
		prepared.HasTopLevelUsage = true
		prepared.TopLevelUsage = usage
	}
	prepared.UsageOnly = prepared.ChoiceCount == 0 && prepared.HasTopLevelUsage
	return prepared, nil
}

func kimiChatStreamFinishSupported(mechanics KimiChatStreamMechanics, finish string) bool {
	for _, candidate := range mechanics.finishReasons {
		if finish == candidate {
			return true
		}
	}
	return false
}

func parseKimiChatStreamUsageObject(raw json.RawMessage, cacheUsageProfile string) (Usage, error) {
	wrapped, err := json.Marshal(map[string]json.RawMessage{"usage": raw})
	if err != nil {
		return Usage{}, err
	}
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		wrapped, EndpointChatCompletions, metering.UsageSourceUpstream, cacheUsageProfile,
	)
	if err != nil {
		return Usage{}, err
	}
	if !present {
		return Usage{}, fmt.Errorf("usage object is missing")
	}
	return usage, nil
}

type KimiChatStreamState struct {
	mechanics         KimiChatStreamMechanics
	finished          bool
	topLevelUsageSeen bool
	usageOnlySeen     bool
	done              bool
	usage             *Usage
}

func NewKimiChatStreamState(mechanics KimiChatStreamMechanics) (*KimiChatStreamState, error) {
	if !mechanics.valid {
		return nil, fmt.Errorf("invalid current Kimi Chat stream mechanics")
	}
	return &KimiChatStreamState{mechanics: mechanics}, nil
}

func (s *KimiChatStreamState) Accept(prepared KimiChatStreamPrepared) error {
	if s == nil || !s.mechanics.valid {
		return fmt.Errorf("current Kimi Chat stream state is not initialized")
	}
	if s.done || s.usageOnlySeen {
		return invalidKimiChatStream(KimiChatStreamIssueDataAfterTerminal)
	}
	if prepared.ChoiceCount == 0 {
		if !s.finished || !prepared.HasTopLevelUsage || !prepared.UsageOnly || len(prepared.ChoiceUsages) != 0 {
			return invalidKimiChatStream(KimiChatStreamIssueUsageOrder)
		}
		if err := s.acceptUsage(prepared.TopLevelUsage); err != nil {
			return err
		}
		s.topLevelUsageSeen = true
		s.usageOnlySeen = true
		return nil
	}
	if prepared.ChoiceCount != 1 || s.finished {
		return invalidKimiChatStream(KimiChatStreamIssueChoices)
	}
	if (prepared.HasTopLevelUsage || len(prepared.ChoiceUsages) != 0) && !prepared.TerminalChoice {
		return invalidKimiChatStream(KimiChatStreamIssueUsageOrder)
	}
	for _, usage := range prepared.ChoiceUsages {
		if err := s.acceptUsage(usage); err != nil {
			return err
		}
	}
	if prepared.HasTopLevelUsage {
		if err := s.acceptUsage(prepared.TopLevelUsage); err != nil {
			return err
		}
		s.topLevelUsageSeen = true
	}
	if prepared.TerminalChoice {
		s.finished = true
	}
	return nil
}

func (s *KimiChatStreamState) AcceptDone() error {
	if s == nil || !s.mechanics.valid {
		return fmt.Errorf("current Kimi Chat stream state is not initialized")
	}
	if s.done || !s.finished || !s.topLevelUsageSeen {
		return invalidKimiChatStream(KimiChatStreamIssueTerminalOrder)
	}
	s.done = true
	return nil
}

func (s *KimiChatStreamState) FinishEOF() error {
	if s == nil || !s.mechanics.valid {
		return fmt.Errorf("current Kimi Chat stream state is not initialized")
	}
	if !s.done {
		return invalidKimiChatStream(KimiChatStreamIssueTerminalOrder)
	}
	return nil
}

func (s *KimiChatStreamState) acceptUsage(candidate Usage) error {
	if s.usage == nil {
		copy := candidate
		s.usage = &copy
		return nil
	}
	if !sameKimiChatStreamUsage(*s.usage, candidate) {
		return invalidKimiChatStream(KimiChatStreamIssueUsageConflict)
	}
	return nil
}

func sameKimiChatStreamUsage(left, right Usage) bool {
	return left.PromptTokens == right.PromptTokens &&
		left.UncachedPromptTokens == right.UncachedPromptTokens &&
		left.CacheReadTokens == right.CacheReadTokens &&
		left.CacheCreationTokens == right.CacheCreationTokens &&
		left.CompletionTokens == right.CompletionTokens &&
		left.TotalTokens == right.TotalTokens
}
