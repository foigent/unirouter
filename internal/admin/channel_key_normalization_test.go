package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/testutil"
)

func TestNormalizeChannelKeyAllowedModels(t *testing.T) {
	got := normalizeChannelKeyAllowedModels(" mimo-v2.5-pro，mimo-v2.5；mimo-v2.5-pro; deepseek-chat\n ")
	want := "mimo-v2.5-pro,mimo-v2.5,deepseek-chat"
	if got != want {
		t.Fatalf("normalizeChannelKeyAllowedModels() = %q, want %q", got, want)
	}
}

func TestNormalizeChannelKeyAllowedModelsEmptyMeansAll(t *testing.T) {
	if got := normalizeChannelKeyAllowedModels(" ，,；;\n "); got != "" {
		t.Fatalf("normalizeChannelKeyAllowedModels() = %q, want empty", got)
	}
}

func TestCreateChannelKeyRejectsNonPositiveChannel(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest(http.MethodPost, "/channel-keys", strings.NewReader(`{"channel_id":-1,"key":"sk-test"}`))
	rec := httptest.NewRecorder()
	h.CreateChannelKey(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateChannelKeyRejectsInvalidState(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	for _, body := range []string{
		`{"channel_id":-1}`,
		`{"status":"cooling"}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/channel-keys/9", strings.NewReader(body))
		req.SetPathValue("id", "9")
		rec := httptest.NewRecorder()
		h.UpdateChannelKey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}
