// Package modelprofile owns the immutable, repository-reviewed model execution
// profile catalog. A profile describes provider capability; customer model
// registry limits remain explicit policy and may only narrow those maxima.
package modelprofile

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
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
	// Current is new-attachment metadata, not part of the immutable execution
	// contract. It is derived from currentProfileManifests; an addressable
	// historical profile may deliberately have no current manifest.
	Current bool `json:"current"`

	TokenizerProfileID      string `json:"tokenizer_profile_id"`
	TokenizerProfileVersion string `json:"tokenizer_profile_version"`
	ReasoningProfile        string `json:"reasoning_profile"`
	CacheUsageProfile       string `json:"cache_usage_profile"`
	ProtocolProfile         string `json:"protocol_profile"`
	// ProtocolProfileVersion is set when ProtocolProfile names an exact
	// endpointprotocol runtime contract rather than only a semantic family.
	// The pair is immutable execution identity and must match the compiled
	// operation binding at onboarding and routing boundaries.
	ProtocolProfileVersion string `json:"protocol_profile_version,omitempty"`
	ProcessingTierPolicy   string `json:"processing_tier_policy,omitempty"`
	// OperationPolicyProfile composes exact operation grammar which is not a
	// capability toggle (for example, Sonnet 5 Messages prefill and refusal
	// semantics). It is immutable per Profile address and never inferred from a
	// mutable model alias.
	OperationPolicyProfile string   `json:"operation_policy_profile,omitempty"`
	Provider               string   `json:"provider"`
	Region                 string   `json:"region"`
	APISurface             string   `json:"api_surface"`
	Operations             []string `json:"operations"`
	EndpointClasses        []string `json:"endpoint_classes"`
	UpstreamModelIDs       []string `json:"upstream_model_ids"`
	// UpstreamModelFamily and UpstreamVersionPolicy are append-only per-address
	// product identity. Empty means that historical Profile version predates
	// typed version-policy modeling; it must not be backfilled from a current
	// ID-level alias. UpstreamModelIDs remains the exact requested wire alias.
	UpstreamModelFamily    string `json:"upstream_model_family,omitempty"`
	UpstreamVersionPolicy  string `json:"upstream_version_policy,omitempty"`
	DefaultOutputAuthority string `json:"default_output_authority"`
	CatalogEvidenceState   string `json:"catalog_evidence_state"`
	// LegacyProtocolAuthority preserves the exact partial or generic Chat
	// validation semantics used by a profile address before full typed Chat
	// contracts existed. It never relaxes reasoning, cache, product identity,
	// operation, or endpoint validation, and new profiles must not set it.
	LegacyProtocolAuthority bool `json:"legacy_protocol_authority"`

	ProviderContextWindow int `json:"provider_context_window"`
	ProviderMaxOutput     int `json:"provider_max_output"`
	// ExecutableContextWindow is the narrower context ceiling UniRouter can
	// currently execute and bill without flattening a provider pricing band.
	// Zero means the provider context window is also the executable ceiling.
	ExecutableContextWindow int `json:"executable_context_window"`
	// ExecutableMaxOutput is a UniRouter policy ceiling used when the provider
	// either documents no output limit or when billing/admission must narrow a
	// larger provider maximum. Zero means ProviderMaxOutput is authoritative;
	// if both are zero, only the positive customer limit and context invariant
	// bound output. This must never be presented as a provider-documented max.
	ExecutableMaxOutput  int `json:"executable_max_output"`
	DefaultContextWindow int `json:"default_context_window"`
	DefaultOutputLimit   int `json:"default_output_limit"`

	// ProviderCapabilities records what the provider documents. Capabilities is
	// the narrower set that UniRouter has verified end-to-end and may safely
	// apply to a model registry record. Keeping the two sets distinct prevents a
	// provider feature such as URL-based vision from bypassing gateway admission
	// and guardrail support merely because the upstream API advertises it.
	ProviderCapabilities []string `json:"provider_capabilities"`
	// Capabilities is the immutable supported set for this exact Profile. It is
	// not a per-invocation routing requirement.
	Capabilities []string `json:"capabilities"`
	// AlwaysRequiredCapabilities is the narrow subset that every invocation of
	// this exact Profile must prove at route selection. Optional and
	// provider-default-derived semantics belong to the invocation Plan instead.
	AlwaysRequiredCapabilities []string `json:"always_required_capabilities"`
	Aliases                    []string `json:"aliases"`
	EvidenceRefs               []string `json:"evidence_refs"`
}

var legacyProviderProfiles = []Profile{
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
}

func buildCatalog() []Profile {
	result := append([]Profile{}, legacyProviderProfiles...)
	result = append(result, currentProviderProfiles()...)
	result = append(result, identityHardenedProviderProfiles()...)
	result = append(result, locallyConformantProviderProfiles()...)
	result = append(result, legacyConformantProviderProfiles()...)
	for index := range result {
		identity, identityOK := stableProfileProductIdentities[result[index].ID]
		if !identityOK {
			continue
		}
		if result[index].Provider == "" {
			result[index].Provider = identity.Provider
		}
		if result[index].Region == "" {
			result[index].Region = identity.Region
		}
		if len(result[index].EndpointClasses) == 0 {
			result[index].EndpointClasses = immutableProfileEndpointClasses(result[index].ID, result[index].Version)
		}
		if len(result[index].UpstreamModelIDs) == 0 {
			result[index].UpstreamModelIDs = immutableProfileUpstreamModelIDs(result[index].ID, result[index].Version)
		}
		if upstreamIdentity, ok := immutableProfileUpstreamIdentity(result[index].ID, result[index].Version); ok {
			result[index].UpstreamModelFamily = upstreamIdentity.ModelFamily
			result[index].UpstreamVersionPolicy = upstreamIdentity.VersionPolicy
		}
		executionIdentity, executionIdentityOK := immutableProfileExecutionIdentity(result[index].ID, result[index].Version)
		if !executionIdentityOK {
			continue
		}
		if result[index].APISurface == "" {
			result[index].APISurface = executionIdentity.APISurface
		}
		if len(result[index].Operations) == 0 {
			result[index].Operations = slices.Clone(executionIdentity.Operations)
		}
		if result[index].DefaultOutputAuthority == "" {
			result[index].DefaultOutputAuthority = executionIdentity.DefaultOutputAuthority
		}
		result[index].LegacyProtocolAuthority = isImmutableLegacyProtocolAuthority(result[index].ID, result[index].Version)
		if result[index].CatalogEvidenceState == "" {
			result[index].CatalogEvidenceState = derivedCatalogEvidenceState(result[index])
		}
	}
	return result
}

func derivedCatalogEvidenceState(profile Profile) string {
	if profile.ID == UnverifiedProfileID {
		return "unverified"
	}
	locallyConformant := len(profile.Capabilities) > 0 || profile.ProtocolProfileVersion != ""
	if !locallyConformant && immutableProfileCatalogEvidenceStates[profile.ID+"@"+profile.Version] == "locally_conformant" &&
		profile.APISurface == APISurfaceOpenAIChat && !profile.LegacyProtocolAuthority {
		contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
		locallyConformant = ok && contract.ProfileID == profile.ProtocolProfile
	}
	if locallyConformant {
		return "locally_conformant"
	}
	return "documented"
}

// immutableProfileCatalogEvidenceStates is an exact-address opt-in for typed
// operation conformance that does not invent a synthetic model capability.
// It is deliberately not inferred for every historical Chat contract.
var immutableProfileCatalogEvidenceStates = map[string]string{
	"doubao.seed-evolving.api@1.2.0": "locally_conformant",
}

const (
	APISurfaceOpenAIChat                        = "openai_chat_completions"
	APISurfaceOpenAIResponses                   = "openai_responses"
	APISurfaceAnthropicMessage                  = "anthropic_messages"
	OperationOpenAIChat                         = "openai.chat_completions.v1"
	OperationOpenAIResponses                    = "openai.responses.v1"
	OperationAnthropicMessage                   = "anthropic.messages.v1"
	DefaultOutputProvider                       = "provider_documented"
	DefaultOutputPolicy                         = "unirouter_policy"
	DefaultOutputCustomer                       = "customer_override"
	UpstreamVersionPinned                       = "pinned"
	UpstreamVersionFloating                     = "floating"
	EndpointClassProviderHTTPS                  = "provider_https"
	EndpointClassOpenAIOfficialOrigin           = "openai_official_origin"
	EndpointClassOpenAIOfficialV1               = "openai_official_v1"
	EndpointClassGeminiOpenAICompatibility      = "gemini_openai_compatibility_v1beta"
	EndpointClassDeepSeekOfficialOpenAI         = "deepseek_official_openai"
	EndpointClassKimiPlatformOpenAIV1           = "kimi_platform_openai_v1"
	EndpointClassMiniMaxCNOpenAIV1              = "minimax_cn_openai_v1"
	EndpointClassGLMOfficialOpenAIV4            = "glm_official_openai_v4"
	EndpointClassXAIOfficialOpenAIV1            = "xai_official_openai_v1"
	EndpointClassAnthropicOfficialOrigin        = "anthropic_official_origin"
	EndpointClassDoubaoArkBeijingOpenAIV3       = "doubao_ark_cn_beijing_openai_v3"
	EndpointClassDashScopeBeijingWorkspacePayGo = "dashscope_cn_beijing_workspace_payg"
	EndpointClassDashScopeBeijingSharedPayGo    = "dashscope_cn_beijing_shared_payg"
	EndpointPathModeCanonicalV1                 = "canonical_v1"
	EndpointPathModeBaseRelative                = "base_relative"
)

type stableProfileProductIdentity struct {
	Provider               string
	Region                 string
	UpstreamModelIDs       []string
	APISurface             string
	Operations             []string
	DefaultOutputAuthority string
}

func chatProviderProduct(provider, region, defaultOutputAuthority string, upstreamModelIDs ...string) stableProfileProductIdentity {
	return stableProfileProductIdentity{
		Provider: provider, Region: region,
		UpstreamModelIDs: slices.Clone(upstreamModelIDs),
		APISurface:       APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: defaultOutputAuthority,
	}
}

func responsesProviderProduct(provider, region, defaultOutputAuthority string, upstreamModelIDs ...string) stableProfileProductIdentity {
	return stableProfileProductIdentity{
		Provider: provider, Region: region,
		UpstreamModelIDs: slices.Clone(upstreamModelIDs),
		APISurface:       APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
		DefaultOutputAuthority: defaultOutputAuthority,
	}
}

func anthropicProviderProduct(region string, upstreamModelIDs ...string) stableProfileProductIdentity {
	return stableProfileProductIdentity{
		Provider: "anthropic", Region: region,
		UpstreamModelIDs: slices.Clone(upstreamModelIDs),
		APISurface:       APISurfaceAnthropicMessage, Operations: []string{OperationAnthropicMessage},
		DefaultOutputAuthority: DefaultOutputPolicy,
	}
}

type profileExecutionIdentity struct {
	APISurface             string
	Operations             []string
	DefaultOutputAuthority string
}

var immutableProfileExecutionIdentityOverrides = map[string]profileExecutionIdentity{
	"doubao.seed-2.1-pro.api@1.1.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-pro.api@1.2.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-pro.api@1.3.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-pro.api@1.4.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-pro.api@1.5.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-pro.api@1.6.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-turbo.api@1.1.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-turbo.api@1.2.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-turbo.api@1.3.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-turbo.api@1.4.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-turbo.api@1.5.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-2.1-turbo.api@1.6.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.1.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.2.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.3.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.4.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.5.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.6.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.7.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"doubao.seed-evolving.api@1.8.0": {
		APISurface: APISurfaceOpenAIChat, Operations: []string{OperationOpenAIChat},
		DefaultOutputAuthority: DefaultOutputProvider,
	},
	"openai.gpt-5.6-sol.api@1.0.0": {
		APISurface: APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
		DefaultOutputAuthority: DefaultOutputPolicy,
	},
	"openai.gpt-5.6-terra.api@1.0.0": {
		APISurface: APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
		DefaultOutputAuthority: DefaultOutputPolicy,
	},
	"openai.gpt-5.6-luna.api@1.0.0": {
		APISurface: APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
		DefaultOutputAuthority: DefaultOutputPolicy,
	},
}

func immutableProfileExecutionIdentity(profileID, version string) (profileExecutionIdentity, bool) {
	if identity, ok := immutableProfileExecutionIdentityOverrides[profileID+"@"+version]; ok {
		identity.Operations = slices.Clone(identity.Operations)
		return identity, true
	}
	product, ok := stableProfileProductIdentities[profileID]
	if !ok {
		return profileExecutionIdentity{}, false
	}
	return profileExecutionIdentity{
		APISurface: product.APISurface, Operations: slices.Clone(product.Operations),
		DefaultOutputAuthority: product.DefaultOutputAuthority,
	}, true
}

// stableProfileProductIdentities freezes provider, region, upstream model,
// API surface, operation, and request-default provenance by Profile ID.
// Endpoint shape and exceptional historical versions are frozen separately
// below. The current/new-attachment pointer is deliberately not consulted, so
// advancing or retiring it cannot reinterpret an addressable Profile.
// Credential billing plans are connection identity, not model identity.
var stableProfileProductIdentities = map[string]stableProfileProductIdentity{
	UnverifiedProfileID:                 chatProviderProduct("custom", "custom", DefaultOutputCustomer),
	"deepseek.v4-flash.api":             chatProviderProduct("deepseek", "cn", DefaultOutputPolicy, "deepseek-v4-flash"),
	"deepseek.v4-pro.api":               chatProviderProduct("deepseek", "cn", DefaultOutputPolicy, "deepseek-v4-pro"),
	"kimi.k2.6.api":                     chatProviderProduct("kimi", "cn", DefaultOutputPolicy, "kimi-k2.6"),
	"kimi.k2.7-code.api":                chatProviderProduct("kimi", "cn", DefaultOutputPolicy, "kimi-k2.7-code"),
	"kimi.k3.api":                       chatProviderProduct("kimi", "cn", DefaultOutputPolicy, "kimi-k3"),
	"minimax.m2.7.token-plan":           chatProviderProduct("minimax", "cn", DefaultOutputPolicy, "MiniMax-M2.7"),
	"minimax.m2.5-highspeed.token-plan": chatProviderProduct("minimax", "cn", DefaultOutputPolicy, "MiniMax-M2.5-highspeed"),
	"minimax.m3.token-plan":             chatProviderProduct("minimax", "cn", DefaultOutputPolicy, "MiniMax-M3"),
	"glm.5.3.api":                       chatProviderProduct("glm", "cn", DefaultOutputPolicy, "glm-5.3"),
	"glm.5.2.api":                       chatProviderProduct("glm", "cn", DefaultOutputPolicy, "glm-5.2"),
	"glm.5v-turbo.api":                  chatProviderProduct("glm", "cn", DefaultOutputPolicy, "glm-5v-turbo"),
	"qwen.3.8-max.api":                  chatProviderProduct("dashscope", "cn-beijing", DefaultOutputProvider, "qwen3.8-max"),
	"qwen.3.7-plus.api":                 chatProviderProduct("dashscope", "cn-beijing", DefaultOutputProvider, "qwen3.7-plus"),
	"qwen.3.7-flash.api":                chatProviderProduct("dashscope", "cn-beijing", DefaultOutputProvider, "qwen3.7-flash"),
	"gemini.3.7-flash.api":              chatProviderProduct("gemini", "global", DefaultOutputPolicy, "gemini-3.7-flash"),
	"gemini.3.5-flash.api":              chatProviderProduct("gemini", "global", DefaultOutputPolicy, "gemini-3.5-flash"),
	"gemini.3.5-flash-lite.api":         chatProviderProduct("gemini", "global", DefaultOutputPolicy, "gemini-3.5-flash-lite"),
	"mimo.v2.5-pro.api":                 chatProviderProduct("mimo", "cn", DefaultOutputPolicy, "mimo-v2.5-pro"),
	"mimo.v2.5.api":                     chatProviderProduct("mimo", "cn", DefaultOutputPolicy, "mimo-v2.5"),
	"hy3.api":                           chatProviderProduct("hy3", "cn", DefaultOutputPolicy, "hy3"),
	"openai.gpt-5.6-sol.chat":           chatProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-sol"),
	"openai.gpt-5.6-sol.responses":      responsesProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-sol"),
	"openai.gpt-5.6-sol.api":            chatProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-sol"),
	"openai.gpt-5.6-terra.chat":         chatProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-terra"),
	"openai.gpt-5.6-terra.responses":    responsesProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-terra"),
	"openai.gpt-5.6-terra.api":          chatProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-terra"),
	"openai.gpt-5.6-luna.chat":          chatProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-luna"),
	"openai.gpt-5.6-luna.responses":     responsesProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-luna"),
	"openai.gpt-5.6-luna.api":           chatProviderProduct("openai", "global", DefaultOutputPolicy, "gpt-5.6-luna"),
	"xai.grok-4.6.api":                  chatProviderProduct("xai", "global", DefaultOutputPolicy, "grok-4.6"),
	"anthropic.claude-opus-5.api":       anthropicProviderProduct("global", "claude-opus-5"),
	"anthropic.claude-sonnet-5.api":     anthropicProviderProduct("global", "claude-sonnet-5"),
	"anthropic.claude-haiku-4.5.api":    anthropicProviderProduct("global", "claude-haiku-4-5-20251001"),
	"doubao.seed-evolving.api":          chatProviderProduct("doubao", "cn-beijing", DefaultOutputPolicy, "doubao-seed-evolving"),
	"doubao.seed-2.1-pro.api":           chatProviderProduct("doubao", "cn-beijing", DefaultOutputPolicy, "doubao-seed-2-1-pro-260628"),
	"doubao.seed-2.1-turbo.api":         chatProviderProduct("doubao", "cn-beijing", DefaultOutputPolicy, "doubao-seed-2-1-turbo-260628"),
}

// immutableProfileUpstreamModelIDOverrides is append-only identity for profile
// versions whose upstream IDs differ from the stable current product. Display
// aliases are intentionally not consulted here.
var immutableProfileUpstreamModelIDOverrides = map[string][]string{
	"minimax.m2.7.token-plan@1.0.0": {"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},
	"minimax.m2.7.token-plan@1.1.0": {"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},

	"kimi.k2.6.api@1.0.0": {"kimi-k2.6", "kimi-k2.6-preview"},
	"kimi.k2.6.api@1.1.0": {"kimi-k2.6", "kimi-k2.6-preview"},
	"kimi.k2.6.api@1.2.0": {"kimi-k2.6", "kimi-k2.6-preview"},
	"kimi.k2.6.api@1.3.0": {"kimi-k2.6", "kimi-k2.6-preview"},
	"kimi.k2.6.api@1.4.0": {"kimi-k2.6", "kimi-k2.6-preview"},
	"kimi.k2.6.api@1.5.0": {"kimi-k2.6", "kimi-k2.6-preview"},

	"kimi.k2.7-code.api@1.0.0": {"kimi-for-coding", "kimi-for-coding-highspeed"},
	"kimi.k2.7-code.api@1.1.0": {"kimi-k2.7-code", "kimi-k2.7-code-highspeed"},

	"kimi.k3.api@1.0.0": {"k3", "k3-256k"},
	"kimi.k3.api@1.1.0": {"kimi-k3", "k3", "k3-256k"},
	"kimi.k3.api@1.2.0": {"kimi-k3", "k3", "k3-256k"},
	"kimi.k3.api@1.3.0": {"kimi-k3", "k3", "k3-256k"},
	"kimi.k3.api@1.4.0": {"kimi-k3", "k3", "k3-256k"},
	"kimi.k3.api@1.5.0": {"kimi-k3", "k3", "k3-256k"},
}

// immutableLegacyProtocolAuthorities freezes only the historical Chat
// protocol boundary. These exact addresses predate the full typed contracts:
// some used provider-specific partial validation and others intentionally fell
// through to the generic measurable Chat admission grammar. Registering new
// semantics under their old protocol IDs would reinterpret sealed evidence.
var immutableLegacyProtocolAuthorities = map[string]struct{}{
	"deepseek.v4-flash.api@1.0.0": {},
	"deepseek.v4-flash.api@1.1.0": {},
	"deepseek.v4-pro.api@1.0.0":   {},
	"deepseek.v4-pro.api@1.1.0":   {},

	"kimi.k2.6.api@1.0.0":      {},
	"kimi.k2.6.api@1.1.0":      {},
	"kimi.k2.6.api@1.2.0":      {},
	"kimi.k2.6.api@1.3.0":      {},
	"kimi.k2.6.api@1.4.0":      {},
	"kimi.k2.6.api@1.5.0":      {},
	"kimi.k2.7-code.api@1.0.0": {},
	"kimi.k2.7-code.api@1.1.0": {},
	"kimi.k3.api@1.0.0":        {},
	"kimi.k3.api@1.1.0":        {},
	"kimi.k3.api@1.2.0":        {},
	"kimi.k3.api@1.3.0":        {},
	"kimi.k3.api@1.4.0":        {},
	"kimi.k3.api@1.5.0":        {},

	"minimax.m2.7.token-plan@1.0.0":           {},
	"minimax.m2.5-highspeed.token-plan@1.0.0": {},
	"minimax.m3.token-plan@1.0.0":             {},
}

func isImmutableLegacyProtocolAuthority(profileID, version string) bool {
	_, ok := immutableLegacyProtocolAuthorities[profileID+"@"+version]
	return ok
}

func immutableProfileUpstreamModelIDs(profileID, version string) []string {
	if modelIDs, ok := immutableProfileUpstreamModelIDOverrides[profileID+"@"+version]; ok {
		return slices.Clone(modelIDs)
	}
	identity, ok := stableProfileProductIdentities[profileID]
	if !ok {
		return nil
	}
	return slices.Clone(identity.UpstreamModelIDs)
}

type profileUpstreamIdentity struct {
	ModelFamily   string
	VersionPolicy string
}

// immutableProfileUpstreamIdentities is append-only and exact-address scoped.
// Historical Profiles without an entry remain explicitly unmodeled; current
// ID-level product metadata must never reinterpret them.
var immutableProfileUpstreamIdentities = map[string]profileUpstreamIdentity{
	"doubao.seed-evolving.api@1.1.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.2.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.3.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.4.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.5.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.6.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.7.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
	"doubao.seed-evolving.api@1.8.0": {
		ModelFamily: "doubao-seed-evolving", VersionPolicy: UpstreamVersionFloating,
	},
}

func immutableProfileUpstreamIdentity(profileID, version string) (profileUpstreamIdentity, bool) {
	identity, ok := immutableProfileUpstreamIdentities[profileID+"@"+version]
	return identity, ok
}

// immutableProfileEndpointClassOverrides is append-only product identity.
// Exact endpoint semantics are introduced by an explicit profile version;
// advancing a current pointer must not tighten or loosen an older version.
var immutableProfileEndpointClassOverrides = map[string][]string{
	"deepseek.v4-flash.api@1.2.0":             {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-flash.api@1.3.0":             {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-flash.api@1.4.0":             {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-flash.api@1.5.0":             {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-pro.api@1.2.0":               {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-pro.api@1.3.0":               {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-pro.api@1.4.0":               {EndpointClassDeepSeekOfficialOpenAI},
	"deepseek.v4-pro.api@1.5.0":               {EndpointClassDeepSeekOfficialOpenAI},
	"kimi.k2.6.api@1.6.0":                     {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k2.6.api@1.7.0":                     {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k2.7-code.api@1.2.0":                {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k2.7-code.api@1.3.0":                {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k3.api@1.6.0":                       {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k3.api@1.7.0":                       {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k3.api@1.8.0":                       {EndpointClassKimiPlatformOpenAIV1},
	"kimi.k3.api@1.9.0":                       {EndpointClassKimiPlatformOpenAIV1},
	"minimax.m2.7.token-plan@1.2.0":           {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.7.token-plan@1.3.0":           {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.7.token-plan@1.4.0":           {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.7.token-plan@1.5.0":           {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.5-highspeed.token-plan@1.2.0": {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.5-highspeed.token-plan@1.3.0": {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.5-highspeed.token-plan@1.4.0": {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m2.5-highspeed.token-plan@1.5.0": {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m3.token-plan@1.2.0":             {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m3.token-plan@1.3.0":             {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m3.token-plan@1.4.0":             {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m3.token-plan@1.5.0":             {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m3.token-plan@1.6.0":             {EndpointClassMiniMaxCNOpenAIV1},
	"minimax.m3.token-plan@1.7.0":             {EndpointClassMiniMaxCNOpenAIV1},
	"glm.5.3.api@1.8.0":                       {EndpointClassGLMOfficialOpenAIV4},

	"qwen.3.8-max.api@1.7.0":   {EndpointClassDashScopeBeijingWorkspacePayGo, EndpointClassDashScopeBeijingSharedPayGo},
	"qwen.3.8-max.api@1.8.0":   {EndpointClassDashScopeBeijingWorkspacePayGo, EndpointClassDashScopeBeijingSharedPayGo},
	"qwen.3.7-plus.api@1.7.0":  {EndpointClassDashScopeBeijingWorkspacePayGo, EndpointClassDashScopeBeijingSharedPayGo},
	"qwen.3.7-flash.api@1.7.0": {EndpointClassDashScopeBeijingWorkspacePayGo, EndpointClassDashScopeBeijingSharedPayGo},

	"gemini.3.7-flash.api@1.6.0":      {EndpointClassGeminiOpenAICompatibility},
	"gemini.3.7-flash.api@1.7.0":      {EndpointClassGeminiOpenAICompatibility},
	"gemini.3.5-flash.api@1.6.0":      {EndpointClassGeminiOpenAICompatibility},
	"gemini.3.5-flash.api@1.7.0":      {EndpointClassGeminiOpenAICompatibility},
	"gemini.3.5-flash-lite.api@1.6.0": {EndpointClassGeminiOpenAICompatibility},
	"gemini.3.5-flash-lite.api@1.7.0": {EndpointClassGeminiOpenAICompatibility},

	"openai.gpt-5.6-sol.api@1.0.0":         {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-sol.responses@1.0.0":   {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-sol.responses@1.1.0":   {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-sol.responses@1.2.0":   {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-sol.responses@1.3.0":   {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-terra.api@1.0.0":       {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-terra.responses@1.0.0": {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-terra.responses@1.1.0": {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-terra.responses@1.2.0": {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-terra.responses@1.3.0": {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-luna.api@1.0.0":        {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-luna.responses@1.0.0":  {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-luna.responses@1.1.0":  {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-luna.responses@1.2.0":  {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-luna.responses@1.3.0":  {EndpointClassOpenAIOfficialOrigin},
	"openai.gpt-5.6-sol.api@1.6.0":         {EndpointClassOpenAIOfficialV1},
	"openai.gpt-5.6-sol.chat@1.0.0":        {EndpointClassOpenAIOfficialV1},
	"openai.gpt-5.6-terra.api@1.6.0":       {EndpointClassOpenAIOfficialV1},
	"openai.gpt-5.6-terra.chat@1.0.0":      {EndpointClassOpenAIOfficialV1},
	"openai.gpt-5.6-luna.api@1.6.0":        {EndpointClassOpenAIOfficialV1},
	"openai.gpt-5.6-luna.chat@1.0.0":       {EndpointClassOpenAIOfficialV1},
	"xai.grok-4.6.api@1.1.0":               {EndpointClassXAIOfficialOpenAIV1},
	"anthropic.claude-opus-5.api@1.1.0":    {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-opus-5.api@1.2.0":    {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-sonnet-5.api@1.1.0":  {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-sonnet-5.api@1.2.0":  {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-sonnet-5.api@1.3.0":  {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-sonnet-5.api@1.4.0":  {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-haiku-4.5.api@1.1.0": {EndpointClassAnthropicOfficialOrigin},
	"anthropic.claude-haiku-4.5.api@1.2.0": {EndpointClassAnthropicOfficialOrigin},
	"doubao.seed-evolving.api@1.1.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.2.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.3.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.4.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.5.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.6.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.7.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-evolving.api@1.8.0":       {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-pro.api@1.1.0":        {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-pro.api@1.2.0":        {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-pro.api@1.3.0":        {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-pro.api@1.4.0":        {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-pro.api@1.5.0":        {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-pro.api@1.6.0":        {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-turbo.api@1.1.0":      {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-turbo.api@1.2.0":      {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-turbo.api@1.3.0":      {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-turbo.api@1.4.0":      {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-turbo.api@1.5.0":      {EndpointClassDoubaoArkBeijingOpenAIV3},
	"doubao.seed-2.1-turbo.api@1.6.0":      {EndpointClassDoubaoArkBeijingOpenAIV3},
}

type profileEndpointProtocolIdentity struct {
	ProfileID      string
	ProfileVersion string
}

// immutableProfileEndpointProtocolIdentities is append-only per-address
// identity. It must never be derived from the current compiled operation
// binding: advancing that binding cannot reinterpret a historical Profile or
// a sealed Release that pins it.
var immutableProfileEndpointProtocolIdentities = map[string]profileEndpointProtocolIdentity{
	"openai.gpt-5.6-sol.responses@1.1.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-sol.responses@1.2.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-sol.responses@1.3.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesTextV2, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-terra.responses@1.1.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-terra.responses@1.2.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-terra.responses@1.3.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesTextV2, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-luna.responses@1.1.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-luna.responses@1.2.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProfileVersion: "1.0.0",
	},
	"openai.gpt-5.6-luna.responses@1.3.0": {
		ProfileID: endpointprotocol.ProfileOpenAIResponsesTextV2, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-opus-5.api@1.1.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesText, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-opus-5.api@1.2.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesTextV2, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-sonnet-5.api@1.1.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesText, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-sonnet-5.api@1.2.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesText, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-sonnet-5.api@1.3.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesTextV2, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-sonnet-5.api@1.4.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesTextV3, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-haiku-4.5.api@1.1.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesText, ProfileVersion: "1.0.0",
	},
	"anthropic.claude-haiku-4.5.api@1.2.0": {
		ProfileID: endpointprotocol.ProfileAnthropicMessagesTextV2, ProfileVersion: "1.0.0",
	},
}

// immutableProfileOperationPolicyIdentities is append-only per-address
// operation grammar. Historical Profiles remain addressable with their exact
// zero-value policy rather than inheriting current provider behavior.
var immutableProfileOperationPolicyIdentities = map[string]string{
	"anthropic.claude-opus-5.api@1.1.0":    AnthropicMessagesOpus5EvidencePolicyV1,
	"anthropic.claude-opus-5.api@1.2.0":    AnthropicMessagesOpus5EvidencePolicyV1,
	"anthropic.claude-sonnet-5.api@1.1.0":  AnthropicMessagesSonnet5CorePolicyV1,
	"anthropic.claude-sonnet-5.api@1.2.0":  AnthropicMessagesSonnet5EvidencePolicyV2,
	"anthropic.claude-sonnet-5.api@1.3.0":  AnthropicMessagesSonnet5EvidencePolicyV2,
	"anthropic.claude-sonnet-5.api@1.4.0":  AnthropicMessagesSonnet5ToolsPolicyV3,
	"anthropic.claude-haiku-4.5.api@1.1.0": AnthropicMessagesHaiku45EvidencePolicyV1,
	"anthropic.claude-haiku-4.5.api@1.2.0": AnthropicMessagesHaiku45EvidencePolicyV1,
}

func immutableProfileEndpointProtocolIdentity(profileID, version string) (profileEndpointProtocolIdentity, bool) {
	identity, ok := immutableProfileEndpointProtocolIdentities[profileID+"@"+version]
	return identity, ok
}

func immutableProfileEndpointClasses(profileID, version string) []string {
	if classes, ok := immutableProfileEndpointClassOverrides[profileID+"@"+version]; ok {
		return slices.Clone(classes)
	}
	if _, ok := stableProfileProductIdentities[profileID]; !ok {
		return nil
	}
	return []string{EndpointClassProviderHTTPS}
}

type currentProfileManifest struct {
	Version string
}

func currentProfile(version string) currentProfileManifest {
	return currentProfileManifest{Version: version}
}

// currentProfileManifests is the sole new-attachment/current-for-surface index
// for automatic onboarding. It contains only an exact version pointer:
// addressability and every execution/product identity field come from the
// immutable catalog manifests above. Retiring an ID here therefore cannot
// invalidate or reinterpret its historical ID@version rows.
var currentProfileManifests = map[string]currentProfileManifest{
	UnverifiedProfileID:                 currentProfile(UnverifiedProfileVersion),
	"deepseek.v4-flash.api":             currentProfile("1.5.0"),
	"deepseek.v4-pro.api":               currentProfile("1.5.0"),
	"kimi.k2.6.api":                     currentProfile("1.7.0"),
	"kimi.k2.7-code.api":                currentProfile("1.3.0"),
	"kimi.k3.api":                       currentProfile("1.9.0"),
	"minimax.m2.7.token-plan":           currentProfile("1.5.0"),
	"minimax.m2.5-highspeed.token-plan": currentProfile("1.5.0"),
	"minimax.m3.token-plan":             currentProfile("1.7.0"),
	"glm.5.3.api":                       currentProfile("1.8.0"),
	"glm.5.2.api":                       currentProfile("1.7.0"),
	"glm.5v-turbo.api":                  currentProfile("1.10.0"),
	"qwen.3.8-max.api":                  currentProfile("1.8.0"),
	"qwen.3.7-plus.api":                 currentProfile("1.7.0"),
	"qwen.3.7-flash.api":                currentProfile("1.7.0"),
	"gemini.3.7-flash.api":              currentProfile("1.7.0"),
	"gemini.3.5-flash.api":              currentProfile("1.7.0"),
	"gemini.3.5-flash-lite.api":         currentProfile("1.7.0"),
	"mimo.v2.5-pro.api":                 currentProfile("1.5.0"),
	"mimo.v2.5.api":                     currentProfile("1.6.0"),
	"hy3.api":                           currentProfile("1.6.0"),
	"openai.gpt-5.6-sol.chat":           currentProfile("1.0.0"),
	"openai.gpt-5.6-sol.responses":      currentProfile("1.3.0"),
	"openai.gpt-5.6-terra.chat":         currentProfile("1.0.0"),
	"openai.gpt-5.6-terra.responses":    currentProfile("1.3.0"),
	"openai.gpt-5.6-luna.chat":          currentProfile("1.0.0"),
	"openai.gpt-5.6-luna.responses":     currentProfile("1.3.0"),
	"xai.grok-4.6.api":                  currentProfile("1.1.0"),
	"anthropic.claude-opus-5.api":       currentProfile("1.2.0"),
	"anthropic.claude-sonnet-5.api":     currentProfile("1.4.0"),
	"anthropic.claude-haiku-4.5.api":    currentProfile("1.2.0"),
	"doubao.seed-evolving.api":          currentProfile("1.8.0"),
	"doubao.seed-2.1-pro.api":           currentProfile("1.6.0"),
	"doubao.seed-2.1-turbo.api":         currentProfile("1.6.0"),
}

// The immutable addressable catalog is built only from stable/exact identity
// manifests and Profile chains. The separately declared current-version index
// is a runtime projection and is not an input to catalog construction.
var catalog = buildCatalog()

// identityHardenedProviderProfiles adds append-only product identity without
// claiming an executable operation or capability. Such a candidate remains
// non-current until its operation milestone closes separately.
func identityHardenedProviderProfiles() []Profile {
	for _, documented := range currentProviderProfiles() {
		if documented.ID != "doubao.seed-evolving.api" {
			continue
		}
		profile := clone(documented)
		profile.Version = "1.1.0"
		profile.Label = "Doubao Seed Evolving Ark Floating Alias Identity"
		profile.DefaultOutputLimit = 4_096
		profile.Capabilities = []string{}
		profile.EvidenceRefs = append(profile.EvidenceRefs,
			"https://ark.volcengine.com/region:cn-beijing/model/comparison?selection=%5B%7B%22columnId%22%3A%22column-1%22%2C%22modelName%22%3A%22doubao-seed-evolving%22%2C%22modelVersion%22%3A%22latest-version%22%7D%5D",
			"https://docs.volcengine.com/docs/82379/2549861?lang=zh",
		)
		return []Profile{profile}
	}
	return nil
}

// locallyConformantProviderProfiles promotes one provider-documented bundle at
// a time after its exact operation contract has local request-path coverage.
// The source profile remains immutable and addressable for historical review.
func locallyConformantProviderProfiles() []Profile {
	profiles := make([]Profile, 0, 27)
	qwenSpecs := map[string]struct {
		reasoningProfile  string
		jsonSchema        bool
		multimodalSources []string
	}{
		"qwen.3.8-max.api": {
			reasoningProfile: "reasoning.qwen3.8-max-hybrid.v1",
			jsonSchema:       true,
			multimodalSources: []string{
				"https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions",
			},
		},
		"qwen.3.7-plus.api": {
			reasoningProfile: "reasoning.qwen3.7-hybrid.v1",
			jsonSchema:       true,
			multimodalSources: []string{
				"https://help.aliyun.com/zh/model-studio/vision-model",
				"https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions",
			},
		},
		"qwen.3.7-flash.api": {
			reasoningProfile: "reasoning.qwen3.7-hybrid.v1",
			jsonSchema:       false,
			multimodalSources: []string{
				"https://help.aliyun.com/zh/model-studio/vision-model",
				"https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions",
			},
		},
	}
	geminiReasoningProfiles := map[string]struct {
		reasoningProfile string
		processingTier   bool
	}{
		"gemini.3.7-flash.api":      {reasoningProfile: "reasoning.gemini-3.7-flash.v1", processingTier: true},
		"gemini.3.5-flash.api":      {reasoningProfile: "reasoning.gemini-3.5-flash.v1", processingTier: true},
		"gemini.3.5-flash-lite.api": {reasoningProfile: "reasoning.gemini-3.5-flash-lite.v1", processingTier: true},
	}
	mimoMultimodal := map[string]bool{
		"mimo.v2.5-pro.api": false,
		"mimo.v2.5.api":     true,
	}
	openAIProfiles := map[string]struct {
		label                     string
		responsesCoreConformant   bool
		responsesReasoningProfile string
	}{
		"openai.gpt-5.6-sol.api":   {label: "GPT-5.6 Sol", responsesCoreConformant: true, responsesReasoningProfile: "reasoning.openai-gpt-5.6-sol-responses.v1"},
		"openai.gpt-5.6-terra.api": {label: "GPT-5.6 Terra", responsesCoreConformant: true, responsesReasoningProfile: "reasoning.openai-gpt-5.6-terra-responses.v1"},
		"openai.gpt-5.6-luna.api":  {label: "GPT-5.6 Luna", responsesCoreConformant: true, responsesReasoningProfile: "reasoning.openai-gpt-5.6-luna-responses.v1"},
	}
	for _, documented := range currentProviderProfiles() {
		if documented.Version != "1.0.0" {
			continue
		}
		if spec, supported := qwenSpecs[documented.ID]; supported {
			profiles = append(profiles, qwenConformantProfileChain(documented, spec.reasoningProfile, spec.jsonSchema, spec.multimodalSources)...)
			continue
		}
		if spec, supported := geminiReasoningProfiles[documented.ID]; supported {
			profiles = append(profiles, geminiConformantProfileChain(documented, spec.reasoningProfile, spec.processingTier)...)
			continue
		}
		if multimodal, supported := mimoMultimodal[documented.ID]; supported {
			profiles = append(profiles, mimoConformantProfileChain(documented, multimodal)...)
			continue
		}
		if surface, supported := openAIProfiles[documented.ID]; supported {
			chatProfiles := openAIConformantProfileChain(documented)
			profiles = append(profiles, chatProfiles...)
			if surface.label != "" {
				profiles = append(profiles, openAISurfaceProfiles(
					documented,
					chatProfiles[len(chatProfiles)-1],
					surface.label,
					surface.responsesCoreConformant,
					surface.responsesReasoningProfile,
				)...)
			}
			continue
		}
		if documented.ID == "anthropic.claude-sonnet-5.api" {
			profiles = append(profiles, anthropicSonnet5ConformantProfiles(documented)...)
			continue
		}
		if documented.ID == "anthropic.claude-opus-5.api" {
			profiles = append(profiles, anthropicOpus5ConformantProfiles(documented)...)
			continue
		}
		if documented.ID == "anthropic.claude-haiku-4.5.api" {
			profiles = append(profiles, anthropicHaiku45ConformantProfiles(documented)...)
			continue
		}
		if documented.ID == "xai.grok-4.6.api" {
			profiles = append(profiles, xAIConformantProfileChain(documented)...)
			continue
		}
		if documented.ID == "doubao.seed-2.1-pro.api" {
			core := doubaoSeed21ProConformantProfile(documented)
			streaming := doubaoSeed21ProStreamingProfile(core)
			tools := doubaoSeed21ProToolsProfile(streaming)
			structured := doubaoSeed21ProStrictJSONSchemaProfile(tools)
			cacheRead := doubaoSeed21ProCacheReadProfile(structured)
			profiles = append(profiles, core, streaming, tools, structured, cacheRead, doubaoSeed21ProInlineJPEGProfile(cacheRead))
			continue
		}
		if documented.ID == "doubao.seed-evolving.api" {
			core := doubaoSeedEvolvingChatCoreProfile(documented)
			reasoning := doubaoSeedEvolvingReasoningProfile(core)
			streaming := doubaoSeedEvolvingStreamingProfile(reasoning)
			tools := doubaoSeedEvolvingToolsProfile(streaming)
			structured := doubaoSeedEvolvingStrictJSONSchemaProfile(tools)
			image := doubaoSeedEvolvingInlineJPEGProfile(structured)
			profiles = append(profiles, core, reasoning, streaming, tools, structured, image, doubaoSeedEvolvingCacheReadProfile(image))
			continue
		}
		if documented.ID == "doubao.seed-2.1-turbo.api" {
			core := doubaoSeed21TurboConformantProfile(documented)
			streaming := doubaoSeed21TurboStreamingProfile(core)
			tools := doubaoSeed21TurboToolsProfile(streaming)
			structured := doubaoSeed21TurboStrictJSONSchemaProfile(tools)
			cacheRead := doubaoSeed21TurboCacheReadProfile(structured)
			profiles = append(profiles, core, streaming, tools, structured, cacheRead, doubaoSeed21TurboInlineJPEGProfile(cacheRead))
			continue
		}
		if documented.ID == "hy3.api" {
			profiles = append(profiles, hy3ConformantProfileChain(documented)...)
		}
	}
	return profiles
}

func doubaoSeedEvolvingChatCoreProfile(documented Profile) Profile {
	profile := clone(documented)
	profile.Version = "1.2.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Strict Non-Stream Core"
	profile.ProtocolProfile = "protocol.doubao-seed-evolving-openai-chat.v1"
	profile.ReasoningProfile = ""
	profile.CacheUsageProfile = ""
	profile.DefaultOutputLimit = 4_096
	profile.Capabilities = []string{}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
		"https://ark.volcengine.com/region:cn-beijing/model/comparison?selection=%5B%7B%22columnId%22%3A%22column-1%22%2C%22modelName%22%3A%22doubao-seed-evolving%22%2C%22modelVersion%22%3A%22latest-version%22%7D%5D",
	)
	return profile
}

func doubaoSeedEvolvingReasoningProfile(core Profile) Profile {
	profile := clone(core)
	profile.Version = "1.3.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Non-Stream Reasoning Core"
	profile.ProtocolProfile = "protocol.doubao-seed-evolving-openai-chat.v2"
	profile.ReasoningProfile = "reasoning.doubao-seed-evolving-toggle-effort.v1"
	profile.Capabilities = []string{CapabilityReasoning, CapabilityReasoningEffort}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1449737?lang=zh",
	)
	return profile
}

func doubaoSeedEvolvingStreamingProfile(reasoning Profile) Profile {
	profile := clone(reasoning)
	profile.Version = "1.4.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Streaming Reasoning Core"
	profile.ProtocolProfile = "protocol.doubao-seed-evolving-openai-chat.v3"
	profile.Capabilities = []string{CapabilityStreaming, CapabilityReasoning, CapabilityReasoningEffort}
	return profile
}

func doubaoSeedEvolvingToolsProfile(streaming Profile) Profile {
	profile := clone(streaming)
	profile.Version = "1.5.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Non-Stream Function Tools"
	profile.ProtocolProfile = "protocol.doubao-seed-evolving-openai-chat.v4"
	profile.ReasoningProfile = "reasoning.doubao-seed-evolving-toggle-effort.v2"
	profile.Capabilities = []string{
		CapabilityStreaming,
		CapabilityReasoning,
		CapabilityReasoningEffort,
		CapabilityTools,
		CapabilityFunctionTools,
		CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed,
		CapabilityToolChoiceReq,
		CapabilityParallelTools,
	}
	return profile
}

func doubaoSeedEvolvingStrictJSONSchemaProfile(tools Profile) Profile {
	profile := clone(tools)
	profile.Version = "1.6.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Non-Stream Strict JSON Schema"
	profile.ProtocolProfile = "protocol.doubao-seed-evolving-openai-chat.v5"
	profile.Capabilities = append(profile.Capabilities, CapabilityStrictJSONSchema)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1568221?lang=zh",
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
		"https://docs.volcengine.com/docs/82379/1330310?lang=zh",
	)
	return profile
}

func doubaoSeedEvolvingInlineJPEGProfile(structured Profile) Profile {
	profile := clone(structured)
	profile.Version = "1.7.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Non-Stream Inline JPEG"
	profile.ProtocolProfile = "protocol.doubao-seed-evolving-openai-chat.v6"
	profile.Capabilities = append(profile.Capabilities, CapabilityVision)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://www.volcengine.com/docs/82379/1362931?lang=zh",
	)
	return profile
}

func doubaoSeedEvolvingCacheReadProfile(image Profile) Profile {
	profile := clone(image)
	profile.Version = "1.8.0"
	profile.Label = "Doubao Seed Evolving Ark Chat Implicit Cache Read Usage"
	profile.CacheUsageProfile = "cache-usage.doubao-ark-chat.v1"
	profile.Capabilities = append(profile.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
	profile.AlwaysRequiredCapabilities = []string{CapabilityPromptCache, CapabilityCacheReadUsage}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/2549861?lang=zh",
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
	)
	return profile
}

func doubaoSeed21ProConformantProfile(documented Profile) Profile {
	profile := clone(documented)
	profile.Version = "1.1.0"
	profile.Label = "Doubao Seed 2.1 Pro Ark Chat Strict Non-Stream Core"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-pro-openai-chat.v1"
	profile.ReasoningProfile = "reasoning.doubao-seed-2.1-pro-toggle.v1"
	profile.CacheUsageProfile = ""
	profile.DefaultOutputLimit = 4_096
	profile.Capabilities = []string{CapabilityReasoning}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
		"https://docs.volcengine.com/docs/82379/1449737?lang=zh",
	)
	return profile
}

func doubaoSeed21ProStreamingProfile(core Profile) Profile {
	profile := clone(core)
	profile.Version = "1.2.0"
	profile.Label = "Doubao Seed 2.1 Pro Ark Chat Streaming Reasoning Core"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-pro-openai-chat.v2"
	profile.Capabilities = append(profile.Capabilities, CapabilityStreaming)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
	)
	return profile
}

func doubaoSeed21ProToolsProfile(streaming Profile) Profile {
	profile := clone(streaming)
	profile.Version = "1.3.0"
	profile.Label = "Doubao Seed 2.1 Pro Ark Chat Non-Stream Function Tools"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-pro-openai-chat.v3"
	profile.ReasoningProfile = "reasoning.doubao-seed-2.1-pro-toggle-tools.v2"
	profile.Capabilities = []string{
		CapabilityReasoning,
		CapabilityStreaming,
		CapabilityTools,
		CapabilityFunctionTools,
		CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed,
		CapabilityToolChoiceReq,
		CapabilityParallelTools,
	}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1262342?lang=zh",
		"https://docs.volcengine.com/docs/82379/2636748?lang=zh",
	)
	return profile
}

func doubaoSeed21ProStrictJSONSchemaProfile(tools Profile) Profile {
	profile := clone(tools)
	profile.Version = "1.4.0"
	profile.Label = "Doubao Seed 2.1 Pro Ark Chat Non-Stream Strict JSON Schema"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-pro-openai-chat.v4"
	profile.Capabilities = append(profile.Capabilities, CapabilityStrictJSONSchema)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1568221?lang=zh",
		"https://docs.volcengine.com/docs/82379/1330310?lang=zh",
	)
	return profile
}

func doubaoSeed21ProCacheReadProfile(structured Profile) Profile {
	profile := clone(structured)
	profile.Version = "1.5.0"
	profile.Label = "Doubao Seed 2.1 Pro Ark Chat Implicit Cache Read Usage"
	profile.CacheUsageProfile = "cache-usage.doubao-ark-chat.v1"
	profile.Capabilities = append(profile.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
	profile.AlwaysRequiredCapabilities = []string{CapabilityPromptCache, CapabilityCacheReadUsage}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/2549861?lang=zh",
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
		"https://docs.volcengine.com/docs/82379/1330310?lang=zh",
	)
	return profile
}

func doubaoSeed21ProInlineJPEGProfile(cacheRead Profile) Profile {
	profile := clone(cacheRead)
	profile.Version = "1.6.0"
	profile.Label = "Doubao Seed 2.1 Pro Ark Chat Non-Stream Inline JPEG"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-pro-openai-chat.v5"
	profile.Capabilities = append(profile.Capabilities, CapabilityVision)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/2549861?lang=zh",
		"https://www.volcengine.com/docs/82379/1362931?lang=zh",
		"https://ark.volcengine.com/region:cn-beijing/model/detail?Id=doubao-seed-2-1-pro&Version=260628",
	)
	return profile
}

func doubaoSeed21TurboConformantProfile(documented Profile) Profile {
	profile := clone(documented)
	profile.Version = "1.1.0"
	profile.Label = "Doubao Seed 2.1 Turbo Ark Chat Strict Non-Stream Reasoning Effort Core"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-turbo-openai-chat.v1"
	profile.ReasoningProfile = "reasoning.doubao-seed-2.1-turbo-effort.v1"
	profile.CacheUsageProfile = ""
	profile.DefaultOutputLimit = 4_096
	profile.Capabilities = []string{CapabilityReasoningEffort}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
		"https://ark.volcengine.com/region:cn-beijing/model/detail?Id=doubao-seed-2-1-turbo&Version=260628",
	)
	return profile
}

func doubaoSeed21TurboStreamingProfile(core Profile) Profile {
	profile := clone(core)
	profile.Version = "1.2.0"
	profile.Label = "Doubao Seed 2.1 Turbo Ark Chat Streaming Reasoning Effort Core"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-turbo-openai-chat.v2"
	profile.Capabilities = append(profile.Capabilities, CapabilityStreaming)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
	)
	return profile
}

func doubaoSeed21TurboToolsProfile(streaming Profile) Profile {
	profile := clone(streaming)
	profile.Version = "1.3.0"
	profile.Label = "Doubao Seed 2.1 Turbo Ark Chat Non-Stream Function Tools"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-turbo-openai-chat.v3"
	profile.ReasoningProfile = "reasoning.doubao-seed-2.1-turbo-effort-tools.v2"
	profile.Capabilities = []string{
		CapabilityReasoningEffort,
		CapabilityStreaming,
		CapabilityTools,
		CapabilityFunctionTools,
		CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed,
		CapabilityToolChoiceReq,
		CapabilityParallelTools,
	}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1262342?lang=zh",
		"https://docs.volcengine.com/docs/82379/2636748?lang=zh",
	)
	return profile
}

func doubaoSeed21TurboStrictJSONSchemaProfile(tools Profile) Profile {
	profile := clone(tools)
	profile.Version = "1.4.0"
	profile.Label = "Doubao Seed 2.1 Turbo Ark Chat Non-Stream Strict JSON Schema"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-turbo-openai-chat.v4"
	profile.Capabilities = append(profile.Capabilities, CapabilityStrictJSONSchema)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/1568221?lang=zh",
		"https://docs.volcengine.com/docs/82379/2549861?lang=zh",
		"https://docs.volcengine.com/docs/82379/1330310?lang=zh",
	)
	return profile
}

func doubaoSeed21TurboCacheReadProfile(structured Profile) Profile {
	profile := clone(structured)
	profile.Version = "1.5.0"
	profile.Label = "Doubao Seed 2.1 Turbo Ark Chat Implicit Cache Read Usage"
	profile.CacheUsageProfile = "cache-usage.doubao-ark-chat.v1"
	profile.Capabilities = append(profile.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
	profile.AlwaysRequiredCapabilities = []string{CapabilityPromptCache, CapabilityCacheReadUsage}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://www.volcengine.com/product/ark",
		"https://docs.volcengine.com/docs/82379/1398933?lang=zh",
		"https://docs.volcengine.com/docs/82379/1494384?lang=zh",
		"https://ark.volcengine.com/region:cn-beijing/model/detail?Id=doubao-seed-2-1-turbo&Version=260628",
	)
	return profile
}

func doubaoSeed21TurboInlineJPEGProfile(cacheRead Profile) Profile {
	profile := clone(cacheRead)
	profile.Version = "1.6.0"
	profile.Label = "Doubao Seed 2.1 Turbo Ark Chat Non-Stream Inline JPEG"
	profile.ProtocolProfile = "protocol.doubao-seed-2.1-turbo-openai-chat.v5"
	profile.Capabilities = append(profile.Capabilities, CapabilityVision)
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://docs.volcengine.com/docs/82379/2549861?lang=zh",
		"https://www.volcengine.com/docs/82379/1362931?lang=zh",
		"https://ark.volcengine.com/region:cn-beijing/model/detail?Id=doubao-seed-2-1-turbo&Version=260628",
	)
	return profile
}

func anthropicSonnet5ConformantProfiles(documented Profile) []Profile {
	profile := clone(documented)
	profile.Version = "1.1.0"
	profile.Label = "Claude Sonnet 5 Anthropic Messages Strict Non-Stream Core Text"
	protocolIdentity, ok := immutableProfileEndpointProtocolIdentity(profile.ID, profile.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + profile.ID + "@" + profile.Version)
	}
	profile.ProtocolProfile = protocolIdentity.ProfileID
	profile.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	profile.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[profile.ID+"@"+profile.Version]
	profile.ReasoningProfile = "reasoning.anthropic-sonnet-5-adaptive.v1"
	profile.Capabilities = []string{CapabilityReasoning}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://platform.claude.com/docs/en/about-claude/models/whats-new-sonnet-5",
		"https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons",
	)
	controls := clone(profile)
	controls.Version = "1.2.0"
	controls.Label = "Claude Sonnet 5 Anthropic Messages Adaptive Thinking And Effort"
	protocolIdentity, ok = immutableProfileEndpointProtocolIdentity(controls.ID, controls.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + controls.ID + "@" + controls.Version)
	}
	controls.ProtocolProfile = protocolIdentity.ProfileID
	controls.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	controls.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[controls.ID+"@"+controls.Version]
	controls.ReasoningProfile = "reasoning.anthropic-sonnet-5-adaptive-effort.v1"
	controls.Capabilities = []string{CapabilityReasoning, CapabilityReasoningEffort}
	controls.EvidenceRefs = append(controls.EvidenceRefs,
		"https://platform.claude.com/docs/en/build-with-claude/thinking",
		"https://platform.claude.com/docs/en/build-with-claude/effort",
	)
	streaming := clone(controls)
	streaming.Version = "1.3.0"
	streaming.Label = "Claude Sonnet 5 Anthropic Messages Strict Text Streaming"
	protocolIdentity, ok = immutableProfileEndpointProtocolIdentity(streaming.ID, streaming.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + streaming.ID + "@" + streaming.Version)
	}
	streaming.ProtocolProfile = protocolIdentity.ProfileID
	streaming.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	streaming.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[streaming.ID+"@"+streaming.Version]
	streaming.Capabilities = []string{CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs,
		"https://platform.claude.com/docs/en/build-with-claude/streaming",
		"https://platform.claude.com/docs/en/api/typescript/messages",
	)
	tools := clone(streaming)
	tools.Version = "1.4.0"
	tools.Label = "Claude Sonnet 5 Anthropic Messages Non-Stream Exact Function Tools"
	protocolIdentity, ok = immutableProfileEndpointProtocolIdentity(tools.ID, tools.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + tools.ID + "@" + tools.Version)
	}
	tools.ProtocolProfile = protocolIdentity.ProfileID
	tools.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	tools.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[tools.ID+"@"+tools.Version]
	tools.Capabilities = []string{
		CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
	}
	tools.EvidenceRefs = append(tools.EvidenceRefs,
		"https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview",
		"https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools",
		"https://platform.claude.com/docs/en/agents-and-tools/tool-use/handle-tool-calls",
		"https://platform.claude.com/docs/en/api/messages/create",
	)
	return []Profile{profile, controls, streaming, tools}
}

func anthropicOpus5ConformantProfiles(documented Profile) []Profile {
	profile := clone(documented)
	profile.Version = "1.1.0"
	profile.Label = "Claude Opus 5 Anthropic Messages Adaptive Thinking And Effort"
	protocolIdentity, ok := immutableProfileEndpointProtocolIdentity(profile.ID, profile.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + profile.ID + "@" + profile.Version)
	}
	profile.ProtocolProfile = protocolIdentity.ProfileID
	profile.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	profile.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[profile.ID+"@"+profile.Version]
	profile.ReasoningProfile = "reasoning.anthropic-opus-5-adaptive-effort.v1"
	profile.Capabilities = []string{CapabilityReasoning, CapabilityReasoningEffort}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://platform.claude.com/docs/en/about-claude/models/whats-new-opus-5",
		"https://platform.claude.com/docs/en/about-claude/models/extended-thinking-models",
		"https://platform.claude.com/docs/en/build-with-claude/effort",
		"https://platform.claude.com/docs/en/api/messages/create",
	)
	streaming := clone(profile)
	streaming.Version = "1.2.0"
	streaming.Label = "Claude Opus 5 Anthropic Messages Strict Text Streaming"
	protocolIdentity, ok = immutableProfileEndpointProtocolIdentity(streaming.ID, streaming.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + streaming.ID + "@" + streaming.Version)
	}
	streaming.ProtocolProfile = protocolIdentity.ProfileID
	streaming.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	streaming.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[streaming.ID+"@"+streaming.Version]
	streaming.Capabilities = []string{CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs,
		"https://platform.claude.com/docs/en/build-with-claude/streaming",
		"https://platform.claude.com/docs/en/api/typescript/messages",
	)
	return []Profile{profile, streaming}
}

func anthropicHaiku45ConformantProfiles(documented Profile) []Profile {
	profile := clone(documented)
	profile.Version = "1.1.0"
	profile.Label = "Claude Haiku 4.5 Anthropic Messages Extended Thinking Budget"
	protocolIdentity, ok := immutableProfileEndpointProtocolIdentity(profile.ID, profile.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + profile.ID + "@" + profile.Version)
	}
	profile.ProtocolProfile = protocolIdentity.ProfileID
	profile.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	profile.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[profile.ID+"@"+profile.Version]
	profile.ReasoningProfile = "reasoning.anthropic-haiku-4.5-extended-budget.v1"
	profile.Capabilities = []string{CapabilityReasoning, CapabilityReasoningBudget}
	profile.EvidenceRefs = append(profile.EvidenceRefs,
		"https://platform.claude.com/docs/en/build-with-claude/extended-thinking",
		"https://platform.claude.com/docs/en/api/messages/create",
	)
	streaming := clone(profile)
	streaming.Version = "1.2.0"
	streaming.Label = "Claude Haiku 4.5 Anthropic Messages Strict Summarized-Thinking Streaming"
	protocolIdentity, ok = immutableProfileEndpointProtocolIdentity(streaming.ID, streaming.Version)
	if !ok {
		panic("missing immutable endpoint protocol identity for " + streaming.ID + "@" + streaming.Version)
	}
	streaming.ProtocolProfile = protocolIdentity.ProfileID
	streaming.ProtocolProfileVersion = protocolIdentity.ProfileVersion
	streaming.OperationPolicyProfile = immutableProfileOperationPolicyIdentities[streaming.ID+"@"+streaming.Version]
	streaming.Capabilities = []string{CapabilityReasoning, CapabilityReasoningBudget, CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs,
		"https://platform.claude.com/docs/en/build-with-claude/streaming",
		"https://platform.claude.com/docs/en/build-with-claude/extended-thinking",
		"https://platform.claude.com/docs/en/api/typescript/messages",
	)
	return []Profile{profile, streaming}
}

func openAISurfaceProfiles(
	documentedResponses,
	conformantChat Profile,
	label string,
	responsesCoreConformant bool,
	responsesReasoningProfile string,
) []Profile {
	profileIDPrefix := strings.TrimSuffix(documentedResponses.ID, ".api")
	responses := clone(documentedResponses)
	responses.ID = profileIDPrefix + ".responses"
	responses.Version = "1.0.0"
	responses.Label = label + " Responses"
	// The provider advertises a 1.05M context, but requests above 272K enter a
	// price band the current Rate Card cannot represent. Keep the provider fact
	// visible while preventing onboarding from advertising an unpriceable cap.
	responses.ExecutableContextWindow = 272_000
	responses.DefaultContextWindow = 272_000
	profiles := []Profile{responses}
	if responsesCoreConformant {
		core := clone(responses)
		core.Version = "1.1.0"
		core.Label = label + " Responses Strict Non-Stream Core Text"
		protocolIdentity, ok := immutableProfileEndpointProtocolIdentity(core.ID, core.Version)
		if !ok {
			panic("missing immutable endpoint protocol identity for " + core.ID + "@" + core.Version)
		}
		core.ProtocolProfile = protocolIdentity.ProfileID
		core.ProtocolProfileVersion = protocolIdentity.ProfileVersion
		// Core text is the operation baseline, not an advanced capability tag.
		// Its local conformance evidence must therefore remain independent from
		// the empty advanced Capabilities set.
		core.CatalogEvidenceState = "locally_conformant"
		profiles = append(profiles, core)
		if responsesReasoningProfile != "" {
			reasoning := clone(core)
			reasoning.Version = "1.2.0"
			reasoning.Label = label + " Responses Reasoning Effort"
			reasoning.ReasoningProfile = responsesReasoningProfile
			reasoning.Capabilities = []string{CapabilityReasoningEffort}
			reasoning.EvidenceRefs = append(reasoning.EvidenceRefs,
				"https://developers.openai.com/api/docs/guides/reasoning",
			)
			profiles = append(profiles, reasoning)
			if responses.ID == "openai.gpt-5.6-sol.responses" ||
				responses.ID == "openai.gpt-5.6-terra.responses" ||
				responses.ID == "openai.gpt-5.6-luna.responses" {
				streaming := clone(reasoning)
				streaming.Version = "1.3.0"
				streaming.Label = label + " Responses Strict Text Streaming"
				protocolIdentity, ok := immutableProfileEndpointProtocolIdentity(streaming.ID, streaming.Version)
				if !ok {
					panic("missing immutable endpoint protocol identity for " + streaming.ID + "@" + streaming.Version)
				}
				streaming.ProtocolProfile = protocolIdentity.ProfileID
				streaming.ProtocolProfileVersion = protocolIdentity.ProfileVersion
				streaming.Capabilities = []string{CapabilityReasoningEffort, CapabilityStreaming}
				streaming.EvidenceRefs = append(streaming.EvidenceRefs,
					"https://developers.openai.com/api/docs/guides/streaming-responses",
					"https://developers.openai.com/api/reference/resources/responses/streaming-events",
				)
				profiles = append(profiles, streaming)
			}
		}
	}

	chat := clone(conformantChat)
	chat.ID = profileIDPrefix + ".chat"
	chat.Version = "1.0.0"
	chat.Label = label + " Chat Completions"

	return append(profiles, chat)
}

func xAIConformantProfileChain(documented Profile) []Profile {
	chatReference := "https://docs.x.ai/developers/model-capabilities/legacy/chat-completions"
	modelReference := "https://docs.x.ai/developers/models/grok-4.6"
	current := clone(documented)
	current.Version = "1.1.0"
	current.Label = "Grok 4.6 OpenAI-compatible Core Chat 子集"
	current.ProtocolProfile = "protocol.xai-openai-chat.v1"
	// xAI explicitly documents no text output limit. Keep that provider fact as
	// ProviderMaxOutput=0 while imposing a separately-labelled UniRouter policy
	// ceiling so admission and billing liability remain finite.
	current.ExecutableMaxOutput = 131_072
	current.Capabilities = []string{CapabilityStreaming}
	current.EvidenceRefs = append(current.EvidenceRefs, modelReference, chatReference)
	return []Profile{current}
}

func openAIConformantProfileChain(documented Profile) []Profile {
	modelGuide := documented.EvidenceRefs[0]
	chatReference := "https://developers.openai.com/api/reference/cli/resources/chat/subresources/completions/methods/create"
	promptCaching := "https://developers.openai.com/api/docs/guides/prompt-caching"
	profiles := make([]Profile, 0, 6)

	streaming := clone(documented)
	streaming.Version = "1.1.0"
	streaming.ProtocolProfile = "protocol.openai-chat.v1"
	streaming.APISurface = APISurfaceOpenAIChat
	streaming.Operations = []string{OperationOpenAIChat}
	streaming.ReasoningProfile = "reasoning.openai-gpt-5.6-chat.v1"
	streaming.CacheUsageProfile = "cache-usage.openai-documented.v1"
	// OpenAI documents 1.05M provider context, but requests above 272K enter a
	// different input/output pricing band. Keep the provider maximum visible
	// while the current executable profile stops at the largest context the
	// present Rate Card can bill without mispricing.
	streaming.ExecutableContextWindow = 272_000
	streaming.DefaultContextWindow = 272_000
	streaming.Capabilities = []string{CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs, chatReference)
	profiles = append(profiles, streaming)

	reasoning := clone(streaming)
	reasoning.Version = "1.2.0"
	reasoning.Capabilities = append(reasoning.Capabilities, "reasoning", "reasoning_effort")
	reasoning.EvidenceRefs = append(reasoning.EvidenceRefs, modelGuide)
	profiles = append(profiles, reasoning)

	tools := clone(reasoning)
	tools.Version = "1.3.0"
	tools.ProtocolProfile = "protocol.openai-chat.v2"
	tools.Capabilities = append(tools.Capabilities,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
	)
	profiles = append(profiles, tools)

	structuredOutput := clone(tools)
	structuredOutput.Version = "1.4.0"
	structuredOutput.ProtocolProfile = "protocol.openai-chat.v3"
	structuredOutput.Capabilities = append(structuredOutput.Capabilities, CapabilityJSONObject, CapabilityStrictJSONSchema)
	profiles = append(profiles, structuredOutput)

	vision := clone(structuredOutput)
	vision.Version = "1.5.0"
	vision.ProtocolProfile = "protocol.openai-chat.v4"
	vision.Capabilities = append(vision.Capabilities, CapabilityVision)
	profiles = append(profiles, vision)

	cache := clone(vision)
	cache.Version = "1.6.0"
	cache.ProtocolProfile = "protocol.openai-chat.v5"
	cache.CacheUsageProfile = "cache-usage.openai-gpt-5.6.v1"
	cache.Capabilities = append(cache.Capabilities,
		CapabilityPromptCache, CapabilityExplicitCache,
		CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
	)
	cache.EvidenceRefs = append(cache.EvidenceRefs, promptCaching)
	profiles = append(profiles, cache)

	return profiles
}

func hy3ConformantProfileChain(documented Profile) []Profile {
	chatGuide := "https://cloud.tencent.com/document/product/1823/132252"
	chatProtocol := "https://cloud.tencent.com/document/product/1823/135872"
	searchGuide := "https://cloud.tencent.com/document/product/1823/132358"
	profiles := make([]Profile, 0, 6)

	streaming := clone(documented)
	streaming.Version = "1.1.0"
	streaming.ProtocolProfile = "protocol.hy3-openai-chat.v1"
	streaming.DefaultOutputLimit = 131_072
	streaming.ProviderCapabilities = []string{
		CapabilityStreaming, "reasoning", "reasoning_effort", CapabilityTools, "tool_choice",
		CapabilityJSONObject, CapabilityJSONSchema, CapabilityPromptCache, CapabilityBuiltinWebSearch,
	}
	streaming.Capabilities = []string{CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs, chatGuide, chatProtocol)
	profiles = append(profiles, streaming)

	reasoning := clone(streaming)
	reasoning.Version = "1.2.0"
	reasoning.ReasoningProfile = "reasoning.hy3-chat.v1"
	reasoning.Capabilities = append(reasoning.Capabilities, "reasoning")
	profiles = append(profiles, reasoning)

	tools := clone(reasoning)
	tools.Version = "1.3.0"
	tools.ProtocolProfile = "protocol.hy3-openai-chat.v2"
	tools.Capabilities = append(tools.Capabilities,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
	)
	profiles = append(profiles, tools)

	structuredOutput := clone(tools)
	structuredOutput.Version = "1.4.0"
	structuredOutput.ProtocolProfile = "protocol.hy3-openai-chat.v3"
	structuredOutput.Capabilities = append(structuredOutput.Capabilities, CapabilityJSONObject, CapabilityJSONSchema)
	profiles = append(profiles, structuredOutput)

	cacheUsage := clone(structuredOutput)
	cacheUsage.Version = "1.5.0"
	cacheUsage.CacheUsageProfile = "cache-usage.hy3-openai.v1"
	cacheUsage.Capabilities = append(cacheUsage.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
	profiles = append(profiles, cacheUsage)

	searchRequest := clone(cacheUsage)
	searchRequest.Version = "1.6.0"
	searchRequest.ProtocolProfile = "protocol.hy3-openai-chat.v4"
	searchRequest.Capabilities = append(searchRequest.Capabilities, CapabilityBuiltinSearchReq)
	searchRequest.EvidenceRefs = append(searchRequest.EvidenceRefs, searchGuide)
	profiles = append(profiles, searchRequest)

	return profiles
}

func qwenConformantProfileChain(documented Profile, reasoningProfile string, jsonSchema bool, multimodalSources []string) []Profile {
	profiles := make([]Profile, 0, 8)
	streaming := clone(documented)
	streaming.Version = "1.1.0"
	streaming.ProtocolProfile = "protocol.qwen-openai-chat.v1"
	streaming.Capabilities = []string{CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs,
		"https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions",
	)
	profiles = append(profiles, streaming)

	reasoning := clone(streaming)
	reasoning.Version = "1.2.0"
	reasoning.ReasoningProfile = reasoningProfile
	reasoning.Capabilities = append(reasoning.Capabilities, "reasoning")
	reasoning.EvidenceRefs = append(reasoning.EvidenceRefs,
		"https://help.aliyun.com/zh/model-studio/deep-thinking",
	)
	profiles = append(profiles, reasoning)

	tools := clone(reasoning)
	tools.Version = "1.3.0"
	tools.ProtocolProfile = "protocol.qwen-openai-chat.v2"
	tools.Capabilities = append(tools.Capabilities,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools, CapabilityToolStream,
	)
	tools.EvidenceRefs = append(tools.EvidenceRefs,
		"https://help.aliyun.com/zh/model-studio/qwen-function-calling",
		"https://help.aliyun.com/zh/model-studio/qwen-api-via-dashscope",
	)
	profiles = append(profiles, tools)

	structuredOutput := clone(tools)
	structuredOutput.Version = "1.4.0"
	structuredOutput.ProtocolProfile = "protocol.qwen-openai-chat.v3"
	if !jsonSchema {
		structuredOutput.ProtocolProfile = "protocol.qwen-openai-chat.flash.v3"
	}
	structuredOutput.Capabilities = append(structuredOutput.Capabilities, CapabilityJSONObject)
	if jsonSchema {
		structuredOutput.Capabilities = append(structuredOutput.Capabilities, CapabilityStrictJSONSchema)
	}
	structuredOutput.EvidenceRefs = append(structuredOutput.EvidenceRefs,
		"https://help.aliyun.com/zh/model-studio/qwen-structured-output",
	)
	profiles = append(profiles, structuredOutput)

	promptCache := clone(structuredOutput)
	promptCache.Version = "1.5.0"
	promptCache.ProtocolProfile = "protocol.qwen-openai-chat.v4"
	if !jsonSchema {
		promptCache.ProtocolProfile = "protocol.qwen-openai-chat.flash.v4"
	}
	promptCache.CacheUsageProfile = "cache-usage.qwen-context-cache.v1"
	promptCache.Capabilities = append(promptCache.Capabilities,
		CapabilityPromptCache, CapabilityExplicitCache,
		CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
	)
	promptCache.EvidenceRefs = append(promptCache.EvidenceRefs,
		"https://help.aliyun.com/zh/model-studio/context-cache",
	)
	profiles = append(profiles, promptCache)

	multimodal := clone(promptCache)
	multimodal.Version = "1.6.0"
	multimodal.ProtocolProfile = "protocol.qwen-openai-chat.v5"
	if !jsonSchema {
		multimodal.ProtocolProfile = "protocol.qwen-openai-chat.flash.v5"
	}
	multimodal.Capabilities = append(multimodal.Capabilities, CapabilityVision, CapabilityVideo)
	multimodal.EvidenceRefs = append(multimodal.EvidenceRefs, multimodalSources...)
	profiles = append(profiles, multimodal)

	searchRequest := clone(multimodal)
	searchRequest.Version = "1.7.0"
	searchRequest.ProtocolProfile = "protocol.qwen-openai-chat.v6"
	if !jsonSchema {
		searchRequest.ProtocolProfile = "protocol.qwen-openai-chat.flash.v6"
	}
	searchRequest.Capabilities = append(searchRequest.Capabilities, CapabilityBuiltinSearchReq)
	searchRequest.EvidenceRefs = append(searchRequest.EvidenceRefs,
		"https://help.aliyun.com/zh/model-studio/web-search",
	)
	profiles = append(profiles, searchRequest)

	if documented.ID == "qwen.3.8-max.api" {
		outputLimitCorrection := clone(searchRequest)
		outputLimitCorrection.Version = "1.8.0"
		outputLimitCorrection.Label = "Qwen3.8-Max 官方 API（131072 思考输出合同）"
		outputLimitCorrection.ReasoningProfile = "reasoning.qwen3.8-max-hybrid.v2"
		outputLimitCorrection.Capabilities = append(outputLimitCorrection.Capabilities, CapabilityReasoningBudget)
		outputLimitCorrection.EvidenceRefs = append(outputLimitCorrection.EvidenceRefs,
			"https://help.aliyun.com/zh/model-studio/qwen3-8-max",
			"https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions",
		)
		profiles = append(profiles, outputLimitCorrection)
	}
	return profiles
}

func geminiConformantProfileChain(documented Profile, reasoningProfile string, processingTier bool) []Profile {
	capacity := 6
	if processingTier {
		capacity++
	}
	profiles := make([]Profile, 0, capacity)
	openAICompatibility := "https://ai.google.dev/gemini-api/docs/openai"

	streaming := clone(documented)
	streaming.Version = "1.1.0"
	streaming.Label = strings.TrimSuffix(documented.Label, " 官方 API") + " OpenAI-compatible Core Chat 子集"
	streaming.ProtocolProfile = "protocol.gemini-openai-chat.v1"
	streaming.Capabilities = []string{CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs, openAICompatibility)
	profiles = append(profiles, streaming)

	reasoning := clone(streaming)
	reasoning.Version = "1.2.0"
	reasoning.ReasoningProfile = reasoningProfile
	reasoning.Capabilities = append(reasoning.Capabilities, "reasoning")
	reasoning.EvidenceRefs = append(reasoning.EvidenceRefs,
		"https://ai.google.dev/gemini-api/docs/thinking",
	)
	profiles = append(profiles, reasoning)

	tools := clone(reasoning)
	tools.Version = "1.3.0"
	tools.ProtocolProfile = "protocol.gemini-openai-chat.v2"
	tools.Capabilities = append(tools.Capabilities,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
	)
	tools.EvidenceRefs = append(tools.EvidenceRefs,
		"https://ai.google.dev/gemini-api/docs/function-calling",
	)
	profiles = append(profiles, tools)

	structuredOutput := clone(tools)
	structuredOutput.Version = "1.4.0"
	structuredOutput.ProtocolProfile = "protocol.gemini-openai-chat.v3"
	structuredOutput.Capabilities = append(structuredOutput.Capabilities, CapabilityStrictJSONSchema)
	structuredOutput.EvidenceRefs = append(structuredOutput.EvidenceRefs,
		"https://ai.google.dev/gemini-api/docs/structured-output",
	)
	profiles = append(profiles, structuredOutput)

	image := clone(structuredOutput)
	image.Version = "1.5.0"
	image.ProtocolProfile = "protocol.gemini-openai-chat.v4"
	image.Capabilities = append(image.Capabilities, CapabilityVision)
	image.EvidenceRefs = append(image.EvidenceRefs, openAICompatibility)
	profiles = append(profiles, image)

	audio := clone(image)
	audio.Version = "1.6.0"
	audio.ProtocolProfile = "protocol.gemini-openai-chat.v5"
	audio.Capabilities = append(audio.Capabilities, CapabilityAudio)
	audio.EvidenceRefs = append(audio.EvidenceRefs, openAICompatibility)
	profiles = append(profiles, audio)

	if processingTier {
		tier := clone(audio)
		tier.Version = "1.7.0"
		tier.ProtocolProfile = "protocol.gemini-openai-chat.v6"
		tier.ProcessingTierPolicy = ProcessingTierPolicyGeminiOpenAICompatibilityV1
		tier.Capabilities = append(tier.Capabilities, CapabilityServiceTier)
		tier.EvidenceRefs = append(tier.EvidenceRefs,
			"https://ai.google.dev/gemini-api/docs/flex-inference",
			"https://ai.google.dev/gemini-api/docs/priority-inference",
		)
		profiles = append(profiles, tier)
	}

	return profiles
}

func mimoConformantProfileChain(documented Profile, multimodal bool) []Profile {
	capacity := 5
	if multimodal {
		capacity++
	}
	profiles := make([]Profile, 0, capacity)
	chatAPI := "https://mimo.mi.com/docs/en-US/api/chat/openai-api"

	streaming := clone(documented)
	streaming.Version = "1.1.0"
	streaming.ProtocolProfile = "protocol.mimo-openai-chat.v1"
	streaming.Capabilities = []string{CapabilityStreaming}
	streaming.EvidenceRefs = append(streaming.EvidenceRefs, chatAPI)
	profiles = append(profiles, streaming)

	reasoning := clone(streaming)
	reasoning.Version = "1.2.0"
	reasoning.ReasoningProfile = "reasoning.mimo-v2.5-thinking-toggle.v1"
	reasoning.Capabilities = append(reasoning.Capabilities, "reasoning")
	profiles = append(profiles, reasoning)

	tools := clone(reasoning)
	tools.Version = "1.3.0"
	tools.ProtocolProfile = "protocol.mimo-openai-chat.v2"
	tools.Capabilities = append(tools.Capabilities,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
	)
	profiles = append(profiles, tools)

	structuredOutput := clone(tools)
	structuredOutput.Version = "1.4.0"
	structuredOutput.ProtocolProfile = "protocol.mimo-openai-chat.v3"
	structuredOutput.Capabilities = append(structuredOutput.Capabilities, CapabilityJSONObject)
	profiles = append(profiles, structuredOutput)

	cacheUsage := clone(structuredOutput)
	cacheUsage.Version = "1.5.0"
	cacheUsage.CacheUsageProfile = "cache-usage.mimo-openai.v1"
	cacheUsage.Capabilities = append(cacheUsage.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
	profiles = append(profiles, cacheUsage)

	if multimodal {
		media := clone(cacheUsage)
		media.Version = "1.6.0"
		media.ProtocolProfile = "protocol.mimo-openai-chat.v4"
		media.Capabilities = append(media.Capabilities, CapabilityVision, CapabilityAudio, CapabilityVideo)
		profiles = append(profiles, media)
	}

	return profiles
}

// legacyConformantProviderProfiles leaves every pre-existing profile bundle
// byte-for-byte addressable and publishes typed protocol improvements only as
// new versions. This prevents a newly modeled operation from retroactively
// changing sealed execution evidence that references an older bundle.
func legacyConformantProviderProfiles() []Profile {
	profiles := make([]Profile, 0, 8)
	for _, existing := range legacyProviderProfiles {
		switch {
		case (existing.ID == "deepseek.v4-flash.api" || existing.ID == "deepseek.v4-pro.api") && existing.Version == "1.1.0":
			promoted := clone(existing)
			promoted.Version = "1.2.0"
			promoted.ProtocolProfile = "protocol.deepseek-openai-chat.v2"
			promoted.ProviderCapabilities = []string{
				CapabilityStreaming, CapabilityReasoning, CapabilityReasoningEffort, CapabilityTools, CapabilityJSONOutput, CapabilityPromptCache,
			}
			promoted.Capabilities = []string{
				CapabilityStreaming, CapabilityReasoning, CapabilityReasoningEffort, CapabilityTools, CapabilityFunctionTools,
				CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq,
				CapabilityParallelTools, CapabilityJSONObject, CapabilityPromptCache, CapabilityCacheReadUsage,
			}
			profiles = append(profiles, promoted)
			candidate := clone(promoted)
			candidate.Version = "1.3.0"
			candidate.ProtocolProfile = "protocol.deepseek-openai-chat.v3"
			if existing.ID == "deepseek.v4-flash.api" {
				candidate.ReasoningProfile = "reasoning.deepseek-v4-flash-thinking-toggle.v3"
			} else {
				candidate.ReasoningProfile = "reasoning.deepseek-v4-pro-thinking-toggle.v3"
			}
			candidate.Capabilities = slices.DeleteFunc(candidate.Capabilities, func(capability string) bool {
				return capability == CapabilityParallelTools
			})
			profiles = append(profiles, candidate)
			toolHistory := clone(candidate)
			toolHistory.Version = "1.4.0"
			toolHistory.ProtocolProfile = "protocol.deepseek-openai-chat.v4"
			if existing.ID == "deepseek.v4-flash.api" {
				toolHistory.ReasoningProfile = "reasoning.deepseek-v4-flash-thinking-toggle.v4"
			} else {
				toolHistory.ReasoningProfile = "reasoning.deepseek-v4-pro-thinking-toggle.v4"
			}
			profiles = append(profiles, toolHistory)
			toolChoiceCompatibility := clone(toolHistory)
			toolChoiceCompatibility.Version = "1.5.0"
			if existing.ID == "deepseek.v4-flash.api" {
				toolChoiceCompatibility.ReasoningProfile = "reasoning.deepseek-v4-flash-thinking-toggle.v5"
			} else {
				toolChoiceCompatibility.ReasoningProfile = "reasoning.deepseek-v4-pro-thinking-toggle.v5"
			}
			profiles = append(profiles, toolChoiceCompatibility)
		case existing.ID == "kimi.k2.6.api" && existing.Version == "1.5.0":
			promoted := clone(existing)
			promoted.Version = "1.6.0"
			promoted.Aliases = []string{"Kimi K2.6", "kimi-k2.6"}
			promoted.ProtocolProfile = "protocol.kimi-openai-chat.v4"
			promoted.ProviderCapabilities = append([]string{CapabilityStreaming}, promoted.ProviderCapabilities...)
			promoted.Capabilities = []string{
				CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
				CapabilityJSONObject, CapabilityPartialMode, CapabilityBuiltinWebSearch,
				CapabilityVision, CapabilityVideo, CapabilityPromptCache, CapabilityCacheReadUsage,
			}
			profiles = append(profiles, promoted)
			candidate := clone(promoted)
			candidate.Version = "1.7.0"
			candidate.ProtocolProfile = "protocol.kimi-openai-chat.v5"
			profiles = append(profiles, candidate)

		case existing.ID == "kimi.k2.7-code.api" && existing.Version == "1.1.0":
			promoted := clone(existing)
			promoted.Version = "1.2.0"
			promoted.Aliases = []string{"Kimi K2.7 Code", "kimi-k2.7-code"}
			promoted.ProtocolProfile = "protocol.kimi-openai-chat.v4"
			promoted.ProviderCapabilities = append([]string{CapabilityStreaming}, promoted.ProviderCapabilities...)
			promoted.Capabilities = []string{
				CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
				CapabilityJSONObject, CapabilityPartialMode, CapabilityVision, CapabilityVideo,
				CapabilityPromptCache, CapabilityCacheReadUsage,
			}
			profiles = append(profiles, promoted)
			candidate := clone(promoted)
			candidate.Version = "1.3.0"
			candidate.ProtocolProfile = "protocol.kimi-openai-chat.v5"
			profiles = append(profiles, candidate)

		case existing.ID == "kimi.k3.api" && existing.Version == "1.5.0":
			promoted := clone(existing)
			promoted.Version = "1.6.0"
			promoted.Aliases = []string{"Kimi K3", "kimi-k3"}
			promoted.ProtocolProfile = "protocol.kimi-openai-chat.k3.v3"
			promoted.ProviderCapabilities = append([]string{CapabilityStreaming}, promoted.ProviderCapabilities...)
			promoted.Capabilities = []string{
				CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
				CapabilityToolChoiceAuto, CapabilityToolChoiceReq,
				CapabilityJSONObject, CapabilityJSONSchema, CapabilityDynamicTools,
				CapabilityPartialMode, CapabilityBuiltinWebSearch, CapabilityVision, CapabilityVideo,
				CapabilityPromptCache, CapabilityCacheReadUsage,
			}
			profiles = append(profiles, promoted)
			candidate := clone(promoted)
			candidate.Version = "1.7.0"
			candidate.ProtocolProfile = "protocol.kimi-openai-chat.k3.v4"
			candidate.Capabilities = slices.DeleteFunc(candidate.Capabilities, func(capability string) bool {
				return capability == CapabilityDynamicTools
			})
			profiles = append(profiles, candidate)
			dynamicCandidate := clone(candidate)
			dynamicCandidate.Version = "1.8.0"
			dynamicCandidate.ProtocolProfile = "protocol.kimi-openai-chat.k3.v5"
			dynamicCandidate.Capabilities = append(dynamicCandidate.Capabilities, CapabilityDynamicTools)
			profiles = append(profiles, dynamicCandidate)
			effortCandidate := clone(dynamicCandidate)
			effortCandidate.Version = "1.9.0"
			effortCandidate.ProviderCapabilities = append(effortCandidate.ProviderCapabilities, CapabilityReasoningEffort)
			effortCandidate.Capabilities = append(effortCandidate.Capabilities, CapabilityReasoningEffort)
			profiles = append(profiles, effortCandidate)

		case (existing.ID == "minimax.m2.7.token-plan" ||
			existing.ID == "minimax.m2.5-highspeed.token-plan" ||
			existing.ID == "minimax.m3.token-plan") && existing.Version == "1.0.0":
			promoted := clone(existing)
			promoted.Version = "1.1.0"
			promoted.ProtocolProfile = "protocol.minimax-openai-chat.v3"
			promoted.ProviderCapabilities = append([]string{CapabilityStreaming}, promoted.ProviderCapabilities...)
			promoted.Capabilities = []string{
				CapabilityStreaming, CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
			}
			profiles = append(profiles, promoted)

			current := clone(promoted)
			current.Version = "1.2.0"
			current.Label = strings.ReplaceAll(current.Label, " Token Plan", " 官方 API（OpenAI Chat 核心子集）")
			if current.ID == "minimax.m2.7.token-plan" {
				current.Aliases = []string{"MiniMax M2.7", "MiniMax-M2.7"}
			}
			if current.ID == "minimax.m3.token-plan" {
				current.ProtocolProfile = "protocol.minimax-m3-openai-chat.v4"
			} else {
				current.ProtocolProfile = "protocol.minimax-m2-openai-chat.v4"
			}
			profiles = append(profiles, current)

			if current.ID == "minimax.m3.token-plan" {
				reasoning := clone(current)
				reasoning.Version = "1.3.0"
				reasoning.Label = "MiniMax M3 官方 API（OpenAI Chat Embedded Reasoning）"
				reasoning.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v1"
				reasoning.ProtocolProfile = "protocol.minimax-m3-openai-chat.v5"
				reasoning.Capabilities = append(reasoning.Capabilities, CapabilityReasoning)
				profiles = append(profiles, reasoning)

				image := clone(reasoning)
				image.Version = "1.4.0"
				image.Label = "MiniMax M3 官方 API（OpenAI Chat Non-Stream Inline JPEG）"
				image.CacheUsageProfile = ""
				image.ProtocolProfile = "protocol.minimax-m3-openai-chat.v6"
				image.Capabilities = append(image.Capabilities, CapabilityVision)
				image.EvidenceRefs = append(image.EvidenceRefs,
					"https://platform.minimaxi.com/docs/api-reference/text-chat-openai",
				)
				profiles = append(profiles, image)

				tier := clone(image)
				tier.Version = "1.5.0"
				tier.Label = "MiniMax M3 官方 API（OpenAI Chat Processing Tier）"
				tier.ProtocolProfile = "protocol.minimax-m3-openai-chat.v7"
				tier.ProcessingTierPolicy = ProcessingTierPolicyMiniMaxOpenAIChatV1
				tier.ProviderCapabilities = append(tier.ProviderCapabilities, CapabilityServiceTier)
				tier.Capabilities = append(tier.Capabilities, CapabilityServiceTier)
				tier.EvidenceRefs = append(tier.EvidenceRefs,
					"https://platform.minimaxi.com/docs/guides/pricing-paygo",
				)
				profiles = append(profiles, tier)

				cacheRead := clone(tier)
				cacheRead.Version = "1.6.0"
				cacheRead.Label = "MiniMax M3 官方 API（OpenAI Chat Automatic Cache Read Usage）"
				cacheRead.CacheUsageProfile = "cache-usage.minimax-openai.v1"
				cacheRead.ProtocolProfile = "protocol.minimax-m3-openai-chat.v8"
				cacheRead.Capabilities = append(cacheRead.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
				cacheRead.AlwaysRequiredCapabilities = append(
					cacheRead.AlwaysRequiredCapabilities, CapabilityPromptCache, CapabilityCacheReadUsage,
				)
				cacheRead.EvidenceRefs = append(cacheRead.EvidenceRefs,
					"https://platform.minimaxi.com/docs/api-reference/text-prompt-caching",
				)
				profiles = append(profiles, cacheRead)

				tools := clone(cacheRead)
				tools.Version = "1.7.0"
				tools.Label = "MiniMax M3 官方 API（OpenAI Chat Exact Function Tools）"
				tools.ProtocolProfile = "protocol.minimax-m3-openai-chat.v9"
				profiles = append(profiles, tools)
			}
			if current.ID == "minimax.m2.7.token-plan" {
				reasoning := clone(current)
				reasoning.Version = "1.3.0"
				reasoning.Label = "MiniMax M2.7 官方 API（OpenAI Chat Required Embedded Reasoning）"
				reasoning.ReasoningProfile = "reasoning.minimax-m2-required-embedded.v1"
				reasoning.ProtocolProfile = "protocol.minimax-m2-openai-chat.v5"
				reasoning.Capabilities = append(reasoning.Capabilities, CapabilityReasoning)
				reasoning.AlwaysRequiredCapabilities = append(reasoning.AlwaysRequiredCapabilities, CapabilityReasoning)
				profiles = append(profiles, reasoning)

				cacheRead := clone(reasoning)
				cacheRead.Version = "1.4.0"
				cacheRead.Label = "MiniMax M2.7 官方 API（OpenAI Chat Automatic Cache Read Usage）"
				cacheRead.CacheUsageProfile = "cache-usage.minimax-openai.v1"
				cacheRead.ProtocolProfile = "protocol.minimax-m2-openai-chat.v6"
				cacheRead.Capabilities = append(cacheRead.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
				cacheRead.AlwaysRequiredCapabilities = append(
					cacheRead.AlwaysRequiredCapabilities, CapabilityPromptCache, CapabilityCacheReadUsage,
				)
				cacheRead.EvidenceRefs = append(cacheRead.EvidenceRefs,
					"https://platform.minimaxi.com/docs/api-reference/text-prompt-caching",
				)
				profiles = append(profiles, cacheRead)

				tools := clone(cacheRead)
				tools.Version = "1.5.0"
				tools.Label = "MiniMax M2.7 官方 API（OpenAI Chat Exact Function Tools）"
				tools.ProtocolProfile = "protocol.minimax-m2-openai-chat.v7"
				profiles = append(profiles, tools)
			}
			if current.ID == "minimax.m2.5-highspeed.token-plan" {
				reasoning := clone(current)
				reasoning.Version = "1.3.0"
				reasoning.Label = "MiniMax M2.5 Highspeed 官方 API（OpenAI Chat Required Embedded Reasoning）"
				reasoning.ReasoningProfile = "reasoning.minimax-m2-required-embedded.v1"
				reasoning.ProtocolProfile = "protocol.minimax-m2-openai-chat.v5"
				reasoning.Capabilities = append(reasoning.Capabilities, CapabilityReasoning)
				reasoning.AlwaysRequiredCapabilities = append(reasoning.AlwaysRequiredCapabilities, CapabilityReasoning)
				profiles = append(profiles, reasoning)

				cacheRead := clone(reasoning)
				cacheRead.Version = "1.4.0"
				cacheRead.Label = "MiniMax M2.5 Highspeed 官方 API（OpenAI Chat Automatic Cache Read Usage）"
				cacheRead.CacheUsageProfile = "cache-usage.minimax-openai.v1"
				cacheRead.ProtocolProfile = "protocol.minimax-m2-openai-chat.v6"
				cacheRead.Capabilities = append(cacheRead.Capabilities, CapabilityPromptCache, CapabilityCacheReadUsage)
				cacheRead.AlwaysRequiredCapabilities = append(
					cacheRead.AlwaysRequiredCapabilities, CapabilityPromptCache, CapabilityCacheReadUsage,
				)
				cacheRead.EvidenceRefs = append(cacheRead.EvidenceRefs,
					"https://platform.minimaxi.com/docs/api-reference/text-prompt-caching",
				)
				profiles = append(profiles, cacheRead)

				tools := clone(cacheRead)
				tools.Version = "1.5.0"
				tools.Label = "MiniMax M2.5 Highspeed 官方 API（OpenAI Chat Exact Function Tools）"
				tools.ProtocolProfile = "protocol.minimax-m2-openai-chat.v7"
				profiles = append(profiles, tools)
			}
		}
	}
	return profiles
}

// currentProviderProfiles contains provider-documented onboarding contracts.
// Capabilities intentionally remains empty until the exact model x operation x
// capability is locally conformant; ProviderCapabilities must not silently
// become routing or admission authority.
func currentProviderProfiles() []Profile {
	return []Profile{
		{
			ID: "glm.5.3.api", Version: "1.8.0", Label: "GLM-5.3 官方开放平台 API（精确 Function Tools）",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-required.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v5",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_object", "prompt_cache"},
			Capabilities: []string{
				CapabilityStreaming, CapabilityReasoning, CapabilityTools, CapabilityFunctionTools,
				CapabilityToolChoiceAuto, CapabilityJSONObject, CapabilityPromptCache,
			},
			Aliases: []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/cn/guide/develop/openai/introduction",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.7.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-required.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v5",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_object", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "json_object", "prompt_cache"},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.6.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-required.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v5",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_object", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "json_object"},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.5.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-required.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v4",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools"},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.4.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-required.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v3",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning"},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.3.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v3",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.2.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v2",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.3.api", Version: "1.1.0", Label: "GLM-5.3 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.3-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_output", "prompt_cache"},
			Aliases:              []string{"GLM-5.3", "glm-5.3"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3",
				"https://bigmodel.cn/pricing",
			},
		},
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
			ID: "glm.5.2.api", Version: "1.7.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.2-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v5",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_object", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "json_object", "prompt_cache"},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.6.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.2-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v5",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_object", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "json_object"},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.5.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.2-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v4",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools"},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.4.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5.2-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v3",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning"},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.3.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v3",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.2.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v2",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_output", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5.2.api", Version: "1.1.0", Label: "GLM-5.2 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v1",
			ProviderContextWindow: 1_000_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 1_000_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_output", "prompt_cache"},
			Aliases:              []string{"GLM-5.2", "glm-5.2"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2",
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
			ID: "glm.5v-turbo.api", Version: "1.10.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.glm5v.v4",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision, CapabilityVideo, CapabilityFiles},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/cn/guide/capabilities/cache",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.9.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.glm5v.v3",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision, CapabilityVideo},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/cn/guide/capabilities/cache",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.8.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.glm5v.v2",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/cn/guide/develop/python/introduction",
				"https://docs.bigmodel.cn/cn/guide/develop/openai/introduction",
				"https://docs.bigmodel.cn/cn/guide/capabilities/cache",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.7.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-openai.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.glm5v.v1",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache"},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/cn/guide/capabilities/cache",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.6.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.glm5v.v1",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools"},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.5.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v4",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning", "tools"},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking",
				"https://docs.bigmodel.cn/cn/guide/capabilities/function-calling",
				"https://docs.bigmodel.cn/cn/guide/develop/openai/introduction",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.4.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-5v-turbo-thinking-toggle.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v3",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "reasoning_effort", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming, "reasoning"},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking",
				"https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.3.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-toggle-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v3",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.2.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-toggle-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v2",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Capabilities:         []string{CapabilityStreaming},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
				"https://bigmodel.cn/pricing",
			},
		},
		{
			ID: "glm.5v-turbo.api", Version: "1.1.0", Label: "GLM-5V-Turbo 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.glm-thinking-toggle-documented.v1", CacheUsageProfile: "cache-usage.glm-documented.v1",
			ProtocolProfile:       "protocol.glm-openai-chat.v1",
			ProviderContextWindow: 200_000, ProviderMaxOutput: 131_072,
			DefaultContextWindow: 200_000, DefaultOutputLimit: 65_536,
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "vision", "video", "files", "prompt_cache"},
			Aliases:              []string{"GLM-5V-Turbo", "glm-5v-turbo"},
			EvidenceRefs: []string{
				"https://docs.bigmodel.cn/cn/api/introduction",
				"https://docs.bigmodel.cn/openapi/openapi.json",
				"https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5v-turbo",
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
			ProviderCapabilities: []string{"reasoning", "tools", "tool_choice", "json_object", "vision", "video", "prompt_cache", "builtin_web_search"},
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
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "video", "audio", "files", "prompt_cache", "builtin_web_search", "service_tier"},
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
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "video", "audio", "files", "prompt_cache", "builtin_web_search", "service_tier"},
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
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_object", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"MiMo V2.5 Pro", "mimo-v2.5-pro"},
			EvidenceRefs: []string{
				"https://mimo.mi.com/docs/en-US/quick-start/model",
				"https://mimo.mi.com/docs/en-US/api/chat/openai-api",
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
			ProviderCapabilities: []string{"streaming", "reasoning", "tools", "tool_choice", "json_object", "vision", "audio", "video", "prompt_cache", "builtin_web_search"},
			Aliases:              []string{"MiMo V2.5", "mimo-v2.5"},
			EvidenceRefs: []string{
				"https://mimo.mi.com/docs/en-US/quick-start/model",
				"https://mimo.mi.com/docs/en-US/api/chat/openai-api",
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
			ProtocolProfile: "protocol.openai-responses.documented.v1",
			APISurface:      APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
			DefaultOutputAuthority: DefaultOutputPolicy,
			ProviderContextWindow:  1_050_000, ProviderMaxOutput: 128_000,
			DefaultContextWindow: 1_050_000, DefaultOutputLimit: 32_768,
			ProviderCapabilities: []string{"reasoning", "reasoning_effort", "tools", "tool_choice", "json_schema", "vision", "prompt_cache", "builtin_web_search", "service_tier"},
			Aliases:              []string{"GPT-5.6 Sol", "gpt-5.6-sol"},
			EvidenceRefs:         []string{"https://developers.openai.com/api/docs/models/gpt-5.6-sol"},
		},
		{
			ID: "openai.gpt-5.6-terra.api", Version: "1.0.0", Label: "GPT-5.6 Terra 官方 API",
			TokenizerProfileID: TokenizerByteUpperID, TokenizerProfileVersion: TokenizerByteUpperVersion,
			ReasoningProfile: "reasoning.openai-gpt-5.6-documented.v1", CacheUsageProfile: "cache-usage.openai-documented.v1",
			ProtocolProfile: "protocol.openai-responses.documented.v1",
			APISurface:      APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
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
			ProtocolProfile: "protocol.openai-responses.documented.v1",
			APISurface:      APISurfaceOpenAIResponses, Operations: []string{OperationOpenAIResponses},
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
		manifest, ok := currentProfileManifests[result[index].ID]
		result[index].Current = ok && manifest.Version == result[index].Version
	}
	return result
}

func Lookup(id, version string) (Profile, bool) {
	profile, ok := lookupAddressableProfile(id, version)
	if !ok || ValidateExecutableComponents(profile) != nil {
		return Profile{}, false
	}
	manifest, current := currentProfileManifests[profile.ID]
	profile.Current = current && manifest.Version == profile.Version
	return profile, true
}

// lookupAddressableProfile returns the immutable catalog row without deriving
// execution validity from mutable evidence or capability projections. Narrow
// identity resolvers may use it only when they independently validate every
// identity field they consume; ordinary callers must continue to use Lookup.
func lookupAddressableProfile(id, version string) (Profile, bool) {
	id = strings.TrimSpace(id)
	version = strings.TrimSpace(version)
	for _, profile := range catalog {
		if profile.ID == id && profile.Version == version {
			return clone(profile), true
		}
	}
	return Profile{}, false
}

// ValidateNewAttachment separates immutable historical execution contracts
// from catalog versions that may be selected for a new public model or
// deployment. Advancing the current pointer never makes an existing deployment
// unreadable or unroutable; it only prevents a new attachment from silently
// reviving a superseded provider product identity.
func ValidateNewAttachment(id, version string) error {
	profile, ok := Lookup(id, version)
	if !ok {
		return fmt.Errorf("unknown model execution profile %s@%s", strings.TrimSpace(id), strings.TrimSpace(version))
	}
	if !profile.Current {
		return fmt.Errorf("model execution profile %s@%s is historical and cannot be attached to new product state", profile.ID, profile.Version)
	}
	return nil
}

func Match(modelName string) (Profile, bool) {
	profile, count := matchCurrent(modelName, "")
	return profile, count == 1
}

// MatchForSurface is the reviewed new-attachment resolver. Provider model
// aliases are not globally unique once one upstream model is available through
// multiple API surfaces, so callers with an operation identity must use this
// resolver instead of relying on catalog order.
func MatchForSurface(modelName, apiSurface string) (Profile, bool) {
	apiSurface = strings.TrimSpace(apiSurface)
	if apiSurface == "" {
		return Profile{}, false
	}
	profile, count := matchCurrent(modelName, apiSurface)
	return profile, count == 1
}

// HasAmbiguousCurrentAlias lets legacy create APIs require an explicit Profile
// instead of silently falling back to an unverified contract when an upstream
// model is attachable through more than one API surface.
func HasAmbiguousCurrentAlias(modelName string) bool {
	_, count := matchCurrent(modelName, "")
	return count > 1
}

func matchCurrent(modelName, apiSurface string) (Profile, int) {
	identity := normalizeIdentity(modelName)
	if identity == "" {
		return Profile{}, 0
	}
	var matched Profile
	count := 0
	for _, profile := range catalog {
		if profile.ID == UnverifiedProfileID {
			continue
		}
		manifest, current := currentProfileManifests[profile.ID]
		if !current || manifest.Version != profile.Version {
			continue
		}
		if apiSurface != "" && profile.APISurface != apiSurface {
			continue
		}
		for _, alias := range profile.Aliases {
			if normalizeIdentity(alias) == identity {
				if ValidateExecutableComponents(profile) != nil {
					return Profile{}, 0
				}
				profile.Current = true
				matched = clone(profile)
				count++
				break
			}
		}
	}
	return matched, count
}

// ValidateExecutableComponents prevents provider-advertised metadata from
// becoming executable routing authority without a matching typed component
// contract. It is used by both catalog selection and tests so a bad catalog
// entry fails closed even outside CI.
func ValidateExecutableComponents(profile Profile) error {
	if strings.TrimSpace(profile.Provider) == "" || strings.TrimSpace(profile.Region) == "" {
		return fmt.Errorf("model profile requires provider and region product identity")
	}
	stableIdentity, stable := stableProfileProductIdentities[profile.ID]
	if !stable || profile.Provider != stableIdentity.Provider || profile.Region != stableIdentity.Region ||
		!slices.Equal(profile.EndpointClasses, immutableProfileEndpointClasses(profile.ID, profile.Version)) ||
		!slices.Equal(profile.UpstreamModelIDs, immutableProfileUpstreamModelIDs(profile.ID, profile.Version)) ||
		profile.LegacyProtocolAuthority != isImmutableLegacyProtocolAuthority(profile.ID, profile.Version) {
		return fmt.Errorf("model profile product identity is not the stable identity for %s", profile.ID)
	}
	if profile.ID != UnverifiedProfileID && len(profile.UpstreamModelIDs) == 0 {
		return fmt.Errorf("reviewed model profile requires exact upstream model IDs")
	}
	upstreamIdentity, upstreamIdentityModeled := immutableProfileUpstreamIdentity(profile.ID, profile.Version)
	profileModelsUpstreamIdentity := profile.UpstreamModelFamily != "" || profile.UpstreamVersionPolicy != ""
	if upstreamIdentityModeled != profileModelsUpstreamIdentity ||
		(upstreamIdentityModeled && (profile.UpstreamModelFamily != upstreamIdentity.ModelFamily ||
			profile.UpstreamVersionPolicy != upstreamIdentity.VersionPolicy)) {
		return fmt.Errorf("model profile upstream version identity is not immutable for %s@%s", profile.ID, profile.Version)
	}
	if upstreamIdentityModeled {
		if strings.TrimSpace(profile.UpstreamModelFamily) != profile.UpstreamModelFamily || profile.UpstreamModelFamily == "" ||
			!slices.Contains([]string{UpstreamVersionPinned, UpstreamVersionFloating}, profile.UpstreamVersionPolicy) {
			return fmt.Errorf("model profile has invalid upstream version identity")
		}
		if profile.UpstreamVersionPolicy == UpstreamVersionFloating &&
			(len(profile.UpstreamModelIDs) != 1 || profile.UpstreamModelIDs[0] != profile.UpstreamModelFamily) {
			return fmt.Errorf("floating upstream identity requires one exact requested family alias")
		}
	}
	executionIdentity, executionIdentityOK := immutableProfileExecutionIdentity(profile.ID, profile.Version)
	if !executionIdentityOK || profile.APISurface != executionIdentity.APISurface ||
		!slices.Equal(profile.Operations, executionIdentity.Operations) ||
		profile.DefaultOutputAuthority != executionIdentity.DefaultOutputAuthority {
		return fmt.Errorf("model profile execution identity is not immutable for %s@%s", profile.ID, profile.Version)
	}
	if len(profile.Operations) != 1 {
		return fmt.Errorf("model profile requires exactly one typed operation")
	}
	switch profile.APISurface {
	case APISurfaceOpenAIChat:
		if profile.Operations[0] != OperationOpenAIChat {
			return fmt.Errorf("OpenAI Chat surface requires operation %s", OperationOpenAIChat)
		}
	case APISurfaceOpenAIResponses:
		if profile.Operations[0] != OperationOpenAIResponses {
			return fmt.Errorf("OpenAI Responses surface requires operation %s", OperationOpenAIResponses)
		}
	case APISurfaceAnthropicMessage:
		if profile.Operations[0] != OperationAnthropicMessage {
			return fmt.Errorf("Anthropic Messages surface requires operation %s", OperationAnthropicMessage)
		}
	default:
		return fmt.Errorf("model profile has unsupported API surface %q", profile.APISurface)
	}
	if !slices.Contains([]string{DefaultOutputProvider, DefaultOutputPolicy, DefaultOutputCustomer}, profile.DefaultOutputAuthority) {
		return fmt.Errorf("model profile has unsupported default output authority %q", profile.DefaultOutputAuthority)
	}
	expectedEvidenceState := derivedCatalogEvidenceState(profile)
	if profile.CatalogEvidenceState != expectedEvidenceState {
		return fmt.Errorf("catalog evidence state %q does not match executable profile state %q", profile.CatalogEvidenceState, expectedEvidenceState)
	}
	supported := make(map[string]struct{}, len(profile.Capabilities))
	for _, raw := range profile.Capabilities {
		capability := strings.TrimSpace(raw)
		if capability == "" || capability != raw {
			return fmt.Errorf("model profile has invalid supported capability %q", raw)
		}
		if _, duplicate := supported[capability]; duplicate {
			return fmt.Errorf("model profile has duplicate supported capability %q", capability)
		}
		supported[capability] = struct{}{}
	}
	alwaysRequired := make(map[string]struct{}, len(profile.AlwaysRequiredCapabilities))
	for _, raw := range profile.AlwaysRequiredCapabilities {
		capability := strings.TrimSpace(raw)
		if capability == "" || capability != raw {
			return fmt.Errorf("model profile has invalid always-required capability %q", raw)
		}
		if _, duplicate := alwaysRequired[capability]; duplicate {
			return fmt.Errorf("model profile has duplicate always-required capability %q", capability)
		}
		if _, ok := supported[capability]; !ok {
			return fmt.Errorf("always-required capability %q is not supported by the model profile", capability)
		}
		alwaysRequired[capability] = struct{}{}
	}
	endpointProtocol, frozenEndpointProtocol := immutableProfileEndpointProtocolIdentity(profile.ID, profile.Version)
	if frozenEndpointProtocol != (profile.ProtocolProfileVersion != "") ||
		(frozenEndpointProtocol && (profile.ProtocolProfile != endpointProtocol.ProfileID ||
			profile.ProtocolProfileVersion != endpointProtocol.ProfileVersion)) {
		return fmt.Errorf("model profile endpoint protocol identity is not immutable for %s@%s", profile.ID, profile.Version)
	}
	if frozenEndpointProtocol {
		if _, err := endpointprotocol.NewRegistry().Resolve(endpointProtocol.ProfileID, endpointProtocol.ProfileVersion); err != nil {
			return fmt.Errorf("model profile endpoint protocol address is unavailable for %s@%s", profile.ID, profile.Version)
		}
	}
	operationPolicy, frozenOperationPolicy := immutableProfileOperationPolicyIdentities[profile.ID+"@"+profile.Version]
	if frozenOperationPolicy != (profile.OperationPolicyProfile != "") ||
		(frozenOperationPolicy && profile.OperationPolicyProfile != operationPolicy) {
		return fmt.Errorf("model profile operation policy identity is not immutable for %s@%s", profile.ID, profile.Version)
	}
	if profile.OperationPolicyProfile != "" {
		if profile.APISurface != APISurfaceAnthropicMessage {
			return fmt.Errorf("Anthropic Messages operation policy requires the Anthropic Messages API surface")
		}
		contract, ok := ResolveAnthropicMessagesOperationContract(profile.OperationPolicyProfile)
		if !ok || contract.ProfileID != profile.OperationPolicyProfile || !contract.RequireFinalUserMessage ||
			len(contract.AllowedResponseStops) == 0 {
			return fmt.Errorf("Anthropic Messages operation policy is unavailable for %s@%s", profile.ID, profile.Version)
		}
	}
	if profile.ExecutableContextWindow < 0 ||
		(profile.ExecutableContextWindow > 0 && profile.ProviderContextWindow > 0 && profile.ExecutableContextWindow > profile.ProviderContextWindow) ||
		(profile.ExecutableContextWindow > 0 && profile.DefaultContextWindow > profile.ExecutableContextWindow) {
		return fmt.Errorf("executable context window must be a positive narrowing of provider capability and contain the default")
	}
	if profile.ExecutableMaxOutput < 0 ||
		(profile.ExecutableMaxOutput > 0 && profile.ProviderMaxOutput > 0 && profile.ExecutableMaxOutput > profile.ProviderMaxOutput) ||
		(profile.ExecutableMaxOutput > 0 && profile.DefaultOutputLimit > profile.ExecutableMaxOutput) {
		return fmt.Errorf("executable max output must be a positive narrowing of provider capability and contain the request default")
	}
	if contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile); ok {
		if err := validateChatProtocolContractShape(contract); err != nil {
			return fmt.Errorf("model protocol profile %s is invalid: %w", profile.ProtocolProfile, err)
		}
	}
	if !profile.LegacyProtocolAuthority && slices.Contains(profile.Capabilities, CapabilityStreaming) {
		modeled := false
		if profile.APISurface == APISurfaceOpenAIResponses &&
			profile.ProtocolProfileVersion != "" {
			if contract, err := endpointprotocol.NewRegistry().Resolve(
				profile.ProtocolProfile, profile.ProtocolProfileVersion,
			); err == nil {
				descriptor := contract.Descriptor()
				modeled = descriptor.EndpointKind == endpointprotocol.EndpointResponses &&
					!slices.Contains(descriptor.UnsupportedSemantics, "streaming")
			}
		} else if profile.APISurface == APISurfaceAnthropicMessage &&
			profile.ProtocolProfileVersion != "" {
			if contract, err := endpointprotocol.NewRegistry().Resolve(
				profile.ProtocolProfile, profile.ProtocolProfileVersion,
			); err == nil {
				descriptor := contract.Descriptor()
				modeled = descriptor.EndpointKind == endpointprotocol.EndpointMessages &&
					!slices.Contains(descriptor.UnsupportedSemantics, "streaming")
			}
		} else if contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile); ok {
			modeled = contract.ProfileID == profile.ProtocolProfile && contract.StreamingModeled && contract.Streaming
		}
		if !modeled {
			return fmt.Errorf("streaming capability requires a modeled streaming protocol profile")
		}
	}
	if !profile.LegacyProtocolAuthority && slices.Contains(profile.Capabilities, CapabilityTools) {
		modeled := false
		if profile.APISurface == APISurfaceAnthropicMessage {
			operation, ok := ResolveAnthropicMessagesOperationContract(profile.OperationPolicyProfile)
			modeled = ok && operation.ProfileID == profile.OperationPolicyProfile && operation.FunctionTools != nil &&
				operation.FunctionTools.MaxTools > 0 && operation.FunctionTools.ExactHistory && operation.FunctionTools.ExactResponse
		} else if contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile); ok {
			modeled = contract.ProfileID == profile.ProtocolProfile && contract.FunctionToolsModeled &&
				contract.FunctionTools && contract.MaxFunctionTools > 0
		}
		if !modeled {
			return fmt.Errorf("tools capability requires a modeled function-tools protocol contract")
		}
	}
	granularToolChecks := map[string]func(ChatProtocolContract) bool{
		CapabilityFunctionTools: func(contract ChatProtocolContract) bool {
			return contract.FunctionToolsModeled && contract.FunctionTools
		},
		CapabilityToolChoiceAuto: func(contract ChatProtocolContract) bool {
			return slices.Contains(contract.FunctionToolChoices, "auto") || contract.FunctionToolProviderDefaultChoice == "auto"
		},
		CapabilityToolChoiceNamed: func(contract ChatProtocolContract) bool { return contract.NamedFunctionToolChoice },
		CapabilityToolChoiceReq: func(contract ChatProtocolContract) bool {
			return slices.Contains(contract.FunctionToolChoices, "required") || contract.FunctionToolProviderDefaultChoice == "required"
		},
		CapabilityParallelTools: func(contract ChatProtocolContract) bool { return contract.ParallelFunctionToolCalls },
		CapabilityToolStream: func(contract ChatProtocolContract) bool {
			return contract.ToolStream && contract.ToolStreamField != ""
		},
	}
	for capability, supportedBy := range granularToolChecks {
		if profile.LegacyProtocolAuthority || !slices.Contains(profile.Capabilities, capability) {
			continue
		}
		modeled := false
		if profile.APISurface == APISurfaceAnthropicMessage {
			operation, ok := ResolveAnthropicMessagesOperationContract(profile.OperationPolicyProfile)
			if ok && operation.ProfileID == profile.OperationPolicyProfile && operation.FunctionTools != nil {
				tools := operation.FunctionTools
				switch capability {
				case CapabilityFunctionTools:
					modeled = true
				case CapabilityToolChoiceAuto:
					modeled = slices.Contains(tools.ToolChoices, "auto") || tools.ProviderDefaultChoice == "auto"
				case CapabilityToolChoiceNamed:
					modeled = tools.NamedChoice && slices.Contains(tools.ToolChoices, "tool")
				case CapabilityToolChoiceReq:
					modeled = slices.Contains(tools.ToolChoices, "any")
				case CapabilityParallelTools:
					modeled = tools.ParallelToolUse
				}
			}
		} else if contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile); ok {
			modeled = contract.ProfileID == profile.ProtocolProfile && supportedBy(contract)
		}
		if !modeled {
			return fmt.Errorf("capability %s is not modeled by function-tools protocol profile", capability)
		}
	}
	for capability, responseType := range map[string]string{
		CapabilityJSONOutput:       "json_object",
		CapabilityJSONObject:       "json_object",
		CapabilityJSONSchema:       "json_schema",
		CapabilityStrictJSONSchema: "json_schema",
	} {
		if profile.LegacyProtocolAuthority || !slices.Contains(profile.Capabilities, capability) {
			continue
		}
		contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
		if !ok || contract.ProfileID != profile.ProtocolProfile || !contract.ResponseFormatModeled || !slices.Contains(contract.ResponseFormatTypes, responseType) {
			return fmt.Errorf("capability %s requires a modeled %s response-format contract", capability, responseType)
		}
	}
	if slices.Contains(profile.Capabilities, CapabilityReasoning) ||
		slices.Contains(profile.Capabilities, CapabilityReasoningEffort) ||
		slices.Contains(profile.Capabilities, CapabilityReasoningBudget) {
		contract, ok := ResolveReasoningContract(profile.ReasoningProfile)
		if !ok || contract.ProfileID != profile.ReasoningProfile {
			return fmt.Errorf("reasoning capability requires a modeled reasoning profile")
		}
		if contract.PreserveAssistantReasoningForToolRequests {
			protocol, protocolOK := ResolveChatProtocolContract(profile.ProtocolProfile)
			if !contract.PreserveAssistantReasoningForTools || !protocolOK || !protocol.ExactFunctionToolHistoryModeled {
				return fmt.Errorf("tool-request reasoning replay requires exact function-tool history and tool-call reasoning replay")
			}
		}
	}
	if slices.Contains(profile.Capabilities, CapabilityReasoningEffort) {
		contract, _ := ResolveReasoningContract(profile.ReasoningProfile)
		if strings.TrimSpace(contract.EffortControl.Field) == "" || len(contract.EffortControl.AllowedValues) == 0 ||
			((profile.APISurface == APISurfaceOpenAIResponses || profile.APISurface == APISurfaceAnthropicMessage) &&
				strings.TrimSpace(contract.EffortRequestObjectField()) == "") {
			return fmt.Errorf("reasoning_effort capability requires a typed effort contract for its API surface")
		}
		if len(contract.EffortValuesWhenDisabled) > 0 {
			if len(contract.ToggleControl.DisabledValues) == 0 {
				return fmt.Errorf("disabled reasoning effort constraint requires a disabled toggle value")
			}
			for _, value := range contract.EffortValuesWhenDisabled {
				if !slices.Contains(contract.EffortControl.AllowedValues, value) {
					return fmt.Errorf("disabled reasoning effort constraint contains unsupported value %s", value)
				}
			}
		}
	}
	if slices.Contains(profile.Capabilities, CapabilityReasoningBudget) {
		contract, _ := ResolveReasoningContract(profile.ReasoningProfile)
		if strings.TrimSpace(contract.BudgetControl.Field) == "" || contract.BudgetControl.Minimum < 0 ||
			contract.BudgetControl.Maximum < contract.BudgetControl.Minimum ||
			(contract.BudgetControl.ProviderDefault != 0 &&
				(contract.BudgetControl.ProviderDefault < contract.BudgetControl.Minimum ||
					contract.BudgetControl.ProviderDefault > contract.BudgetControl.Maximum)) ||
			(profile.APISurface == APISurfaceAnthropicMessage && strings.TrimSpace(contract.ToggleRequestObjectField()) == "") {
			return fmt.Errorf("reasoning_budget capability requires a typed budget contract for its API surface")
		}
	}
	if slices.Contains(profile.Capabilities, "prompt_cache") {
		if contract, ok := ResolveCacheUsageContract(profile.CacheUsageProfile); !ok || contract.ProfileID != profile.CacheUsageProfile {
			return fmt.Errorf("prompt_cache capability requires a modeled cache-usage profile")
		} else if err := validateCacheUsageContractShape(contract); err != nil {
			return fmt.Errorf("cache-usage profile %s is invalid: %w", profile.CacheUsageProfile, err)
		}
	}
	if slices.Contains(profile.Capabilities, CapabilityExplicitCache) {
		contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
		if !ok || !contract.PromptCacheControlModeled || !contract.ExplicitPromptCache ||
			(contract.PromptCacheControlType == "" && contract.PromptCacheBreakpointField == "") ||
			contract.MaxPromptCacheMarkers <= 0 {
			return fmt.Errorf("explicit_prompt_cache capability requires a modeled request cache-control contract")
		}
	}
	if slices.Contains(profile.Capabilities, CapabilityBuiltinSearchReq) {
		contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
		if !ok || !contract.SearchRequestModeled || !contract.BuiltinSearchRequest ||
			len(contract.SearchStrategies) == 0 || contract.SearchDefaultStrategy == "" {
			return fmt.Errorf("builtin_web_search_request capability requires a modeled request contract")
		}
	}
	if slices.Contains(profile.Capabilities, CapabilityServiceTier) {
		contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
		if !ok || contract.ProcessingTierPolicyID == "" || contract.ProcessingTierPolicyID != profile.ProcessingTierPolicy {
			return fmt.Errorf("service_tier capability requires an exact processing-tier policy reference")
		}
		policy, ok := ResolveProcessingTierPolicy(profile.ProcessingTierPolicy)
		if !ok || policy.Provider != profile.Provider || policy.APISurface != profile.APISurface ||
			policy.RequestField != "service_tier" || policy.OmittedNormalizedTier == "" || len(policy.RequestValues) == 0 {
			return fmt.Errorf("service_tier capability requires a provider/surface processing-tier policy")
		}
		if err := validateProcessingTierPolicyShape(policy); err != nil {
			return fmt.Errorf("processing-tier policy %s is invalid: %w", policy.ID, err)
		}
	} else if profile.ProcessingTierPolicy != "" {
		return fmt.Errorf("processing-tier policy requires executable service_tier capability")
	}
	for capability, reports := range map[string]func(CacheUsageContract) bool{
		CapabilityCacheReadUsage:   func(contract CacheUsageContract) bool { return contract.ReportsRead },
		CapabilityCacheCreateUsage: func(contract CacheUsageContract) bool { return contract.ReportsCreation },
	} {
		if !slices.Contains(profile.Capabilities, capability) {
			continue
		}
		contract, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
		if !ok || !reports(contract) {
			return fmt.Errorf("capability %s requires an exact cache-usage wire contract", capability)
		}
	}
	advancedChat := false
	for _, capability := range []string{
		CapabilityPartialMode, CapabilityDynamicTools, CapabilityBuiltinWebSearch,
		CapabilityVision, CapabilityAudio, CapabilityVideo, CapabilityFiles,
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
			CapabilityVision:           contract.InlineImageDataURL || contract.RemoteImageHTTPSURL,
			CapabilityAudio:            contract.InlineAudioBase64 || contract.RemoteAudioHTTPSURL,
			CapabilityVideo:            contract.InlineVideoDataURL || contract.RemoteVideoHTTPSURL,
			CapabilityFiles:            contract.RemoteFileHTTPSURL,
		}
		for capability, supported := range checks {
			if slices.Contains(profile.Capabilities, capability) && !supported {
				return fmt.Errorf("capability %s is not modeled by protocol profile", capability)
			}
		}
	}
	return nil
}

func validateCacheUsageContractShape(contract CacheUsageContract) error {
	if !contract.RequireReadCounter {
		return nil
	}
	if !contract.ReportsRead {
		return fmt.Errorf("required cache-read counter must be reported")
	}
	switch contract.WireSchema {
	case CacheUsageWireOpenAICachedTokensV1:
		return nil
	default:
		return fmt.Errorf("required cache-read counter has no enforced wire schema")
	}
}

// ValidateEffectiveLimits ensures customer policy narrows provider capability.
// The unverified profile has no provider maxima but still requires positive
// effective limits before it can be used by canonical execution.
func ValidateEffectiveLimits(profile Profile, contextWindow, outputLimit int) error {
	if contextWindow <= 0 || outputLimit <= 0 {
		return fmt.Errorf("model execution limits must be positive")
	}
	contextMaximum := executableContextMaximum(profile)
	if contextMaximum > 0 && contextWindow > contextMaximum {
		return fmt.Errorf("context window %d exceeds executable profile maximum %d", contextWindow, contextMaximum)
	}
	outputMaximum := executableOutputMaximum(profile)
	if outputMaximum > 0 && outputLimit > outputMaximum {
		return fmt.Errorf("output limit %d exceeds executable profile maximum %d", outputLimit, outputMaximum)
	}
	if outputLimit > contextWindow {
		return fmt.Errorf("output limit %d exceeds effective context window %d", outputLimit, contextWindow)
	}
	return nil
}

// DefaultPublicContextLimit and DefaultPublicOutputLimit are the sole backend
// authority for omitted reviewed-model limits in supported API/CLI creation.
// Public limits are executable hard ceilings, not per-request defaults. The
// latter remain DefaultContextWindow and DefaultOutputLimit and are injected
// only when a request omits its own limit.
func DefaultPublicContextLimit(profile Profile) int {
	if maximum := executableContextMaximum(profile); maximum > 0 {
		return maximum
	}
	return profile.DefaultContextWindow
}

func DefaultPublicOutputLimit(profile Profile) int {
	if maximum := executableOutputMaximum(profile); maximum > 0 {
		return maximum
	}
	return profile.DefaultOutputLimit
}

// DefaultRequestOutputLimit keeps an omitted per-request limit distinct from
// the public hard ceiling while honoring an explicit customer narrowing.
func DefaultRequestOutputLimit(profile Profile, publicOutputLimit int) int {
	requestDefault := profile.DefaultOutputLimit
	if publicOutputLimit > 0 && requestDefault > publicOutputLimit {
		return publicOutputLimit
	}
	return requestDefault
}

func executableContextMaximum(profile Profile) int {
	maximum := profile.ProviderContextWindow
	if profile.ExecutableContextWindow > 0 && (maximum == 0 || profile.ExecutableContextWindow < maximum) {
		maximum = profile.ExecutableContextWindow
	}
	return maximum
}

func executableOutputMaximum(profile Profile) int {
	maximum := profile.ProviderMaxOutput
	if profile.ExecutableMaxOutput > 0 && (maximum == 0 || profile.ExecutableMaxOutput < maximum) {
		maximum = profile.ExecutableMaxOutput
	}
	return maximum
}

func clone(profile Profile) Profile {
	// Keep the public catalog contract stable: empty collections are encoded as
	// [] rather than null. Besides being easier for typed clients to consume,
	// this prevents the deliberately sparse unverified profile from changing
	// the runtime shape of the response.
	profile.ProviderCapabilities = append([]string{}, profile.ProviderCapabilities...)
	profile.Capabilities = append([]string{}, profile.Capabilities...)
	profile.AlwaysRequiredCapabilities = append([]string{}, profile.AlwaysRequiredCapabilities...)
	profile.Operations = append([]string{}, profile.Operations...)
	profile.EndpointClasses = append([]string{}, profile.EndpointClasses...)
	profile.UpstreamModelIDs = append([]string{}, profile.UpstreamModelIDs...)
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
