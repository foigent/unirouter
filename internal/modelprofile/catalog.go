// Package modelprofile owns the immutable, repository-reviewed model execution
// profile catalog. A profile describes provider capability; customer model
// registry limits remain explicit policy and may only narrow those maxima.
package modelprofile

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

const (
	UnverifiedProfileID      = "unverified.chat"
	UnverifiedProfileVersion = "1.0.0"

	TokenizerByteUpperID       = "tokenizer.byte-upper"
	TokenizerByteUpperVersion  = "1.0.0"
	TokenizerKimiK26ID         = "tokenizer.kimi-k2.6"
	TokenizerKimiK26Version    = "1.0.0"
	TokenizerMiniMaxM27ID      = "tokenizer.minimax-m2.7"
	TokenizerMiniMaxM27Version = "1.0.0"
)

// Profile is one immutable execution-contract bundle. Individual component
// identities remain explicit so evidence can say which axis is verified and
// future profiles may safely share a tokenizer without sharing reasoning or
// protocol behavior.
type Profile struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Label   string `json:"label"`

	TokenizerProfileID      string `json:"tokenizer_profile_id"`
	TokenizerProfileVersion string `json:"tokenizer_profile_version"`
	ReasoningProfile        string `json:"reasoning_profile"`
	CacheUsageProfile       string `json:"cache_usage_profile"`
	ProtocolProfile         string `json:"protocol_profile"`

	ProviderContextWindow int `json:"provider_context_window"`
	ProviderMaxOutput     int `json:"provider_max_output"`
	DefaultContextWindow  int `json:"default_context_window"`
	DefaultOutputLimit    int `json:"default_output_limit"`

	// ProviderCapabilities records what the provider documents. Capabilities is
	// the narrower set that UniRouter has verified end-to-end and may safely
	// apply to a model registry record. Keeping the two sets distinct prevents a
	// provider feature such as URL-based vision from bypassing gateway admission
	// and guardrail support merely because the upstream API advertises it.
	ProviderCapabilities []string `json:"provider_capabilities"`
	Capabilities         []string `json:"capabilities"`
	Aliases              []string `json:"aliases"`
	EvidenceRefs         []string `json:"evidence_refs"`
}

var catalog = append([]Profile{
	{
		ID: UnverifiedProfileID, Version: UnverifiedProfileVersion, Label: "自定义 / 未验证模型",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.unverified.v1", CacheUsageProfile: "cache-usage.unverified.v1",
		ProtocolProfile: "protocol.openai-compatible.unverified.v1",
	},
	{
		ID: "deepseek.v4-flash.api", Version: "1.1.0", Label: "DeepSeek V4 Flash 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.deepseek-thinking-toggle.v2", CacheUsageProfile: "cache-usage.deepseek-v4.v1",
		ProtocolProfile:       "protocol.deepseek-openai-chat.v1",
		ProviderContextWindow: 1_000_000, ProviderMaxOutput: 384_000,
		DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
		Capabilities:         []string{"reasoning", "tools", "json_output", "prompt_cache"},
		ProviderCapabilities: []string{"reasoning", "tools", "json_output", "prompt_cache"},
		Aliases:              []string{"DeepSeek V4 Flash", "DeepSeek-V4-Flash", "deepseek-v4-flash"},
		EvidenceRefs: []string{
			"https://api-docs.deepseek.com/zh-cn/quick_start/pricing/",
			"https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/",
			"https://api-docs.deepseek.com/zh-cn/api/create-chat-completion/",
		},
	},
	{
		ID: "deepseek.v4-flash.api", Version: "1.0.0", Label: "DeepSeek V4 Flash 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.deepseek-thinking-toggle.v1", CacheUsageProfile: "cache-usage.deepseek-v4.v1",
		ProtocolProfile:       "protocol.deepseek-openai-chat.v1",
		ProviderContextWindow: 1_000_000, ProviderMaxOutput: 384_000,
		DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
		Capabilities:         []string{"reasoning", "tools", "json_output", "prompt_cache"},
		ProviderCapabilities: []string{"reasoning", "tools", "json_output", "prompt_cache"},
		Aliases:              []string{"DeepSeek V4 Flash", "DeepSeek-V4-Flash", "deepseek-v4-flash"},
		EvidenceRefs: []string{
			"https://api-docs.deepseek.com/quick_start/pricing/",
			"https://api-docs.deepseek.com/guides/thinking_mode",
			"https://api-docs.deepseek.com/api/create-chat-completion",
		},
	},
	{
		ID: "deepseek.v4-pro.api", Version: "1.1.0", Label: "DeepSeek V4 Pro 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.deepseek-thinking-toggle.v2", CacheUsageProfile: "cache-usage.deepseek-v4.v1",
		ProtocolProfile:       "protocol.deepseek-openai-chat.v1",
		ProviderContextWindow: 1_000_000, ProviderMaxOutput: 384_000,
		DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
		Capabilities:         []string{"reasoning", "tools", "json_output", "prompt_cache"},
		ProviderCapabilities: []string{"reasoning", "tools", "json_output", "prompt_cache"},
		Aliases:              []string{"DeepSeek V4 Pro", "DeepSeek-V4-Pro", "deepseek-v4-pro"},
		EvidenceRefs: []string{
			"https://api-docs.deepseek.com/zh-cn/quick_start/pricing/",
			"https://api-docs.deepseek.com/zh-cn/guides/thinking_mode/",
			"https://api-docs.deepseek.com/zh-cn/api/create-chat-completion/",
		},
	},
	{
		ID: "deepseek.v4-pro.api", Version: "1.0.0", Label: "DeepSeek V4 Pro 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.deepseek-thinking-toggle.v1", CacheUsageProfile: "cache-usage.deepseek-v4.v1",
		ProtocolProfile:       "protocol.deepseek-openai-chat.v1",
		ProviderContextWindow: 1_000_000, ProviderMaxOutput: 384_000,
		DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
		Capabilities:         []string{"reasoning", "tools", "json_output", "prompt_cache"},
		ProviderCapabilities: []string{"reasoning", "tools", "json_output", "prompt_cache"},
		Aliases:              []string{"DeepSeek V4 Pro", "DeepSeek-V4-Pro", "deepseek-v4-pro"},
		EvidenceRefs: []string{
			"https://api-docs.deepseek.com/quick_start/pricing/",
			"https://api-docs.deepseek.com/guides/thinking_mode",
			"https://api-docs.deepseek.com/api/create-chat-completion",
		},
	},
	{
		ID: "kimi.k2.6.api", Version: "1.5.0", Label: "Kimi K2.6 官方 API",
		TokenizerProfileID: TokenizerKimiK26ID, TokenizerProfileVersion: TokenizerKimiK26Version,
		ReasoningProfile: "reasoning.kimi-k2.6-thinking-toggle.v2", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v3",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", "json_object", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K2.6", "kimi-k2.6", "kimi-k2.6-preview"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k26",
			"https://platform.kimi.com/docs/guide/kimi-k2-6-quickstart",
			"https://platform.kimi.com/docs/api/models-overview",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/api/estimate",
			"https://huggingface.co/moonshotai/Kimi-K2.6/tree/7eb5002f6aadc958aed6a9177b7ed26bb94011bb",
			"https://platform.kimi.com/docs/guide/use-json-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-web-search",
			"https://platform.kimi.com/docs/pricing/tools",
		},
	},
	{
		ID: "kimi.k2.6.api", Version: "1.4.0", Label: "Kimi K2.6 官方 API",
		TokenizerProfileID: TokenizerKimiK26ID, TokenizerProfileVersion: TokenizerKimiK26Version,
		ReasoningProfile: "reasoning.kimi-k2.6-thinking-toggle.v2", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v2",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", "json_object", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", CapabilityPartialMode,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K2.6", "kimi-k2.6", "kimi-k2.6-preview"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k26",
			"https://platform.kimi.com/docs/guide/kimi-k2-6-quickstart",
			"https://platform.kimi.com/docs/api/models-overview",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/api/estimate",
			"https://huggingface.co/moonshotai/Kimi-K2.6/tree/7eb5002f6aadc958aed6a9177b7ed26bb94011bb",
			"https://platform.kimi.com/docs/guide/use-json-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
		},
	},
	{
		ID: "kimi.k2.6.api", Version: "1.3.0", Label: "Kimi K2.6 官方 API",
		TokenizerProfileID: TokenizerKimiK26ID, TokenizerProfileVersion: TokenizerKimiK26Version,
		ReasoningProfile: "reasoning.kimi-k2.6-thinking-toggle.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v2",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", CapabilityPartialMode, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K2.6", "kimi-k2.6", "kimi-k2.6-preview"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k26",
			"https://platform.kimi.com/docs/guide/kimi-k2-6-quickstart",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/api/estimate",
			"https://huggingface.co/moonshotai/Kimi-K2.6/tree/7eb5002f6aadc958aed6a9177b7ed26bb94011bb",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
		},
	},
	{
		ID: "kimi.k2.6.api", Version: "1.2.0", Label: "Kimi K2.6 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k2.6-thinking-toggle.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v2",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", CapabilityPartialMode, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K2.6", "kimi-k2.6", "kimi-k2.6-preview"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k26",
			"https://platform.kimi.com/docs/guide/kimi-k2-6-quickstart",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
		},
	},
	{
		ID: "kimi.k2.6.api", Version: "1.1.0", Label: "Kimi K2.6 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k2.6-thinking-toggle.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v2",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", CapabilityPartialMode, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K2.6", "kimi-k2.6", "kimi-k2.6-preview"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k26",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-web-search",
		},
	},
	{
		ID: "kimi.k2.6.api", Version: "1.0.0", Label: "Kimi K2.6 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k2.6-thinking-toggle.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v1",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{"reasoning", "tools", "vision", "prompt_cache"},
		Capabilities:         []string{"reasoning", "tools", "prompt_cache"},
		Aliases:              []string{"Kimi K2.6", "kimi-k2.6", "kimi-k2.6-preview"},
		EvidenceRefs:         []string{"https://platform.moonshot.ai/docs"},
	},
	{
		ID: "kimi.k2.7-code.api", Version: "1.1.0", Label: "Kimi K2.7 Code 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k2.7-thinking-required.v2", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.v2",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", "json_object", CapabilityPartialMode,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", CapabilityPartialMode,
			CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K2.7 Code", "kimi-k2.7-code", "kimi-k2.7-code-highspeed"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k27-code",
			"https://platform.kimi.com/docs/guide/kimi-k2-7-code-quickstart",
			"https://platform.kimi.com/docs/api/models-overview",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-context-caching-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-json-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
		},
	},
	{
		ID: "kimi.k2.7-code.api", Version: "1.0.0", Label: "Kimi K2.7 Code",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k2.7-thinking-required.v1", CacheUsageProfile: "cache-usage.kimi-coding.v1",
		ProtocolProfile:       "protocol.kimi-coding-openai-chat.v1",
		ProviderContextWindow: 262_144, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{"reasoning", "tools", "vision", "video", "prompt_cache"},
		Capabilities:         []string{"reasoning", "tools"},
		Aliases:              []string{"Kimi K2.7 Code", "kimi-for-coding", "kimi-for-coding-highspeed"},
		EvidenceRefs:         []string{"https://www.kimi.com/code/docs/en/kimi-code/models.html"},
	},
	{
		ID: "kimi.k3.api", Version: "1.5.0", Label: "Kimi K3 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k3-effort.v2", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.k3.v2",
		ProviderContextWindow: 1_048_576, ProviderMaxOutput: 1_048_576,
		DefaultContextWindow: 262_144, DefaultOutputLimit: 131_072,
		ProviderCapabilities: []string{
			"reasoning", "tools", "tool_choice", CapabilityDynamicTools, "json_object", "json_schema",
			CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", "json_schema", CapabilityDynamicTools,
			CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K3", "kimi-k3", "k3", "k3-256k"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k3",
			"https://platform.kimi.com/docs/guide/kimi-k3-quickstart",
			"https://platform.kimi.com/docs/guide/use-reasoning-effort",
			"https://platform.kimi.com/docs/api/models-overview",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-dynamic-tool-loading",
			"https://platform.kimi.com/docs/guide/use-web-search",
			"https://platform.kimi.com/docs/pricing/tools",
		},
	},
	{
		ID: "kimi.k3.api", Version: "1.4.0", Label: "Kimi K3 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k3-effort.v2", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.k3.v2",
		ProviderContextWindow: 1_048_576, ProviderMaxOutput: 1_048_576,
		DefaultContextWindow: 262_144, DefaultOutputLimit: 131_072,
		ProviderCapabilities: []string{
			"reasoning", "tools", "tool_choice", CapabilityDynamicTools, "json_object", "json_schema",
			CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", "json_schema", CapabilityDynamicTools,
			CapabilityPartialMode, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K3", "kimi-k3", "k3", "k3-256k"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k3",
			"https://platform.kimi.com/docs/guide/kimi-k3-quickstart",
			"https://platform.kimi.com/docs/guide/use-reasoning-effort",
			"https://platform.kimi.com/docs/api/models-overview",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-dynamic-tool-loading",
		},
	},
	{
		ID: "kimi.k3.api", Version: "1.3.0", Label: "Kimi K3 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k3-effort.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.k3.v2",
		ProviderContextWindow: 1_048_576, ProviderMaxOutput: 1_048_576,
		DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", "tool_choice", CapabilityDynamicTools, "json_object", "json_schema",
			CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", "json_schema", CapabilityDynamicTools,
			CapabilityPartialMode, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K3", "kimi-k3", "k3", "k3-256k"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k3",
			"https://platform.kimi.com/docs/guide/kimi-k3-quickstart",
			"https://platform.kimi.com/docs/guide/use-reasoning-effort",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-dynamic-tool-loading",
		},
	},
	{
		ID: "kimi.k3.api", Version: "1.2.0", Label: "Kimi K3 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k3-effort.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.k3.v2",
		ProviderContextWindow: 1_048_576, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", "tool_choice", CapabilityDynamicTools, "json_object", "json_schema",
			CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Capabilities: []string{
			"reasoning", "tools", "json_object", "json_schema", CapabilityDynamicTools,
			CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo, "prompt_cache",
		},
		Aliases: []string{"Kimi K3", "kimi-k3", "k3", "k3-256k"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k3",
			"https://platform.kimi.com/docs/guide/use-reasoning-effort",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-partial-mode-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-dynamic-tool-loading",
			"https://platform.kimi.com/docs/guide/use-web-search",
		},
	},
	{
		ID: "kimi.k3.api", Version: "1.1.0", Label: "Kimi K3 官方 API",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k3-effort.v1", CacheUsageProfile: "cache-usage.kimi-platform.v1",
		ProtocolProfile:       "protocol.kimi-openai-chat.k3.v1",
		ProviderContextWindow: 1_048_576, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{
			"reasoning", "tools", "tool_choice", "dynamic_tools", "json_object", "json_schema",
			"partial_mode", "builtin_web_search", "vision", "video", "prompt_cache",
		},
		Capabilities: []string{"reasoning", "tools", "json_object", "json_schema", "prompt_cache"},
		Aliases:      []string{"Kimi K3", "kimi-k3", "k3", "k3-256k"},
		EvidenceRefs: []string{
			"https://platform.kimi.com/docs/pricing/chat-k3",
			"https://platform.kimi.com/docs/guide/use-reasoning-effort",
			"https://platform.kimi.com/docs/api/chat",
			"https://platform.kimi.com/docs/guide/use-context-caching-feature-of-kimi-api",
			"https://platform.kimi.com/docs/guide/use-tool-choice",
		},
	},
	{
		ID: "kimi.k3.api", Version: "1.0.0", Label: "Kimi K3（套餐上限受权益约束）",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.kimi-k3-effort.v1", CacheUsageProfile: "cache-usage.kimi-coding.v1",
		ProtocolProfile:       "protocol.kimi-coding-openai-chat.v1",
		ProviderContextWindow: 1_048_576, DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
		ProviderCapabilities: []string{"reasoning", "tools", "vision", "video", "prompt_cache"},
		Capabilities:         []string{"reasoning", "tools"},
		Aliases:              []string{"Kimi K3", "k3", "k3-256k"},
		EvidenceRefs:         []string{"https://www.kimi.com/code/docs/en/kimi-code/models.html"},
	},
	{
		ID: "minimax.m2.7.token-plan", Version: "1.0.0", Label: "MiniMax M2.7 Token Plan",
		TokenizerProfileID: TokenizerMiniMaxM27ID, TokenizerProfileVersion: TokenizerMiniMaxM27Version,
		ReasoningProfile: "reasoning.minimax-m2-thinking.v1", CacheUsageProfile: "cache-usage.minimax-m2.v1",
		ProtocolProfile:       "protocol.minimax-openai-chat.v2",
		ProviderContextWindow: 204_800, ProviderMaxOutput: 204_800,
		DefaultContextWindow: 204_800, DefaultOutputLimit: 65_536,
		ProviderCapabilities: []string{"reasoning", "tools", "prompt_cache"},
		Capabilities:         []string{"tools"},
		Aliases:              []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},
		EvidenceRefs: []string{
			"https://platform.minimaxi.com/docs/api-reference/api-overview",
			"https://platform.minimaxi.com/docs/api-reference/text-chat-openai",
			"https://platform.minimaxi.com/docs/token-plan/intro",
			"https://huggingface.co/MiniMaxAI/MiniMax-M2.7/commit/d494266a4affc0d2995ba1fa35c8481cbd84294b",
		},
	},
	{
		ID: "minimax.m2.5-highspeed.token-plan", Version: "1.0.0", Label: "MiniMax M2.5 Highspeed Token Plan",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.minimax-m2-thinking.v1", CacheUsageProfile: "cache-usage.minimax-m2.v1",
		ProtocolProfile:       "protocol.minimax-openai-chat.v2",
		ProviderContextWindow: 204_800, ProviderMaxOutput: 204_800,
		DefaultContextWindow: 204_800, DefaultOutputLimit: 65_536,
		ProviderCapabilities: []string{"reasoning", "tools", "prompt_cache"},
		Capabilities:         []string{"tools"},
		Aliases:              []string{"MiniMax-M2.5-highspeed"},
		EvidenceRefs: []string{
			"https://platform.minimaxi.com/docs/api-reference/api-overview",
			"https://platform.minimaxi.com/docs/api-reference/text-chat-openai",
			"https://platform.minimaxi.com/docs/token-plan/intro",
		},
	},
	{
		ID: "minimax.m3.token-plan", Version: "1.0.0", Label: "MiniMax M3 Token Plan",
		TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
		ReasoningProfile: "reasoning.minimax-m3.v1", CacheUsageProfile: "cache-usage.minimax-m3.v1",
		ProtocolProfile:       "protocol.minimax-openai-chat.v2",
		ProviderContextWindow: 1_000_000, ProviderMaxOutput: 524_288,
		DefaultContextWindow: 1_000_000, DefaultOutputLimit: 131_072,
		ProviderCapabilities: []string{"reasoning", "tools", "vision", "video", "prompt_cache"},
		Capabilities:         []string{"tools"},
		Aliases:              []string{"MiniMax M3", "MiniMax-M3"},
		EvidenceRefs: []string{
			"https://platform.minimaxi.com/docs/api-reference/api-overview",
			"https://platform.minimaxi.com/docs/api-reference/text-chat-openai",
			"https://platform.minimaxi.com/docs/token-plan/intro",
			"https://platform.minimaxi.com/docs/token-plan/openclaw",
		},
	},
}, currentProviderProfiles()...)

// currentProviderProfiles contains provider-documented onboarding contracts.
// Capabilities intentionally remains empty until the exact model x operation x
// capability is locally conformant; ProviderCapabilities must not silently
// become routing or admission authority.
func currentProviderProfiles() []Profile {
	return []Profile{
		{
			ID: "glm.5.3.api", Version: "1.0.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_output", "prompt_cache"},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.0.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_output", "prompt_cache"},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.0.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-toggle-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.documented.v1",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "qwen.3.8-max.api", Version: "1.0.0", Label: "Qwen3.8-Max 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.qwen-hybrid-documented.v1", CacheUsageProfile: "cache-usage.qwen-documented.v1",
			ProtocolProfile:       "protocol.qwen-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 131_072,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"Qwen3.8-Max", "qwen3.8-max"},
			EvidenceRefs:         []string{"https://help.aliyun.com/zh/model-studio/qwen3-8-max"},
		},
		{
			ID: "qwen.3.7-plus.api", Version: "1.0.0", Label: "Qwen3.7-Plus 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.qwen-hybrid-documented.v1", CacheUsageProfile: "cache-usage.qwen-documented.v1",
			ProtocolProfile:       "protocol.qwen-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 131_072,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"Qwen3.7-Plus", "qwen3.7-plus"},
			EvidenceRefs:         []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-plus"},
		},
		{
			ID: "qwen.3.7-flash.api", Version: "1.0.0", Label: "Qwen3.7-Flash 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.qwen-hybrid-documented.v1", CacheUsageProfile: "cache-usage.qwen-documented.v1",
			ProtocolProfile:       "protocol.qwen-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 131_072,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"Qwen3.7-Flash", "qwen3.7-flash"},
			EvidenceRefs:         []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-flash"},
		},
		{
			ID: "gemini.3.7-flash.api", Version: "1.0.0", Label: "Gemini 3.7 Flash 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.gemini-thinking-level-documented.v1", CacheUsageProfile: "cache-usage.gemini-documented.v1",
			ProtocolProfile:       "protocol.gemini-openai-chat.documented.v1",
			ProviderContextWindow: 1_048_576, ProviderMaxOutput: 65_536,
			DefaultContextWindow: 1_048_576, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "video", "audio", "files", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"Gemini 3.7 Flash", "gemini-3.7-flash"},
			EvidenceRefs: []string{
				"https://ai.google.dev/gemini-api/docs/models/gemini-3.7-flash",
				"https://ai.google.dev/gemini-api/docs/pricing",
			},
		},
		{
			ID: "gemini.3.5-flash.api", Version: "1.0.0", Label: "Gemini 3.5 Flash 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.gemini-thinking-level-documented.v1", CacheUsageProfile: "cache-usage.gemini-documented.v1",
			ProtocolProfile:       "protocol.gemini-openai-chat.documented.v1",
			ProviderContextWindow: 1_048_576, ProviderMaxOutput: 65_536,
			DefaultContextWindow: 1_048_576, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "audio", "files", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"Gemini 3.5 Flash", "gemini-3.5-flash"},
			EvidenceRefs: []string{
				"https://ai.google.dev/gemini-api/docs/models/gemini-3.5-flash",
				"https://ai.google.dev/gemini-api/docs/pricing",
			},
		},
		{
			ID: "gemini.3.5-flash-lite.api", Version: "1.0.0", Label: "Gemini 3.5 Flash-Lite 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.gemini-thinking-level-documented.v1", CacheUsageProfile: "cache-usage.gemini-documented.v1",
			ProtocolProfile:       "protocol.gemini-openai-chat.documented.v1",
			ProviderContextWindow: 1_048_576, ProviderMaxOutput: 65_536,
			DefaultContextWindow: 1_048_576, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "audio", "files", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"Gemini 3.5 Flash-Lite", "gemini-3.5-flash-lite"},
			EvidenceRefs: []string{
				"https://ai.google.dev/gemini-api/docs/models/gemini-3.5-flash-lite",
				"https://ai.google.dev/gemini-api/docs/pricing",
			},
		},
		{
			ID: "mimo.v2.5-pro.api", Version: "1.0.0", Label: "MiMo V2.5 Pro 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.mimo-documented.v1", CacheUsageProfile: "cache-usage.mimo-documented.v1",
			ProtocolProfile:       "protocol.mimo-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 131_072,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"MiMo V2.5 Pro", "mimo-v2.5-pro"},
			EvidenceRefs: []string{
				"https://mimo.mi.com/docs/en-US/quick-start/model",
				"https://mimo.mi.com/docs/en-US/price/pay-as-you-go",
			},
		},
		{
			ID: "mimo.v2.5.api", Version: "1.0.0", Label: "MiMo V2.5 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.mimo-documented.v1", CacheUsageProfile: "cache-usage.mimo-documented.v1",
			ProtocolProfile:       "protocol.mimo-openai-chat.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"MiMo V2.5", "mimo-v2.5"},
			EvidenceRefs: []string{
				"https://mimo.mi.com/docs/en-US/quick-start/model",
				"https://mimo.mi.com/docs/en-US/price/pay-as-you-go",
			},
		},
		{
			ID: "hy3.api", Version: "1.0.0", Label: "腾讯混元 Hy3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.hy3-documented.v1", CacheUsageProfile: "cache-usage.hy3-documented.v1",
			ProtocolProfile:       "protocol.hy3-openai-chat.documented.v1",
			ProviderContextWindow: 262_144, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 262_144, DefaultOutputLimit: 16_384,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "prompt_cache"},
			Aliases:              []string{"Hy3", "hy3"},
			EvidenceRefs: []string{
				"https://cloud.tencent.com/document/product/1823/130051",
				"https://cloud.tencent.com/document/product/1759/127342",
			},
		},
		{
			ID: "openai.gpt-5.6-sol.api", Version: "1.0.0", Label: "GPT-5.6 Sol 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.openai-gpt-5.6-documented.v1", CacheUsageProfile: "cache-usage.openai-documented.v1",
			ProtocolProfile:       "protocol.openai-responses.documented.v1",
			ProviderContextWindow: 1_050_000, ProviderMaxOutput: 128_000,
			DefaultContextWindow: 1_050_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"GPT-5.6 Sol", "gpt-5.6-sol"},
			EvidenceRefs:         []string{"https://developers.openai.com/api/docs/models/gpt-5.6-sol"},
		},
		{
			ID: "openai.gpt-5.6-terra.api", Version: "1.0.0", Label: "GPT-5.6 Terra 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.openai-gpt-5.6-documented.v1", CacheUsageProfile: "cache-usage.openai-documented.v1",
			ProtocolProfile:       "protocol.openai-responses.documented.v1",
			ProviderContextWindow: 1_050_000, ProviderMaxOutput: 128_000,
			DefaultContextWindow: 1_050_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"GPT-5.6 Terra", "gpt-5.6-terra"},
			EvidenceRefs:         []string{"https://developers.openai.com/api/docs/models/gpt-5.6-terra"},
		},
		{
			ID: "openai.gpt-5.6-luna.api", Version: "1.0.0", Label: "GPT-5.6 Luna 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.openai-gpt-5.6-documented.v1", CacheUsageProfile: "cache-usage.openai-documented.v1",
			ProtocolProfile:       "protocol.openai-responses.documented.v1",
			ProviderContextWindow: 1_050_000, ProviderMaxOutput: 128_000,
			DefaultContextWindow: 1_050_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"GPT-5.6 Luna", "gpt-5.6-luna"},
			EvidenceRefs:         []string{"https://developers.openai.com/api/docs/models/gpt-5.6-luna"},
		},
		{
			ID: "xai.grok-4.6.api", Version: "1.0.0", Label: "Grok 4.6 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.xai-documented.v1", CacheUsageProfile: "cache-usage.xai-documented.v1",
			ProtocolProfile:       "protocol.xai-openai-chat.documented.v1",
			ProviderContextWindow: 500_000,
			DefaultContextWindow:  500_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"Grok 4.6", "grok-4.6"},
			EvidenceRefs: []string{
				"https://docs.x.ai/developers/models",
				"https://docs.x.ai/developers/pricing",
			},
		},
		{
			ID: "anthropic.claude-opus-5.api", Version: "1.0.0", Label: "Claude Opus 5 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.anthropic-documented.v1", CacheUsageProfile: "cache-usage.anthropic-ttl-documented.v1",
			ProtocolProfile:       "protocol.anthropic-messages.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 128_000,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "prompt_cache"},
			Aliases:              []string{"Claude Opus 5", "claude-opus-5"},
			EvidenceRefs: []string{
				"https://platform.claude.com/docs/en/about-claude/models/overview",
				"https://platform.claude.com/docs/en/about-claude/pricing",
			},
		},
		{
			ID: "anthropic.claude-sonnet-5.api", Version: "1.0.0", Label: "Claude Sonnet 5 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.anthropic-documented.v1", CacheUsageProfile: "cache-usage.anthropic-ttl-documented.v1",
			ProtocolProfile:       "protocol.anthropic-messages.documented.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 128_000,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "prompt_cache"},
			Aliases:              []string{"Claude Sonnet 5", "claude-sonnet-5"},
			EvidenceRefs: []string{
				"https://platform.claude.com/docs/en/about-claude/models/overview",
				"https://platform.claude.com/docs/en/about-claude/pricing",
			},
		},
		{
			ID: "anthropic.claude-haiku-4.5.api", Version: "1.0.0", Label: "Claude Haiku 4.5 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.anthropic-documented.v1", CacheUsageProfile: "cache-usage.anthropic-ttl-documented.v1",
			ProtocolProfile:       "protocol.anthropic-messages.documented.v1",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 64_000,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 16_384,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "prompt_cache"},
			Aliases:              []string{"Claude Haiku 4.5", "claude-haiku-4-5-20251001"},
			EvidenceRefs: []string{
				"https://platform.claude.com/docs/en/about-claude/models/overview",
				"https://platform.claude.com/docs/en/about-claude/pricing",
			},
		},
		{
			ID: "doubao.seed-evolving.api", Version: "1.0.0", Label: "Doubao Seed Evolving 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.doubao-documented.v1", CacheUsageProfile: "cache-usage.doubao-documented.v1",
			ProtocolProfile:       "protocol.doubao-openai-chat.documented.v1",
			ProviderContextWindow: 1_048_576, ProviderMaxOutput: 262_144,
			DefaultContextWindow: 1_048_576, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "prompt_cache", "builtin_web_search", "mutable_alias"},
			Aliases:              []string{"Doubao Seed Evolving", "doubao-seed-evolving"},
			EvidenceRefs: []string{
				"https://ark.volcengine.com/region:cn-beijing/model/comparison",
				"https://www.volcengine.com/product/doubao",
			},
		},
		{
			ID: "doubao.seed-2.1-pro.api", Version: "1.0.0", Label: "Doubao Seed 2.1 Pro 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.doubao-documented.v1", CacheUsageProfile: "cache-usage.doubao-documented.v1",
			ProtocolProfile:       "protocol.doubao-openai-chat.documented.v1",
			ProviderContextWindow: 262_144, ProviderMaxOutput: 262_144,
			DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"Doubao Seed 2.1 Pro", "doubao-seed-2-1-pro-260628"},
			EvidenceRefs: []string{
				"https://ark.volcengine.com/region:cn-beijing/model/comparison",
				"https://www.volcengine.com/product/doubao",
			},
		},
		{
			ID: "doubao.seed-2.1-turbo.api", Version: "1.0.0", Label: "Doubao Seed 2.1 Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.doubao-documented.v1", CacheUsageProfile: "cache-usage.doubao-documented.v1",
			ProtocolProfile:       "protocol.doubao-openai-chat.documented.v1",
			ProviderContextWindow: 262_144, ProviderMaxOutput: 262_144,
			DefaultContextWindow: 262_144, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_schema", "vision", "video", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"Doubao Seed 2.1 Turbo", "doubao-seed-2-1-turbo-260628"},
			EvidenceRefs: []string{
				"https://ark.volcengine.com/region:cn-beijing/model/comparison",
				"https://www.volcengine.com/product/doubao",
			},
		},
	}
}

func All() []Profile {
	result := make([]Profile, len(catalog))
	for index := range catalog {
		result[index] = clone(catalog[index])
	}
	return result
}

func Lookup(id, version string) (Profile, bool) {
	id = strings.TrimSpace(id)
	version = strings.TrimSpace(version)
	for _, profile := range catalog {
		if profile.ID == id && profile.Version == version {
			if ValidateExecutableComponents(profile) != nil {
				return Profile{}, false
			}
			return clone(profile), true
		}
	}
	return Profile{}, false
}

func Match(modelName string) (Profile, bool) {
	identity := normalizeIdentity(modelName)
	if identity == "" {
		return Profile{}, false
	}
	for _, profile := range catalog[1:] {
		for _, alias := range profile.Aliases {
			if normalizeIdentity(alias) == identity {
				if ValidateExecutableComponents(profile) != nil {
					return Profile{}, false
				}
				return clone(profile), true
			}
		}
	}
	return Profile{}, false
}

// ValidateExecutableComponents prevents provider-advertised metadata from
// becoming executable routing authority without a matching typed component
// contract. It is used by both catalog selection and tests so a bad catalog
// entry fails closed even outside CI.
func ValidateExecutableComponents(profile Profile) error {
	if slices.Contains(profile.Capabilities, "reasoning") {
		if contract, ok := ResolveReasoningContract(profile.ReasoningProfile); !ok || contract.ProfileID != profile.ReasoningProfile {
			return fmt.Errorf("reasoning capability requires a modeled reasoning profile")
		}
	}
	if slices.Contains(profile.Capabilities, "prompt_cache") {
		if contract, ok := ResolveCacheUsageContract(profile.CacheUsageProfile); !ok || contract.ProfileID != profile.CacheUsageProfile {
			return fmt.Errorf("prompt_cache capability requires a modeled cache-usage profile")
		}
	}
	advancedChat := false
	for _, capability := range []string{
		CapabilityPartialMode, CapabilityDynamicTools, CapabilityBuiltinWebSearch,
		CapabilityVision, CapabilityVideo,
	} {
		if slices.Contains(profile.Capabilities, capability) {
			advancedChat = true
			break
		}
	}
	if advancedChat {
		contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
		if !ok || contract.ProfileID != profile.ProtocolProfile {
			return fmt.Errorf("advanced chat capabilities require a modeled protocol profile")
		}
		checks := map[string]bool{
			CapabilityPartialMode:      contract.PartialMode,
			CapabilityDynamicTools:     contract.DynamicSystemTools,
			CapabilityBuiltinWebSearch: contract.BuiltinWebSearch,
			CapabilityVision:           contract.InlineImageDataURL,
			CapabilityVideo:            contract.InlineVideoDataURL,
		}
		for capability, supported := range checks {
			if slices.Contains(profile.Capabilities, capability) && !supported {
				return fmt.Errorf("capability %s is not modeled by protocol profile", capability)
			}
		}
	}
	return nil
}

// ValidateEffectiveLimits ensures customer policy narrows provider capability.
// The unverified profile has no provider maxima but still requires positive
// effective limits before it can be used by canonical execution.
func ValidateEffectiveLimits(profile Profile, contextWindow, outputLimit int) error {
	if contextWindow <= 0 || outputLimit <= 0 {
		return fmt.Errorf("model execution limits must be positive")
	}
	if profile.ProviderContextWindow > 0 && contextWindow > profile.ProviderContextWindow {
		return fmt.Errorf("context window %d exceeds profile maximum %d", contextWindow, profile.ProviderContextWindow)
	}
	if profile.ProviderMaxOutput > 0 && outputLimit > profile.ProviderMaxOutput {
		return fmt.Errorf("output limit %d exceeds profile maximum %d", outputLimit, profile.ProviderMaxOutput)
	}
	if outputLimit > contextWindow {
		return fmt.Errorf("output limit %d exceeds effective context window %d", outputLimit, contextWindow)
	}
	return nil
}

func clone(profile Profile) Profile {
	// Keep the public catalog contract stable: empty collections are encoded as
	// [] rather than null. Besides being easier for typed clients to consume,
	// this prevents the deliberately sparse unverified profile from changing
	// the runtime shape of the response.
	profile.ProviderCapabilities = append([]string{}, profile.ProviderCapabilities...)
	profile.Capabilities = append([]string{}, profile.Capabilities...)
	profile.Aliases = append([]string{}, profile.Aliases...)
	profile.EvidenceRefs = append([]string{}, profile.EvidenceRefs...)
	return profile
}

func normalizeIdentity(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}
