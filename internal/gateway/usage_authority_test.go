package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F41AChatMissingUsageUsesVersionedConservativeEstimate(t *testing.T) {
	var settled billing.SettleRequest
	relay := newTestRelay(&testutil.MockDB{
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil },
	}, &mockBilling{
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	})
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl-missing","choices":[{"message":{"role":"assistant","content":"bounded answer"}}]}`,
		)),
	}
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(
		contextWithAttemptID(context.Background(), "att_p6f41_chat"), recorder, resp,
		&RouteResult{Channel: testOpenAIChannel()}, "op_p6f41_chat", 1, 1,
		"gpt-4o", "gpt-4o", "127.0.0.1",
		logger.New(logger.Fields{RequestID: "op_p6f41_chat", OrgID: 1}), time.Now(),
		nil, 120, 40, "upstream-p6f41-chat",
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected bounded estimate to permit response, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if settled.PromptTokens != 120 || settled.CompletionTokens != 40 ||
		settled.UsageAuthority != metering.UsageAuthorityConservativeEstimate ||
		settled.UsagePurpose != metering.UsagePurposeCustomerCharge ||
		settled.UsageFinality != metering.UsageFinalityEstimated ||
		settled.UsageContractVersion != metering.UsageContractVersion ||
		settled.UsageEstimateVersion != metering.ConservativeEstimateVersionV1 {
		t.Fatalf("missing Chat usage did not retain its bounded estimate contract: %+v", settled)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	cost := body["_uniroute"].(map[string]any)["cost"].(map[string]any)
	if cost["usage_authority"] != string(metering.UsageAuthorityConservativeEstimate) || cost["usage_estimate_version"] != metering.ConservativeEstimateVersionV1 {
		t.Fatalf("client receipt lost estimate labels: %#v", cost)
	}
	if cost["charged"] != float64(0) || cost["projected_charge"] == nil || cost["charge_finality"] != "pending_local_delivery" {
		t.Fatalf("pre-delivery response falsely claimed a terminal customer charge: %#v", cost)
	}
}

func TestP6F41AChatUnfundedConservativeEstimateWithholdsResponse(t *testing.T) {
	cancelled := false
	relay := newTestRelay(&testutil.MockDB{
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil },
	}, &mockBilling{
		prepareDeliveryFn: func(_ context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			return &billing.PrepareDeliveryResult{Funded: false, ErrorCode: "insufficient_topup"}, nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelled = true
			return nil
		},
	})
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"choices":[{"message":{"role":"assistant","content":"must stay private"}}]}`,
		)),
	}
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(
		context.Background(), recorder, resp, &RouteResult{Channel: testOpenAIChannel()},
		"op_p6f41_unfunded", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1",
		logger.New(logger.Fields{RequestID: "op_p6f41_unfunded", OrgID: 1}), time.Now(),
		nil, 120, 40, "",
	)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "must stay private") {
		t.Fatalf("unfunded estimate was not withheld: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if cancelled {
		t.Fatal("funding-pending provider cost must not use ordinary cancellation")
	}
}

func TestP6F41BChatWriterFailurePersistsReceiptWithoutCustomerCharge(t *testing.T) {
	var outcomes []string
	settleCalls := 0
	relay := newTestRelay(&testutil.MockDB{
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil },
	}, &mockBilling{
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			outcomes = append(outcomes, event.Outcome)
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{Success: true}, nil
		},
	})
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl-write-fail","choices":[{"message":{"role":"assistant","content":"must not be charged"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		)),
	}
	writer := &failAfterWritesResponseWriter{header: make(http.Header), failAt: 1}
	relay.relayNonStreamRequest(
		contextWithAttemptID(context.Background(), "att_p6f41_write_fail"), writer, resp,
		&RouteResult{Channel: testOpenAIChannel()}, "op_p6f41_write_fail", 1, 1,
		"gpt-4o", "gpt-4o", "127.0.0.1",
		logger.New(logger.Fields{RequestID: "op_p6f41_write_fail", OrgID: 1}), time.Now(),
		nil, 120, 40, "upstream-p6f41-write-fail",
	)
	if got, want := strings.Join(outcomes, ","), "write_started,write_failed"; got != want {
		t.Fatalf("delivery outcomes = %s, want %s", got, want)
	}
	if settleCalls != 0 {
		t.Fatalf("writer failure created %d customer settlements, want 0", settleCalls)
	}
}

func TestP6F41AEmbeddingsMissingUsageUsesInputOnlyConservativeEstimate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}],"model":"text-embedding-3-small"}`))
	}))
	defer upstream.Close()
	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-p6f41"), encKey)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(...any) error{
				embeddingsRouteRow(1, "embedding-primary", "openai", upstream.URL, encryptedKey, adapters.TypeOpenAICompatible, 1),
			}}, nil
		},
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil },
	}
	var settled billing.SettleRequest
	relay := NewRelay(db, nil, NewRouter(db), &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{Amount: 10, Success: true}, nil
		},
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
	}, nil, encKey)
	recorder := httptest.NewRecorder()
	relay.HandleEmbeddings(recorder, embeddingsRequest("op_p6f41_embeddings"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if settled.PromptTokens <= 0 || settled.CompletionTokens != 0 ||
		settled.UsageAuthority != metering.UsageAuthorityConservativeEstimate ||
		settled.UsageEstimateVersion != metering.ConservativeEstimateVersionV1 {
		t.Fatalf("missing Embeddings usage did not become an input-only bounded estimate: %+v", settled)
	}
}

func TestP6F41BEmbeddingsWriterFailureHasNoCustomerCharge(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}],"model":"text-embedding-3-small","usage":{"prompt_tokens":4,"total_tokens":4}}`))
	}))
	defer upstream.Close()
	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, err := auth.EncryptKey([]byte("sk-p6f41-writer"), encKey)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(...any) error{
				embeddingsRouteRow(1, "embedding-primary", "openai", upstream.URL, encryptedKey, adapters.TypeOpenAICompatible, 1),
			}}, nil
		},
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil },
	}
	var outcomes []string
	settleCalls := 0
	relay := NewRelay(db, nil, NewRouter(db), &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{Amount: 10, Success: true}, nil
		},
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			outcomes = append(outcomes, event.Outcome)
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{Success: true}, nil
		},
	}, nil, encKey)
	writer := &failAfterWritesResponseWriter{header: make(http.Header), failAt: 1}
	relay.HandleEmbeddings(writer, embeddingsRequest("op_p6f41_embeddings_write_fail"))
	if got, want := strings.Join(outcomes, ","), "write_started,write_failed"; got != want {
		t.Fatalf("delivery outcomes = %s, want %s", got, want)
	}
	if settleCalls != 0 {
		t.Fatalf("Embeddings writer failure created %d customer settlements, want 0", settleCalls)
	}
}

func testOpenAIChannel() models.Channel {
	return models.Channel{ID: 1, Name: "openai", Provider: "openai", AdapterType: adapters.TypeOpenAICompatible}
}
