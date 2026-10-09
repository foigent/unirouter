package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

// ErrToolStreamModelMismatch is a fixed classification for diagnostics. It
// carries no provider-supplied model name or response content.
var ErrToolStreamModelMismatch = errors.New("tool stream model mismatch")

// ToolStreamEnvelope selects finite provider wire preparation for new exact
// implementations. It neither grants qualification nor owns tool assembly,
// downstream delivery, or settlement. Historical M111/M113 use no preparer.
type ToolStreamEnvelope struct {
	contract    modelprofile.ChatStreamContract
	model       string
	kimi        *KimiChatStreamState
	miniMaxTier bool
	qwen        bool
}

func NewToolStreamEnvelope(protocol, provider, model, cache string) (*ToolStreamEnvelope, error) {
	var kimi bool
	switch protocol {
	case "protocol.qwen-openai-chat.flash.v7", "protocol.qwen-openai-chat.flash.v8":
		if provider != "dashscope" || cache != "cache-usage.qwen-context-cache.v1" || model == "" {
			return nil, fmt.Errorf("Qwen tool stream component mismatch")
		}
		return &ToolStreamEnvelope{model: model, qwen: true}, nil
	case "protocol.minimax-m3-openai-chat.v11":
		if provider != "minimax" || cache != "cache-usage.minimax-openai.v2" || model == "" {
			return nil, fmt.Errorf("MiniMax tool stream component mismatch")
		}
		return &ToolStreamEnvelope{model: model, miniMaxTier: true}, nil
	case "protocol.deepseek-openai-chat.v5", modelprofile.DeepSeekProviderCompletionProtocol:
		if provider != "deepseek" || cache != "cache-usage.deepseek-v4.v1" {
			return nil, fmt.Errorf("DeepSeek tool stream component mismatch")
		}
	case "protocol.kimi-openai-chat.v6", "protocol.kimi-openai-chat.k3.v6":
		if provider != "kimi" || cache != kimiChatCacheUsageProfile {
			return nil, fmt.Errorf("Kimi tool stream component mismatch")
		}
		kimi = true
	default:
		return nil, nil
	}
	contract, ok := modelprofile.ResolveChatProtocolContract(protocol)
	if !ok || !contract.StreamContract.FunctionToolDeltas || model == "" {
		return nil, fmt.Errorf("unavailable exact tool stream implementation")
	}
	s := &ToolStreamEnvelope{contract: contract.StreamContract, model: model}
	if kimi {
		mechanics := KimiChatStreamMechanics{provider: provider, upstreamModel: model,
			protocolProfileID: protocol, cacheUsageProfile: cache,
			finishReasons: contract.StreamContract.FinishReasons, valid: true}
		var err error
		s.kimi, err = NewKimiChatStreamState(mechanics)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Prepare returns a canonical fact envelope. Skip is allowed only for Kimi's
// single, identical usage-only repeat following authoritative terminal usage.
// The full counter vector is checked by the existing Kimi state before skip;
// unknown metadata, conflicting usage and post-terminal model data still fail.
func (s *ToolStreamEnvelope) Prepare(data []byte) (body []byte, skip bool, err error) {
	if s == nil {
		return data, false, nil
	}
	if s.qwen {
		body, err := prepareQwenToolEnvelope(data, s.model)
		return body, false, err
	}
	if s.miniMaxTier {
		body, err := prepareMiniMaxM3StreamTier(data, s.model)
		return body, false, err
	}
	if err := strictjson.Validate(data); err != nil {
		return nil, false, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || envelope == nil {
		return nil, false, fmt.Errorf("invalid exact tool stream envelope")
	}
	if raw, exists := envelope["system_fingerprint"]; exists {
		var fingerprint *string
		if json.Unmarshal(raw, &fingerprint) != nil {
			return nil, false, fmt.Errorf("invalid system_fingerprint")
		}
		delete(envelope, "system_fingerprint")
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(envelope["choices"], &choices) != nil || choices == nil || len(choices) > 1 {
		return nil, false, fmt.Errorf("invalid tool stream choices")
	}
	for _, choice := range choices {
		if raw, exists := choice["logprobs"]; exists {
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return nil, false, fmt.Errorf("unmodeled non-null logprobs")
			}
			delete(choice, "logprobs")
		}
	}
	var prepared KimiChatStreamPrepared
	if s.kimi != nil {
		prepared, err = PrepareKimiChatStreamEvent(data, s.kimi.mechanics)
		if err != nil {
			return nil, false, err
		}
		// Only the supplier's exact terminal choice can provide this usage.
		// Projection preserves those bytes; no missing counters are invented.
		if len(prepared.ChoiceUsages) != 0 {
			if len(choices) != 1 || !prepared.TerminalChoice {
				return nil, false, fmt.Errorf("choice usage before tool stream terminal")
			}
			if !prepared.HasTopLevelUsage {
				envelope["usage"] = choices[0]["usage"]
				prepared.HasTopLevelUsage = true
				prepared.TopLevelUsage = prepared.ChoiceUsages[0]
			}
		}
		for _, choice := range choices {
			delete(choice, "usage")
		}
	}
	envelope["choices"], err = json.Marshal(choices)
	if err != nil {
		return nil, false, err
	}
	body, err = json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	var model string
	if json.Unmarshal(envelope["model"], &model) != nil || model != s.model {
		return nil, false, ErrToolStreamModelMismatch
	}
	if s.kimi != nil {
		skip = prepared.UsageOnly && s.kimi.usage != nil
		if err := s.kimi.Accept(prepared); err != nil {
			return nil, false, err
		}
	}
	// Safe, identity-bound usage is still a supplier fact when an unrelated
	// delivery field is invalid. Return the prepared bytes with that error;
	// callers must reject delivery, while Runtime may retain the usage fact.
	// Kimi's complete counter-vector and ordering checks above must succeed
	// before any such evidence can escape, including repeated usage frames.
	if _, err := ParseFunctionToolStreamFactsForContract(body, s.contract); err != nil {
		return body, false, err
	}
	return body, skip, nil
}

func (s *ToolStreamEnvelope) AcceptDone() error {
	if s != nil && s.kimi != nil {
		return s.kimi.AcceptDone()
	}
	return nil
}
