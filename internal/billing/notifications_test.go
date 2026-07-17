package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestFinancialNotificationEventTaxonomy(t *testing.T) {
	got := FinancialNotificationEventTypes()
	want := []string{EventProjectBudgetThreshold, EventTokenMeteringEstimatedRatioHigh}
	if len(got) != len(want) {
		t.Fatalf("FinancialNotificationEventTypes() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] || !IsFinancialNotificationEventType(got[i]) {
			t.Fatalf("financial notification taxonomy entry %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, eventType := range []string{
		EventAPIKeyDisabled,
		EventModelDeprecated,
		EventModelRetired,
		EventProviderChannelFailure,
		EventLicenseWarning,
		"unknown.event",
	} {
		if IsFinancialNotificationEventType(eventType) {
			t.Fatalf("non-financial event %q must not enter the billing taxonomy", eventType)
		}
	}

	got[0] = EventAPIKeyDisabled
	if FinancialNotificationEventTypes()[0] != EventProjectBudgetThreshold {
		t.Fatal("callers must not be able to mutate the financial notification taxonomy")
	}
}

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
			if !strings.Contains(query, "FROM notification_webhooks") || !strings.Contains(query, "scope_verified = TRUE") {
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
	svc := NewEventService(db, "").WithHTTPClient(server.Client())
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
	svc := NewEventService(db, "").WithHTTPClient(server.Client())

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
					*dest[0].(*int64) = 12_000_000
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
			if !strings.Contains(query, "FROM notification_webhooks") || !strings.Contains(query, "scope_verified = TRUE") {
				t.Fatalf("unexpected Query: %s", query)
			}
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db, "")
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
	if payload["usage_percent"].(float64) != 120 || payload["budget_cents"].(float64) != 1000 ||
		payload["budget_micros"] != "10000000" || payload["spent_micros"] != "12000000" {
		t.Fatalf("unexpected budget payload: %#v", payload)
	}
}

func TestEventService_RetryDueDeliveries(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode retry body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var deliveryUpdated bool
	var eventState string
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "FROM notification_deliveries d") || !strings.Contains(query, "next_retry_at <= NOW()") || !strings.Contains(query, "w.scope_verified = TRUE") {
				t.Fatalf("unexpected retry query: %s", query)
			}
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*int64) = 60
					*dest[1].(*int64) = 50
					*dest[2].(*int64) = 8
					*dest[3].(*string) = server.URL
					*dest[4].(*int64) = 3
					*dest[5].(*int64) = 9
					*dest[6].(*string) = EventProjectBudgetThreshold
					*dest[7].(*string) = NotificationSeverityCritical
					*dest[8].(*string) = "Budget reached"
					*dest[9].(*string) = "Project Core reached budget"
					*dest[10].(*string) = "project"
					*dest[11].(*string) = "9"
					*dest[12].(*[]byte) = []byte(`{"spent_cents":1200}`)
					return nil
				},
			}}, nil
		},
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "COUNT(*) FILTER") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 0
				*dest[1].(*int64) = 1
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "UPDATE notification_deliveries") && strings.Contains(query, "delivered") {
				deliveryUpdated = true
				if args[1].(int64) != 60 {
					t.Fatalf("expected retry to update original delivery id 60, got %v", args[1])
				}
			}
			if strings.Contains(query, "UPDATE notification_events") {
				eventState = args[0].(string)
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db, "").WithHTTPClient(server.Client())

	retried, err := svc.RetryDueDeliveries(context.Background(), 10)
	if err != nil {
		t.Fatalf("retry due deliveries: %v", err)
	}
	if retried != 1 || !deliveryUpdated || eventState != DeliveryStateDelivered {
		t.Fatalf("expected delivered retry, retried=%d delivery=%v event=%s", retried, deliveryUpdated, eventState)
	}
	if received["event_id"].(float64) != 50 || received["event_type"] != EventProjectBudgetThreshold {
		t.Fatalf("unexpected retry body: %#v", received)
	}
}

func TestEventService_RetryDueDeliveriesKeepsEventFailedWhenOtherDeliveriesRemain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var eventState string
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "FROM notification_deliveries d") {
				t.Fatalf("unexpected retry query: %s", query)
			}
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*int64) = 61
					*dest[1].(*int64) = 51
					*dest[2].(*int64) = 9
					*dest[3].(*string) = server.URL
					*dest[4].(*int64) = 3
					*dest[5].(*int64) = 9
					*dest[6].(*string) = EventProjectBudgetThreshold
					*dest[7].(*string) = NotificationSeverityCritical
					*dest[8].(*string) = "Budget reached"
					*dest[9].(*string) = "Project Core reached budget"
					*dest[10].(*string) = "project"
					*dest[11].(*string) = "9"
					*dest[12].(*[]byte) = []byte(`{"spent_cents":1200}`)
					return nil
				},
			}}, nil
		},
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "COUNT(*) FILTER") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 1
				*dest[1].(*int64) = 1
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "UPDATE notification_events") {
				eventState = args[0].(string)
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db, "").WithHTTPClient(server.Client())

	retried, err := svc.RetryDueDeliveries(context.Background(), 10)
	if err != nil {
		t.Fatalf("retry due deliveries: %v", err)
	}
	if retried != 1 || eventState != DeliveryStateFailed {
		t.Fatalf("expected event to remain failed while another delivery is not delivered, retried=%d state=%s", retried, eventState)
	}
}

func TestEventService_CommercialAdmissionFailsClosedBeforePersistenceOrDelivery(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			t.Fatal("commercially disabled notifications must not query")
			return nil, nil
		},
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			t.Fatal("commercially disabled notifications must not query")
			return &testutil.MockRow{}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			t.Fatal("commercially disabled notifications must not write")
			return pgconn.CommandTag{}, nil
		},
	}
	svc := NewEventService(db, "")
	svc.SetAdmission(func() bool { return false })
	result, err := svc.Emit(context.Background(), EventInput{OrgID: 1, EventType: EventAPIKeyDisabled, Title: "disabled"})
	if err != nil || result.Created || result.DeliveryState != DeliveryStateNotConfigured {
		t.Fatalf("Emit() = %#v, %v", result, err)
	}
	if retried, err := svc.RetryDueDeliveries(context.Background(), 10); err != nil || retried != 0 {
		t.Fatalf("RetryDueDeliveries() = %d, %v", retried, err)
	}
	event := outbox.Event{Type: outbox.EventNotificationDelivery, Payload: []byte(`{"delivery_id":1}`)}
	if err := svc.ProcessNotificationDelivery(context.Background(), event); err != nil {
		t.Fatalf("ProcessNotificationDelivery() error = %v", err)
	}
	event = outbox.Event{Type: outbox.EventBudgetEvaluate, Payload: []byte(`{}`)}
	if err := svc.ProcessBudgetEvaluation(context.Background(), event); err != nil {
		t.Fatalf("ProcessBudgetEvaluation() error = %v", err)
	}
}

func TestEventService_ProcessNotificationDeliveryPropagatesDeliveredStatePersistenceFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	persistErr := errors.New("delivery state write failed")
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			if !strings.Contains(query, "FROM notification_deliveries d") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "pending"
				*dest[1].(*int64) = 42
				*dest[2].(*int64) = 7
				*dest[3].(*string) = server.URL
				*dest[4].(*bool) = true
				*dest[5].(*bool) = true
				*dest[6].(*bool) = false
				*dest[7].(*int64) = 3
				*dest[8].(*int64) = 9
				*dest[9].(*string) = EventProjectBudgetThreshold
				*dest[10].(*string) = NotificationSeverityWarning
				*dest[11].(*string) = "Budget threshold"
				*dest[12].(*string) = "Budget reached"
				*dest[13].(*string) = "project"
				*dest[14].(*string) = "9"
				*dest[15].(*[]byte) = []byte(`{}`)
				return nil
			}}
		},
		ExecFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "UPDATE notification_deliveries") && strings.Contains(query, "status = 'delivered'") {
				return pgconn.CommandTag{}, persistErr
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db, "").WithHTTPClient(server.Client())
	event := outbox.Event{Type: outbox.EventNotificationDelivery, Payload: []byte(`{"delivery_id":60}`)}

	err := svc.ProcessNotificationDelivery(context.Background(), event)
	if !errors.Is(err, persistErr) {
		t.Fatalf("ProcessNotificationDelivery() error = %v, want %v", err, persistErr)
	}
}

func TestEventService_ProcessNotificationDeliveryRepairsEventStateAfterDeliveredRetry(t *testing.T) {
	var eventState string
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "FROM notification_deliveries d"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "delivered"
					*dest[1].(*int64) = 42
					*dest[2].(*int64) = 7
					*dest[3].(*string) = "https://webhook.example.test/events"
					*dest[4].(*bool) = true
					*dest[5].(*bool) = true
					*dest[6].(*bool) = false
					*dest[7].(*int64) = 3
					*dest[8].(*int64) = 9
					*dest[9].(*string) = EventProjectBudgetThreshold
					*dest[10].(*string) = NotificationSeverityWarning
					*dest[11].(*string) = "Budget threshold"
					*dest[12].(*string) = "Budget reached"
					*dest[13].(*string) = "project"
					*dest[14].(*string) = "9"
					*dest[15].(*[]byte) = []byte(`{}`)
					return nil
				}}
			case strings.Contains(query, "COUNT(*) FILTER"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 0
					*dest[1].(*int64) = 1
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow: %s", query)
				return &testutil.MockRow{}
			}
		},
		ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "UPDATE notification_events") {
				eventState = args[0].(string)
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	svc := NewEventService(db, "")
	event := outbox.Event{Type: outbox.EventNotificationDelivery, Payload: []byte(`{"delivery_id":60}`)}

	if err := svc.ProcessNotificationDelivery(context.Background(), event); err != nil {
		t.Fatalf("ProcessNotificationDelivery() error = %v", err)
	}
	if eventState != DeliveryStateDelivered {
		t.Fatalf("event delivery state = %q, want %q", eventState, DeliveryStateDelivered)
	}
}

func TestEventService_WebhookSignatureUsesTimestampAndBody(t *testing.T) {
	var timestamp, signature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamp = r.Header.Get(WebhookTimestampHeader)
		signature = r.Header.Get(WebhookSignatureHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	db := &testutil.MockDB{ExecFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}}
	body := []byte(`{"event_id":42}`)
	svc := NewEventService(db, "test-secret").WithHTTPClient(server.Client())
	svc.now = func() time.Time { return time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC) }

	delivered, err := svc.sendWebhookAndUpdateDelivery(context.Background(), 9, server.URL, body)
	if err != nil || !delivered {
		t.Fatal("expected isolated webhook delivery to succeed")
	}
	wantTimestamp := "1784163723"
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(wantTimestamp + "."))
	_, _ = mac.Write(body)
	wantSignature := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if timestamp != wantTimestamp || signature != wantSignature {
		t.Fatalf("unexpected signing headers: timestamp=%q signature=%q", timestamp, signature)
	}
}

func TestEventService_DefaultPolicyBlocksLoopbackWithoutSending(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var failure string
	db := &testutil.MockDB{ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(query, "UPDATE notification_deliveries") && strings.Contains(query, "failed") {
			failure = args[1].(string)
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}}
	svc := NewEventService(db, "")

	loopbackHTTPS := "https://" + strings.TrimPrefix(server.URL, "http://")
	delivered, err := svc.sendWebhookAndUpdateDelivery(context.Background(), 10, loopbackHTTPS, []byte(`{}`))
	if err != nil || delivered {
		t.Fatal("loopback webhook must not be delivered by the production policy")
	}
	if called || failure != "webhook target is not allowed" || strings.Contains(failure, server.URL) {
		t.Fatalf("unsafe target was not safely rejected: called=%v failure=%q", called, failure)
	}
}

func TestEventService_WebhookNetworkPolicyAllowsOnlyPublicAddresses(t *testing.T) {
	for _, raw := range []string{
		"0.1.2.3",
		"10.20.30.40",
		"100.64.0.1",
		"100.100.100.200",
		"127.0.0.1",
		"169.254.169.254",
		"172.16.0.1",
		"192.168.1.20",
		"198.18.0.1",
		"192.0.2.1",
		"240.0.0.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"fec0::1",
		"::10.0.0.1",
		"64:ff9b::a00:1",
		"2001:2::1",
		"2001:db8::1",
		"2002:0a00:0001::1",
		"::ffff:10.0.0.1",
	} {
		if !blockedNotificationWebhookIP(net.ParseIP(raw)) {
			t.Fatalf("non-public webhook address %s must be blocked", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if blockedNotificationWebhookIP(net.ParseIP(raw)) {
			t.Fatalf("public webhook address %s must remain allowed", raw)
		}
	}
}

func TestEventService_ProductionWebhookURLRequiresHTTPS(t *testing.T) {
	httpTarget := httptest.NewRequest(http.MethodGet, "http://1.1.1.1/hook", nil).URL
	if validNotificationWebhookURL(httpTarget) {
		t.Fatal("production webhook policy must reject plaintext HTTP even for public targets")
	}
	httpsTarget := httptest.NewRequest(http.MethodGet, "https://1.1.1.1/hook", nil).URL
	if !validNotificationWebhookURL(httpsTarget) {
		t.Fatal("production webhook policy must allow a syntactically valid public HTTPS target")
	}
}

func TestEventService_WebhookDialRechecksDNSAgainstRebinding(t *testing.T) {
	resolver := &sequenceNotificationWebhookResolver{results: [][]net.IPAddr{
		{{IP: net.ParseIP("8.8.8.8")}},
		{{IP: net.ParseIP("10.0.0.5")}},
	}}
	addresses, err := resolveNotificationWebhookHostWithResolver(context.Background(), "rebind.example.test", resolver)
	if err != nil || len(addresses) != 1 || !addresses[0].Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("public preflight resolution failed: addresses=%v err=%v", addresses, err)
	}
	dialed := false
	_, err = dialNotificationWebhook(context.Background(), "tcp", "rebind.example.test:443", resolver, func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("test dial must not run")
	})
	if err == nil || dialed || resolver.calls != 2 {
		t.Fatalf("dial-time DNS rebinding was not blocked: err=%v dialed=%v calls=%d", err, dialed, resolver.calls)
	}
}

func TestEventService_WebhookRedirectToPrivateAddressIsNotSent(t *testing.T) {
	client := newNotificationWebhookClient(time.Second)
	requests := 0
	client.Transport = notificationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests > 1 {
			t.Fatalf("redirect target reached transport: %s", req.URL)
		}
		header := make(http.Header)
		header.Set("Location", "https://192.168.1.25/internal/admin")
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	resp, err := client.Get("https://8.8.8.8/start")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || requests != 1 {
		t.Fatalf("private redirect must fail before the second request: err=%v requests=%d", err, requests)
	}
}

type sequenceNotificationWebhookResolver struct {
	results [][]net.IPAddr
	calls   int
}

func (r *sequenceNotificationWebhookResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	if r.calls >= len(r.results) {
		return nil, errors.New("unexpected resolver call")
	}
	result := r.results[r.calls]
	r.calls++
	return result, nil
}

type notificationRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn notificationRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
