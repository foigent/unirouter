package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
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
