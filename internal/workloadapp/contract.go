// Package workloadapp defines UniRouter's product-local application identity
// and client compatibility contract. Applications never grant Tool or MCP
// authority and cannot widen an API key, project or user permission.
package workloadapp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	ContractVersion         = "unirouter.workload-application.v2"
	LegacyContractVersion   = "unirouter.workload-application.v1"
	IdentityContractVersion = "unirouter.workload-identity.v2"
	LegacyIdentityVersion   = "unirouter.workload-identity.v1"
)

const (
	KindService            = "service"
	KindAgent              = "agent"
	KindWorkflow           = "workflow"
	KindInteractiveClient  = "interactive_client"
	KindIntegrationAdapter = "integration_adapter"

	IdentityServiceCredential   = "service_credential"
	IdentityPersonalDevelopment = "personal_development"

	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusArchived = "archived"

	RuntimeSupported       = "runtime_supported"
	RuntimeProtocolOnly    = "protocol_compatible"
	RuntimeContractOnly    = "contract_only"
	RuntimeAdapterRequired = "adapter_required"

	EvidenceVerified           = "verified"
	EvidenceProtocolCompatible = "protocol_compatible"
	EvidenceUnsupported        = "unsupported"
)

var (
	ErrInvalid = errors.New("workloadapp: invalid application")

	applicationRefPattern = regexp.MustCompile(`^app_[a-z0-9][a-z0-9._-]{2,59}$`)
	environmentPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

type Draft struct {
	ApplicationRef string `json:"application_ref"`
	DisplayName    string `json:"display_name"`
	Kind           string `json:"kind"`
	Environment    string `json:"environment"`
	IdentityMode   string `json:"identity_mode"`
	ClientProfile  string `json:"client_profile"`
	OwnerTeam      string `json:"owner_team"`
	Description    string `json:"description"`
	Status         string `json:"status"`
}

type V2Draft struct {
	ApplicationRef       string   `json:"application_ref"`
	LogicalRef           string   `json:"logical_ref"`
	DisplayName          string   `json:"display_name"`
	Kind                 string   `json:"kind"`
	Environment          string   `json:"environment"`
	ProductionUse        bool     `json:"production_use"`
	ClientProfiles       []string `json:"client_profiles"`
	PrimaryClientProfile string   `json:"primary_client_profile,omitempty"`
	OwnerTeam            string   `json:"owner_team"`
	Description          string   `json:"description"`
	Status               string   `json:"status"`
}

type ClientProfile struct {
	ProfileID          string   `json:"profile_id"`
	DisplayName        string   `json:"display_name"`
	ClientFamily       string   `json:"client_family"`
	APIStyle           string   `json:"api_style"`
	SupportedEndpoints []string `json:"supported_endpoints"`
	RuntimeState       string   `json:"runtime_state"`
	EvidenceLevel      string   `json:"evidence_level"`
	RecommendedForPOC  bool     `json:"recommended_for_poc"`
	ConfigurationNote  string   `json:"configuration_note"`
}

var profiles = []ClientProfile{
	{
		ProfileID: "client.openai-sdk.chat.v1", DisplayName: "OpenAI SDK / Chat Completions",
		ClientFamily: "openai_sdk", APIStyle: "openai_chat_completions",
		SupportedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
		RuntimeState:       RuntimeSupported, EvidenceLevel: EvidenceVerified, RecommendedForPOC: true,
		ConfigurationNote: "Set api_key and base_url to the selected UniRouter Gateway Endpoint.",
	},
	{
		ProfileID: "client.openai-agents.chat.v1", DisplayName: "OpenAI Agents SDK / Chat Completions model",
		ClientFamily: "openai_agents_sdk", APIStyle: "openai_chat_completions",
		SupportedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
		RuntimeState:       RuntimeProtocolOnly, EvidenceLevel: EvidenceProtocolCompatible, RecommendedForPOC: true,
		ConfigurationNote: "Use the SDK Chat Completions model adapter; the default Responses path is not a runtime claim.",
	},
	{
		ProfileID: "client.openai-agents.responses.v1", DisplayName: "OpenAI Agents SDK / Responses",
		ClientFamily: "openai_agents_sdk", APIStyle: "openai_responses",
		SupportedEndpoints: []string{"/v1/responses"},
		RuntimeState:       RuntimeContractOnly, EvidenceLevel: EvidenceUnsupported,
		ConfigurationNote: "Endpoint Profile exists as a contract only; runtime use is blocked until independently implemented and verified.",
	},
	{
		ProfileID: "client.langchain-openai.chat.v1", DisplayName: "LangChain ChatOpenAI",
		ClientFamily: "langchain", APIStyle: "openai_chat_completions",
		SupportedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
		RuntimeState:       RuntimeProtocolOnly, EvidenceLevel: EvidenceProtocolCompatible, RecommendedForPOC: true,
		ConfigurationNote: "Configure base_url, api_key and an explicitly approved model alias.",
	},
	{
		ProfileID: "client.autogen-openai.chat.v1", DisplayName: "AutoGen OpenAI-compatible client",
		ClientFamily: "autogen", APIStyle: "openai_chat_completions",
		SupportedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
		RuntimeState:       RuntimeProtocolOnly, EvidenceLevel: EvidenceProtocolCompatible, RecommendedForPOC: true,
		ConfigurationNote: "Use an OpenAI-compatible model client with the UniRouter Base URL.",
	},
	{
		ProfileID: "client.openclaw-openai.chat.v1", DisplayName: "OpenClaw OpenAI-compatible provider",
		ClientFamily: "openclaw", APIStyle: "openai_chat_completions",
		SupportedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
		RuntimeState:       RuntimeProtocolOnly, EvidenceLevel: EvidenceProtocolCompatible, RecommendedForPOC: true,
		ConfigurationNote: "Register UniRouter as an OpenAI-compatible provider and keep secrets outside exported profiles.",
	},
	{
		ProfileID: "client.tencent-workbuddy.chat.v1", DisplayName: "Tencent WorkBuddy custom model",
		ClientFamily: "tencent_workbuddy", APIStyle: "openai_chat_completions",
		SupportedEndpoints: []string{"/v1/chat/completions", "/v1/models"},
		RuntimeState:       RuntimeProtocolOnly, EvidenceLevel: EvidenceProtocolCompatible, RecommendedForPOC: true,
		ConfigurationNote: "Use the custom OpenAI-compatible model entry and Chat Completions path.",
	},
	{
		ProfileID: "client.feishu-bot.adapter.v1", DisplayName: "Feishu enterprise bot adapter",
		ClientFamily: "feishu_bot", APIStyle: "isolated_adapter",
		SupportedEndpoints: []string{"/v1/chat/completions"},
		RuntimeState:       RuntimeAdapterRequired, EvidenceLevel: EvidenceUnsupported,
		ConfigurationNote: "Requires a separately deployed event adapter with idempotent acknowledgement; not part of the Gateway process.",
	},
	{
		ProfileID: "client.feishu-aily.adapter.v1", DisplayName: "Feishu Aily adapter",
		ClientFamily: "feishu_aily", APIStyle: "isolated_adapter",
		SupportedEndpoints: []string{"/v1/chat/completions"},
		RuntimeState:       RuntimeAdapterRequired, EvidenceLevel: EvidenceUnsupported,
		ConfigurationNote: "Requires an exact customer Aily environment and separately verified adapter contract.",
	},
}

func NormalizeDraft(input Draft) (Draft, error) {
	input.ApplicationRef = strings.ToLower(strings.TrimSpace(input.ApplicationRef))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Environment = strings.ToLower(strings.TrimSpace(input.Environment))
	input.IdentityMode = strings.ToLower(strings.TrimSpace(input.IdentityMode))
	input.ClientProfile = strings.ToLower(strings.TrimSpace(input.ClientProfile))
	input.OwnerTeam = strings.TrimSpace(input.OwnerTeam)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = StatusActive
	}
	if !applicationRefPattern.MatchString(input.ApplicationRef) {
		return Draft{}, fmt.Errorf("%w: application_ref", ErrInvalid)
	}
	if input.DisplayName == "" || len(input.DisplayName) > 160 {
		return Draft{}, fmt.Errorf("%w: display_name", ErrInvalid)
	}
	if !slices.Contains([]string{KindService, KindAgent, KindWorkflow, KindInteractiveClient, KindIntegrationAdapter}, input.Kind) {
		return Draft{}, fmt.Errorf("%w: kind", ErrInvalid)
	}
	if !environmentPattern.MatchString(input.Environment) {
		return Draft{}, fmt.Errorf("%w: environment", ErrInvalid)
	}
	if !slices.Contains([]string{IdentityServiceCredential, IdentityPersonalDevelopment}, input.IdentityMode) {
		return Draft{}, fmt.Errorf("%w: identity_mode", ErrInvalid)
	}
	if input.Kind == KindIntegrationAdapter && input.IdentityMode != IdentityServiceCredential {
		return Draft{}, fmt.Errorf("%w: adapter_identity_mode", ErrInvalid)
	}
	if _, ok := LookupClientProfile(input.ClientProfile); !ok {
		return Draft{}, fmt.Errorf("%w: client_profile", ErrInvalid)
	}
	if len(input.OwnerTeam) > 160 || len(input.Description) > 1000 {
		return Draft{}, fmt.Errorf("%w: metadata", ErrInvalid)
	}
	if !slices.Contains([]string{StatusActive, StatusDisabled}, input.Status) {
		return Draft{}, fmt.Errorf("%w: status", ErrInvalid)
	}
	return input, nil
}

func NormalizeV2Draft(input V2Draft) (V2Draft, error) {
	input.ApplicationRef = strings.ToLower(strings.TrimSpace(input.ApplicationRef))
	input.LogicalRef = strings.ToLower(strings.TrimSpace(input.LogicalRef))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Environment = strings.ToLower(strings.TrimSpace(input.Environment))
	input.PrimaryClientProfile = strings.ToLower(strings.TrimSpace(input.PrimaryClientProfile))
	input.OwnerTeam = strings.TrimSpace(input.OwnerTeam)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = StatusActive
	}
	if !applicationRefPattern.MatchString(input.ApplicationRef) ||
		!regexp.MustCompile(`^workload_[a-z0-9][a-z0-9._-]{2,54}$`).MatchString(input.LogicalRef) {
		return V2Draft{}, fmt.Errorf("%w: identity_ref", ErrInvalid)
	}
	if input.DisplayName == "" || len(input.DisplayName) > 160 ||
		!slices.Contains([]string{KindService, KindAgent, KindWorkflow, KindIntegrationAdapter}, input.Kind) ||
		!environmentPattern.MatchString(input.Environment) || len(input.OwnerTeam) > 160 || len(input.Description) > 1000 ||
		!slices.Contains([]string{StatusActive, StatusDisabled}, input.Status) {
		return V2Draft{}, fmt.Errorf("%w: metadata", ErrInvalid)
	}
	profiles := make([]string, 0, len(input.ClientProfiles))
	seen := make(map[string]struct{}, len(input.ClientProfiles))
	for _, raw := range input.ClientProfiles {
		profile := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := LookupClientProfile(profile); !ok {
			return V2Draft{}, fmt.Errorf("%w: client_profiles", ErrInvalid)
		}
		if _, ok := seen[profile]; !ok {
			seen[profile] = struct{}{}
			profiles = append(profiles, profile)
		}
	}
	slices.Sort(profiles)
	input.ClientProfiles = profiles
	if input.PrimaryClientProfile != "" {
		if _, ok := seen[input.PrimaryClientProfile]; !ok {
			return V2Draft{}, fmt.Errorf("%w: primary_client_profile", ErrInvalid)
		}
	}
	return input, nil
}

// NormalizeCompatibleV2Kind accepts the canonical v2 vocabulary plus the
// three unambiguous legacy service-kind spellings supported for one endpoint
// compatibility window. It never maps interactive or personal identities.
func NormalizeCompatibleV2Kind(raw string) (kind string, adapted bool, err error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case KindService, KindAgent, KindWorkflow, KindIntegrationAdapter:
		return strings.ToLower(strings.TrimSpace(raw)), false, nil
	case "batch_job":
		return KindWorkflow, true, nil
	case "agent_runtime":
		return KindAgent, true, nil
	case "service_integration", "integration":
		return KindIntegrationAdapter, true, nil
	default:
		return "", false, fmt.Errorf("%w: kind", ErrInvalid)
	}
}

// DeriveV2LogicalRef deterministically adapts an unambiguous legacy
// application_ref. Long refs retain a readable prefix plus a stable digest;
// no database lookup or actor-controlled randomness participates.
func DeriveV2LogicalRef(applicationRef string) (string, error) {
	applicationRef = strings.ToLower(strings.TrimSpace(applicationRef))
	if !applicationRefPattern.MatchString(applicationRef) {
		return "", fmt.Errorf("%w: application_ref", ErrInvalid)
	}
	suffix := strings.TrimPrefix(applicationRef, "app_")
	if len(suffix) > 55 {
		digest := sha256.Sum256([]byte(applicationRef))
		suffix = suffix[:38] + "_" + hex.EncodeToString(digest[:8])
	}
	return "workload_" + suffix, nil
}

func ClientProfiles() []ClientProfile {
	result := make([]ClientProfile, len(profiles))
	for index, profile := range profiles {
		profile.SupportedEndpoints = slices.Clone(profile.SupportedEndpoints)
		result[index] = profile
	}
	return result
}

func LookupClientProfile(profileID string) (ClientProfile, bool) {
	profileID = strings.ToLower(strings.TrimSpace(profileID))
	for _, profile := range profiles {
		if profile.ProfileID == profileID {
			profile.SupportedEndpoints = slices.Clone(profile.SupportedEndpoints)
			return profile, true
		}
	}
	return ClientProfile{}, false
}
