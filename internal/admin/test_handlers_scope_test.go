package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestChannelTestModelConfiguredUsesExactNormalizedMembership(t *testing.T) {
	models := " moonshot-v1-8k,moonshot-v1-32k,moonshot-v1-128k "
	if !channelTestModelConfigured(models, "moonshot-v1-32k") {
		t.Fatal("expected configured model to match")
	}
	if channelTestModelConfigured(models, "moonshot-v1") {
		t.Fatal("partial model name must not match")
	}
}

func TestTestChannelRequiresEligibleCredentialForSelectedModel(t *testing.T) {
	queryCount := 0
	var credentialQuery string
	var credentialArgs []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch queryCount {
				case 1:
					*dest[0].(*string) = "https://provider.invalid"
					*dest[1].(*string) = "moonshot-v1-8k,moonshot-v1-32k"
					*dest[2].(*string) = "openai_compatible"
					*dest[3].(*json.RawMessage) = json.RawMessage(`{}`)
					return nil
				case 2:
					credentialQuery = sql
					credentialArgs = append([]any(nil), args...)
					return pgx.ErrNoRows
				default:
					t.Fatalf("unexpected query count %d", queryCount)
					return nil
				}
			}}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/channels/6/test", strings.NewReader(`{"model":"moonshot-v1-8k","prompt":"ping"}`))
	req.SetPathValue("id", "6")
	rec := httptest.NewRecorder()

	h.TestChannel(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"status":"no_eligible_key"`) {
		t.Fatalf("expected no_eligible_key 400, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, required := range []string{"ck.status = 'active'", "ck.cooldown_until", "ck.allowed_models"} {
		if !strings.Contains(credentialQuery, required) {
			t.Fatalf("credential query missing %q: %s", required, credentialQuery)
		}
	}
	if len(credentialArgs) != 2 || credentialArgs[0] != int64(6) || credentialArgs[1] != "moonshot-v1-8k" {
		t.Fatalf("unexpected credential query args: %#v", credentialArgs)
	}
}
