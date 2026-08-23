package modelprofile

import (
	"slices"
	"strings"
)

const (
	CapabilityPartialMode      = "partial_mode"
	CapabilityDynamicTools     = "dynamic_tools"
	CapabilityBuiltinWebSearch = "builtin_web_search"
	CapabilityVision           = "vision"
	CapabilityVideo            = "video"
)

// ChatProtocolContract describes advanced Chat Completions shapes whose wire
// compatibility alone is insufficient. The gateway still owns admission,
// guardrails, and billing; this profile says which exact shapes may be used by
// an immutable model execution profile.
type ChatProtocolContract struct {
	ProfileID string

	PartialMode          bool
	DynamicSystemTools   bool
	BuiltinWebSearch     bool
	BuiltinWebSearchName string
	BuiltinSearchMeter   string

	// Builtin search v1 is intentionally bounded to one non-stream call per
	// provider response. This gives Freeze a finite liability ceiling and lets
	// the non-stream response parser produce authoritative usage evidence.
	BuiltinSearchRequiresNonStream        bool
	BuiltinSearchRequiresSerial           bool
	BuiltinSearchRequiresThinkingDisabled bool
	MaxBuiltinSearchCalls                 int64

	InlineImageDataURL   bool
	InlineVideoDataURL   bool
	InlineImageMIMETypes []string
	InlineVideoMIMETypes []string
}

var kimiImageMIMETypes = []string{
	"image/png", "image/jpeg", "image/webp", "image/gif",
}

var kimiVideoMIMETypes = []string{
	"video/mp4", "video/mpeg", "video/quicktime", "video/x-msvideo",
	"video/avi", "video/x-flv", "video/webm", "video/x-ms-wmv", "video/3gpp",
}

var chatProtocolContracts = map[string]ChatProtocolContract{
	"protocol.kimi-openai-chat.v2": {
		ProfileID:        "protocol.kimi-openai-chat.v2",
		PartialMode:      true,
		BuiltinWebSearch: true, BuiltinWebSearchName: "$web_search",
		BuiltinSearchMeter:             "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		BuiltinSearchRequiresThinkingDisabled: true,
		MaxBuiltinSearchCalls:                 1,
		InlineImageDataURL:                    true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	},
	"protocol.kimi-openai-chat.k3.v2": {
		ProfileID:   "protocol.kimi-openai-chat.k3.v2",
		PartialMode: true, DynamicSystemTools: true,
		BuiltinWebSearch: true, BuiltinWebSearchName: "$web_search",
		BuiltinSearchMeter:             "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		MaxBuiltinSearchCalls: 1,
		InlineImageDataURL:    true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	},
}

func ResolveChatProtocolContract(profileID string) (ChatProtocolContract, bool) {
	contract, ok := chatProtocolContracts[strings.TrimSpace(profileID)]
	contract.InlineImageMIMETypes = slices.Clone(contract.InlineImageMIMETypes)
	contract.InlineVideoMIMETypes = slices.Clone(contract.InlineVideoMIMETypes)
	return contract, ok
}
