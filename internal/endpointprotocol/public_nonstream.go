package endpointprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	// PublicResponsesDeliveryContractV2 is the exact dual-mode Responses
	// delivery identity. It is append-only and never reinterprets the historical
	// non-stream delivery contract above.
	PublicResponsesDeliveryContractV2 = "unirouter.public-responses-delivery.v2"
	// PublicAnthropicMessagesDeliveryContractV2 is the append-only dual-mode
	// Messages identity. V1 remains an exact non-stream historical contract.
	PublicAnthropicMessagesDeliveryContractV2 = "unirouter.public-anthropic-messages-delivery.v2"
	PublicMessagesDualExecutorContractV1      = "unirouter.endpoint-profile-messages-dual-executor.v1"
	PublicAnthropicMessagesToolsDeliveryV1    = "unirouter.public-anthropic-messages-tools-delivery.v1"
	PublicMessagesToolsExecutorContractV1     = "unirouter.endpoint-profile-messages-tools-executor.v1"

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

// PublicReasoningRequestContract is the endpoint-facing projection of one
// immutable Model Execution Profile reasoning component. It deliberately
// carries only wire grammar; model identity, evidence, routing and lifecycle
// remain gateway/modelprofile authorities.
type PublicReasoningRequestContract struct {
	RequestObjectField string   `json:"request_object_field"`
	EffortField        string   `json:"effort_field"`
	AllowedEfforts     []string `json:"allowed_efforts"`
}

// PublicAnthropicMessagesContract is the request-scoped projection of
// one exact Anthropic Messages model policy. It narrows the shared core codec;
// the zero value preserves the historical public contract.
type PublicAnthropicMessagesContract struct {
	ForbiddenRootFields     []string                                 `json:"forbidden_root_fields,omitempty"`
	RequireFinalUserMessage bool                                     `json:"require_final_user_message,omitempty"`
	AllowedResponseStops    []string                                 `json:"allowed_response_stops,omitempty"`
	RefusalStopDetails      bool                                     `json:"refusal_stop_details,omitempty"`
	ResponseInferenceGeo    bool                                     `json:"response_inference_geo,omitempty"`
	ResponseServiceTier     bool                                     `json:"response_service_tier,omitempty"`
	Reasoning               *PublicAnthropicReasoningRequestContract `json:"reasoning,omitempty"`
	FunctionTools           *PublicAnthropicFunctionToolsContract    `json:"function_tools,omitempty"`
}

// PublicAnthropicFunctionToolsContract is the request-bound projection of the
// immutable Messages operation policy. Tool definitions and history remain in
// the prepared request document; only typed facts needed by response parsing
// and invocation planning are carried in the binding.
type PublicAnthropicFunctionToolsContract struct {
	MaxTools                    int      `json:"max_tools"`
	ToolNamePattern             string   `json:"tool_name_pattern"`
	ToolChoices                 []string `json:"tool_choices"`
	ProviderDefaultChoice       string   `json:"provider_default_choice"`
	NamedChoice                 bool     `json:"named_choice"`
	ParallelToolUse             bool     `json:"parallel_tool_use"`
	ParallelProviderDefault     bool     `json:"parallel_provider_default"`
	ExactHistory                bool     `json:"exact_history"`
	ExactResponse               bool     `json:"exact_response"`
	RequiresNonStream           bool     `json:"requires_non_stream"`
	ToolInputIdentityGuarded    bool     `json:"tool_input_identity_guarded"`
	ThinkingHistoryIdentityOnly bool     `json:"thinking_history_identity_only"`
}

// PublicAnthropicReasoningRequestContract projects the independently nested
// toggle, effort, and optional manual-budget controls from one immutable
// reasoning Profile. It is request-scoped; the Model Execution Profile remains
// the only wire-path/value authority.
type PublicAnthropicReasoningRequestContract struct {
	ThinkingObjectField      string   `json:"thinking_object_field"`
	ThinkingTypeField        string   `json:"thinking_type_field"`
	AllowedThinkingTypes     []string `json:"allowed_thinking_types"`
	DisabledThinkingTypes    []string `json:"disabled_thinking_types"`
	ThinkingProviderDefault  string   `json:"thinking_provider_default"`
	OutputConfigObjectField  string   `json:"output_config_object_field"`
	EffortField              string   `json:"effort_field"`
	AllowedEfforts           []string `json:"allowed_efforts"`
	EffortsAllowedDisabled   []string `json:"efforts_allowed_disabled,omitempty"`
	EffortProviderDefault    string   `json:"effort_provider_default"`
	BudgetField              string   `json:"budget_field,omitempty"`
	BudgetMinimum            int64    `json:"budget_minimum,omitempty"`
	BudgetMaximum            int64    `json:"budget_maximum,omitempty"`
	BudgetRequiredTypes      []string `json:"budget_required_types,omitempty"`
	ResponseThinkingTextMode string   `json:"response_thinking_text_mode"`
}

// PublicRequestContract composes optional capability grammar onto the exact
// public operation contract after Planner has selected an immutable execution
// Profile. The zero value is the historical strict core contract.
type PublicRequestContract struct {
	ExecutionProfileID      string                           `json:"execution_profile_id,omitempty"`
	ExecutionProfileVersion string                           `json:"execution_profile_version,omitempty"`
	Reasoning               *PublicReasoningRequestContract  `json:"reasoning,omitempty"`
	AnthropicMessages       *PublicAnthropicMessagesContract `json:"anthropic_messages,omitempty"`
}

func clonePublicRequestContract(value PublicRequestContract) PublicRequestContract {
	result := value
	if value.Reasoning != nil {
		reasoning := *value.Reasoning
		reasoning.AllowedEfforts = append([]string(nil), value.Reasoning.AllowedEfforts...)
		result.Reasoning = &reasoning
	}
	if value.AnthropicMessages != nil {
		messages := *value.AnthropicMessages
		messages.ForbiddenRootFields = slices.Clone(value.AnthropicMessages.ForbiddenRootFields)
		messages.AllowedResponseStops = slices.Clone(value.AnthropicMessages.AllowedResponseStops)
		if value.AnthropicMessages.Reasoning != nil {
			reasoning := *value.AnthropicMessages.Reasoning
			reasoning.AllowedThinkingTypes = slices.Clone(value.AnthropicMessages.Reasoning.AllowedThinkingTypes)
			reasoning.DisabledThinkingTypes = slices.Clone(value.AnthropicMessages.Reasoning.DisabledThinkingTypes)
			reasoning.AllowedEfforts = slices.Clone(value.AnthropicMessages.Reasoning.AllowedEfforts)
			reasoning.EffortsAllowedDisabled = slices.Clone(value.AnthropicMessages.Reasoning.EffortsAllowedDisabled)
			reasoning.BudgetRequiredTypes = slices.Clone(value.AnthropicMessages.Reasoning.BudgetRequiredTypes)
			messages.Reasoning = &reasoning
		}
		if value.AnthropicMessages.FunctionTools != nil {
			tools := *value.AnthropicMessages.FunctionTools
			tools.ToolChoices = slices.Clone(value.AnthropicMessages.FunctionTools.ToolChoices)
			messages.FunctionTools = &tools
		}
		result.AnthropicMessages = &messages
	}
	return result
}

func resolvePublicRequestContract(values []PublicRequestContract) (PublicRequestContract, error) {
	if len(values) > 1 {
		return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_request_contract_ambiguous")
	}
	contract := PublicRequestContract{}
	if len(values) == 1 {
		contract = clonePublicRequestContract(values[0])
	}
	profileID := strings.TrimSpace(contract.ExecutionProfileID)
	profileVersion := strings.TrimSpace(contract.ExecutionProfileVersion)
	if profileID != contract.ExecutionProfileID || profileVersion != contract.ExecutionProfileVersion ||
		(profileID == "") != (profileVersion == "") {
		return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_request_contract_profile_invalid")
	}
	if contract.Reasoning == nil && contract.AnthropicMessages == nil {
		return contract, nil
	}
	if profileID == "" {
		return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_request_contract_profile_invalid")
	}
	if contract.Reasoning != nil {
		if contract.Reasoning.RequestObjectField == "" || contract.Reasoning.EffortField == "" ||
			len(contract.Reasoning.AllowedEfforts) == 0 {
			return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_reasoning_contract_invalid")
		}
		seen := make(map[string]struct{}, len(contract.Reasoning.AllowedEfforts))
		for _, effort := range contract.Reasoning.AllowedEfforts {
			if effort == "" || effort != strings.TrimSpace(effort) {
				return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_reasoning_contract_invalid")
			}
			if _, duplicate := seen[effort]; duplicate {
				return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_reasoning_contract_invalid")
			}
			seen[effort] = struct{}{}
		}
	}
	if messages := contract.AnthropicMessages; messages != nil {
		if !messages.RequireFinalUserMessage || len(messages.AllowedResponseStops) == 0 ||
			!slices.IsSorted(messages.ForbiddenRootFields) || !slices.IsSorted(messages.AllowedResponseStops) {
			return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_messages_contract_invalid")
		}
		for _, values := range [][]string{messages.ForbiddenRootFields, messages.AllowedResponseStops} {
			for index, value := range values {
				if value == "" || value != strings.TrimSpace(value) || (index > 0 && values[index-1] == value) {
					return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_messages_contract_invalid")
				}
			}
		}
		if messages.RefusalStopDetails && !slices.Contains(messages.AllowedResponseStops, "refusal") {
			return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_messages_contract_invalid")
		}
		if reasoning := messages.Reasoning; reasoning != nil {
			if reasoning.ThinkingObjectField == "" || reasoning.ThinkingTypeField == "" ||
				len(reasoning.AllowedThinkingTypes) == 0 ||
				!slices.Contains(reasoning.AllowedThinkingTypes, reasoning.ThinkingProviderDefault) ||
				(reasoning.EffortField == "") != (reasoning.OutputConfigObjectField == "") ||
				(reasoning.EffortField != "" && (len(reasoning.AllowedEfforts) == 0 ||
					!slices.Contains(reasoning.AllowedEfforts, reasoning.EffortProviderDefault))) ||
				(reasoning.BudgetField == "") != (len(reasoning.BudgetRequiredTypes) == 0) ||
				(reasoning.BudgetField != "" && (reasoning.BudgetMinimum <= 0 || reasoning.BudgetMaximum < reasoning.BudgetMinimum)) ||
				!slices.Contains([]string{"omitted", "summarized"}, reasoning.ResponseThinkingTextMode) {
				return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
			}
			fields := []string{
				reasoning.ThinkingObjectField, reasoning.ThinkingTypeField,
				reasoning.OutputConfigObjectField, reasoning.EffortField, reasoning.BudgetField,
				reasoning.ThinkingProviderDefault, reasoning.EffortProviderDefault,
				reasoning.ResponseThinkingTextMode,
			}
			for _, value := range fields {
				if value != strings.TrimSpace(value) {
					return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
				}
			}
			if reasoning.ThinkingObjectField == reasoning.OutputConfigObjectField {
				return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
			}
			for _, values := range [][]string{
				reasoning.AllowedThinkingTypes,
				reasoning.DisabledThinkingTypes,
				reasoning.AllowedEfforts,
				reasoning.EffortsAllowedDisabled,
				reasoning.BudgetRequiredTypes,
			} {
				seen := make(map[string]struct{}, len(values))
				for _, value := range values {
					if value == "" || value != strings.TrimSpace(value) {
						return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
					}
					if _, duplicate := seen[value]; duplicate {
						return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
					}
					seen[value] = struct{}{}
				}
			}
			for _, value := range reasoning.DisabledThinkingTypes {
				if !slices.Contains(reasoning.AllowedThinkingTypes, value) {
					return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
				}
			}
			for _, value := range reasoning.EffortsAllowedDisabled {
				if !slices.Contains(reasoning.AllowedEfforts, value) {
					return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
				}
			}
			for _, value := range reasoning.BudgetRequiredTypes {
				if !slices.Contains(reasoning.AllowedThinkingTypes, value) || slices.Contains(reasoning.DisabledThinkingTypes, value) {
					return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
				}
			}
			if len(reasoning.EffortsAllowedDisabled) > 0 && len(reasoning.DisabledThinkingTypes) == 0 {
				return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
			}
		}
		if tools := messages.FunctionTools; tools != nil {
			if tools.MaxTools <= 0 || tools.ToolNamePattern == "" || len(tools.ToolChoices) == 0 ||
				!slices.IsSorted(tools.ToolChoices) || !slices.Contains(tools.ToolChoices, tools.ProviderDefaultChoice) ||
				!tools.NamedChoice || !tools.ParallelToolUse || !tools.ParallelProviderDefault ||
				!tools.ExactHistory || !tools.ExactResponse || !tools.RequiresNonStream ||
				!tools.ToolInputIdentityGuarded || !tools.ThinkingHistoryIdentityOnly {
				return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_tools_contract_invalid")
			}
			for index, value := range tools.ToolChoices {
				if value == "" || value != strings.TrimSpace(value) ||
					(index > 0 && tools.ToolChoices[index-1] == value) {
					return PublicRequestContract{}, protocolError(ErrInvalidConfiguration, "public_anthropic_tools_contract_invalid")
				}
			}
		}
	}
	return contract, nil
}

type publicRequestDocument interface {
	canonicalBody() ([]byte, error)
	texts() []PublicText
	withTexts([]string) (publicRequestDocument, error)
}

// PublicPreparedRequest is an immutable, provider-independent request. It is
// safe to hash and inspect before a route or provider credential is selected.
type PublicPreparedRequest struct {
	identity        PublicContractIdentity
	endpointKind    EndpointKind
	requestedModel  string
	maxOutput       int64
	requestContract PublicRequestContract
	document        publicRequestDocument
	semantics       PublicInvocationSemantics
}

// PublicInvocationSemantics is derived by the same typed preparation that
// creates the immutable request document. Gateway routing consumes it instead
// of re-parsing raw client JSON into a second capability authority.
type PublicInvocationSemantics struct {
	ReasoningControlExplicit  bool
	ReasoningControlEffective string
	ReasoningEffortExplicit   bool
	ReasoningEffortEffective  string
	ReasoningBudgetExplicit   bool
	ReasoningBudgetEffective  int64
	Streaming                 bool
	FunctionToolsRequested    bool
	DeclaredToolNames         []string
	ToolChoiceExplicit        bool
	ToolChoiceEffective       string
	NamedToolChoice           string
	ParallelToolUseExplicit   bool
	ParallelToolUseEffective  bool
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

func (request *PublicPreparedRequest) RequestContract() PublicRequestContract {
	if request == nil {
		return PublicRequestContract{}
	}
	return clonePublicRequestContract(request.requestContract)
}

func (request *PublicPreparedRequest) InvocationSemantics() PublicInvocationSemantics {
	if request == nil {
		return PublicInvocationSemantics{}
	}
	result := request.semantics
	result.DeclaredToolNames = slices.Clone(request.semantics.DeclaredToolNames)
	return result
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
	PrepareRequest(body []byte, requestedModel string, contract ...PublicRequestContract) (*PublicPreparedRequest, error)
	BuildUpstream(context.Context, *PublicPreparedRequest, PublicUpstreamInput) (*PublicUpstreamRequest, error)
	parseDelivery(context.Context, *publicResponseBinding, int, []byte) (PublicDelivery, error)
	ClassifyHTTPStatus(statusCode int) (failureKind string, retryable bool)
}

type publicResponseBinding struct {
	identity            PublicContractIdentity
	base                *Binding
	requestedModel      string
	mappedModel         string
	requestContract     PublicRequestContract
	invocationSemantics PublicInvocationSemantics
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
		publicOpenAIResponsesProfile{
			base: openAIResponsesProfile{},
			identity: publicIdentity(
				modeloperation.IDResponses,
				modeloperation.PathResponses,
				ProfileOpenAIResponsesText,
				"1.0.0",
			),
		},
		publicOpenAIResponsesProfile{
			base: openAIResponsesProfileV2{},
			identity: PublicContractIdentity{
				ModelOperationID:           string(modeloperation.IDResponses),
				PublicPath:                 modeloperation.PathResponses,
				ExecutorContractVersion:    "unirouter.endpoint-profile-responses-dual-executor.v1",
				ProfileID:                  ProfileOpenAIResponsesTextV2,
				ProfileVersion:             "1.0.0",
				DeliveryContractVersion:    PublicResponsesDeliveryContractV2,
				ChargeContractVersion:      PublicCustomerChargeContractV1,
				IdempotencyContractVersion: PublicIdempotencyContractV1,
			},
		},
		publicAnthropicMessagesProfile{base: anthropicMessagesProfile{}},
		publicAnthropicMessagesProfile{
			base: anthropicMessagesProfile{},
			identity: PublicContractIdentity{
				ModelOperationID:           string(modeloperation.IDMessages),
				PublicPath:                 modeloperation.PathMessages,
				ExecutorContractVersion:    PublicMessagesDualExecutorContractV1,
				ProfileID:                  ProfileAnthropicMessagesTextV2,
				ProfileVersion:             "1.0.0",
				DeliveryContractVersion:    PublicAnthropicMessagesDeliveryContractV2,
				ChargeContractVersion:      PublicCustomerChargeContractV1,
				IdempotencyContractVersion: PublicIdempotencyContractV1,
			},
		},
		publicAnthropicMessagesProfile{
			base: anthropicMessagesProfile{},
			identity: PublicContractIdentity{
				ModelOperationID:           string(modeloperation.IDMessages),
				PublicPath:                 modeloperation.PathMessages,
				ExecutorContractVersion:    PublicMessagesToolsExecutorContractV1,
				ProfileID:                  ProfileAnthropicMessagesTextV3,
				ProfileVersion:             "1.0.0",
				DeliveryContractVersion:    PublicAnthropicMessagesToolsDeliveryV1,
				ChargeContractVersion:      PublicCustomerChargeContractV1,
				IdempotencyContractVersion: PublicIdempotencyContractV1,
			},
		},
	)
}

// PublicStreamRegistry is an exact transport-specific executor index. The
// immutable Responses v2 public identity is shared with the synchronous
// registry, while this registry fixes stream=true before request preparation.
type PublicStreamRegistry struct {
	profiles map[PublicContractIdentity]PublicNonStreamProfile
}

func NewPublicStreamRegistry() *PublicStreamRegistry {
	responses := publicOpenAIResponsesProfile{
		base: openAIResponsesProfileV2{},
		identity: PublicContractIdentity{
			ModelOperationID:           string(modeloperation.IDResponses),
			PublicPath:                 modeloperation.PathResponses,
			ExecutorContractVersion:    "unirouter.endpoint-profile-responses-dual-executor.v1",
			ProfileID:                  ProfileOpenAIResponsesTextV2,
			ProfileVersion:             "1.0.0",
			DeliveryContractVersion:    PublicResponsesDeliveryContractV2,
			ChargeContractVersion:      PublicCustomerChargeContractV1,
			IdempotencyContractVersion: PublicIdempotencyContractV1,
		},
		streaming: true,
	}
	messages := publicAnthropicMessagesProfile{
		base: anthropicMessagesProfile{},
		identity: PublicContractIdentity{
			ModelOperationID:           string(modeloperation.IDMessages),
			PublicPath:                 modeloperation.PathMessages,
			ExecutorContractVersion:    PublicMessagesDualExecutorContractV1,
			ProfileID:                  ProfileAnthropicMessagesTextV2,
			ProfileVersion:             "1.0.0",
			DeliveryContractVersion:    PublicAnthropicMessagesDeliveryContractV2,
			ChargeContractVersion:      PublicCustomerChargeContractV1,
			IdempotencyContractVersion: PublicIdempotencyContractV1,
		},
		streaming: true,
	}
	messagesTools := publicAnthropicMessagesProfile{
		base: anthropicMessagesProfile{},
		identity: PublicContractIdentity{
			ModelOperationID:           string(modeloperation.IDMessages),
			PublicPath:                 modeloperation.PathMessages,
			ExecutorContractVersion:    PublicMessagesToolsExecutorContractV1,
			ProfileID:                  ProfileAnthropicMessagesTextV3,
			ProfileVersion:             "1.0.0",
			DeliveryContractVersion:    PublicAnthropicMessagesToolsDeliveryV1,
			ChargeContractVersion:      PublicCustomerChargeContractV1,
			IdempotencyContractVersion: PublicIdempotencyContractV1,
		},
		streaming: true,
	}
	return &PublicStreamRegistry{profiles: map[PublicContractIdentity]PublicNonStreamProfile{
		responses.identity:     responses,
		messages.identity:      messages,
		messagesTools.identity: messagesTools,
	}}
}

func (registry *PublicStreamRegistry) Resolve(identity PublicContractIdentity) (PublicNonStreamProfile, error) {
	if registry == nil {
		return nil, protocolError(ErrInvalidConfiguration, "public_stream_registry_unavailable")
	}
	if !identity.valid() {
		return nil, protocolError(ErrInvalidConfiguration, "public_contract_identity_invalid")
	}
	profile, exists := registry.profiles[identity]
	if !exists || profile == nil || !profile.Descriptor().Streaming {
		return nil, protocolError(ErrPublicContractMismatch, "public_stream_contract_not_compiled")
	}
	return profile, nil
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
			identity:        prepared.identity,
			base:            binding,
			requestedModel:  prepared.requestedModel,
			mappedModel:     input.MappedModel,
			requestContract: clonePublicRequestContract(prepared.requestContract),
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
