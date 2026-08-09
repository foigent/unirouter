package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessprovisioning"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCredentialCreatingHandlerErrorContracts(t *testing.T) {
	t.Parallel()

	writers := []struct {
		name  string
		write func(http.ResponseWriter, error)
	}{
		{name: "canonical issue and rotate", write: writeCredentialCreationError},
		{name: "legacy create adapter", write: writeLegacyCredentialCreationError},
	}
	commercialCases := []struct {
		name       string
		message    string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "commercial capacity trigger",
			message:    "commercial License limit exceeded",
			wantStatus: http.StatusConflict,
			wantCode:   "license_error.resource_limit_exceeded",
		},
		{
			name:       "commercial expiry trigger",
			message:    "commercial License is expired",
			wantStatus: http.StatusForbidden,
			wantCode:   "license_error.expired",
		},
	}

	for _, writer := range writers {
		writer := writer
		t.Run(writer.name, func(t *testing.T) {
			t.Parallel()
			for _, test := range commercialCases {
				test := test
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					pgErr := &pgconn.PgError{
						Code:           "P0001",
						ConstraintName: "commercial_license_active_api_keys",
						Message:        test.message,
					}
					// The access provisioning store wraps insert failures with
					// operation context. This verifies that the HTTP boundary uses
					// errors.As rather than losing the established License contract.
					wrapped := fmt.Errorf("insert grant credential: %w", pgErr)
					recorder := httptest.NewRecorder()
					writer.write(recorder, wrapped)

					require.Equal(t, test.wantStatus, recorder.Code, recorder.Body.String())
					require.JSONEq(t, fmt.Sprintf(
						`{"error":%q,"code":%q}`, test.message, test.wantCode,
					), recorder.Body.String())
				})
			}
		})
	}
}

func TestRotateAccessGrantCredentialRejectsCallerSuppliedScopeFields(t *testing.T) {
	t.Parallel()

	handler := NewHandler(&testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			t.Fatal("body/path mismatch must fail before database access")
			return nil, nil
		},
	}, "rotate-path-test", []byte("0123456789abcdef01234567"), nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/access-grants/grant_test/credentials/42/rotate",
		strings.NewReader(`{"name":"successor","predecessor_key_id":43}`))
	request.SetPathValue("ref", "grant_test")
	request.SetPathValue("key_id", "42")
	response := httptest.NewRecorder()
	handler.RotateAccessGrantCredential(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"reason":"invalid_request"`)
}

func TestCredentialCreatingHandlerNameConflictPrecedesGenericAccessErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		write    func(http.ResponseWriter, error)
		wantBody string
	}{
		{
			name:     "canonical issue and rotate",
			write:    writeCredentialCreationError,
			wantBody: `{"error":"Credential name already exists","reason":"credential_name_conflict"}`,
		},
		{
			name:     "legacy create adapter",
			write:    writeLegacyCredentialCreationError,
			wantBody: `{"error":"API key name already exists"}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			test.write(recorder, errors.Join(
				accessprovisioning.ErrConflict,
				accessprovisioning.ErrCredentialNameConflict,
			))

			require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
			require.JSONEq(t, test.wantBody, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), "service_unavailable")
		})
	}
}

func TestPublicAccessRequestRejectsReservedInternalSubmissionModesBeforeStoreAccess(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			t.Fatal("reserved submission mode must fail before database access")
			return nil, nil
		},
	}, "reserved-mode-test", []byte("0123456789abcdef01234567"), nil)
	for _, mode := range []string{
		accessprovisioning.SubmissionMigration,
		accessprovisioning.SubmissionBootstrap,
		accessprovisioning.SubmissionRecovery,
	} {
		t.Run(mode, func(t *testing.T) {
			body := fmt.Sprintf(`{
				"org_id":1,"project_id":1,"subject":{"type":"user","user_id":1},
				"request_kind":"human_personal","requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
				"environment":"personal","production_use":false,"submission_mode":%q
			}`, mode)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", strings.NewReader(body))
			response := httptest.NewRecorder()
			handler.CreateAccessRequest(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), `"reason":"invalid_request"`)
		})
	}
}
