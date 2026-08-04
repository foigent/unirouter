// Package promptcompression implements UniRouter's content-ephemeral,
// deterministic prompt-compression shadow and canary evaluator.
//
// V1 deliberately removes whitespace only. It never calls another model,
// persists prompt content, or depends on X-Insight in the request path.
package promptcompression

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/tokenutil"
)

const (
	ContractVersion  = "unirouter.prompt-compression.evaluation.v1"
	AlgorithmVersion = "unirouter.prompt-compression.whitespace-safe.v1"
)

const (
	ReasonEligible            = "eligible"
	ReasonInvalidJSON         = "invalid_json"
	ReasonUnsupportedShape    = "unsupported_request_shape"
	ReasonStreaming           = "streaming_not_supported"
	ReasonTools               = "tools_not_supported"
	ReasonNoCompressibleText  = "no_compressible_user_text"
	ReasonBelowMinimumTokens  = "below_minimum_tokens"
	ReasonInsufficientSavings = "insufficient_estimated_savings"
	ReasonReductionLimit      = "maximum_reduction_exceeded"
	ReasonSafetyInvariant     = "safety_invariant_failed"
	ReasonQualityInvariant    = "quality_invariant_failed"
	ReasonSensitiveSyntax     = "sensitive_or_structured_syntax"
	ReasonStructuredControls  = "structured_controls_not_supported"
)

var (
	codeFencePattern  = regexp.MustCompile("(?m)`|~~~")
	urlPattern        = regexp.MustCompile(`(?i)https?://|www\.`)
	markupPattern     = regexp.MustCompile(`(?m)^\s*(?:#{1,6}\s|[-*+]\s|\d+[.)]\s|>\s)|\|`)
	xmlPattern        = regexp.MustCompile(`(?s)<[A-Za-z][^>]{0,120}>`)
	base64Pattern     = regexp.MustCompile(`(?:[A-Za-z0-9+/]{80,}={0,2})`)
	codeSyntaxPattern = regexp.MustCompile(`(?im)(?:^|\n)\s*(?:def|class|func|function|package|import|from|select|insert|update|delete|create|alter)\b|[{}\[\]]|(?:^|\n)\s*[A-Za-z_][A-Za-z0-9_.-]*\s*[:=]\s*\S`)
	horizontalPattern = regexp.MustCompile(`[ \t]+`)
	blankRunPattern   = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)
)

type Config struct {
	MinimumInputTokens  int `json:"minimum_input_tokens"`
	MinimumSavedTokens  int `json:"minimum_saved_tokens"`
	MaximumReductionBPS int `json:"maximum_reduction_basis_points"`
}

func DefaultConfig() Config {
	return Config{MinimumInputTokens: 128, MinimumSavedTokens: 8, MaximumReductionBPS: 2500}
}

func (c Config) Validate() bool {
	return c.MinimumInputTokens >= 1 && c.MinimumInputTokens <= 1_000_000 &&
		c.MinimumSavedTokens >= 1 && c.MinimumSavedTokens <= 1_000_000 &&
		c.MaximumReductionBPS >= 1 && c.MaximumReductionBPS <= 5000
}

type Result struct {
	ContractVersion      string `json:"contract_version"`
	AlgorithmVersion     string `json:"algorithm_version"`
	Eligible             bool   `json:"eligible"`
	Reason               string `json:"reason"`
	OriginalBytes        int    `json:"original_bytes"`
	CandidateBytes       int    `json:"candidate_bytes"`
	OriginalTokens       int    `json:"original_estimated_tokens"`
	CandidateTokens      int    `json:"candidate_estimated_tokens"`
	SavedTokens          int    `json:"estimated_saved_tokens"`
	ReductionBasisPoints int    `json:"reduction_basis_points"`
	SafetyPassed         bool   `json:"safety_passed"`
	QualityPassed        bool   `json:"quality_passed"`
	CandidateBody        []byte `json:"-"`
}

type wireRequest struct {
	Stream            bool            `json:"stream"`
	Messages          []wireMessage   `json:"messages"`
	Tools             json.RawMessage `json:"tools"`
	ToolChoice        json.RawMessage `json:"tool_choice"`
	ParallelToolCalls json.RawMessage `json:"parallel_tool_calls"`
	Functions         json.RawMessage `json:"functions"`
	FunctionCall      json.RawMessage `json:"function_call"`
	ResponseFormat    json.RawMessage `json:"response_format"`
	Modalities        json.RawMessage `json:"modalities"`
	Audio             json.RawMessage `json:"audio"`
}

type wireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Evaluate builds an in-memory candidate and returns metadata-only evidence.
// The caller owns CandidateBody and must not persist it as evaluation evidence.
func Evaluate(body []byte, config Config) Result {
	result := Result{
		ContractVersion:  ContractVersion,
		AlgorithmVersion: AlgorithmVersion,
		Reason:           ReasonUnsupportedShape,
		OriginalBytes:    len(body),
		CandidateBytes:   len(body),
	}
	if !config.Validate() || len(body) == 0 {
		return result
	}
	if strict := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strict != nil {
		result.Reason = ReasonInvalidJSON
		return result
	}
	var request wireRequest
	if err := json.Unmarshal(body, &request); err != nil || len(request.Messages) == 0 {
		result.Reason = ReasonUnsupportedShape
		return result
	}
	if request.Stream {
		result.Reason = ReasonStreaming
		return result
	}
	if presentJSON(request.Tools) || presentJSON(request.ToolChoice) ||
		presentJSON(request.ParallelToolCalls) || presentJSON(request.Functions) ||
		presentJSON(request.FunctionCall) {
		result.Reason = ReasonTools
		return result
	}
	if presentJSON(request.ResponseFormat) || presentJSON(request.Modalities) || presentJSON(request.Audio) {
		result.Reason = ReasonStructuredControls
		return result
	}

	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil || document == nil {
		result.Reason = ReasonUnsupportedShape
		return result
	}
	messages, ok := document["messages"].([]any)
	if !ok || len(messages) != len(request.Messages) {
		result.Reason = ReasonUnsupportedShape
		return result
	}

	originalTexts := make([]string, 0, len(messages))
	candidateTexts := make([]string, 0, len(messages))
	changed := false
	qualityPassed := true
	for index, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			result.Reason = ReasonUnsupportedShape
			return result
		}
		role, _ := message["role"].(string)
		content, ok := message["content"].(string)
		if !ok {
			result.Reason = ReasonUnsupportedShape
			return result
		}
		originalTexts = append(originalTexts, content)
		candidate := content
		if strings.EqualFold(strings.TrimSpace(role), "user") && strings.TrimSpace(content) != "" {
			if sensitiveSyntax(content) {
				result.Reason = ReasonSensitiveSyntax
				return result
			}
			candidate = compressWhitespace(content)
			if candidate != content {
				message["content"] = candidate
				messages[index] = message
				changed = true
			}
		}
		candidateTexts = append(candidateTexts, candidate)
		if strings.Join(strings.Fields(content), " ") != strings.Join(strings.Fields(candidate), " ") {
			qualityPassed = false
		}
	}
	if !changed {
		result.Reason = ReasonNoCompressibleText
		return result
	}

	originalJoined := strings.Join(originalTexts, "\x00")
	candidateJoined := strings.Join(candidateTexts, "\x00")
	result.OriginalTokens = tokenutil.EstimateTokens(originalJoined)
	result.CandidateTokens = tokenutil.EstimateTokens(candidateJoined)
	result.SavedTokens = result.OriginalTokens - result.CandidateTokens
	if result.OriginalTokens < config.MinimumInputTokens {
		result.Reason = ReasonBelowMinimumTokens
		return result
	}
	if result.SavedTokens < config.MinimumSavedTokens {
		result.Reason = ReasonInsufficientSavings
		return result
	}
	result.ReductionBasisPoints = result.SavedTokens * 10_000 / result.OriginalTokens
	if result.ReductionBasisPoints > config.MaximumReductionBPS {
		result.Reason = ReasonReductionLimit
		return result
	}
	result.SafetyPassed = stripWhitespace(originalJoined) == stripWhitespace(candidateJoined)
	if !result.SafetyPassed {
		result.Reason = ReasonSafetyInvariant
		return result
	}
	result.QualityPassed = qualityPassed
	if !result.QualityPassed {
		result.Reason = ReasonQualityInvariant
		return result
	}
	document["messages"] = messages
	candidateBody, err := json.Marshal(document)
	if err != nil {
		result.Reason = ReasonUnsupportedShape
		return result
	}
	result.Eligible = true
	result.Reason = ReasonEligible
	result.CandidateBody = candidateBody
	result.CandidateBytes = len(candidateBody)
	return result
}

func presentJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("[]"))
}

func sensitiveSyntax(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || codeFencePattern.MatchString(value) || urlPattern.MatchString(value) ||
		markupPattern.MatchString(value) || xmlPattern.MatchString(value) || base64Pattern.MatchString(value) ||
		codeSyntaxPattern.MatchString(value) {
		return true
	}
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)) {
		return true
	}
	return false
}

func compressWhitespace(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(horizontalPattern.ReplaceAllString(lines[index], " "))
	}
	value = strings.Join(lines, "\n")
	value = blankRunPattern.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			return -1
		default:
			return r
		}
	}, value)
}
