package xinsightconnector

import (
	"bytes"
	"context"
	"fmt"
)

// ControlCounterpart is the product-local transport seam for the fixed
// X-Insight management API. It deliberately carries no URL or HTTP path: the
// later runtime wiring owns that fixed network binding. Poll returns the exact
// operation bytes supplied by X, and Acknowledge receives only canonical
// terminal-result bytes.
type ControlCounterpart interface {
	Poll(context.Context) (rawOperation []byte, found bool, err error)
	Acknowledge(context.Context, []byte) error
}

// PhaseExecutor applies one already-validated phase to UniRouter-owned product
// state. Implementations must be idempotent because a crash after the product
// action but before journal completion can cause a fenced lease recovery.
// CP2A uses a fake implementation; the completed-feed lifecycle is not bound.
type PhaseExecutor interface {
	Execute(context.Context, ControlOperationV2, *ControlResultV2) (ControlResultV2, error)
}

// PhaseJournalAcquisition is either an exclusive execution claim or a durable
// terminal replay. A non-first phase also carries the exact canonical terminal
// result of its immediate predecessor.
type PhaseJournalAcquisition struct {
	ClaimToken                 string
	PredecessorCanonicalResult []byte
	TerminalCanonicalResult    []byte
}

// PhaseJournal owns operation identity, request-digest conflict detection,
// predecessor lookup, execution fencing, and durable terminal replay. Complete
// must compare the claim token and request digest before making the terminal
// result visible.
type PhaseJournal interface {
	Acquire(context.Context, ControlOperationV2, string) (PhaseJournalAcquisition, error)
	Complete(
		context.Context,
		ControlOperationV2,
		string,
		string,
		[]byte,
		string,
	) error
	MarkAcknowledged(context.Context, ControlOperationV2, string, string) error
}

// Agent consumes exactly one X-owned control-v2 operation at a time. Terminal
// state is committed before acknowledgement, so an acknowledgement loss replays
// the same canonical result without executing the product phase twice.
type Agent struct {
	installationID        string
	counterpart           ControlCounterpart
	journal               PhaseJournal
	executor              PhaseExecutor
	transactionalJournal  *PostgresPhaseJournal
	transactionalExecutor PhaseTxExecutor
}

func NewAgent(
	installationID string,
	counterpart ControlCounterpart,
	journal PhaseJournal,
	executor PhaseExecutor,
) (*Agent, error) {
	if !validControlExternalIdentity(installationID, "xci_") ||
		counterpart == nil || journal == nil || executor == nil {
		return nil, fmt.Errorf("%w: connector agent dependencies are required", ErrInvalid)
	}
	return &Agent{
		installationID: installationID,
		counterpart:    counterpart, journal: journal, executor: executor,
	}, nil
}

// NewTransactionalAgent wires the same polling/acknowledgement loop to the
// production-safe execution seam: each product effect and its immutable
// terminal result commit in one PostgreSQL transaction. NewAgent remains the
// fake/idempotent seam used by CP2A contract tests.
func NewTransactionalAgent(
	installationID string,
	counterpart ControlCounterpart,
	journal *PostgresPhaseJournal,
	executor PhaseTxExecutor,
) (*Agent, error) {
	if !validControlExternalIdentity(installationID, "xci_") ||
		counterpart == nil || journal == nil || executor == nil {
		return nil, fmt.Errorf("%w: transactional connector agent dependencies are required", ErrInvalid)
	}
	return &Agent{
		installationID:        installationID,
		counterpart:           counterpart,
		journal:               journal,
		transactionalJournal:  journal,
		transactionalExecutor: executor,
	}, nil
}

// RunOnce polls and processes at most one operation. processed reports whether
// the counterpart returned an operation, including when validation or
// acknowledgement failed.
func (agent *Agent) RunOnce(ctx context.Context) (processed bool, err error) {
	legacyExecution := agent != nil && agent.executor != nil &&
		agent.transactionalJournal == nil && agent.transactionalExecutor == nil
	transactionalExecution := agent != nil && agent.executor == nil &&
		agent.transactionalJournal != nil && agent.transactionalExecutor != nil &&
		agent.journal == agent.transactionalJournal
	if agent == nil || !validControlExternalIdentity(agent.installationID, "xci_") ||
		agent.counterpart == nil || agent.journal == nil ||
		(!legacyExecution && !transactionalExecution) {
		return false, fmt.Errorf("%w: connector agent is unavailable", ErrUnavailable)
	}
	rawOperation, found, err := agent.counterpart.Poll(ctx)
	if err != nil || !found {
		return false, err
	}

	operation, err := DecodeControlOperationV2(rawOperation)
	if err != nil {
		return true, err
	}
	if operation.InstallationID != agent.installationID {
		return true, ErrConflict
	}
	canonicalOperation, err := operation.CanonicalJSON()
	if err != nil {
		return true, err
	}
	if !bytes.Equal(rawOperation, canonicalOperation) {
		return true, controlV2Error("operation_noncanonical", nil)
	}
	requestSHA256 := hashBytes(canonicalOperation)

	acquisition, err := agent.journal.Acquire(ctx, operation, requestSHA256)
	if err != nil {
		return true, err
	}
	predecessor, err := decodeAgentPredecessor(acquisition.PredecessorCanonicalResult)
	if err != nil {
		return true, err
	}
	if err := ValidateControlOperationPredecessorV2(operation, predecessor); err != nil {
		return true, err
	}

	if len(acquisition.TerminalCanonicalResult) != 0 {
		if acquisition.ClaimToken != "" {
			return true, fmt.Errorf("%w: phase journal returned a claim and terminal replay", ErrUnavailable)
		}
		result, err := decodeAgentCanonicalResult(acquisition.TerminalCanonicalResult)
		if err != nil {
			return true, err
		}
		if err := ValidateControlResultForOperationV2(operation, result); err != nil {
			return true, err
		}
		resultSHA256 := hashBytes(acquisition.TerminalCanonicalResult)
		return true, agent.acknowledge(
			ctx, operation, requestSHA256, acquisition.TerminalCanonicalResult, resultSHA256,
		)
	}
	if acquisition.ClaimToken == "" {
		return true, fmt.Errorf("%w: phase journal returned neither a claim nor terminal replay", ErrUnavailable)
	}
	if transactionalExecution {
		canonicalResult, resultSHA256, err := agent.transactionalJournal.ExecuteClaimedPhase(
			ctx, operation, requestSHA256, acquisition.ClaimToken, agent.transactionalExecutor,
		)
		if err != nil {
			return true, err
		}
		result, err := decodeAgentCanonicalResult(canonicalResult)
		if err != nil {
			return true, err
		}
		if err := ValidateControlResultForOperationV2(operation, result); err != nil ||
			resultSHA256 != hashBytes(canonicalResult) {
			return true, fmt.Errorf("%w: transactional phase result is invalid", ErrUnavailable)
		}
		return true, agent.acknowledge(ctx, operation, requestSHA256, canonicalResult, resultSHA256)
	}

	executionOperation := operation
	executionOperation.EventScope = append([]string(nil), operation.EventScope...)
	result, err := agent.executor.Execute(ctx, executionOperation, predecessor)
	if err != nil {
		return true, err
	}
	if err := ValidateControlResultForOperationV2(operation, result); err != nil {
		return true, err
	}
	canonicalResult, err := result.CanonicalJSON()
	if err != nil {
		return true, err
	}
	resultSHA256 := hashBytes(canonicalResult)
	if err := agent.journal.Complete(
		ctx,
		operation,
		requestSHA256,
		acquisition.ClaimToken,
		bytes.Clone(canonicalResult),
		resultSHA256,
	); err != nil {
		return true, err
	}
	return true, agent.acknowledge(ctx, operation, requestSHA256, canonicalResult, resultSHA256)
}

func (agent *Agent) acknowledge(
	ctx context.Context,
	operation ControlOperationV2,
	requestSHA256 string,
	canonicalResult []byte,
	resultSHA256 string,
) error {
	if err := agent.counterpart.Acknowledge(ctx, bytes.Clone(canonicalResult)); err != nil {
		return err
	}
	return agent.journal.MarkAcknowledged(ctx, operation, requestSHA256, resultSHA256)
}

func decodeAgentPredecessor(raw []byte) (*ControlResultV2, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	result, err := decodeAgentCanonicalResult(raw)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func decodeAgentCanonicalResult(raw []byte) (ControlResultV2, error) {
	result, err := DecodeControlResultV2(raw)
	if err != nil {
		return ControlResultV2{}, err
	}
	canonical, err := result.CanonicalJSON()
	if err != nil {
		return ControlResultV2{}, err
	}
	if !bytes.Equal(raw, canonical) {
		return ControlResultV2{}, controlV2Error("result_noncanonical", nil)
	}
	return result, nil
}
