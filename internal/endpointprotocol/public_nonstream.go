package endpointprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

const (
	// PublicDeliveryContractV1 is the first customer-visible, non-stream text
	// delivery contract. It is deliberately separate from ContractVersion: a
	// locally conformed provider profile is not thereby executable publicly.
	PublicDeliveryContractV1 = "unirouter.public-nonstream-delivery.v1"

	// EndpointProfileNonStreamExecutorV1 is the M75 provider executor identity.
	// Public execution requires this exact compiled support tuple in addition to
	// the three M77 delivery/charge/idempotency contracts below.
	EndpointProfileNonStreamExecutorV1 = "unirouter.endpoint-profile-nonstream-executor.v1"

	// PublicCustomerChargeContractV1 maps final provider usage into the four
	// canonical token counters accepted by the customer settlement boundary.
	PublicCustomerChargeContractV1 = "unirouter.provider-final-token-charge.v1"

	// PublicIdempotencyContractV1 reserves a request identity at most once. The
	// first public release does not promise response replay.
	PublicIdempotencyContractV1 = "unirouter.public-at-most-once.v1"

	PublicCachePolicyBypass = "bypass"

	// PublicNonStreamMaxResponseBytes is the customer-delivery envelope bound.
	// The executor enforces it before protocol parsing or output guardrails.
	PublicNonStreamMaxResponseBytes int64 = 16 << 20
)

var (
	ErrPublicContractMismatch = errors.New("endpointprotocol: public contract mismatch")
	ErrPublicTextReplacement  = errors.New("endpointprotocol: public text replacement invalid")
)

// PublicContractIdentity is the exact immutable compiled half of a future M77
// route binding. All fields participate in lookup; in particular, an existing
// M75 production qualification never gains public execution merely because
// its provider profile ID matches.
type PublicContractIdentity struct {
	ModelOperationID           string `json:"model_operation_id"`
	PublicPath                 string `json:"public_path"`
	ExecutorContractVersion    string `json:"executor_contract_version"`
	ProfileID                  string `json:"profile_id"`
	ProfileVersion             string `json:"profile_version"`
	DeliveryContractVersion    string `json:"delivery_contract_version"`
	ChargeContractVersion      string `json:"charge_contract_version"`
	IdempotencyContractVersion string `json:"idempotency_contract_version"`
}

func (identity PublicContractIdentity) valid() bool {
	values := []string{
		identity.ModelOperationID,
		identity.PublicPath,
		identity.ExecutorContractVersion,
		identity.ProfileID,
		identity.ProfileVersion,
		identity.DeliveryContractVersion,
		identity.ChargeContractVersion,
		identity.IdempotencyContractVersion,
	}
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

type PublicNonStreamDescriptor struct {
	Identity           PublicContractIdentity `json:"identity"`
	EndpointKind       EndpointKind           `json:"endpoint_kind"`
	DownstreamEndpoint string                 `json:"downstream_endpoint"`
	UpstreamEndpoint   string                 `json:"upstream_endpoint"`
	CachePolicy        string                 `json:"cache_policy"`
	Streaming          bool                   `json:"streaming"`
	Modality           string                 `json:"modality"`
}

type PublicText struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

func clonePublicTexts(values []PublicText) []PublicText {
	return append([]PublicText(nil), values...)
}

// PublicAdmission contains only deterministic request facts. Token
// estimation, workload admission, guardrails, planning, funding and provider
// authorization remain gateway responsibilities and occur outside the
// protocol package.
type PublicAdmission struct {
	EndpointKind            EndpointKind `json:"endpoint_kind"`
	RequestedModel          string       `json:"requested_model"`
	ExplicitMaxOutputTokens int64        `json:"explicit_max_output_tokens"`
	CanonicalBodyBytes      int          `json:"canonical_body_bytes"`
	InputTextBytes          int          `json:"input_text_bytes"`
	Texts                   []PublicText `json:"texts"`
	CachePolicy             string       `json:"cache_policy"`
}

type publicRequestDocument interface {
	canonicalBody() ([]byte, error)
	texts() []PublicText
	withTexts([]string) (publicRequestDocument, error)
}

// PublicPreparedRequest is an immutable, provider-independent request. It is
// safe to hash and inspect before a route or provider credential is selected.
type PublicPreparedRequest struct {
	identity       PublicContractIdentity
	endpointKind   EndpointKind
	requestedModel string
	maxOutput      int64
	document       publicRequestDocument
}

func (request *PublicPreparedRequest) Admission() PublicAdmission {
	if request == nil || request.document == nil {
		return PublicAdmission{}
	}
	body, err := request.document.canonicalBody()
	if err != nil {
		return PublicAdmission{}
	}
	texts := request.document.texts()
	inputBytes := 0
	for _, value := range texts {
		inputBytes += len(value.Text)
	}
	return PublicAdmission{
		EndpointKind:            request.endpointKind,
		RequestedModel:          request.requestedModel,
		ExplicitMaxOutputTokens: request.maxOutput,
		CanonicalBodyBytes:      len(body),
		InputTextBytes:          inputBytes,
		Texts:                   clonePublicTexts(texts),
		CachePolicy:             PublicCachePolicyBypass,
	}
}

func (request *PublicPreparedRequest) CanonicalBody() []byte {
	if request == nil || request.document == nil {
		return nil
	}
	body, err := request.document.canonicalBody()
	if err != nil {
		return nil
	}
	return append([]byte(nil), body...)
}

// WithTexts applies a complete, positional guardrail transformation. Partial
// replacement is rejected so a caller cannot silently skip an inspected text
// leaf.
func (request *PublicPreparedRequest) WithTexts(replacements []string) (*PublicPreparedRequest, error) {
	if request == nil || request.document == nil {
		return nil, ErrPublicTextReplacement
	}
	document, err := request.document.withTexts(replacements)
	if err != nil {
		return nil, err
	}
	if err := validatePublicRequestDocument(document); err != nil {
		return nil, err
	}
	result := *request
	result.document = document
	return &result, nil
}

type PublicUpstreamInput struct {
	BaseURL     string
	APIKey      string
	RequestID   string
	MappedModel string
}

// PublicNonStreamProfile is deliberately not an extension of Profile. The
// historical Profile contract remains local_contract_only/observed-only;
// public execution is available solely through this separate registry.
type PublicNonStreamProfile interface {
	Descriptor() PublicNonStreamDescriptor
	PrepareRequest(body []byte, requestedModel string) (*PublicPreparedRequest, error)
	BuildUpstream(context.Context, *PublicPreparedRequest, PublicUpstreamInput) (*PublicUpstreamRequest, error)
	parseDelivery(context.Context, *publicResponseBinding, int, []byte) (PublicDelivery, error)
	ClassifyHTTPStatus(statusCode int) (failureKind string, retryable bool)
}

type publicResponseBinding struct {
	identity       PublicContractIdentity
	base           *Binding
	requestedModel string
	mappedModel    string
}

// PublicUpstreamRequest couples the exact public contract, provider request,
// and response parse capability. Callers can clone the HTTP request for an
// authorized attempt but cannot manufacture the private response binding.
type PublicUpstreamRequest struct {
	profile PublicNonStreamProfile
	request *http.Request
	binding *publicResponseBinding
}

func (upstream *PublicUpstreamRequest) NewHTTPRequest(ctx context.Context) (*http.Request, error) {
	if upstream == nil || upstream.request == nil || upstream.binding == nil || upstream.profile == nil {
		return nil, protocolError(ErrInvalidConfiguration, "public_upstream_request_invalid")
	}
	clone := upstream.request.Clone(ctx)
	if upstream.request.GetBody != nil {
		body, err := upstream.request.GetBody()
		if err != nil {
			return nil, protocolError(ErrInvalidConfiguration, "public_upstream_body_unavailable")
		}
		clone.Body = body
	}
	return clone, nil
}

func (upstream *PublicUpstreamRequest) ParseDelivery(
	ctx context.Context,
	statusCode int,
	body []byte,
) (PublicDelivery, error) {
	if upstream == nil || upstream.profile == nil || upstream.binding == nil {
		return PublicDelivery{}, protocolError(ErrInvalidConfiguration, "public_upstream_request_invalid")
	}
	return upstream.profile.parseDelivery(ctx, upstream.binding, statusCode, body)
}

func (upstream *PublicUpstreamRequest) ClassifyHTTPStatus(statusCode int) (string, bool) {
	if upstream == nil || upstream.profile == nil || upstream.binding == nil {
		return "invalid_response", false
	}
	return upstream.profile.ClassifyHTTPStatus(statusCode)
}

type PublicCustomerUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
}

func (usage PublicCustomerUsage) validate() error {
	if usage.InputTokens < 0 || usage.CacheReadTokens < 0 || usage.CacheWriteTokens < 0 || usage.OutputTokens < 0 {
		return protocolError(ErrInvalidResponse, "customer_usage_negative")
	}
	if usage.InputTokens > metering.MaxPersistedTokenCount ||
		usage.CacheReadTokens > metering.MaxPersistedTokenCount ||
		usage.CacheWriteTokens > metering.MaxPersistedTokenCount ||
		usage.OutputTokens > metering.MaxPersistedTokenCount ||
		usage.InputTokens+usage.OutputTokens > metering.MaxPersistedTokenCount {
		return protocolError(ErrInvalidResponse, "customer_usage_persisted_limit")
	}
	if usage.CacheReadTokens > usage.InputTokens ||
		usage.CacheWriteTokens > usage.InputTokens-usage.CacheReadTokens {
		return protocolError(ErrInvalidResponse, "customer_usage_cache_exceeds_input")
	}
	return nil
}

type publicDeliveryDocument interface {
	canonicalBody() ([]byte, error)
	texts() []PublicText
	withTexts([]string) (publicDeliveryDocument, error)
}

// PublicDelivery contains only the normalized customer-visible payload and
// canonical settlement usage. Raw provider bytes and mapped model identity are
// intentionally not represented.
type PublicDelivery struct {
	endpointKind   EndpointKind
	providerObject string
	usage          PublicCustomerUsage
	document       publicDeliveryDocument
}

func (delivery PublicDelivery) EndpointKind() EndpointKind         { return delivery.endpointKind }
func (delivery PublicDelivery) ProviderObject() string             { return delivery.providerObject }
func (delivery PublicDelivery) CustomerUsage() PublicCustomerUsage { return delivery.usage }
func (delivery PublicDelivery) ContentType() string                { return "application/json" }
func (delivery PublicDelivery) CachePolicy() string                { return PublicCachePolicyBypass }

func (delivery PublicDelivery) Body() []byte {
	if delivery.document == nil {
		return nil
	}
	body, err := delivery.document.canonicalBody()
	if err != nil {
		return nil
	}
	return append([]byte(nil), body...)
}

func (delivery PublicDelivery) Texts() []PublicText {
	if delivery.document == nil {
		return nil
	}
	return clonePublicTexts(delivery.document.texts())
}

func (delivery PublicDelivery) WithTexts(replacements []string) (PublicDelivery, error) {
	if delivery.document == nil {
		return PublicDelivery{}, ErrPublicTextReplacement
	}
	document, err := delivery.document.withTexts(replacements)
	if err != nil {
		return PublicDelivery{}, err
	}
	if err := validatePublicDeliveryDocument(document); err != nil {
		return PublicDelivery{}, err
	}
	delivery.document = document
	return delivery, nil
}

type PublicNonStreamRegistry struct {
	profiles map[PublicContractIdentity]PublicNonStreamProfile
	invalid  bool
}

func NewPublicNonStreamRegistry() *PublicNonStreamRegistry {
	return newPublicNonStreamRegistry(
		publicOpenAIResponsesProfile{base: openAIResponsesProfile{}},
		publicAnthropicMessagesProfile{base: anthropicMessagesProfile{}},
	)
}

func newPublicNonStreamRegistry(profiles ...PublicNonStreamProfile) *PublicNonStreamRegistry {
	registry := &PublicNonStreamRegistry{profiles: make(map[PublicContractIdentity]PublicNonStreamProfile, len(profiles))}
	for _, profile := range profiles {
		if profile == nil {
			registry.invalid = true
			continue
		}
		identity := profile.Descriptor().Identity
		if !identity.valid() {
			registry.invalid = true
			continue
		}
		if _, duplicate := registry.profiles[identity]; duplicate {
			registry.invalid = true
			continue
		}
		registry.profiles[identity] = profile
	}
	return registry
}

func (registry *PublicNonStreamRegistry) Resolve(identity PublicContractIdentity) (PublicNonStreamProfile, error) {
	if registry == nil {
		return nil, protocolError(ErrInvalidConfiguration, "public_registry_unavailable")
	}
	if registry.invalid {
		return nil, protocolError(ErrInvalidConfiguration, "public_registry_invalid")
	}
	if !identity.valid() {
		return nil, protocolError(ErrInvalidConfiguration, "public_contract_identity_invalid")
	}
	profile, exists := registry.profiles[identity]
	if !exists {
		return nil, protocolError(ErrPublicContractMismatch, "public_contract_not_compiled")
	}
	return profile, nil
}

func publicIdentity(
	operationID modeloperation.ID,
	publicPath string,
	profileID string,
	profileVersion string,
) PublicContractIdentity {
	return PublicContractIdentity{
		ModelOperationID:           string(operationID),
		PublicPath:                 publicPath,
		ExecutorContractVersion:    EndpointProfileNonStreamExecutorV1,
		ProfileID:                  profileID,
		ProfileVersion:             profileVersion,
		DeliveryContractVersion:    PublicDeliveryContractV1,
		ChargeContractVersion:      PublicCustomerChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
}

func requirePublicPrepared(profile PublicNonStreamProfile, request *PublicPreparedRequest) error {
	if profile == nil || request == nil || request.document == nil || request.identity != profile.Descriptor().Identity {
		return protocolError(ErrPublicContractMismatch, "public_prepared_request_mismatch")
	}
	return nil
}

func buildPublicUpstream(
	ctx context.Context,
	profile PublicNonStreamProfile,
	base Profile,
	prepared *PublicPreparedRequest,
	input PublicUpstreamInput,
) (*PublicUpstreamRequest, error) {
	if err := requirePublicPrepared(profile, prepared); err != nil {
		return nil, err
	}
	body := prepared.CanonicalBody()
	if len(body) == 0 {
		return nil, protocolError(ErrInvalidRequest, "public_request_encoding_failed")
	}
	request, binding, err := base.BuildRequest(ctx, BuildInput{
		BaseURL:        input.BaseURL,
		APIKey:         input.APIKey,
		RequestID:      input.RequestID,
		RequestedModel: prepared.requestedModel,
		MappedModel:    input.MappedModel,
		Body:           body,
	})
	if err != nil {
		return nil, err
	}
	return &PublicUpstreamRequest{
		profile: profile,
		request: request,
		binding: &publicResponseBinding{
			identity:       prepared.identity,
			base:           binding,
			requestedModel: prepared.requestedModel,
			mappedModel:    input.MappedModel,
		},
	}, nil
}

func requirePublicBinding(profile PublicNonStreamProfile, binding *publicResponseBinding) error {
	if profile == nil || binding == nil || binding.base == nil || binding.identity != profile.Descriptor().Identity ||
		binding.requestedModel == "" || binding.mappedModel == "" {
		return protocolError(ErrPublicContractMismatch, "public_response_binding_mismatch")
	}
	return nil
}

type exactJSONMemberSchema struct {
	object    exactJSONObjectSchema
	arrayItem exactJSONObjectSchema
}

type exactJSONObjectSchema map[string]exactJSONMemberSchema

// validateExactJSONObjectMembers closes encoding/json's case-insensitive
// struct-field matching. StrictJSON has already rejected byte-exact duplicate
// keys; this recursive member walk additionally rejects case aliases and
// semantic duplicates such as max_tokens + MAX_TOKENS before any typed decoder
// can collapse them into one Go field.
func validateExactJSONObjectMembers(
	body []byte,
	maxBytes int,
	kind error,
	codePrefix string,
	schema exactJSONObjectSchema,
) error {
	if err := validateStrictJSON(body, maxBytes, kind); err != nil {
		return err
	}
	return walkExactJSONObjectMembers(body, kind, codePrefix, schema)
}

func walkExactJSONObjectMembers(
	raw []byte,
	kind error,
	codePrefix string,
	schema exactJSONObjectSchema,
) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return protocolError(kind, codePrefix+"_shape_invalid")
	}
	for name, value := range object {
		member, allowed := schema[name]
		if !allowed {
			return protocolError(kind, codePrefix+"_unknown_member")
		}
		if member.object != nil && !isExplicitJSONNull(value) {
			if err := walkExactJSONObjectMembers(value, kind, codePrefix, member.object); err != nil {
				return err
			}
		}
		if member.arrayItem != nil && !isExplicitJSONNull(value) {
			var items []json.RawMessage
			if err := json.Unmarshal(value, &items); err != nil {
				return protocolError(kind, codePrefix+"_shape_invalid")
			}
			for _, item := range items {
				if isExplicitJSONNull(item) {
					continue
				}
				if err := walkExactJSONObjectMembers(item, kind, codePrefix, member.arrayItem); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func decodeStrictPublicResponse(body []byte, target any, schema exactJSONObjectSchema) error {
	if err := validateExactJSONObjectMembers(
		body,
		maxResponseBytes,
		ErrInvalidResponse,
		"public_response",
		schema,
	); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return protocolError(ErrInvalidResponse, "public_response_shape_invalid")
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return protocolError(ErrInvalidJSON, "trailing_json")
	}
	return nil
}

func validatePublicRequestDocument(document publicRequestDocument) error {
	if document == nil {
		return protocolError(ErrInvalidRequest, "public_request_document_invalid")
	}
	for _, value := range document.texts() {
		if !utf8.ValidString(value.Text) {
			return protocolError(ErrInvalidRequest, "public_request_text_invalid")
		}
	}
	body, err := document.canonicalBody()
	if err != nil || len(body) == 0 || len(body) > maxRequestBytes {
		return protocolError(ErrInvalidRequest, "public_request_body_size_invalid")
	}
	return nil
}

func validatePublicDeliveryDocument(document publicDeliveryDocument) error {
	if document == nil {
		return protocolError(ErrInvalidResponse, "public_delivery_document_invalid")
	}
	var textBytes int64
	for _, value := range document.texts() {
		if !utf8.ValidString(value.Text) || int64(len(value.Text)) > PublicNonStreamMaxResponseBytes-textBytes {
			return protocolError(ErrInvalidResponse, "public_delivery_text_size_invalid")
		}
		textBytes += int64(len(value.Text))
	}
	body, err := document.canonicalBody()
	if err != nil || len(body) == 0 || int64(len(body)) > PublicNonStreamMaxResponseBytes {
		return protocolError(ErrInvalidResponse, "public_delivery_body_size_invalid")
	}
	return nil
}

func replacementError(want, got int) error {
	return fmt.Errorf("%w: got %d texts, want %d", ErrPublicTextReplacement, got, want)
}
