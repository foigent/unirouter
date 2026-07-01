package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEventService_EmitDeliversWebhook(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode webhook body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	var deliveryStatus string
	var eventState string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			switch {
			case strings.Contains(query, "INSERT INTO notification_events"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				}}
			case strings.Contains(query, "INSERT INTO notification_deliveries"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 99
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow: %s", query)
				return &testutil.MockRow{}
			}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "FROM notification_webhooks") {
				t.Fatalf("unexpected Query: %s", query)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 7
						*dest[1].(*string) = server.URL
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "UPDATE notification_deliveries") {
				deliveryStatus = "delivered"
			}
			if strings.Contains(query, "UPDATE notification_events") {
				eventState = args[0].(string)
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db)
	svc.now = func() time.Time { return time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC) }

	result, err := svc.Emit(context.Background(), EventInput{
		OrgID:     3,
		ProjectID: 9,
		EventType: "api_key.disabled",
		Severity:  NotificationSeverityWarning,
		Title:     "API key disabled",
		Payload:   map[string]interface{}{"key_id": 11},
	})
	if err != nil {
		t.Fatalf("emit event: %v", err)
	}
	if result.EventID != 42 || !result.Created || result.DeliveryState != DeliveryStateDelivered {
		t.Fatalf("unexpected result: %+v", result)
	}
	if deliveryStatus != "delivered" || eventState != DeliveryStateDelivered {
		t.Fatalf("expected delivered states, delivery=%s event=%s", deliveryStatus, eventState)
	}
	if received["event_type"] != "api_key.disabled" || received["project_id"].(float64) != 9 {
		t.Fatalf("unexpected webhook body: %#v", received)
	}
}

func TestEventService_EmitRecordsFailedDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var failureRecorded bool
	var eventState string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			switch {
			case strings.Contains(query, "INSERT INTO notification_events"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 50
					return nil
				}}
			case strings.Contains(query, "INSERT INTO notification_deliveries"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 60
					return nil
				}}
			default:
				return &testutil.MockRow{}
			}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 8
						*dest[1].(*string) = server.URL
						return nil
					},
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "UPDATE notification_deliveries") && strings.Contains(query, "failed") {
				failureRecorded = true
				if args[0].(int) != http.StatusInternalServerError {
					t.Fatalf("expected response status 500, got %v", args[0])
				}
				if _, ok := args[2].(time.Time); !ok {
					t.Fatalf("expected next_retry_at time, got %T", args[2])
				}
			}
			if strings.Contains(query, "UPDATE notification_events") {
				eventState = args[0].(string)
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db)

	result, err := svc.Emit(context.Background(), EventInput{
		OrgID:     3,
		EventType: "provider.failure",
		Title:     "Provider failure",
	})
	if err != nil {
		t.Fatalf("emit event: %v", err)
	}
	if result.DeliveryState != DeliveryStateFailed || !failureRecorded || eventState != DeliveryStateFailed {
		t.Fatalf("expected failed delivery state, result=%+v failure=%v event=%s", result, failureRecorded, eventState)
	}
}

func TestEventService_CheckProjectBudgetEmitsThresholdEvent(t *testing.T) {
	step := 0
	var eventPayload []byte
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			step++
			switch step {
			case 1:
				if !strings.Contains(query, "FROM projects") {
					t.Fatalf("expected project budget query, got %s", query)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "Core"
					*dest[1].(*sql.NullInt64) = sql.NullInt64{Int64: 1000, Valid: true}
					*dest[2].(*string) = "monthly"
					return nil
				}}
			case 2:
				if !strings.Contains(query, "FROM call_logs") {
					t.Fatalf("expected usage query, got %s", query)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1200
					return nil
				}}
			case 3:
				if !strings.Contains(query, "INSERT INTO notification_events") {
					t.Fatalf("expected event insert, got %s", query)
				}
				eventPayload = args[8].([]byte)
				if args[2] != EventProjectBudgetThreshold {
					t.Fatalf("expected budget event type, got %v", args[2])
				}
				if args[9] != "project_budget.threshold:9:20260701T000000Z:100" {
					t.Fatalf("unexpected dedupe key: %v", args[9])
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 70
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow step %d: %s", step, query)
				return &testutil.MockRow{}
			}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "FROM notification_webhooks") {
				t.Fatalf("unexpected Query: %s", query)
			}
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db)
	svc.now = func() time.Time { return time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC) }

	result, err := svc.CheckProjectBudget(context.Background(), 3, 9)
	if err != nil {
		t.Fatalf("check project budget: %v", err)
	}
	if result == nil || result.EventID != 70 || result.DeliveryState != DeliveryStateNotConfigured {
		t.Fatalf("unexpected result: %+v", result)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(eventPayload, &payload); err != nil {
		t.Fatalf("unmarshal event payload: %v", err)
	}
	if payload["usage_percent"].(float64) != 120 || payload["budget_cents"].(float64) != 1000 {
		t.Fatalf("unexpected budget payload: %#v", payload)
	}
}
