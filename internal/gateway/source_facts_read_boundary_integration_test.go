//go:build integration

package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// Exercise the actual source reader without creating a source receiver or
// invoking its pending lifecycle handlers. Successful authenticated pulls and
// purpose/profile authorization remain separate acceptance requirements.
func TestSourceFactsReaderRejectsUnauthenticatedAndInvalidHTTPWithoutWritesIntegration(t *testing.T) {
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, _, modelKey := sourceGatewayAttachSource(t, f)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	handler := source.Handler(resolver)
	disabled, err := sourcefacts.New(nil, sourcefacts.Options{})
	require.NoError(t, err)
	const path = "/api/v1/integrations/xinsight/source-facts"
	valid := path + "?profile=" + sourcefacts.ContextProfile
	readCounts := func() [8]int64 {
		var counts [8]int64
		require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT
		 (SELECT count(*) FROM execution_source_facts),
		 (SELECT count(*) FROM execution_source_streams),
		 (SELECT count(*) FROM execution_source_connections),
		 (SELECT count(*) FROM execution_source_credentials),
		 (SELECT count(*) FROM execution_source_acknowledgements),
		 (SELECT count(*) FROM freeze_idempotency),
		 (SELECT count(*) FROM usage_finalization_journal),
		 (SELECT count(*) FROM api_keys)`).Scan(
			&counts[0], &counts[1], &counts[2], &counts[3],
			&counts[4], &counts[5], &counts[6], &counts[7]))
		return counts
	}
	before := readCounts()
	for _, tc := range []struct {
		name, method, target string
		authorizations       []string
		status               int
		code                 string
		disabled             bool
	}{
		{name: "disabled", method: "GET", target: valid, status: 503, code: "source_disabled", disabled: true},
		{name: "write_method", method: "POST", target: valid, status: 405, code: "method_not_allowed"},
		{name: "missing_profile", method: "GET", target: path, status: 400, code: "unsupported_profile"},
		{name: "unknown_profile", method: "GET", target: path + "?profile=unknown", status: 400, code: "unsupported_profile"},
		{name: "duplicate_profile", method: "GET", target: valid + "&profile=" + sourcefacts.IdentityProfile, status: 400, code: "invalid_query"},
		{name: "caller_scope_override", method: "GET", target: valid + "&project_id=sensitive-query-marker", status: 400, code: "invalid_query"},
		{name: "duplicate_limit", method: "GET", target: valid + "&limit=1&limit=2", status: 400, code: "invalid_query"},
		{name: "zero_limit", method: "GET", target: valid + "&limit=0", status: 400, code: "invalid_limit"},
		{name: "over_page_limit", method: "GET", target: valid + "&limit=51", status: 400, code: "invalid_limit"},
		{name: "fractional_limit", method: "GET", target: valid + "&limit=1.5", status: 400, code: "invalid_limit"},
		{name: "capabilities_query", method: "GET", target: path + "/capabilities?limit=1", status: 400, code: "invalid_query"},
		{name: "missing_bearer", method: "GET", target: valid, status: 401, code: "source_credential_denied"},
		{name: "model_bearer", method: "GET", target: valid, authorizations: []string{"Bearer " + modelKey}, status: 401, code: "source_credential_denied"},
		{name: "duplicate_authorization", method: "GET", target: valid, authorizations: []string{"Bearer " + modelKey, "Bearer " + modelKey}, status: 401, code: "source_credential_denied"},
		{name: "malformed_authorization", method: "GET", target: valid, authorizations: []string{"Basic sensitive-header-marker"}, status: 401, code: "source_credential_denied"},
		{name: "unauthenticated_capabilities", method: "GET", target: path + "/capabilities", status: 401, code: "source_credential_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			for _, value := range tc.authorizations {
				r.Header.Add("Authorization", value)
			}
			w := httptest.NewRecorder()
			selected := handler
			if tc.disabled {
				selected = disabled.Handler(resolver)
			}
			selected.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			var body struct {
				Error struct {
					Code      string `json:"code"`
					Retryable bool   `json:"retryable"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, tc.code, body.Error.Code)
			require.Equal(t, tc.status == http.StatusServiceUnavailable, body.Error.Retryable)
			if tc.status == http.StatusMethodNotAllowed {
				require.Equal(t, "GET", w.Header().Get("Allow"))
			}
			for _, secret := range []string{modelKey, sourceGatewaySyntheticSecret, "sensitive-query-marker", "sensitive-header-marker"} {
				require.NotContains(t, w.Body.String(), secret)
			}
		})
	}
	require.Equal(t, before, readCounts(), "rejected reads must not create credentials, advance streams/acks or write business facts")
	require.Zero(t, f.ProviderCalls())
}
