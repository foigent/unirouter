package gateway

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func TestClaudeSonnet5ToolsContractProjectsTypedInvocationIntoOnePlanAuthority(t *testing.T) {
	profile, ok := modelprofile.Lookup("anthropic.claude-sonnet-5.api", "1.4.0")
	require.True(t, ok)
	binding, err := modelqualification.CompiledBindingFor(
		modelprofile.OperationAnthropicMessage, profile.ProtocolProfile, profile.ProtocolProfileVersion,
	)
	require.NoError(t, err)
	identity, err := modelqualification.CompiledPublicBindingFor(binding)
	require.NoError(t, err)
	contract, err := publicRequestContractForProfile(profile, modelprofile.OperationAnthropicMessage, identity)
	require.NoError(t, err)
	require.NotNil(t, contract.AnthropicMessages)
	require.NotNil(t, contract.AnthropicMessages.FunctionTools)
	require.Equal(t, []string{"any", "auto", "none", "tool"}, contract.AnthropicMessages.FunctionTools.ToolChoices)

	publicProfile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	require.NoError(t, err)
	prepared, err := publicProfile.PrepareRequest([]byte(`{
		"model":"public-claude","max_tokens":64,
		"tools":[{"name":"weather","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"any","disable_parallel_tool_use":true},
		"messages":[{"role":"user","content":"hello"}]
	}`), "public-claude", contract)
	require.NoError(t, err)
	requirements, err := publicInvocationCapabilityRequirements(profile, prepared)
	require.NoError(t, err)
	slices.SortFunc(requirements, func(left, right modelrouting.CapabilityRequirement) int {
		return strings.Compare(left.CapabilityID, right.CapabilityID)
	})
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityToolChoiceReq, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}, FunctionToolChoice: toolChoiceConstraint([]string{"weather"}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, requirements)

	normalized, err := modelrouting.ResolveCapabilityRequirements(profile, modelrouting.Request{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: 11, PublicModel: "public-claude",
		ModelOperationID: modelprofile.OperationAnthropicMessage, SelectionKey: "request-1",
		InvocationCapabilities: requirements,
	}, modelprofile.Definition{})
	require.NoError(t, err)
	require.Equal(t, requirements, normalized)
}

func TestClaudeSonnet5ToolsRuntimeUsesExactBindingUsageAndSettlement(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	fixture.planner.plan = publicRuntimePlanForProfileOperation(
		t, 1, "anthropic.claude-sonnet-5.api", "1.4.0", modeloperation.IDMessages,
	)
	fixture.planner.plan.PublicModel = "public-claude"
	fixture.request.Operation = modeloperation.IDMessages
	fixture.request.Path = modeloperation.PathMessages
	fixture.request.PublicModel = "public-claude"
	fixture.request.ResolvedModel = "public-claude"
	fixture.request.Body = []byte(`{
		"model":"public-claude","max_tokens":64,
		"tools":[{"name":"weather","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"any","disable_parallel_tool_use":true},
		"messages":[{"role":"user","content":"hello"}]
	}`)
	fixture.orchestrator.doer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		*fixture.planner.events = append(*fixture.planner.events, "provider:"+request.URL.Host)
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		fixture.providerBodies[request.URL.Host] = string(body)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
			"id":"msg_tool","type":"message","role":"assistant","model":"provider-private-model",
			"content":[{"type":"thinking","thinking":"","signature":"opaque"},{"type":"tool_use","id":"toolu_1","name":"weather","input":{"city":"Paris"}}],
			"stop_reason":"tool_use","stop_sequence":null,
			"usage":{"input_tokens":10,"output_tokens":4,"output_tokens_details":{"thinking_tokens":1}}
		}`))}, nil
	})
	writer := &publicRuntimeWriter{events: &events}
	require.NoError(t, fixture.orchestrator.execute(t.Context(), writer, fixture.request))

	route := fixture.planner.plan.Routes[0]
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesTextV3, route.ProtocolProfileID)
	require.Equal(t, modelqualification.MessagesToolsExecutorContractV1, route.ExecutorContractVersion)
	require.Equal(t, endpointprotocol.PublicAnthropicMessagesToolsDeliveryV1, route.PublicEvidence.DeliveryContractVersion)
	require.Contains(t, fixture.providerBodies["provider-1.example"], `"tool_choice":{"type":"any","disable_parallel_tool_use":true}`)
	require.Contains(t, writer.body.String(), `"model":"public-claude"`)
	require.Contains(t, writer.body.String(), `"type":"tool_use"`)
	require.Len(t, fixture.billing.freeze, 1)
	require.Equal(t, fixture.planner.plan.SnapshotHash, fixture.billing.freeze[0].ExecutionSnapshotHash)
	require.Len(t, fixture.billing.settled, 1)
	require.Equal(t, 10, fixture.billing.settled[0].PromptTokens)
	require.Equal(t, 4, fixture.billing.settled[0].CompletionTokens)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityToolChoiceReq, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}, FunctionToolChoice: toolChoiceConstraint([]string{"weather"}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, fixture.planner.requirements)
}

func TestClaudeSonnet5InvalidToolResponseCannotSettleOrDeliver(t *testing.T) {
	events := []string{}
	fixture := newPublicRuntimeFixture(t, &events, 1)
	fixture.planner.plan = publicRuntimePlanForProfileOperation(
		t, 1, "anthropic.claude-sonnet-5.api", "1.4.0", modeloperation.IDMessages,
	)
	fixture.planner.plan.PublicModel = "public-claude"
	fixture.request.Operation = modeloperation.IDMessages
	fixture.request.Path = modeloperation.PathMessages
	fixture.request.PublicModel = "public-claude"
	fixture.request.ResolvedModel = "public-claude"
	fixture.request.Body = []byte(`{"model":"public-claude","max_tokens":64,"tools":[{"name":"weather","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]}`)
	fixture.orchestrator.doer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		*fixture.planner.events = append(*fixture.planner.events, "provider:"+request.URL.Host)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"msg_bad","type":"message","role":"assistant","model":"provider-private-model","content":[{"type":"tool_use","id":"toolu_1","name":"undeclared","input":{}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`))}, nil
	})
	err := fixture.orchestrator.execute(t.Context(), &publicRuntimeWriter{events: &events}, fixture.request)
	require.Error(t, err)
	require.Empty(t, fixture.billing.settled)
	require.Empty(t, fixture.billing.prepared)
	require.Empty(t, fixture.billing.deliveries)
	require.Len(t, fixture.billing.completions, 1)
	require.Equal(t, billing.ProviderAttemptOutcomeResponseUnusable, fixture.billing.completions[0].Outcome)
}
