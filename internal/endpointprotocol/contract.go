// Package endpointprotocol defines strict, fixture-verifiable provider wire
// contracts. GPT Image V1 is wired only into UniRouter's repository-local
// asynchronous image job plane; the Responses, Anthropic Messages, and rerank
// profiles remain contract-only.
package endpointprotocol

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

const (
	ContractVersion = "unirouter.endpoint-protocol.v1"

	ProfileOpenAIResponsesText   = "unirouter.openai.responses.text.v1"
	ProfileAnthropicMessagesText = "unirouter.anthropic.messages.text.v1"
	ProfileCohereRerankV2        = "unirouter.cohere.rerank.v2"
	ProfileOpenAIImageGPT        = "unirouter.openai.images.gpt-image.v1"

	RuntimeContractOnly      = "local_contract_only"
	RuntimeRepositoryImageV1 = "repository_local_image_v1"

	ChargeObservedOnly = "provider_observed_not_customer_chargeable"
)

type EndpointKind string

const (
	EndpointResponses       EndpointKind = "responses"
	EndpointMessages        EndpointKind = "messages"
	EndpointRerank          EndpointKind = "rerank"
	EndpointImageGeneration EndpointKind = "image_generation"
)

type UnitClass string

const (
	UnitInputTokens       UnitClass = "input_tokens"
	UnitCacheReadTokens   UnitClass = "cache_read_tokens"
	UnitCacheWriteTokens  UnitClass = "cache_write_tokens"
	UnitOutputTokens      UnitClass = "output_tokens"
	UnitSearchUnits       UnitClass = "search_units"
	UnitInputTextTokens   UnitClass = "input_text_tokens"
	UnitInputImageTokens  UnitClass = "input_image_tokens"
	UnitOutputImageTokens UnitClass = "output_image_tokens"
)

var (
	ErrInvalidConfiguration = errors.New("endpointprotocol: invalid configuration")
	ErrInvalidJSON          = errors.New("endpointprotocol: invalid json")
	ErrInvalidRequest       = errors.New("endpointprotocol: invalid request")
	ErrUnsupportedSemantics = errors.New("endpointprotocol: unsupported semantics")
	ErrInvalidResponse      = errors.New("endpointprotocol: invalid response")
	ErrNonFinalResponse     = errors.New("endpointprotocol: non-final response")
	ErrMissingUsage         = errors.New("endpointprotocol: missing usage")
	ErrUpstreamResponse     = errors.New("endpointprotocol: upstream response")
)

type ProtocolError struct {
	Code string `json:"code"`
	kind error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "endpointprotocol: unknown error"
	}
	return "endpointprotocol: " + e.Code
}

func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.kind
}

func protocolError(kind error, code string) error {
	return &ProtocolError{Code: code, kind: kind}
}

type Descriptor struct {
	ContractVersion      string       `json:"contract_version"`
	ProfileID            string       `json:"profile_id"`
	ProfileVersion       string       `json:"profile_version"`
	Provider             string       `json:"provider"`
	EndpointKind         EndpointKind `json:"endpoint_kind"`
	DownstreamEndpoint   string       `json:"downstream_endpoint"`
	UpstreamEndpoint     string       `json:"upstream_endpoint"`
	Authentication       string       `json:"authentication"`
	RuntimeState         string       `json:"runtime_state"`
	ProviderFinality     string       `json:"provider_finality"`
	CustomerChargeState  string       `json:"customer_charge_state"`
	SupportedModalities  []string     `json:"supported_modalities"`
	ObservedUnitClasses  []UnitClass  `json:"observed_unit_classes"`
	UnsupportedSemantics []string     `json:"unsupported_semantics"`
}

type BuildInput struct {
	BaseURL        string
	APIKey         string
	RequestID      string
	RequestedModel string
	MappedModel    string
	Body           []byte
}

// Binding is an opaque parse capability created only after a request passes a
// profile's strict request validation. It binds response checks to the exact
// request semantics without becoming a log or API payload.
type Binding struct {
	profileID      string
	profileVersion string
	requestedModel string
	mappedModel    string
	documentCount  int
	topN           int
	outputCount    int
}

type UnitObservation struct {
	Class       UnitClass `json:"class"`
	Quantity    string    `json:"quantity"`
	Authority   string    `json:"authority"`
	Finality    string    `json:"finality"`
	ChargeState string    `json:"charge_state"`
}

type Outcome struct {
	ContractVersion string            `json:"contract_version"`
	ProfileID       string            `json:"profile_id"`
	ProviderState   string            `json:"provider_state"`
	ProviderObject  string            `json:"provider_object_id"`
	MappedModel     string            `json:"mapped_model,omitempty"`
	OutputCount     int               `json:"output_count"`
	Units           []UnitObservation `json:"units"`
}

type Profile interface {
	Descriptor() Descriptor
	BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error)
	ParseResponse(ctx context.Context, binding *Binding, statusCode int, body []byte) (Outcome, error)
	ClassifyHTTPStatus(statusCode int) (failureKind string, retryable bool)
}

type profileIdentity struct {
	profileID      string
	profileVersion string
}

type Registry struct {
	profiles map[profileIdentity]Profile
	byID     map[string][]profileIdentity
	order    []profileIdentity
	invalid  bool
}

func NewRegistry() *Registry {
	return newRegistry(
		openAIResponsesProfile{},
		anthropicMessagesProfile{},
		cohereRerankProfile{},
		openAIImageProfile{},
	)
}

func newRegistry(profiles ...Profile) *Registry {
	registry := &Registry{
		profiles: make(map[profileIdentity]Profile, len(profiles)),
		byID:     make(map[string][]profileIdentity, len(profiles)),
		order:    make([]profileIdentity, 0, len(profiles)),
	}
	for _, profile := range profiles {
		if profile == nil {
			registry.invalid = true
			continue
		}
		descriptor := profile.Descriptor()
		identity := profileIdentity{
			profileID:      descriptor.ProfileID,
			profileVersion: descriptor.ProfileVersion,
		}
		if strings.TrimSpace(identity.profileID) == "" || strings.TrimSpace(identity.profileVersion) == "" {
			registry.invalid = true
			continue
		}
		if _, duplicate := registry.profiles[identity]; duplicate {
			registry.invalid = true
			continue
		}
		registry.profiles[identity] = profile
		registry.byID[identity.profileID] = append(registry.byID[identity.profileID], identity)
		registry.order = append(registry.order, identity)
	}
	return registry
}

// Resolve selects one executable profile by its exact immutable identity. It
// never performs a latest-version lookup or version fallback.
func (r *Registry) Resolve(profileID, profileVersion string) (Profile, error) {
	if r == nil {
		return nil, protocolError(ErrInvalidConfiguration, "registry_unavailable")
	}
	if r.invalid {
		return nil, protocolError(ErrInvalidConfiguration, "registry_invalid")
	}
	if strings.TrimSpace(profileID) == "" || strings.TrimSpace(profileVersion) == "" {
		return nil, protocolError(ErrInvalidConfiguration, "profile_identity_invalid")
	}
	profile, exists := r.profiles[profileIdentity{profileID: profileID, profileVersion: profileVersion}]
	if exists {
		return profile, nil
	}
	if len(r.byID[profileID]) != 0 {
		return nil, protocolError(ErrInvalidConfiguration, "unknown_profile_version")
	}
	return nil, protocolError(ErrInvalidConfiguration, "unknown_profile")
}

// Get is the compatibility lookup for callers whose profile ID currently has
// exactly one registered version. It fails closed once an ID has more than one
// version; callers needing executable behavior must then use Resolve.
func (r *Registry) Get(profileID string) (Profile, error) {
	if r == nil {
		return nil, protocolError(ErrInvalidConfiguration, "registry_unavailable")
	}
	if r.invalid {
		return nil, protocolError(ErrInvalidConfiguration, "registry_invalid")
	}
	if strings.TrimSpace(profileID) == "" {
		return nil, protocolError(ErrInvalidConfiguration, "profile_identity_invalid")
	}
	identities := r.byID[profileID]
	if len(identities) == 0 {
		return nil, protocolError(ErrInvalidConfiguration, "unknown_profile")
	}
	if len(identities) != 1 {
		return nil, protocolError(ErrInvalidConfiguration, "ambiguous_profile_version")
	}
	return r.profiles[identities[0]], nil
}

func (r *Registry) Descriptors() []Descriptor {
	if r == nil || r.invalid {
		return nil
	}
	result := make([]Descriptor, 0, len(r.order))
	for _, identity := range r.order {
		if profile, exists := r.profiles[identity]; exists {
			result = append(result, cloneDescriptor(profile.Descriptor()))
		}
	}
	return result
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	descriptor.SupportedModalities = append([]string(nil), descriptor.SupportedModalities...)
	descriptor.ObservedUnitClasses = append([]UnitClass(nil), descriptor.ObservedUnitClasses...)
	descriptor.UnsupportedSemantics = append([]string(nil), descriptor.UnsupportedSemantics...)
	return descriptor
}
