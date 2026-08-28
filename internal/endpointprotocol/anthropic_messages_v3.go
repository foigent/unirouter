package endpointprotocol

import (
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

// anthropicMessagesProfileV3 is the append-only client-tools identity. It
// reuses the public typed Messages codec so fixture conformance and public
// execution cannot disagree about tool definitions, history, choice, or
// response evidence. V1 and V2 remain immutable and continue rejecting tools.
type anthropicMessagesProfileV3 struct{}

func (anthropicMessagesProfileV3) Descriptor() Descriptor {
	descriptor := (anthropicMessagesProfileV2{}).Descriptor()
	descriptor.ProfileID = ProfileAnthropicMessagesTextV3
	unsupported := make([]string, 0, len(descriptor.UnsupportedSemantics)+2)
	for _, semantic := range descriptor.UnsupportedSemantics {
		if semantic != "server_or_client_tools" {
			unsupported = append(unsupported, semantic)
		}
	}
	unsupported = append(unsupported, "server_tools", "streaming_client_tools")
	slices.Sort(unsupported)
	descriptor.UnsupportedSemantics = unsupported
	return descriptor
}

func anthropicMessagesV3PublicIdentity() PublicContractIdentity {
	return PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDMessages),
		PublicPath:                 modeloperation.PathMessages,
		ExecutorContractVersion:    PublicMessagesToolsExecutorContractV1,
		ProfileID:                  ProfileAnthropicMessagesTextV3,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    PublicAnthropicMessagesToolsDeliveryV1,
		ChargeContractVersion:      PublicCustomerChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
}

func anthropicMessagesV3FixtureContract() PublicRequestContract {
	return PublicRequestContract{
		ExecutionProfileID:      "endpointprotocol.anthropic-messages-tools-fixture",
		ExecutionProfileVersion: "1.0.0",
		AnthropicMessages: &PublicAnthropicMessagesContract{
			RequireFinalUserMessage: true,
			AllowedResponseStops:    []string{"end_turn", "max_tokens", "refusal", "stop_sequence", "tool_use"},
			RefusalStopDetails:      true,
			FunctionTools: &PublicAnthropicFunctionToolsContract{
				MaxTools: 64, ToolNamePattern: `^[a-zA-Z0-9_-]{1,64}$`,
				ToolChoices: []string{"any", "auto", "none", "tool"}, ProviderDefaultChoice: "auto",
				NamedChoice: true, ParallelToolUse: true, ParallelProviderDefault: true,
				ExactHistory: true, ExactResponse: true, RequiresNonStream: true,
				ToolInputIdentityGuarded: true, ThinkingHistoryIdentityOnly: true,
			},
		},
	}
}

func (anthropicMessagesProfileV3) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	envelope, err := objectKeys(input.Body)
	if err != nil {
		return nil, nil, err
	}
	streaming, err := exactAnthropicMessagesStreamValue(envelope)
	if err != nil {
		return nil, nil, err
	}
	publicProfile := publicAnthropicMessagesProfile{
		base: anthropicMessagesProfile{}, identity: anthropicMessagesV3PublicIdentity(), streaming: streaming,
	}
	prepared, err := publicProfile.PrepareRequest(input.Body, input.RequestedModel, anthropicMessagesV3FixtureContract())
	if err != nil {
		return nil, nil, err
	}
	upstream, err := publicProfile.BuildUpstream(ctx, prepared, PublicUpstreamInput{
		BaseURL: input.BaseURL, APIKey: input.APIKey, RequestID: input.RequestID, MappedModel: input.MappedModel,
	})
	if err != nil {
		return nil, nil, err
	}
	upstream.binding.base.publicContract = &upstream.binding.requestContract
	upstream.binding.base.publicSemantics = upstream.binding.invocationSemantics
	return upstream.request, upstream.binding.base, nil
}

func (anthropicMessagesProfileV3) ParseResponse(
	ctx context.Context,
	binding *Binding,
	statusCode int,
	body []byte,
) (Outcome, error) {
	profile := publicAnthropicMessagesProfile{base: anthropicMessagesProfile{}, identity: anthropicMessagesV3PublicIdentity()}
	if err := requireBinding(binding, (anthropicMessagesProfileV3{}).Descriptor()); err != nil {
		return Outcome{}, err
	}
	if binding.streaming {
		return Outcome{}, protocolError(ErrInvalidConfiguration, "streaming_response_requires_event_parser")
	}
	if binding.publicContract == nil {
		return Outcome{}, protocolError(ErrPublicContractMismatch, "public_request_contract_missing")
	}
	delivery, err := profile.parseDelivery(ctx, &publicResponseBinding{
		identity: anthropicMessagesV3PublicIdentity(), base: binding,
		requestedModel: binding.requestedModel, mappedModel: binding.mappedModel,
		requestContract:     clonePublicRequestContract(*binding.publicContract),
		invocationSemantics: binding.publicSemantics,
	}, statusCode, body)
	if err != nil {
		return Outcome{}, err
	}
	usage := delivery.CustomerUsage()
	return Outcome{
		ContractVersion: ContractVersion, ProfileID: ProfileAnthropicMessagesTextV3,
		ProviderState: "completed", ProviderObject: delivery.ProviderObject(), MappedModel: binding.mappedModel,
		OutputCount: 1,
		Units: []UnitObservation{
			observedUnit(UnitInputTokens, strconv.FormatInt(usage.InputTokens-usage.CacheReadTokens-usage.CacheWriteTokens, 10)),
			observedUnit(UnitCacheWriteTokens, strconv.FormatInt(usage.CacheWriteTokens, 10)),
			observedUnit(UnitCacheReadTokens, strconv.FormatInt(usage.CacheReadTokens, 10)),
			observedUnit(UnitOutputTokens, strconv.FormatInt(usage.OutputTokens, 10)),
		},
	}, nil
}

func (anthropicMessagesProfileV3) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return (anthropicMessagesProfile{}).ClassifyHTTPStatus(statusCode)
}
