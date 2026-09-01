package adapters

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func exactKimiRuntimeStreamIdentity(profileID, profileVersion, model, protocol string) KimiChatStreamRuntimeIdentity {
	return KimiChatStreamRuntimeIdentity{
		OperationID: modelprofile.OperationOpenAIChat, ExecutionProfileID: profileID,
		ExecutionProfileVersion: profileVersion, Provider: "kimi", UpstreamModel: model,
		ProtocolProfileID: protocol, ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
		CacheUsageProfile: kimiChatCacheUsageProfile,
	}
}

func TestSplitSSELinesAndAssemblerOwnWireFraming(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(": keepalive\rdata: first\r\ndata: second\n\rdata: third"))
	scanner.Split(SplitSSELines)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, []string{": keepalive", "data: first", "data: second", "", "data: third"}, lines)

	assembler, err := NewChatSSEEventAssembler(32)
	require.NoError(t, err)
	for _, line := range []string{": comment", "data: first", "data:second"} {
		_, ready, acceptErr := assembler.AcceptLine(line)
		require.NoError(t, acceptErr)
		require.False(t, ready)
	}
	event, ready, err := assembler.AcceptLine("")
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, "first\nsecond", string(event.Data))
	_, _, err = assembler.AcceptLine("event: message")
	require.ErrorIs(t, err, ErrChatSSEUnsupportedField)

	tooSmall, err := NewChatSSEEventAssembler(3)
	require.NoError(t, err)
	_, _, err = tooSmall.AcceptLine("data: four")
	require.ErrorIs(t, err, ErrChatSSEEventTooLarge)
}

func TestCurrentKimiChatStreamMechanicsRequireExactRuntimeAndProbeBindings(t *testing.T) {
	profiles := []struct {
		profileID, profileVersion, model, protocol, probeVersion string
	}{
		{"kimi.k2.6.api", "1.7.0", "kimi-k2.6", "protocol.kimi-openai-chat.v5", "3.0.0"},
		{"kimi.k2.7-code.api", "1.3.0", "kimi-k2.7-code", "protocol.kimi-openai-chat.v5", "4.0.0"},
		{"kimi.k3.api", "1.9.0", "kimi-k3", "protocol.kimi-openai-chat.k3.v5", "4.0.0"},
	}
	for _, profile := range profiles {
		runtimeIdentity := exactKimiRuntimeStreamIdentity(profile.profileID, profile.profileVersion, profile.model, profile.protocol)
		_, found, err := ResolveKimiRuntimeChatStreamMechanics(runtimeIdentity)
		require.NoError(t, err)
		require.True(t, found)

		probeIdentity := KimiChatStreamProbeIdentity{
			OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityStreaming,
			ExecutionProfileID: profile.profileID, ExecutionProfileVersion: profile.profileVersion,
			ProtocolProfileID: profile.protocol, ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
			ProbeContractID: "unirouter.capability-probe.kimi-chat-sse.v1", ProbeContractVersion: profile.probeVersion,
			ExpectedProvider: "kimi", ExpectedUpstreamModel: profile.model, PublicPath: "/v1/chat/completions",
		}
		_, found, err = ResolveKimiProbeChatStreamMechanics(probeIdentity)
		require.NoError(t, err)
		require.True(t, found)

		runtimeIdentity.ProtocolProfileVersion = "2.0.0"
		_, found, err = ResolveKimiRuntimeChatStreamMechanics(runtimeIdentity)
		require.Error(t, err)
		require.False(t, found)
		probeIdentity.ExecutionProfileVersion = "0.0.0"
		_, found, err = ResolveKimiProbeChatStreamMechanics(probeIdentity)
		require.Error(t, err)
		require.False(t, found)
	}

	_, found, err := ResolveKimiRuntimeChatStreamMechanics(exactKimiRuntimeStreamIdentity(
		"kimi.k3.api", "1.8.0", "kimi-k3", "protocol.kimi-openai-chat.k3.v4",
	))
	require.NoError(t, err)
	require.False(t, found, "historical K3 keeps its existing fallback")
}

func TestCurrentKimiChatStreamPreparationAndStateConvergeUsageAndDone(t *testing.T) {
	mechanics, found, err := ResolveKimiRuntimeChatStreamMechanics(exactKimiRuntimeStreamIdentity(
		"kimi.k2.6.api", "1.7.0", "kimi-k2.6", "protocol.kimi-openai-chat.v5",
	))
	require.NoError(t, err)
	require.True(t, found)
	state, err := NewKimiChatStreamState(mechanics)
	require.NoError(t, err)

	first, err := PrepareKimiChatStreamEvent([]byte(`{"model":"kimi-k2.6","choices":[{"index":0,"delta":{"content":"ok","reasoning_content":""},"finish_reason":null}]}`), mechanics)
	require.NoError(t, err)
	require.True(t, first.Changed)
	require.NotContains(t, string(first.Body), "reasoning_content")
	require.NoError(t, state.Accept(first))

	usage := `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cached_tokens":4}`
	terminal, err := PrepareKimiChatStreamEvent([]byte(`{"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":`+usage+`}]}`), mechanics)
	require.NoError(t, err)
	require.NoError(t, state.Accept(terminal))
	usageOnly, err := PrepareKimiChatStreamEvent([]byte(`{"model":"kimi-k2.6","choices":[],"usage":`+usage+`}`), mechanics)
	require.NoError(t, err)
	require.True(t, usageOnly.UsageOnly)
	require.NoError(t, state.Accept(usageOnly))
	require.NoError(t, state.AcceptDone())
	require.NoError(t, state.FinishEOF())
	require.EqualValues(t, 4, usageOnly.TopLevelUsage.CacheReadTokens)

	duplicate := []byte(`{"choices":[],"usage":{"prompt_tokens":1,"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"cached_tokens":0}}`)
	_, err = PrepareKimiChatStreamEvent(duplicate, mechanics)
	require.Equal(t, KimiChatStreamIssuePreparation, KimiChatStreamIssueForError(err))
}

func TestCurrentKimiChatStreamMechanicsKeepRuntimeAndProbeFinishSetsDistinct(t *testing.T) {
	runtime, found, err := ResolveKimiRuntimeChatStreamMechanics(exactKimiRuntimeStreamIdentity(
		"kimi.k3.api", "1.9.0", "kimi-k3", "protocol.kimi-openai-chat.k3.v5",
	))
	require.NoError(t, err)
	require.True(t, found)
	for _, finish := range []string{"stop", "length", "content_filter"} {
		_, err = PrepareKimiChatStreamEvent([]byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"`+finish+`"}]}`), runtime)
		require.NoError(t, err, finish)
	}
	_, err = PrepareKimiChatStreamEvent([]byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`), runtime)
	require.Equal(t, KimiChatStreamIssueFinishReason, KimiChatStreamIssueForError(err))

	probe, found, err := ResolveKimiProbeChatStreamMechanics(KimiChatStreamProbeIdentity{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityStreaming,
		ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.9.0",
		ProtocolProfileID: "protocol.kimi-openai-chat.k3.v5", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
		ProbeContractID: "unirouter.capability-probe.kimi-chat-sse.v1", ProbeContractVersion: "4.0.0",
		ExpectedProvider: "kimi", ExpectedUpstreamModel: "kimi-k3", PublicPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	require.True(t, found)
	_, err = PrepareKimiChatStreamEvent([]byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`), probe)
	require.Equal(t, KimiChatStreamIssueFinishReason, KimiChatStreamIssueForError(err))
}

func TestCurrentKimiProbePreparationPreservesMissingAndNullChoicesAsUsageOnly(t *testing.T) {
	mechanics, found, err := ResolveKimiProbeChatStreamMechanics(KimiChatStreamProbeIdentity{
		OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityStreaming,
		ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
		ProtocolProfileID: "protocol.kimi-openai-chat.v5", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
		ProbeContractID: "unirouter.capability-probe.kimi-chat-sse.v1", ProbeContractVersion: "3.0.0",
		ExpectedProvider: "kimi", ExpectedUpstreamModel: "kimi-k2.6", PublicPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	require.True(t, found)

	for _, raw := range []string{
		`{"model":"kimi-k2.6","usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"cached_tokens":0}}`,
		`{"model":"kimi-k2.6","choices":null,"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"cached_tokens":0}}`,
	} {
		prepared, prepareErr := PrepareKimiChatStreamEvent([]byte(raw), mechanics)
		require.NoError(t, prepareErr)
		require.Zero(t, prepared.ChoiceCount)
		require.True(t, prepared.UsageOnly)
	}
}

func TestCurrentKimiChatStreamStateRejectsIncompleteAndConflictingAuthority(t *testing.T) {
	mechanics, found, err := ResolveKimiRuntimeChatStreamMechanics(exactKimiRuntimeStreamIdentity(
		"kimi.k3.api", "1.9.0", "kimi-k3", "protocol.kimi-openai-chat.k3.v5",
	))
	require.NoError(t, err)
	require.True(t, found)
	prepare := func(raw string) KimiChatStreamPrepared {
		prepared, prepareErr := PrepareKimiChatStreamEvent([]byte(raw), mechanics)
		require.NoError(t, prepareErr)
		return prepared
	}

	state, err := NewKimiChatStreamState(mechanics)
	require.NoError(t, err)
	err = state.Accept(prepare(`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cached_tokens":0}}`))
	require.Equal(t, KimiChatStreamIssueUsageOrder, KimiChatStreamIssueForError(err))
	require.Equal(t, KimiChatStreamIssueTerminalOrder, KimiChatStreamIssueForError(state.FinishEOF()))

	state, err = NewKimiChatStreamState(mechanics)
	require.NoError(t, err)
	require.NoError(t, state.Accept(prepare(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"cached_tokens":0}}]}`)))
	err = state.Accept(prepare(`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4,"cached_tokens":0}}`))
	require.Equal(t, KimiChatStreamIssueUsageConflict, KimiChatStreamIssueForError(err))
	require.Equal(t, KimiChatStreamIssueTerminalOrder, KimiChatStreamIssueForError(state.AcceptDone()))
}
