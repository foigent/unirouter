package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamAttemptPolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
		status int
		ack    string
	}{
		{name: "absent", status: http.StatusNoContent},
		{name: "one", values: []string{"1"}, status: http.StatusNoContent, ack: "1"},
		{name: "empty", values: []string{""}, status: http.StatusBadRequest},
		{name: "other value", values: []string{"2"}, status: http.StatusBadRequest},
		{name: "repeated", values: []string{"1", "1"}, status: http.StatusBadRequest},
		{name: "joined", values: []string{"1, 1"}, status: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			for _, value := range test.values {
				r.Header.Add(ChatUpstreamAttemptsHeader, value)
			}
			w := httptest.NewRecorder()
			handler := UpstreamAttemptPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, test.ack == "1", SingleUpstreamAttemptRequested(r.Context()))
				w.WriteHeader(http.StatusNoContent)
			}))
			handler.ServeHTTP(w, r)
			require.Equal(t, test.status, w.Code)
			require.Equal(t, test.ack, w.Header().Get(ChatUpstreamAttemptsHeader))
		})
	}
}

func TestChatUpstreamAttemptPolicyChangesOnlyOptedInFingerprint(t *testing.T) {
	fingerprint := func(body, optIn string) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		if optIn != "" {
			r.Header.Set(ChatUpstreamAttemptsHeader, optIn)
		}
		var digest string
		handler := UpstreamAttemptPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			digest, err = fingerprintRequest(r, "test-fingerprint-secret")
			require.NoError(t, err)
			w.WriteHeader(http.StatusNoContent)
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNoContent, w.Code)
		return digest
	}

	const body = `{"model":"fixture"}`
	original := fingerprint(body, "")
	require.Equal(t, "f4df8719320b8c0e9bcba66c986c54d3d9e55d7cd0f444e5892b5f77167b35c2", original)
	single := fingerprint(body, "1")
	require.NotEqual(t, original, single)
	require.Equal(t, single, fingerprint(`{ "model": "fixture" }`, "1"))
}

func TestChatUpstreamAttemptPolicyRejectsChangingOptInForExistingKey(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			for _, single := range []bool{false, true} {
				var executions int
				handler := UpstreamAttemptPolicy(scopedIdempotencyHandler(newMemoryIdempotencyStore(),
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						executions++
						w.WriteHeader(http.StatusOK)
					}),
				))
				send := func(single bool) *httptest.ResponseRecorder {
					r := newIdempotentRequest(`{"model":"fixture"}`, "same-request")
					r.URL.Path = path
					if single {
						r.Header.Set(ChatUpstreamAttemptsHeader, "1")
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					return w
				}
				require.Equal(t, http.StatusOK, send(single).Code)
				conflict := send(!single)
				require.Equal(t, http.StatusConflict, conflict.Code)
				replay := send(single)
				require.Equal(t, http.StatusConflict, replay.Code)
				if path == "/v1/messages" {
					require.Contains(t, conflict.Body.String(), "The Idempotency-Key was already used with a different request")
					require.Contains(t, replay.Body.String(), "The request already completed; its model response was not stored for replay")
				} else {
					require.Contains(t, conflict.Body.String(), "idempotency_error.key_reused_with_different_request")
					require.Contains(t, replay.Body.String(), "idempotency_error.already_completed")
				}
				if single {
					require.Equal(t, "1", replay.Header().Get(ChatUpstreamAttemptsHeader))
				}
				require.Equal(t, 1, executions)
			}
		})
	}
}
