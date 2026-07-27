package pullv1

import (
	"context"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

var (
	connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$`)
	semverPattern      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

const (
	EventBatchVersion         = "foigent.event-batch.v1"
	CheckpointVersion         = "foigent.connector-checkpoint.v1"
	OperationalReceiptVersion = "foigent.operational-receipt.v1"
	MaxBatchItems             = 50
	MaxCursorBytes            = 512
)

type EventBatch struct {
	BatchVersion     string                          `json:"batch_version"`
	ConnectorID      string                          `json:"connector_id"`
	ConnectorVersion string                          `json:"connector_version"`
	ProducerInstance string                          `json:"producer_instance"`
	GeneratedAt      string                          `json:"generated_at"`
	Events           []eventconnector.ImmutableEvent `json:"events"`
	NextCursor       string                          `json:"next_cursor"`
	HighWatermark    string                          `json:"high_watermark"`
	HasMore          bool                            `json:"has_more"`
}

func (batch EventBatch) Validate() *eventconnector.ConnectorError {
	if batch.BatchVersion != EventBatchVersion {
		return deliveryContractError("invalid_batch", "validate_batch", "Event Batch version is unsupported")
	}
	if err := validateConnectorIdentity(batch.ConnectorID, batch.ConnectorVersion); err != nil {
		return err
	}
	if _, err := eventconnector.NewOpaqueString("batch.producer_instance", batch.ProducerInstance, 1, 128, false); err != nil {
		return err
	}
	if eventconnector.ValidateCanonicalMicrosecondTime(batch.GeneratedAt) != nil {
		return deliveryContractError("invalid_batch_time", "validate_batch", "Event Batch generated_at is invalid")
	}
	if batch.Events == nil {
		return deliveryContractError("invalid_batch_events", "validate_batch", "Event Batch events must be an array")
	}
	if len(batch.Events) > MaxBatchItems {
		return deliveryContractError("batch_too_large", "validate_batch", "Event Batch exceeds the protocol item limit")
	}
	if !validCursor(batch.NextCursor) || !validCursor(batch.HighWatermark) {
		return cursorError("invalid_cursor", "validate_batch", "Event Batch cursor is invalid", false)
	}
	for _, event := range batch.Events {
		if event.ConnectorID() != batch.ConnectorID ||
			event.ConnectorVersion() != batch.ConnectorVersion ||
			event.ProducerInstance() != batch.ProducerInstance ||
			len(event.EnvelopeBytes()) == 0 {
			return deliveryContractError("event_batch_binding_mismatch", "validate_batch", "Event does not match its Event Batch binding")
		}
	}
	return nil
}

type Checkpoint struct {
	CheckpointVersion string `json:"checkpoint_version"`
	ConnectorID       string `json:"connector_id"`
	ConnectorVersion  string `json:"connector_version"`
	ProducerInstance  string `json:"producer_instance"`
	Cursor            string `json:"cursor"`
	HighWatermark     string `json:"high_watermark"`
	Phase             string `json:"phase"`
	UpdatedAt         string `json:"updated_at"`
}

func (checkpoint Checkpoint) Validate() *eventconnector.ConnectorError {
	if checkpoint.CheckpointVersion != CheckpointVersion {
		return deliveryContractError("invalid_checkpoint", "validate_checkpoint", "Connector Checkpoint version is unsupported")
	}
	if err := validateConnectorIdentity(checkpoint.ConnectorID, checkpoint.ConnectorVersion); err != nil {
		return err
	}
	if _, err := eventconnector.NewOpaqueString("checkpoint.producer_instance", checkpoint.ProducerInstance, 1, 128, false); err != nil {
		return err
	}
	if !validCursor(checkpoint.Cursor) || !validCursor(checkpoint.HighWatermark) {
		return cursorError("invalid_cursor", "validate_checkpoint", "Connector Checkpoint cursor is invalid", false)
	}
	switch checkpoint.Phase {
	case "bootstrap", "steady", "error":
	default:
		return deliveryContractError("invalid_checkpoint_phase", "validate_checkpoint", "Connector Checkpoint phase is unsupported")
	}
	if eventconnector.ValidateCanonicalMicrosecondTime(checkpoint.UpdatedAt) != nil {
		return deliveryContractError("invalid_checkpoint_time", "validate_checkpoint", "Connector Checkpoint updated_at is invalid")
	}
	return nil
}

type ItemOutcome string

const (
	OutcomeInserted       ItemOutcome = "inserted"
	OutcomeDuplicateNoop  ItemOutcome = "duplicate_noop"
	OutcomeConflict       ItemOutcome = "conflict"
	OutcomeRejected       ItemOutcome = "rejected"
	OutcomeRetryableError ItemOutcome = "retryable_error"
)

type ConvergedBatchOutcome struct {
	Items []ItemOutcome
}

func (outcome ConvergedBatchOutcome) Validate(eventCount int) *eventconnector.ConnectorError {
	if len(outcome.Items) != eventCount {
		return deliveryContractError("outcome_count_mismatch", "validate_batch_outcome", "Batch outcome count differs from Event count")
	}
	for _, item := range outcome.Items {
		switch item {
		case OutcomeInserted, OutcomeDuplicateNoop, OutcomeConflict, OutcomeRejected, OutcomeRetryableError:
		default:
			return deliveryContractError("invalid_item_outcome", "validate_batch_outcome", "Batch item outcome is unsupported")
		}
	}
	return nil
}

func (outcome ConvergedBatchOutcome) CanAdvance() bool {
	for _, item := range outcome.Items {
		if item != OutcomeInserted && item != OutcomeDuplicateNoop {
			return false
		}
	}
	return true
}

// AdvanceCheckpoint is a pure state transition. The runtime owner remains
// responsible for compare-and-swap persistence through CheckpointPort.
func AdvanceCheckpoint(
	expected *Checkpoint,
	batch EventBatch,
	outcome ConvergedBatchOutcome,
	updatedAt string,
) (Checkpoint, *eventconnector.ConnectorError) {
	if err := batch.Validate(); err != nil {
		return Checkpoint{}, err
	}
	if err := outcome.Validate(len(batch.Events)); err != nil {
		return Checkpoint{}, err
	}
	if expected != nil {
		if err := expected.Validate(); err != nil {
			return Checkpoint{}, err
		}
		if expected.ConnectorID != batch.ConnectorID ||
			expected.ConnectorVersion != batch.ConnectorVersion ||
			expected.ProducerInstance != batch.ProducerInstance {
			return Checkpoint{}, deliveryContractError("checkpoint_binding_mismatch", "commit_checkpoint", "Checkpoint and Event Batch bindings differ")
		}
		if len(batch.Events) == 0 && batch.NextCursor != expected.Cursor {
			return Checkpoint{}, cursorError("empty_page_cursor_advance", "commit_checkpoint", "Empty steady-state page must not advance its cursor", false)
		}
		if len(batch.Events) > 0 && batch.NextCursor == expected.Cursor {
			return Checkpoint{}, cursorError("nonempty_page_no_progress", "commit_checkpoint", "Nonempty page must advance its cursor", false)
		}
	} else {
		if len(batch.Events) == 0 && batch.NextCursor != "" {
			return Checkpoint{}, cursorError("empty_page_cursor_advance", "commit_checkpoint", "Empty bootstrap page must not advance its cursor", false)
		}
		if len(batch.Events) > 0 && batch.NextCursor == "" {
			return Checkpoint{}, cursorError("nonempty_page_no_progress", "commit_checkpoint", "Nonempty bootstrap page must advance its cursor", false)
		}
	}
	if !outcome.CanAdvance() {
		for _, classification := range []ItemOutcome{
			OutcomeConflict,
			OutcomeRejected,
			OutcomeRetryableError,
		} {
			for _, item := range outcome.Items {
				if item != classification {
					continue
				}
				switch classification {
				case OutcomeConflict:
					return Checkpoint{}, eventconnector.NewConnectorError(
						"conflict",
						"event_content_conflict",
						"checkpoint",
						"commit_checkpoint",
						false,
						"Event identity resolved to different canonical content",
						"operator_repair",
						nil,
					)
				case OutcomeRetryableError:
					return Checkpoint{}, eventconnector.NewConnectorError(
						"persistence",
						"partial_page_failure",
						"checkpoint",
						"commit_checkpoint",
						true,
						"Page did not converge and must replay through the old Checkpoint",
						"automatic_retry",
						nil,
					)
				case OutcomeRejected:
					return Checkpoint{}, eventconnector.NewConnectorError(
						"contract",
						"event_rejected",
						"checkpoint",
						"commit_checkpoint",
						false,
						"Page contains a permanently rejected Event",
						"operator_repair",
						nil,
					)
				}
			}
		}
	}
	if eventconnector.ValidateCanonicalMicrosecondTime(updatedAt) != nil {
		return Checkpoint{}, deliveryContractError("invalid_checkpoint_time", "commit_checkpoint", "Checkpoint update time is invalid")
	}
	checkpoint := Checkpoint{
		CheckpointVersion: CheckpointVersion,
		ConnectorID:       batch.ConnectorID,
		ConnectorVersion:  batch.ConnectorVersion,
		ProducerInstance:  batch.ProducerInstance,
		Cursor:            batch.NextCursor,
		HighWatermark:     batch.HighWatermark,
		Phase:             "steady",
		UpdatedAt:         updatedAt,
	}
	if expected == nil && len(batch.Events) == 0 {
		checkpoint.Phase = "bootstrap"
	}
	return checkpoint, checkpoint.Validate()
}

type ReceiptCounts struct {
	Received      int `json:"received"`
	Inserted      int `json:"inserted"`
	DuplicateNoop int `json:"duplicate_noop"`
	Conflict      int `json:"conflict"`
	Rejected      int `json:"rejected"`
}

type OperationalReceipt struct {
	ReceiptVersion   string                         `json:"receipt_version"`
	OperationID      string                         `json:"operation_id"`
	ConnectorID      string                         `json:"connector_id"`
	ProducerInstance string                         `json:"producer_instance"`
	Operation        string                         `json:"operation"`
	Status           string                         `json:"status"`
	Counts           ReceiptCounts                  `json:"counts"`
	CheckpointBefore string                         `json:"checkpoint_before,omitempty"`
	CheckpointAfter  string                         `json:"checkpoint_after,omitempty"`
	ObservedAt       string                         `json:"observed_at"`
	Error            *eventconnector.ConnectorError `json:"error,omitempty"`
}

func (receipt OperationalReceipt) Validate() *eventconnector.ConnectorError {
	if receipt.ReceiptVersion != OperationalReceiptVersion {
		return deliveryContractError("invalid_receipt", "validate_receipt", "Operational Receipt version is unsupported")
	}
	if _, err := eventconnector.NewOpaqueString("receipt.operation_id", receipt.OperationID, 1, 128, true); err != nil {
		return err
	}
	if _, err := eventconnector.NewProtocolString("receipt.connector_id", receipt.ConnectorID); err != nil {
		return err
	}
	if !connectorIDPattern.MatchString(receipt.ConnectorID) || len(receipt.ConnectorID) > 160 {
		return deliveryContractError("invalid_connector_id", "validate_receipt", "Operational Receipt connector_id is invalid")
	}
	if _, err := eventconnector.NewOpaqueString("receipt.producer_instance", receipt.ProducerInstance, 1, 128, false); err != nil {
		return err
	}
	switch receipt.Operation {
	case "activate", "poll", "commit_page", "pause", "resume", "stop", "start", "rotate", "retire":
	default:
		return deliveryContractError("invalid_receipt_operation", "validate_receipt", "Operational Receipt operation is unsupported")
	}
	switch receipt.Status {
	case "accepted", "completed":
		if receipt.Error != nil {
			return deliveryContractError("unexpected_receipt_error", "validate_receipt", "Successful Operational Receipt must not contain an error")
		}
	case "retryable_error", "permanent_error":
		if receipt.Error == nil {
			return deliveryContractError("missing_receipt_error", "validate_receipt", "Failed Operational Receipt requires an error")
		}
	default:
		return deliveryContractError("invalid_receipt_status", "validate_receipt", "Operational Receipt status is unsupported")
	}
	for name, value := range map[string]int{
		"received":       receipt.Counts.Received,
		"inserted":       receipt.Counts.Inserted,
		"duplicate_noop": receipt.Counts.DuplicateNoop,
		"conflict":       receipt.Counts.Conflict,
		"rejected":       receipt.Counts.Rejected,
	} {
		if value < 0 {
			return deliveryContractError("invalid_receipt_count", "validate_receipt", "Operational Receipt "+name+" count is negative")
		}
	}
	if !validCursor(receipt.CheckpointBefore) || !validCursor(receipt.CheckpointAfter) {
		return cursorError("invalid_cursor", "validate_receipt", "Operational Receipt checkpoint is invalid", false)
	}
	if eventconnector.ValidateCanonicalMicrosecondTime(receipt.ObservedAt) != nil {
		return deliveryContractError("invalid_receipt_time", "validate_receipt", "Operational Receipt observed_at is invalid")
	}
	if receipt.Error != nil {
		if err := receipt.Error.Validate(); err != nil {
			return deliveryContractError("invalid_receipt_error", "validate_receipt", "Operational Receipt error is invalid")
		}
		if receipt.Status == "retryable_error" && !receipt.Error.Retryable {
			return deliveryContractError("receipt_retryability_mismatch", "validate_receipt", "Receipt and Connector Error retryability differ")
		}
		if receipt.Status == "permanent_error" && receipt.Error.Retryable {
			return deliveryContractError("receipt_retryability_mismatch", "validate_receipt", "Receipt and Connector Error retryability differ")
		}
	}
	return nil
}

type CheckpointRecord struct {
	Checkpoint Checkpoint
	Generation uint64
}

type CheckpointBinding struct {
	ConnectorID      string
	ConnectorVersion string
	ProducerInstance string
}

func (binding CheckpointBinding) Validate() *eventconnector.ConnectorError {
	if err := validateConnectorIdentity(binding.ConnectorID, binding.ConnectorVersion); err != nil {
		return err
	}
	if _, err := eventconnector.NewOpaqueString(
		"checkpoint_binding.producer_instance",
		binding.ProducerInstance,
		1,
		128,
		false,
	); err != nil {
		return err
	}
	return nil
}

type CheckpointPort interface {
	Load(context.Context, CheckpointBinding) (CheckpointRecord, *eventconnector.ConnectorError)
	CompareAndSwap(context.Context, CheckpointRecord, Checkpoint) (CheckpointRecord, *eventconnector.ConnectorError)
}

type ReceiptPort interface {
	Record(context.Context, OperationalReceipt) *eventconnector.ConnectorError
}

func validCursor(value string) bool {
	return utf8.ValidString(value) && len(value) <= MaxCursorBytes
}

func validateConnectorIdentity(connectorID, connectorVersion string) *eventconnector.ConnectorError {
	if _, err := eventconnector.NewProtocolString("connector_id", connectorID); err != nil {
		return err
	}
	if _, err := eventconnector.NewProtocolString("connector_version", connectorVersion); err != nil {
		return err
	}
	if !connectorIDPattern.MatchString(connectorID) || len(connectorID) > 160 {
		return deliveryContractError("invalid_connector_id", "validate_connector_identity", "Connector ID is invalid")
	}
	if !semverPattern.MatchString(connectorVersion) {
		return deliveryContractError("invalid_connector_version", "validate_connector_identity", "Connector version is invalid")
	}
	return nil
}

func deliveryContractError(code, operation, message string) *eventconnector.ConnectorError {
	return eventconnector.NewConnectorError(
		"contract",
		code,
		"validate",
		operation,
		false,
		message,
		"operator_repair",
		nil,
	)
}

func cursorError(code, operation, message string, retryable bool) *eventconnector.ConnectorError {
	recovery := "operator_repair"
	if retryable {
		recovery = "automatic_retry"
	}
	return eventconnector.NewConnectorError(
		"cursor",
		code,
		"checkpoint",
		operation,
		retryable,
		message,
		recovery,
		fmt.Errorf("%s", message),
	)
}
