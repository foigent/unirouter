package pullv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	phase2BConnectorID      = "foigent.unirouter.test"
	phase2BConnectorVersion = "1.0.0"
	phase2BProducerInstance = "source-instance"
	phase2BTime             = "2026-07-25T04:05:06.000000Z"
)

func TestPhase2BEventBatchWireAndDeliveryIsolation(t *testing.T) {
	t.Parallel()

	event := phase2BTestEvent(t, "event-1", "alpha")
	beforeEventBytes := event.EnvelopeBytes()
	beforeHash := event.EnvelopeHash()
	beforeIdempotency := event.IdempotencyKey()

	first := phase2BTestBatch([]eventconnector.ImmutableEvent{event})
	second := first
	second.GeneratedAt = "2026-07-25T04:05:07.000000Z"
	second.NextCursor = "cursor-999"
	second.HighWatermark = "high-999"
	second.HasMore = !first.HasMore
	for index, batch := range []EventBatch{first, second} {
		if err := batch.Validate(); err != nil {
			t.Fatalf("batch %d validation: %v", index, err)
		}
	}
	if !bytes.Equal(event.EnvelopeBytes(), beforeEventBytes) ||
		event.EnvelopeHash() != beforeHash ||
		event.IdempotencyKey() != beforeIdempotency {
		t.Fatal("delivery metadata mutation changed immutable Event content or identity")
	}

	firstWire, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal Event Batch: %v", err)
	}
	secondWire, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal changed Event Batch: %v", err)
	}
	if bytes.Equal(firstWire, secondWire) {
		t.Fatal("delivery-only Event Batches unexpectedly have identical wire bytes")
	}
	var decoded map[string]any
	if err := json.Unmarshal(firstWire, &decoded); err != nil {
		t.Fatalf("decode Event Batch wire: %v", err)
	}
	events, ok := decoded["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("events=%#v", decoded["events"])
	}
	envelope, ok := events[0].(map[string]any)
	if !ok {
		t.Fatalf("Event wire=%#v", events[0])
	}
	for _, key := range []string{
		"next_cursor",
		"high_watermark",
		"generated_at",
		"has_more",
		"checkpoint",
		"retry",
		"ingested_at",
	} {
		if _, present := envelope[key]; present {
			t.Fatalf("delivery field %q entered Event wire", key)
		}
	}
}

func TestPhase2BCheckpointReplayTransitions(t *testing.T) {
	t.Parallel()

	firstEvent := phase2BTestEvent(t, "event-1", "alpha")
	secondEvent := phase2BTestEvent(t, "event-2", "beta")
	expected := Checkpoint{
		CheckpointVersion: CheckpointVersion,
		ConnectorID:       phase2BConnectorID,
		ConnectorVersion:  phase2BConnectorVersion,
		ProducerInstance:  phase2BProducerInstance,
		Cursor:            "cursor-0",
		HighWatermark:     "high-0",
		Phase:             "steady",
		UpdatedAt:         phase2BTime,
	}

	t.Run("same-identity-same-hash", func(t *testing.T) {
		batch := phase2BTestBatch([]eventconnector.ImmutableEvent{firstEvent})
		inserted, err := AdvanceCheckpoint(
			&expected,
			batch,
			ConvergedBatchOutcome{Items: []ItemOutcome{OutcomeInserted}},
			"2026-07-25T04:05:07.000000Z",
		)
		if err != nil {
			t.Fatalf("inserted transition: %v", err)
		}
		duplicate, err := AdvanceCheckpoint(
			&expected,
			batch,
			ConvergedBatchOutcome{Items: []ItemOutcome{OutcomeDuplicateNoop}},
			"2026-07-25T04:05:07.000000Z",
		)
		if err != nil {
			t.Fatalf("duplicate transition: %v", err)
		}
		if !reflect.DeepEqual(inserted, duplicate) || inserted.Cursor != batch.NextCursor {
			t.Fatalf("inserted=%#v duplicate=%#v", inserted, duplicate)
		}
	})

	t.Run("same-identity-different-hash", func(t *testing.T) {
		batch := phase2BTestBatch([]eventconnector.ImmutableEvent{firstEvent})
		checkpoint, err := AdvanceCheckpoint(
			&expected,
			batch,
			ConvergedBatchOutcome{Items: []ItemOutcome{OutcomeConflict}},
			"2026-07-25T04:05:07.000000Z",
		)
		if err == nil ||
			err.Category != "conflict" ||
			err.Code != "event_content_conflict" ||
			err.Retryable ||
			err.Recovery != "operator_repair" {
			t.Fatalf("conflict error=%#v", err)
		}
		if checkpoint != (Checkpoint{}) {
			t.Fatalf("conflict advanced checkpoint: %#v", checkpoint)
		}
	})

	t.Run("partial-page-failure-and-replay", func(t *testing.T) {
		batch := phase2BTestBatch([]eventconnector.ImmutableEvent{firstEvent, secondEvent})
		checkpoint, err := AdvanceCheckpoint(
			&expected,
			batch,
			ConvergedBatchOutcome{Items: []ItemOutcome{OutcomeInserted, OutcomeRetryableError}},
			"2026-07-25T04:05:07.000000Z",
		)
		if err == nil ||
			err.Category != "persistence" ||
			err.Code != "partial_page_failure" ||
			!err.Retryable ||
			err.Recovery != "automatic_retry" {
			t.Fatalf("partial-page error=%#v", err)
		}
		if checkpoint != (Checkpoint{}) {
			t.Fatalf("partial page advanced checkpoint: %#v", checkpoint)
		}
		replayed, err := AdvanceCheckpoint(
			&expected,
			batch,
			ConvergedBatchOutcome{Items: []ItemOutcome{OutcomeDuplicateNoop, OutcomeInserted}},
			"2026-07-25T04:05:08.000000Z",
		)
		if err != nil {
			t.Fatalf("replay transition: %v", err)
		}
		if replayed.Cursor != batch.NextCursor || replayed.HighWatermark != batch.HighWatermark {
			t.Fatalf("replayed checkpoint=%#v", replayed)
		}
	})
}

func TestPhase2BCheckpointProgressGates(t *testing.T) {
	t.Parallel()

	expected := Checkpoint{
		CheckpointVersion: CheckpointVersion,
		ConnectorID:       phase2BConnectorID,
		ConnectorVersion:  phase2BConnectorVersion,
		ProducerInstance:  phase2BProducerInstance,
		Cursor:            "cursor-0",
		HighWatermark:     "high-0",
		Phase:             "steady",
		UpdatedAt:         phase2BTime,
	}
	empty := phase2BTestBatch([]eventconnector.ImmutableEvent{})
	if _, err := AdvanceCheckpoint(
		&expected,
		empty,
		ConvergedBatchOutcome{Items: []ItemOutcome{}},
		phase2BTime,
	); err == nil || err.Code != "empty_page_cursor_advance" {
		t.Fatalf("empty-page error=%v", err)
	}

	empty.NextCursor = expected.Cursor
	checkpoint, err := AdvanceCheckpoint(
		&expected,
		empty,
		ConvergedBatchOutcome{Items: []ItemOutcome{}},
		"2026-07-25T04:05:07.000000Z",
	)
	if err != nil || checkpoint.Cursor != expected.Cursor {
		t.Fatalf("stationary empty-page checkpoint=%#v error=%v", checkpoint, err)
	}

	nonempty := phase2BTestBatch([]eventconnector.ImmutableEvent{
		phase2BTestEvent(t, "event-1", "alpha"),
	})
	nonempty.NextCursor = expected.Cursor
	if _, err := AdvanceCheckpoint(
		&expected,
		nonempty,
		ConvergedBatchOutcome{Items: []ItemOutcome{OutcomeInserted}},
		phase2BTime,
	); err == nil || err.Code != "nonempty_page_no_progress" {
		t.Fatalf("nonempty-no-progress error=%v", err)
	}

	bootstrap := phase2BTestBatch([]eventconnector.ImmutableEvent{})
	bootstrap.NextCursor = ""
	checkpoint, err = AdvanceCheckpoint(
		nil,
		bootstrap,
		ConvergedBatchOutcome{Items: []ItemOutcome{}},
		phase2BTime,
	)
	if err != nil || checkpoint.Phase != "bootstrap" || checkpoint.Cursor != "" {
		t.Fatalf("bootstrap checkpoint=%#v error=%v", checkpoint, err)
	}
}

func TestPhase2BDeliveryRejectsControlProducerAndNULCheckpoints(t *testing.T) {
	t.Parallel()

	event := phase2BTestEvent(t, "event-1", "alpha")
	batch := phase2BTestBatch([]eventconnector.ImmutableEvent{event})
	batch.ProducerInstance = "source\u0085instance"
	if err := batch.Validate(); err == nil || err.Code != "invalid_opaque_string" {
		t.Fatalf("batch producer error=%#v", err)
	}

	batch = phase2BTestBatch([]eventconnector.ImmutableEvent{event})
	batch.NextCursor = "cursor\x00next"
	if err := batch.Validate(); err == nil || err.Code != "invalid_cursor" {
		t.Fatalf("batch cursor error=%#v", err)
	}
	batch = phase2BTestBatch([]eventconnector.ImmutableEvent{event})
	batch.HighWatermark = "high\x00watermark"
	if err := batch.Validate(); err == nil || err.Code != "invalid_cursor" {
		t.Fatalf("batch high-watermark error=%#v", err)
	}

	checkpoint := Checkpoint{
		CheckpointVersion: CheckpointVersion,
		ConnectorID:       phase2BConnectorID,
		ConnectorVersion:  phase2BConnectorVersion,
		ProducerInstance:  phase2BProducerInstance,
		Cursor:            "cursor\x00checkpoint",
		HighWatermark:     "high-0",
		Phase:             "steady",
		UpdatedAt:         phase2BTime,
	}
	if err := checkpoint.Validate(); err == nil || err.Code != "invalid_cursor" {
		t.Fatalf("checkpoint cursor error=%#v", err)
	}

	receipt := phase2BCompletedReceipt()
	receipt.CheckpointAfter = "cursor\x00receipt"
	if err := receipt.Validate(); err == nil || err.Code != "invalid_cursor" {
		t.Fatalf("receipt checkpoint error=%#v", err)
	}
}

func TestPhase2BOperationalReceiptValidation(t *testing.T) {
	t.Parallel()

	receipt := phase2BCompletedReceipt()
	if err := receipt.Validate(); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	duplicate := receipt
	duplicate.Counts.Inserted = 0
	duplicate.Counts.DuplicateNoop = 1
	if err := duplicate.Validate(); err != nil {
		t.Fatalf("valid duplicate-noop receipt rejected: %v", err)
	}
	wire, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	for _, prohibited := range []string{
		"event_id",
		"envelope_hash",
		"idempotency_key",
		"source_hash",
	} {
		if bytes.Contains(wire, []byte(prohibited)) {
			t.Fatalf("Operational Receipt became event-linked through %q: %s", prohibited, wire)
		}
	}

	receipt.Status = "retryable_error"
	receipt.Error = eventconnector.NewConnectorError(
		"transport",
		"source_unavailable",
		"fetch",
		"fetch_page",
		false,
		"Source is unavailable",
		"operator_repair",
		nil,
	)
	if err := receipt.Validate(); err == nil || err.Code != "receipt_retryability_mismatch" {
		t.Fatalf("retryability mismatch error=%v", err)
	}
}

func TestPhase2BCompletedReceiptRequiresFullConvergence(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		counts ReceiptCounts
	}{
		{
			name:   "unaccounted-received",
			counts: ReceiptCounts{Received: 2, Inserted: 1},
		},
		{
			name:   "too-many-successes",
			counts: ReceiptCounts{Received: 1, Inserted: 1, DuplicateNoop: 1},
		},
		{
			name:   "conflict",
			counts: ReceiptCounts{Received: 1, Inserted: 1, Conflict: 1},
		},
		{
			name:   "rejected",
			counts: ReceiptCounts{Received: 1, Inserted: 1, Rejected: 1},
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			receipt := phase2BCompletedReceipt()
			receipt.Counts = testCase.counts
			if err := receipt.Validate(); err == nil ||
				err.Code != "completed_receipt_count_mismatch" ||
				err.Operation != "validate_receipt" {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}

type phase2BSourceAdapter struct {
	descriptor eventconnector.Descriptor
	eventID    string
	value      string
}

func (adapter phase2BSourceAdapter) Descriptor() eventconnector.Descriptor {
	return adapter.descriptor
}

func (adapter phase2BSourceAdapter) Project(
	artifact eventconnector.SourceArtifact,
) (eventconnector.EventDraft, *eventconnector.ConnectorError) {
	eventID, err := eventconnector.NewOpaqueString("event_id", adapter.eventID, 1, 128, true)
	if err != nil {
		return eventconnector.EventDraft{}, err
	}
	eventType, err := eventconnector.NewProtocolString("event_type", "test.event")
	if err != nil {
		return eventconnector.EventDraft{}, err
	}
	schemaRef, err := eventconnector.NewProtocolString("schema_ref", "foigent://events/test/event/v1")
	if err != nil {
		return eventconnector.EventDraft{}, err
	}
	version, err := eventconnector.NewProtocolString("schema_version", "1")
	if err != nil {
		return eventconnector.EventDraft{}, err
	}
	subjectType, err := eventconnector.NewProtocolString("subject.type", "record")
	if err != nil {
		return eventconnector.EventDraft{}, err
	}
	revision, err := eventconnector.NewOpaqueString("source_revision", artifact.Revision(), 1, 64, true)
	if err != nil {
		return eventconnector.EventDraft{}, err
	}
	return eventconnector.EventDraft{
		EventID:       eventID,
		EventType:     eventType,
		SchemaRef:     schemaRef,
		SchemaVersion: version,
		Scope: eventconnector.EventScope{
			OrgRef:     "101",
			ProjectRef: "202",
		},
		Subject: eventconnector.EventSubject{
			Type: subjectType,
			Ref:  eventID,
		},
		Actor:          map[string]any{"type": "system"},
		Time:           map[string]any{"occurred_at": phase2BTime},
		Correlation:    map[string]any{},
		SourceRevision: revision,
		DataPolicy: map[string]any{
			"payload_capture_mode": "metadata_only",
			"raw_payload_present":  false,
		},
		Provenance: map[string]any{
			"adapter_fidelity": "F2",
			"field_groups": map[string]any{
				"payload.value": "direct",
			},
		},
		Payload: map[string]any{"value": adapter.value},
	}, nil
}

func phase2BTestEvent(t *testing.T, eventID, value string) eventconnector.ImmutableEvent {
	t.Helper()

	descriptor, err := eventconnector.NewDescriptor(eventconnector.DescriptorBinding{
		ProtocolVersion:      "1.0.0",
		ProtocolCommit:       strings.Repeat("1", 40),
		ReleaseDigest:        "sha256:" + strings.Repeat("2", 64),
		ManifestDigest:       "sha256:" + strings.Repeat("3", 64),
		EnvelopeSpecVersion:  eventconnector.EventEnvelopeSpecVersion,
		EnvelopeSchemaDigest: "sha256:" + strings.Repeat("4", 64),
		SourceProduct:        "unirouter",
		ConnectorID:          phase2BConnectorID,
		ConnectorVersion:     phase2BConnectorVersion,
		SourceSchema: eventconnector.SchemaBinding{
			SchemaID: "unirouter.test.v1",
			SHA256:   "sha256:" + strings.Repeat("5", 64),
		},
		EmittedEvents: []eventconnector.EventSchemaBinding{
			{
				EventType:     "test.event",
				SchemaRef:     "foigent://events/test/event/v1",
				SchemaVersion: "1",
				SHA256:        "sha256:" + strings.Repeat("6", 64),
			},
		},
	})
	if err != nil {
		t.Fatalf("create descriptor: %v", err)
	}
	source := []byte(fmt.Sprintf(`{"event_id":%q,"value":%q}`, eventID, value))
	artifact, err := eventconnector.NewSourceArtifact(
		descriptor,
		phase2BProducerInstance,
		"1",
		source,
		eventconnector.StrictJSONOptions{RejectNull: true},
	)
	if err != nil {
		t.Fatalf("create Source Artifact: %v", err)
	}
	event, err := eventconnector.ProjectAndFinalize(
		phase2BSourceAdapter{
			descriptor: descriptor,
			eventID:    eventID,
			value:      value,
		},
		artifact,
	)
	if err != nil {
		t.Fatalf("finalize Event: %v", err)
	}
	return event
}

func phase2BTestBatch(events []eventconnector.ImmutableEvent) EventBatch {
	return EventBatch{
		BatchVersion:     EventBatchVersion,
		ConnectorID:      phase2BConnectorID,
		ConnectorVersion: phase2BConnectorVersion,
		ProducerInstance: phase2BProducerInstance,
		GeneratedAt:      phase2BTime,
		Events:           events,
		NextCursor:       "cursor-1",
		HighWatermark:    "high-1",
		HasMore:          true,
	}
}

func phase2BCompletedReceipt() OperationalReceipt {
	return OperationalReceipt{
		ReceiptVersion:   OperationalReceiptVersion,
		OperationID:      "operation-1",
		ConnectorID:      phase2BConnectorID,
		ProducerInstance: phase2BProducerInstance,
		Operation:        "commit_page",
		Status:           "completed",
		Counts: ReceiptCounts{
			Received: 1,
			Inserted: 1,
		},
		CheckpointBefore: "cursor-0",
		CheckpointAfter:  "cursor-1",
		ObservedAt:       phase2BTime,
	}
}
