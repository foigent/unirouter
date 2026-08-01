package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/telemetryexport"
	"github.com/uniroute/unirouter/internal/testutil"
)

type captureTraceExporter struct {
	events []telemetryexport.SourceEvent
	err    error
}

func (e *captureTraceExporter) TryExport(event telemetryexport.SourceEvent) error {
	e.events = append(e.events, event)
	return e.err
}

func TestRecordTraceEventExportsOnlyAfterDurableOutboxEnqueue(t *testing.T) {
	var outboxPayload string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "INSERT INTO internal_outbox_events") {
				return &testutil.MockRow{}
			}
			outboxPayload = args[5].(string)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 101
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			t.Fatalf("durable outbox success must not use synchronous trace insert")
			return pgconn.CommandTag{}, nil
		},
	}
	outboxService, err := outbox.New(db, outbox.DefaultOptions())
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	exporter := &captureTraceExporter{err: telemetryexport.ErrQueueFull}
	relay := newTestRelay(db, &mockBilling{})
	relay.SetOutbox(outboxService)
	relay.SetTraceExporter(exporter)

	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(42))
	ctx = context.WithValue(ctx, middleware.CtxProjectID, int64(7))
	ctx = contextWithTelemetryOperationKind(ctx, telemetryexport.OperationChatCompletion)
	relay.recordTraceEvent(
		ctx,
		"op_00000000000000000000000000000001",
		"provider.response_accepted",
		"provider",
		"info",
		"gpt-4o",
		"company-chat",
		nil,
		200,
		25,
		map[string]any{
			"estimated_tokens": 12,
			"prompt":           "must never leave UniRouter",
		},
	)

	if len(exporter.events) != 1 {
		t.Fatalf("expected one export observation, got %d", len(exporter.events))
	}
	event := exporter.events[0]
	if event.OperationKind != telemetryexport.OperationChatCompletion ||
		event.OrgID != 42 ||
		event.ProjectID != 7 ||
		event.EventType != "provider.response_accepted" ||
		len(event.SourceEventHash) != 64 {
		t.Fatalf("unexpected export snapshot: %+v", event)
	}
	if _, exists := event.Metadata["prompt"]; exists {
		t.Fatalf("restricted prompt reached exporter: %#v", event.Metadata)
	}
	if event.CaptureGap != "redacted" {
		t.Fatalf("expected source redaction gap, got %q", event.CaptureGap)
	}
	var persisted traceOutboxPayload
	if err := json.Unmarshal([]byte(outboxPayload), &persisted); err != nil {
		t.Fatalf("decode outbox source payload: %v", err)
	}
	if persisted.RequestID != "op_00000000000000000000000000000001" ||
		persisted.SourceEventHash != event.SourceEventHash ||
		string(persisted.Metadata) != `{"estimated_tokens":12}` {
		t.Fatalf("source outbox fact changed or diverged: %+v", persisted)
	}
}

func TestRecordTraceEventDoesNotExportFallbackWithoutExactSourceTimestamp(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				return errors.New("outbox unavailable")
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO request_trace_events") {
				t.Fatalf("unexpected fallback statement: %s", sql)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	outboxService, err := outbox.New(db, outbox.DefaultOptions())
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	exporter := &captureTraceExporter{}
	relay := newTestRelay(db, &mockBilling{})
	relay.SetOutbox(outboxService)
	relay.SetTraceExporter(exporter)
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(42))
	relay.recordTraceEvent(
		ctx,
		"op_00000000000000000000000000000001",
		"gateway.request.accepted",
		"gateway",
		"info",
		"gpt-4o",
		"gpt-4o",
		nil,
		0,
		1,
		nil,
	)
	if len(exporter.events) != 0 {
		t.Fatalf("synchronous fallback without an exact occurred_at source was exported: %+v", exporter.events)
	}
}

func TestRecordTraceEventDoesNotFabricateExportForOutboxDuplicate(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "INSERT INTO internal_outbox_events") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					return pgx.ErrNoRows
				}}
			}
			if strings.Contains(sql, "SELECT id FROM internal_outbox_events") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 101
					return nil
				}}
			}
			return &testutil.MockRow{}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			t.Fatalf("outbox duplicate must not fall back to a synchronous insert")
			return pgconn.CommandTag{}, nil
		},
	}
	outboxService, err := outbox.New(db, outbox.DefaultOptions())
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	exporter := &captureTraceExporter{}
	relay := newTestRelay(db, &mockBilling{})
	relay.SetOutbox(outboxService)
	relay.SetTraceExporter(exporter)
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(42))
	relay.recordTraceEvent(
		ctx,
		"op_00000000000000000000000000000001",
		"gateway.request.accepted",
		"gateway",
		"info",
		"gpt-4o",
		"gpt-4o",
		nil,
		0,
		1,
		nil,
	)
	if len(exporter.events) != 0 {
		t.Fatalf("deduplicated outbox event created a non-source export span: %+v", exporter.events)
	}
}
