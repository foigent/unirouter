package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestReauthorizeXInsightFeedControlTxUsesExistingCommitOrderInvariant(t *testing.T) {
	tests := []struct {
		name          string
		operation     apiKeyMutationOperation
		currentRole   string
		actorStatus   string
		projectStatus string
		mustBeActive  bool
		wantError     bool
	}{
		{name: "authorized create", operation: apiKeyMutationCreate, currentRole: auth.RolePlatformAdmin, actorStatus: "active", projectStatus: "active", mustBeActive: true},
		{name: "authorized update", operation: apiKeyMutationUpdate, currentRole: auth.RolePlatformAdmin, actorStatus: "active", projectStatus: "active", mustBeActive: true},
		{name: "actor disabled", operation: apiKeyMutationCreate, currentRole: auth.RolePlatformAdmin, actorStatus: "disabled", projectStatus: "active", mustBeActive: true, wantError: true},
		{name: "role changed", operation: apiKeyMutationCreate, currentRole: auth.RoleOrgAdmin, actorStatus: "active", projectStatus: "active", mustBeActive: true, wantError: true},
		{name: "project disabled blocks expansion", operation: apiKeyMutationUpdate, currentRole: auth.RolePlatformAdmin, actorStatus: "active", projectStatus: "disabled", mustBeActive: true, wantError: true},
		{name: "project disabled permits cleanup", operation: apiKeyMutationUpdate, currentRole: auth.RolePlatformAdmin, actorStatus: "active", projectStatus: "disabled"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var operations []string
			tx := &testutil.MockTx{
				ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
					if !strings.Contains(sql, "pg_advisory_xact_lock") {
						return pgconn.CommandTag{}, errors.New("unexpected statement before feed control lock: " + sql)
					}
					operations = append(operations, "actor-advisory-lock")
					return pgconn.NewCommandTag("SELECT 1"), nil
				},
				QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						switch {
						case strings.Contains(sql, "FROM users u") && strings.Contains(sql, "JOIN orgs o"):
							operations = append(operations, "actor")
							organizationRole, _ := auth.NormalizeOrganizationRole(test.currentRole)
							*dest[0].(*int64) = 1
							*dest[1].(*string) = organizationRole
							*dest[2].(*string) = test.currentRole
							*dest[3].(*string) = test.actorStatus
							*dest[4].(*string) = "active"
							return nil
						case strings.Contains(sql, "SELECT status FROM orgs"):
							operations = append(operations, "target-org")
							*dest[0].(*string) = "active"
							return nil
						case strings.Contains(sql, "SELECT status FROM projects"):
							operations = append(operations, "target-project")
							*dest[0].(*string) = test.projectStatus
							return nil
						default:
							return errors.New("unexpected feed reauthorization query: " + sql)
						}
					}}
				},
			}
			handler := &Handler{}
			request := apiKeySecurityRequest(http.MethodPost, "/api/v1/data-exchange/xinsight/connections/11", "", auth.RolePlatformAdmin)

			err := handler.reauthorizeXInsightFeedControlTx(
				request.Context(), tx, 1, 11, test.operation, test.mustBeActive,
			)

			if test.wantError {
				require.ErrorIs(t, err, errAPIKeyAuthorizationChanged)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"actor-advisory-lock", "actor", "target-org", "target-project"}, operations)
			}
			require.NotEmpty(t, operations)
			require.Equal(t, "actor-advisory-lock", operations[0], "user advisory lock must precede project and feed/key locks")
		})
	}
}

func TestWriteXInsightFeedControlErrorMapsReauthorizationChangeToForbidden(t *testing.T) {
	response := httptest.NewRecorder()

	writeXInsightFeedControlError(response, errAPIKeyAuthorizationChanged)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.JSONEq(t, `{"error":"Forbidden"}`, response.Body.String())
}
