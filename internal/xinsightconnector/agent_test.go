package xinsightconnector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type agentTestCounterpart struct {
	operations [][]byte
	pollIndex  int
	ackErrors  []error
	acks       [][]byte
}

func (counterpart *agentTestCounterpart) Poll(context.Context) ([]byte, bool, error) {
	if counterpart.pollIndex >= len(counterpart.operations) {
		return nil, false, nil
	}
	operation := bytes.Clone(counterpart.operations[counterpart.pollIndex])
	counterpart.pollIndex++
	return operation, true, nil
}

func (counterpart *agentTestCounterpart) Acknowledge(_ context.Context, result []byte) error {
	counterpart.acks = append(counterpart.acks, bytes.Clone(result))
	index := len(counterpart.acks) - 1
	if index < len(counterpart.ackErrors) {
		return counterpart.ackErrors[index]
	}
	return nil
}

type agentTestJournalRecord struct {
	operation     ControlOperationV2
	requestSHA256 string
	claimToken    string
	terminal      []byte
	resultSHA256  string
	acknowledged  bool
}

type agentTestJournal struct {
	records       map[string]*agentTestJournalRecord
	acquireCalls  int
	completeCalls int
	markAckCalls  int
}

func newAgentTestJournal() *agentTestJournal {
	return &agentTestJournal{records: make(map[string]*agentTestJournalRecord)}
}

func (journal *agentTestJournal) Acquire(
	_ context.Context,
	operation ControlOperationV2,
	requestSHA256 string,
) (PhaseJournalAcquisition, error) {
	journal.acquireCalls++
	predecessor := journal.predecessor(operation.PredecessorOperationID)
	if existing := journal.records[operation.OperationID]; existing != nil {
		if existing.requestSHA256 != requestSHA256 ||
			existing.operation.InstallationID != operation.InstallationID ||
			existing.operation.Phase != operation.Phase {
			return PhaseJournalAcquisition{}, ErrConflict
		}
		if len(existing.terminal) == 0 {
			return PhaseJournalAcquisition{}, ErrConflict
		}
		return PhaseJournalAcquisition{
			PredecessorCanonicalResult: predecessor,
			TerminalCanonicalResult:    bytes.Clone(existing.terminal),
		}, nil
	}
	claim := "claim:" + operation.OperationID
	journal.records[operation.OperationID] = &agentTestJournalRecord{
		operation: operation, requestSHA256: requestSHA256, claimToken: claim,
	}
	return PhaseJournalAcquisition{
		ClaimToken:                 claim,
		PredecessorCanonicalResult: predecessor,
	}, nil
}

func (journal *agentTestJournal) Complete(
	_ context.Context,
	operation ControlOperationV2,
	requestSHA256, claimToken string,
	canonicalResult []byte,
	resultSHA256 string,
) error {
	record := journal.records[operation.OperationID]
	if record == nil || record.requestSHA256 != requestSHA256 || record.claimToken != claimToken ||
		len(record.terminal) != 0 || hashBytes(canonicalResult) != resultSHA256 {
		return ErrConflict
	}
	journal.completeCalls++
	record.terminal = bytes.Clone(canonicalResult)
	record.resultSHA256 = resultSHA256
	return nil
}

func (journal *agentTestJournal) MarkAcknowledged(
	_ context.Context,
	operation ControlOperationV2,
	requestSHA256, resultSHA256 string,
) error {
	record := journal.records[operation.OperationID]
	if record == nil || record.requestSHA256 != requestSHA256 ||
		record.resultSHA256 != resultSHA256 || len(record.terminal) == 0 {
		return ErrConflict
	}
	journal.markAckCalls++
	record.acknowledged = true
	return nil
}

func (journal *agentTestJournal) predecessor(operationID string) []byte {
	if operationID == "" {
		return nil
	}
	if record := journal.records[operationID]; record != nil {
		return bytes.Clone(record.terminal)
	}
	return nil
}

type agentTestExecutor struct {
	results       map[string]ControlResultV2
	calls         map[string]int
	sourceState   string
	receiverState string
}

func newAgentTestExecutor(t *testing.T) *agentTestExecutor {
	t.Helper()
	root := filepath.Join("testdata", "control-v2", "valid")
	return &agentTestExecutor{
		results: map[string]ControlResultV2{
			ControlPhasePreparePaused:    mustDecodeControlV2Result(t, filepath.Join(root, "02-prepare-paused-source-result.json")),
			ControlPhaseRegisterReceiver: mustDecodeControlV2Result(t, filepath.Join(root, "04-register-receiver-result.json")),
			ControlPhaseResumeSource:     mustDecodeControlV2Result(t, filepath.Join(root, "06-resume-source-result.json")),
		},
		calls: make(map[string]int),
	}
}

func (executor *agentTestExecutor) Execute(
	_ context.Context,
	operation ControlOperationV2,
	predecessor *ControlResultV2,
) (ControlResultV2, error) {
	executor.calls[operation.Phase]++
	switch operation.Phase {
	case ControlPhasePreparePaused:
		if predecessor != nil || executor.sourceState != "" {
			return ControlResultV2{}, fmt.Errorf("prepare barrier violated")
		}
		executor.sourceState = DesiredPaused
	case ControlPhaseRegisterReceiver:
		if predecessor == nil || predecessor.Phase != ControlPhasePreparePaused ||
			executor.sourceState != DesiredPaused || executor.receiverState != "" {
			return ControlResultV2{}, fmt.Errorf("register barrier violated")
		}
		executor.receiverState = "feed_staged"
	case ControlPhaseResumeSource:
		if predecessor == nil || predecessor.Phase != ControlPhaseRegisterReceiver ||
			executor.sourceState != DesiredPaused || executor.receiverState != "feed_staged" {
			return ControlResultV2{}, fmt.Errorf("resume barrier violated")
		}
		executor.sourceState = DesiredActive
		executor.receiverState = DesiredActive
	default:
		return ControlResultV2{}, fmt.Errorf("unexpected phase %q", operation.Phase)
	}
	return executor.results[operation.Phase], nil
}

func TestAgentRunsFrozenThreePhaseFakeLifecycle(t *testing.T) {
	counterpart := &agentTestCounterpart{operations: agentTestThreePhaseOperations(t)}
	journal := newAgentTestJournal()
	executor := newAgentTestExecutor(t)
	agent, err := NewAgent("xci_AAAAAAAAAAAAAAAAAAAAAA", counterpart, journal, executor)
	require.NoError(t, err)

	for range 3 {
		processed, err := agent.RunOnce(context.Background())
		require.True(t, processed)
		require.NoError(t, err)
	}
	processed, err := agent.RunOnce(context.Background())
	require.NoError(t, err)
	require.False(t, processed)

	require.Equal(t, DesiredActive, executor.sourceState)
	require.Equal(t, DesiredActive, executor.receiverState)
	require.Equal(t, map[string]int{
		ControlPhasePreparePaused: 1, ControlPhaseRegisterReceiver: 1, ControlPhaseResumeSource: 1,
	}, executor.calls)
	require.Equal(t, 3, journal.completeCalls)
	require.Equal(t, 3, journal.markAckCalls)
	require.Len(t, counterpart.acks, 3)
	for _, raw := range append(append([][]byte{}, counterpart.operations...), counterpart.acks...) {
		assertAgentWireHasNoReceiverSecret(t, raw)
	}
}

func TestAgentReplaysDurableTerminalAfterAcknowledgementLoss(t *testing.T) {
	operation := agentTestCanonicalFixture(t, "valid/01-prepare-paused-source-operation.json")
	ackLost := errors.New("ack response lost")
	counterpart := &agentTestCounterpart{
		operations: [][]byte{operation, operation},
		ackErrors:  []error{ackLost, nil},
	}
	journal := newAgentTestJournal()
	executor := newAgentTestExecutor(t)
	agent, err := NewAgent("xci_AAAAAAAAAAAAAAAAAAAAAA", counterpart, journal, executor)
	require.NoError(t, err)

	processed, err := agent.RunOnce(context.Background())
	require.True(t, processed)
	require.ErrorIs(t, err, ackLost)
	require.Zero(t, journal.markAckCalls, "lost acknowledgement must not mark the journal")
	processed, err = agent.RunOnce(context.Background())
	require.True(t, processed)
	require.NoError(t, err)

	require.Equal(t, 1, executor.calls[ControlPhasePreparePaused])
	require.Equal(t, 1, journal.completeCalls)
	require.Equal(t, 1, journal.markAckCalls)
	require.Len(t, counterpart.acks, 2)
	require.Equal(t, counterpart.acks[0], counterpart.acks[1])
}

func TestAgentRejectsOperationIdentityDriftBeforeReexecution(t *testing.T) {
	base := agentTestCanonicalFixture(t, "valid/01-prepare-paused-source-operation.json")
	tests := []struct {
		name  string
		drift []byte
	}{
		{
			name:  "same xco changed request",
			drift: bytes.Replace(base, []byte("project.second-integration"), []byte("project.changed-integration"), 1),
		},
		{
			name:  "same xco cross installation",
			drift: bytes.Replace(base, []byte("xci_AAAAAAAAAAAAAAAAAAAAAA"), []byte("xci_ZZZZZZZZZZZZZZZZZZZZZZ"), 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			counterpart := &agentTestCounterpart{operations: [][]byte{base, test.drift}}
			journal := newAgentTestJournal()
			executor := newAgentTestExecutor(t)
			agent, err := NewAgent("xci_AAAAAAAAAAAAAAAAAAAAAA", counterpart, journal, executor)
			require.NoError(t, err)
			processed, err := agent.RunOnce(context.Background())
			require.True(t, processed)
			require.NoError(t, err)
			processed, err = agent.RunOnce(context.Background())
			require.True(t, processed)
			require.ErrorIs(t, err, ErrConflict)
			require.Equal(t, 1, executor.calls[ControlPhasePreparePaused])
			require.Len(t, counterpart.acks, 1)
		})
	}
}

func TestAgentRejectsInvalidPredecessorBeforePhaseExecution(t *testing.T) {
	prepare := agentTestCanonicalFixture(t, "valid/01-prepare-paused-source-operation.json")
	register := agentTestCanonicalFixture(t, "valid/03-register-receiver-operation.json")
	tests := []struct {
		name     string
		register []byte
		code     string
		errIs    error
	}{
		{
			name: "wrong predecessor digest",
			register: bytes.Replace(register,
				[]byte("sha256:85602897ca158c594e658b6f7ef724259ce9acc8a639089093bc067d7ac22002"),
				[]byte("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 1),
			code: "predecessor_result_conflict",
		},
		{
			name: "cross installation predecessor",
			register: bytes.Replace(register,
				[]byte("xci_AAAAAAAAAAAAAAAAAAAAAA"), []byte("xci_ZZZZZZZZZZZZZZZZZZZZZZ"), 1),
			errIs: ErrConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			counterpart := &agentTestCounterpart{operations: [][]byte{prepare, test.register}}
			journal := newAgentTestJournal()
			executor := newAgentTestExecutor(t)
			agent, err := NewAgent("xci_AAAAAAAAAAAAAAAAAAAAAA", counterpart, journal, executor)
			require.NoError(t, err)
			processed, err := agent.RunOnce(context.Background())
			require.True(t, processed)
			require.NoError(t, err)
			processed, err = agent.RunOnce(context.Background())
			require.True(t, processed)
			if test.errIs != nil {
				require.ErrorIs(t, err, test.errIs)
			} else {
				require.Equal(t, test.code, ControlV2ErrorCode(err))
			}
			require.Zero(t, executor.calls[ControlPhaseRegisterReceiver])
			require.Len(t, counterpart.acks, 1)
		})
	}
}

func TestAgentRejectsSecretBearingOperationBeforeJournalOrExecution(t *testing.T) {
	raw := agentTestCanonicalFixture(t, "valid/03-register-receiver-operation.json")
	raw = bytes.Replace(raw,
		[]byte(`"required_source_state":"paused"}`),
		[]byte(`"required_source_state":"paused","receiver_bearer":"must-never-enter"}`), 1)
	counterpart := &agentTestCounterpart{operations: [][]byte{raw}}
	journal := newAgentTestJournal()
	executor := newAgentTestExecutor(t)
	agent, err := NewAgent("xci_AAAAAAAAAAAAAAAAAAAAAA", counterpart, journal, executor)
	require.NoError(t, err)

	processed, err := agent.RunOnce(context.Background())
	require.True(t, processed)
	require.Equal(t, "unknown_or_secret_field", ControlV2ErrorCode(err))
	require.Zero(t, journal.acquireCalls)
	require.Empty(t, executor.calls)
	require.Empty(t, counterpart.acks)
}

func agentTestThreePhaseOperations(t *testing.T) [][]byte {
	t.Helper()
	return [][]byte{
		agentTestCanonicalFixture(t, "valid/01-prepare-paused-source-operation.json"),
		agentTestCanonicalFixture(t, "valid/03-register-receiver-operation.json"),
		agentTestCanonicalFixture(t, "valid/05-resume-source-operation.json"),
	}
}

func agentTestCanonicalFixture(t *testing.T, relative string) []byte {
	t.Helper()
	raw := mustReadControlV2Fixture(t, filepath.Join("testdata", "control-v2", filepath.FromSlash(relative)))
	return bytes.TrimSuffix(raw, []byte{'\n'})
}

func assertAgentWireHasNoReceiverSecret(t *testing.T, raw []byte) {
	t.Helper()
	lower := strings.ToLower(string(raw))
	require.NotContains(t, lower, "receiver_bearer")
	require.NotContains(t, lower, `"bearer":`)
	require.NotContains(t, lower, "must-never-enter")
}
