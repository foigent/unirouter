package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
)

func TestValidateXInsightFeedRegistrationFrozenBundle(t *testing.T) {
	rawToken := auth.ReceiverFeedTokenPrefix + strings.Repeat("ab", 32)
	verifier, err := auth.ReceiverFeedVerifierHex(rawToken)
	require.NoError(t, err)
	valid := xInsightFeedCredentialRegistrationV1{
		SchemaVersion:  auth.ReceiverFeedRegistrationSchema,
		TokenFormat:    auth.ReceiverFeedTokenFormat,
		VerifierScheme: auth.ReceiverFeedVerifierScheme,
		KeyPrefix:      rawToken[:auth.ReceiverFeedCredentialPrefixLength],
		VerifierSHA256: verifier,
		OperationID:    strings.Repeat("cd", 16),
	}
	decoded, err := validateXInsightFeedRegistration(valid)
	require.NoError(t, err)
	require.Len(t, decoded, 32)

	tests := []struct {
		name   string
		mutate func(*xInsightFeedCredentialRegistrationV1)
	}{
		{"schema", func(bundle *xInsightFeedCredentialRegistrationV1) { bundle.SchemaVersion += " " }},
		{"token format", func(bundle *xInsightFeedCredentialRegistrationV1) { bundle.TokenFormat = "ur-xf1-hex256" }},
		{"scheme", func(bundle *xInsightFeedCredentialRegistrationV1) {
			bundle.VerifierScheme = auth.LegacyAPIVerifierScheme
		}},
		{"prefix case", func(bundle *xInsightFeedCredentialRegistrationV1) {
			bundle.KeyPrefix = strings.ToUpper(bundle.KeyPrefix)
		}},
		{"prefix length", func(bundle *xInsightFeedCredentialRegistrationV1) { bundle.KeyPrefix += "0" }},
		{"verifier case", func(bundle *xInsightFeedCredentialRegistrationV1) {
			bundle.VerifierSHA256 = strings.ToUpper(bundle.VerifierSHA256)
		}},
		{"operation case", func(bundle *xInsightFeedCredentialRegistrationV1) {
			bundle.OperationID = strings.ToUpper(bundle.OperationID)
		}},
		{"operation whitespace", func(bundle *xInsightFeedCredentialRegistrationV1) { bundle.OperationID += "\n" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			_, err := validateXInsightFeedRegistration(candidate)
			require.ErrorIs(t, err, errXInsightFeedRegistration)
		})
	}
}

func TestXInsightFeedRegistrationJSONIsStrictAndResponseIsSecretFree(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/feed",
		strings.NewReader(`{"allowed_ip":"127.0.0.1/32","registration":{},"extra":true}`),
	)
	response := httptest.NewRecorder()
	var body createXInsightFeedCredentialRequest
	require.False(t, decodeXInsightFeedControlJSON(response, request, &body))
	require.Equal(t, http.StatusBadRequest, response.Code)

	request = httptest.NewRequest(
		http.MethodPost,
		"/feed",
		strings.NewReader(`{"allowed_ip":"127.0.0.1/32","registration":{}} {}`),
	)
	response = httptest.NewRecorder()
	require.False(t, decodeXInsightFeedControlJSON(response, request, &body))
	require.Equal(t, http.StatusBadRequest, response.Code)

	encoded, err := json.Marshal(stagedXInsightFeedCredentialResponse{
		ID:             1,
		KeyPrefix:      "ur-xf1-0123456789abc",
		Lifecycle:      "staged",
		ProjectID:      2,
		Purpose:        xInsightFeedKeyPurpose,
		KeyType:        "service",
		VerifierScheme: auth.ReceiverFeedVerifierScheme,
		OperationID:    "0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"key"`)
	require.NotContains(t, string(encoded), "verifier_sha256")
}

func TestXInsightFeedRegistrationHandlersRejectMalformedBundleBeforeDatabaseAccess(t *testing.T) {
	handler := &Handler{}
	tests := []struct {
		name   string
		target string
		body   string
		invoke func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "initial missing registration",
			target: "/api/v1/data-exchange/xinsight/connections/2/credentials",
			body:   `{"allowed_ip":"127.0.0.1/32","registration":{}}`,
			invoke: handler.CreateXInsightFeedCredential,
		},
		{
			name:   "initial unknown nested field",
			target: "/api/v1/data-exchange/xinsight/connections/2/credentials",
			body:   `{"allowed_ip":"127.0.0.1/32","registration":{"unknown":true}}`,
			invoke: handler.CreateXInsightFeedCredential,
		},
		{
			name:   "rotation noncanonical values",
			target: "/api/v1/data-exchange/xinsight/connections/2/rotation/prepare",
			body:   `{"registration":{"schema_version":"unirouter.xinsight.feed-credential-registration.v1 ","token_format":"ur-xf1-hex256.v1","verifier_scheme":"receiver_sha256_v1","key_prefix":"ur-xf1-0123456789abc","verifier_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","operation_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`,
			invoke: handler.PrepareXInsightFeedRotation,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.target, strings.NewReader(test.body))
			request.SetPathValue("project_id", "2")
			response := httptest.NewRecorder()
			test.invoke(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "verifier_sha256")
		})
	}
}
