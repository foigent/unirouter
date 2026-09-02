package modelprofile

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// ProductBinding is the effective, server-observed connection identity bound
// to one exact model execution profile. Provider and endpoint values supplied
// by a client are never themselves evidence of product identity.
type ProductBinding struct {
	Provider               string
	BaseURL                string
	AdapterType            string
	EndpointPathMode       string
	OperationID            string
	ProtocolProfileID      string
	ProtocolProfileVersion string
	UpstreamModel          string
}

// ValidateProductBinding keeps the immutable profile identity authoritative at
// both administration and runtime boundaries. The unverified profile remains
// the explicit escape hatch for custom providers and protocol experiments.
func ValidateProductBinding(profile Profile, binding ProductBinding) error {
	if profile.ID == UnverifiedProfileID {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(binding.Provider), profile.Provider) {
		return fmt.Errorf("provider %q does not match profile provider %q", binding.Provider, profile.Provider)
	}
	if !slices.Contains(profile.Operations, strings.TrimSpace(binding.OperationID)) {
		return fmt.Errorf("operation %q is not part of profile %s", binding.OperationID, profile.ID)
	}
	if profile.ProtocolProfileVersion != "" &&
		(strings.TrimSpace(binding.ProtocolProfileID) != profile.ProtocolProfile ||
			strings.TrimSpace(binding.ProtocolProfileVersion) != profile.ProtocolProfileVersion) {
		return fmt.Errorf("endpoint protocol %q@%q does not match profile %s contract %q@%q",
			binding.ProtocolProfileID, binding.ProtocolProfileVersion, profile.ID,
			profile.ProtocolProfile, profile.ProtocolProfileVersion)
	}
	if !slices.Contains(profile.UpstreamModelIDs, strings.TrimSpace(binding.UpstreamModel)) {
		return fmt.Errorf("upstream model %q is not part of profile %s", binding.UpstreamModel, profile.ID)
	}
	expectedAdapter := ""
	switch profile.APISurface {
	case APISurfaceOpenAIChat, APISurfaceOpenAIResponses:
		expectedAdapter = "openai_compatible"
	case APISurfaceAnthropicMessage:
		expectedAdapter = "anthropic_native"
	}
	if expectedAdapter == "" || strings.TrimSpace(binding.AdapterType) != expectedAdapter {
		return fmt.Errorf("adapter %q does not match profile API surface %q", binding.AdapterType, profile.APISurface)
	}
	endpointClass := EndpointClassProviderHTTPS
	var err error
	if !slices.Contains(profile.EndpointClasses, EndpointClassProviderHTTPS) {
		endpointClass, err = ClassifyEndpoint(profile.Provider, binding.BaseURL)
	} else {
		err = validateReviewedHTTPSURL(binding.BaseURL)
	}
	if err != nil {
		return err
	}
	if !slices.Contains(profile.EndpointClasses, endpointClass) {
		return fmt.Errorf("endpoint class %q is not allowed by profile %s", endpointClass, profile.ID)
	}
	if requiredMode := requiredEndpointPathMode(endpointClass); requiredMode != "" && binding.EndpointPathMode != requiredMode {
		return fmt.Errorf("endpoint class %q requires path mode %q", endpointClass, requiredMode)
	}
	return nil
}

func requiredEndpointPathMode(endpointClass string) string {
	switch endpointClass {
	case EndpointClassGeminiOpenAICompatibility, EndpointClassDeepSeekOfficialOpenAI,
		EndpointClassGLMOfficialOpenAIV4,
		EndpointClassDoubaoArkBeijingOpenAIV3:
		return EndpointPathModeBaseRelative
	case EndpointClassOpenAIOfficialOrigin, EndpointClassOpenAIOfficialV1,
		EndpointClassKimiPlatformOpenAIV1, EndpointClassMiniMaxCNOpenAIV1,
		EndpointClassMiniMaxOfficialCNOpenAIV1,
		EndpointClassXAIOfficialOpenAIV1, EndpointClassAnthropicOfficialOrigin,
		EndpointClassDashScopeBeijingWorkspacePayGo, EndpointClassDashScopeBeijingSharedPayGo:
		return EndpointPathModeCanonicalV1
	default:
		return ""
	}
}

// RequiredEndpointPathMode reports whether a profile version has frozen URL
// path composition as part of its exact endpoint identity. Generic historical
// and unverified profiles deliberately return an empty requirement.
func RequiredEndpointPathMode(profile Profile) string {
	required := ""
	for _, endpointClass := range profile.EndpointClasses {
		mode := requiredEndpointPathMode(endpointClass)
		if mode == "" {
			continue
		}
		if required == "" {
			required = mode
			continue
		}
		if required != mode {
			return ""
		}
	}
	return required
}

func validateReviewedHTTPSURL(rawBaseURL string) error {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if strings.ContainsAny(rawBaseURL, "?#") {
		return fmt.Errorf("reviewed provider endpoint must not contain query or fragment delimiters")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("reviewed provider endpoint must be an HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

// ClassifyEndpoint derives an endpoint class from the effective URL. It never
// trusts a caller-provided region or plan label. Providers without a reviewed
// host taxonomy are deliberately classified only as provider_https; their
// exact-host hardening can advance independently without changing this API.
func ClassifyEndpoint(provider, rawBaseURL string) (string, error) {
	if err := validateReviewedHTTPSURL(rawBaseURL); err != nil {
		return "", err
	}
	parsed, _ := url.Parse(strings.TrimSpace(rawBaseURL))
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "openai" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.openai.com") {
			switch strings.TrimSuffix(parsed.EscapedPath(), "/") {
			case "":
				return EndpointClassOpenAIOfficialOrigin, nil
			case "/v1":
				return EndpointClassOpenAIOfficialV1, nil
			}
		}
		return "", fmt.Errorf("OpenAI endpoint must be an exact official API origin or /v1 root")
	}
	if provider == "gemini" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "generativelanguage.googleapis.com") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/v1beta/openai" {
			return EndpointClassGeminiOpenAICompatibility, nil
		}
		return "", fmt.Errorf("Gemini endpoint must be the official OpenAI compatibility API root")
	}
	if provider == "deepseek" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.deepseek.com") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "" {
			return EndpointClassDeepSeekOfficialOpenAI, nil
		}
		return "", fmt.Errorf("DeepSeek endpoint must be the official OpenAI-format API root")
	}
	if provider == "kimi" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.moonshot.cn") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/v1" {
			return EndpointClassKimiPlatformOpenAIV1, nil
		}
		return "", fmt.Errorf("Kimi endpoint must be the official pay-as-you-go platform OpenAI API root")
	}
	if provider == "minimax" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.minimaxi.com") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/v1" {
			return EndpointClassMiniMaxCNOpenAIV1, nil
		}
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.minimax.cn") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/v1" {
			return EndpointClassMiniMaxOfficialCNOpenAIV1, nil
		}
		return "", fmt.Errorf("MiniMax endpoint must be an exact reviewed China OpenAI API root")
	}
	if provider == "glm" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "open.bigmodel.cn") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/api/paas/v4" {
			return EndpointClassGLMOfficialOpenAIV4, nil
		}
		return "", fmt.Errorf("GLM endpoint must be the official open platform OpenAI API root")
	}
	if provider == "xai" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.x.ai") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/v1" {
			return EndpointClassXAIOfficialOpenAIV1, nil
		}
		return "", fmt.Errorf("xAI endpoint must be the exact official OpenAI API /v1 root")
	}
	if provider == "anthropic" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "api.anthropic.com") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "" {
			return EndpointClassAnthropicOfficialOrigin, nil
		}
		return "", fmt.Errorf("Anthropic endpoint must be the exact official API origin")
	}
	if provider == "doubao" {
		if parsed.Port() == "" && strings.EqualFold(parsed.Hostname(), "ark.cn-beijing.volces.com") &&
			strings.TrimSuffix(parsed.EscapedPath(), "/") == "/api/v3" {
			return EndpointClassDoubaoArkBeijingOpenAIV3, nil
		}
		return "", fmt.Errorf("Doubao endpoint must be the exact Ark Beijing OpenAI API /api/v3 root")
	}
	if provider != "dashscope" {
		return EndpointClassProviderHTTPS, nil
	}
	if parsed.Port() != "" {
		return "", fmt.Errorf("DashScope endpoint must not specify a port")
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if host == "dashscope.aliyuncs.com" && path == "/compatible-mode/v1" {
		return EndpointClassDashScopeBeijingSharedPayGo, nil
	}
	const workspaceSuffix = ".cn-beijing.maas.aliyuncs.com"
	workspaceID := strings.TrimSuffix(host, workspaceSuffix)
	if workspaceID != host && workspaceID != "" && !strings.Contains(workspaceID, ".") &&
		workspaceID != "trial" && workspaceID != "token-plan" && path == "/compatible-mode/v1" {
		return EndpointClassDashScopeBeijingWorkspacePayGo, nil
	}
	return "", fmt.Errorf("DashScope endpoint is not a Beijing pay-as-you-go workspace or legacy shared OpenAI endpoint")
}
