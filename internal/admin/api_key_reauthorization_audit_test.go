package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCreateAPIKeyReauthorizesActorAtCommit(t *testing.T) {
	state := newAPIKeySecurityTestState()
	state.actorStatus = "disabled"
	h := newAPIKeySecurityTestHandler(state)
	recorder := httptest.NewRecorder()
	req := apiKeySecurityRequest(http.MethodPost, "/api/v1/keys",
		`{"org_id":1,"project_id":11,"name":"stale-create","key_type":"shared"}`,
		auth.RoleProjectAdmin)

	h.CreateAPIKey(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("revoked actor must not create a key, got status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if state.keyMutated || state.committed || !state.rolledBack {
		t.Fatalf("revoked create must roll back before key insertion: %+v", state)
	}
	if strings.Contains(recorder.Body.String(), `"key"`) {
		t.Fatalf("revoked create must not return raw key material: %s", recorder.Body.String())
	}
}

func TestCreateAPIKeyReauthorizesOwnerAtCommit(t *testing.T) {
	state := newAPIKeySecurityTestState()
	state.actorRole = auth.RoleOrgAdmin
	state.ownerMembership = false
	h := newAPIKeySecurityTestHandler(state)
	recorder := httptest.NewRecorder()
	req := apiKeySecurityRequest(http.MethodPost, "/api/v1/keys",
		`{"org_id":1,"project_id":11,"owner_user_id":8,"name":"stale-owner","key_type":"personal"}`,
		auth.RoleOrgAdmin)

	h.CreateAPIKey(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("owner removed from the project must prevent key creation, got status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if state.keyMutated || state.committed || !state.rolledBack {
		t.Fatalf("invalid owner create must roll back before key insertion: %+v", state)
	}
}

func TestCreateAPIKeyReauthorizationAllowsAuthorizedControl(t *testing.T) {
	state := newAPIKeySecurityTestState()
	h := newAPIKeySecurityTestHandler(state)
	recorder := httptest.NewRecorder()
	req := apiKeySecurityRequest(http.MethodPost, "/api/v1/keys",
		`{"org_id":1,"project_id":11,"name":"authorized-create","key_type":"shared"}`,
		auth.RoleProjectAdmin)

	h.CreateAPIKey(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("currently authorized actor must still create a key, got status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !state.actorLock || !state.keyMutated || !state.auditAttempted || !state.committed || state.rolledBack {
		t.Fatalf("authorized create must reauthorize, mutate, audit, and commit: %+v", state)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	rawKey, _ := response["key"].(string)
	if !strings.HasPrefix(rawKey, "ur-0001-") {
		t.Fatalf("authorized create must preserve one-time raw-key response: %s", recorder.Body.String())
	}
}

func TestUpdateAPIKeyReauthorizesActorAtCommit(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*apiKeySecurityTestState)
	}{
		{name: "user_disabled", mutate: func(state *apiKeySecurityTestState) { state.actorStatus = "disabled" }},
		{name: "role_changed", mutate: func(state *apiKeySecurityTestState) { state.actorRole = auth.RoleDeveloper }},
		{name: "organization_disabled", mutate: func(state *apiKeySecurityTestState) { state.actorOrgStatus = "disabled" }},
		{name: "membership_removed", mutate: func(state *apiKeySecurityTestState) { state.actorMembership = false }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state := newAPIKeySecurityTestState()
			testCase.mutate(state)
			h := newAPIKeySecurityTestHandler(state)
			recorder := httptest.NewRecorder()
			req := apiKeySecurityRequest(http.MethodPut, "/api/v1/keys/42", `{"status":"disabled"}`, auth.RoleProjectAdmin)
			req.SetPathValue("id", "42")

			h.UpdateAPIKey(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("stale authorization must not update a key, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if state.keyMutated || state.committed || !state.rolledBack {
				t.Fatalf("stale update must roll back before mutation: %+v", state)
			}
			if state.auditAttempted {
				t.Fatal("rejected stale update must not write an update audit")
			}
		})
	}
}

func TestUpdateAPIKeyAuditRetainsRedactedBeforeAfter(t *testing.T) {
	state := newAPIKeySecurityTestState()
	state.actorRole = auth.RoleOrgAdmin
	h := newAPIKeySecurityTestHandler(state)
	recorder := httptest.NewRecorder()
	req := apiKeySecurityRequest(http.MethodPut, "/api/v1/keys/42",
		`{"status":"disabled","rpm_limit":30,"rps_limit":5,"allowed_ips":"203.0.113.10/32","allowed_models":"model-b","policy_labels":["restricted"]}`,
		auth.RoleOrgAdmin)
	req.SetPathValue("id", "42")

	h.UpdateAPIKey(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("authorized update must remain available, got status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !state.actorLock || !state.keyMutated || !state.auditAttempted || !state.committed {
		t.Fatalf("authorized update must reauthorize, mutate, audit, and commit: %+v", state)
	}
	var before, after map[string]any
	var detail map[string]any
	if err := json.Unmarshal([]byte(state.auditDetail), &detail); err != nil {
		t.Fatalf("decode audit detail: %v (%q)", err, state.auditDetail)
	}
	if err := json.Unmarshal([]byte(state.auditBefore), &before); err != nil {
		t.Fatalf("decode before audit state: %v (%q)", err, state.auditBefore)
	}
	if err := json.Unmarshal([]byte(state.auditAfter), &after); err != nil {
		t.Fatalf("decode after audit state: %v (%q)", err, state.auditAfter)
	}
	wantFields := []any{"allowed_ips", "allowed_models", "policy_labels", "status", "rpm_limit", "rps_limit"}
	if !reflect.DeepEqual(detail["fields"], wantFields) {
		t.Fatalf("audit fields=%#v, want %#v", detail["fields"], wantFields)
	}
	assertAPIKeyAuditValue(t, before, "status", "active")
	assertAPIKeyAuditValue(t, after, "status", "disabled")
	assertAPIKeyAuditValue(t, before, "rpm_limit", float64(60))
	assertAPIKeyAuditValue(t, after, "rpm_limit", float64(30))
	assertAPIKeyAuditValue(t, before, "rps_limit", float64(10))
	assertAPIKeyAuditValue(t, after, "rps_limit", float64(5))
	assertAPIKeyAuditValue(t, before, "qps_limit", float64(10))
	assertAPIKeyAuditValue(t, after, "qps_limit", float64(5))
	assertAPIKeyAuditValue(t, before, "allowed_ips", "198.51.100.0/24")
	assertAPIKeyAuditValue(t, after, "allowed_ips", "203.0.113.10/32")
	assertAPIKeyAuditValue(t, before, "allowed_models", "model-a")
	assertAPIKeyAuditValue(t, after, "allowed_models", "model-b")
	if !reflect.DeepEqual(before["policy_labels"], []any{"baseline"}) || !reflect.DeepEqual(after["policy_labels"], []any{"restricted"}) {
		t.Fatalf("audit policy labels must preserve canonical transition, before=%#v after=%#v", before, after)
	}
	for label, snapshot := range map[string]map[string]any{"detail": detail, "before": before, "after": after} {
		assertNoAPIKeyCredentialAuditFields(t, label, snapshot)
	}

	combined := strings.ToLower(state.auditDetail + state.auditBefore + state.auditAfter)
	for _, forbidden := range []string{"raw-key-sentinel", "stored-hash-sentinel", "key_hash", "key_prefix"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("audit leaked forbidden credential material %q: %s", forbidden, combined)
		}
	}
}

func TestUpdateAPIKeyAuditIncludesImplicitOwnerTransition(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		initialOwner  int64
		initialType   string
		wantType      string
		body          string
		wantBeforeOwn any
		wantAfterOwn  any
	}{
		{
			name:          "personal_to_shared_clears_owner",
			initialOwner:  7,
			initialType:   "personal",
			wantType:      "shared",
			body:          `{"key_type":"shared"}`,
			wantBeforeOwn: float64(7),
			wantAfterOwn:  nil,
		},
		{
			name:          "shared_to_personal_assigns_actor",
			initialOwner:  0,
			initialType:   "shared",
			wantType:      "personal",
			body:          `{"key_type":"personal"}`,
			wantBeforeOwn: nil,
			wantAfterOwn:  float64(7),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state := newAPIKeySecurityTestState()
			state.actorRole = auth.RoleOrgAdmin
			state.keyState.ownerUserID = testCase.initialOwner
			state.keyState.keyType = testCase.initialType
			h := newAPIKeySecurityTestHandler(state)
			recorder := httptest.NewRecorder()
			req := apiKeySecurityRequest(http.MethodPut, "/api/v1/keys/42", testCase.body, auth.RoleOrgAdmin)
			req.SetPathValue("id", "42")

			h.UpdateAPIKey(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("authorized identity update must succeed, got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var detail, before, after map[string]any
			if err := json.Unmarshal([]byte(state.auditDetail), &detail); err != nil {
				t.Fatalf("decode audit detail: %v", err)
			}
			if err := json.Unmarshal([]byte(state.auditBefore), &before); err != nil {
				t.Fatalf("decode before audit state: %v", err)
			}
			if err := json.Unmarshal([]byte(state.auditAfter), &after); err != nil {
				t.Fatalf("decode after audit state: %v", err)
			}
			if !reflect.DeepEqual(detail["fields"], []any{"owner_user_id", "key_type"}) {
				t.Fatalf("implicit ownership change is missing from audit fields: %#v", detail)
			}
			if before["owner_user_id"] != testCase.wantBeforeOwn || after["owner_user_id"] != testCase.wantAfterOwn {
				t.Fatalf("owner transition before=%#v after=%#v, want %#v -> %#v", before["owner_user_id"], after["owner_user_id"], testCase.wantBeforeOwn, testCase.wantAfterOwn)
			}
			assertAPIKeyAuditValue(t, before, "key_type", testCase.initialType)
			assertAPIKeyAuditValue(t, after, "key_type", testCase.wantType)
		})
	}
}

func assertNoAPIKeyCredentialAuditFields(t *testing.T, label string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for field, child := range typed {
			switch strings.ToLower(field) {
			case "key", "raw_key", "key_hash", "key_prefix":
				t.Fatalf("%s audit contains credential field %q: %#v", label, field, value)
			}
			assertNoAPIKeyCredentialAuditFields(t, label, child)
		}
	case []any:
		for _, child := range typed {
			assertNoAPIKeyCredentialAuditFields(t, label, child)
		}
	}
}

func assertAPIKeyAuditValue(t *testing.T, state map[string]any, field string, want any) {
	t.Helper()
	if got := state[field]; got != want {
		t.Fatalf("audit %s=%#v, want %#v; state=%#v", field, got, want, state)
	}
}

func apiKeySecurityRequest(method, target, body, role string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	organizationRole, _ := auth.NormalizeOrganizationRole(role)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, organizationRole)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "api-key-security-test")
	return req.WithContext(ctx)
}

type apiKeySecurityTestState struct {
	actorOrgID      int64
	actorRole       string
	actorStatus     string
	actorOrgStatus  string
	actorMembership bool
	ownerStatus     string
	ownerMembership bool
	keyState        apiKeySecurityRow
	actorLock       bool
	keyMutated      bool
	auditAttempted  bool
	commitAttempted bool
	committed       bool
	rolledBack      bool
	auditDetail     string
	auditBefore     string
	auditAfter      string
}

type apiKeySecurityRow struct {
	orgID         int64
	projectID     int64
	ownerUserID   int64
	keyType       string
	purpose       string
	name          string
	status        string
	rpmLimit      int
	qpsLimit      int
	allowedIPs    string
	allowedModels string
	policyLabels  string
}

func newAPIKeySecurityTestState() *apiKeySecurityTestState {
	return &apiKeySecurityTestState{
		actorOrgID:      1,
		actorRole:       auth.RoleProjectAdmin,
		actorStatus:     "active",
		actorOrgStatus:  "active",
		actorMembership: true,
		ownerStatus:     "active",
		ownerMembership: true,
		keyState: apiKeySecurityRow{
			orgID: 1, projectID: 11, keyType: "shared", name: "audit-target", status: "active",
			rpmLimit: 60, qpsLimit: 10, allowedIPs: "198.51.100.0/24", allowedModels: "model-a", policyLabels: "baseline",
		},
	}
}

func newAPIKeySecurityTestHandler(state *apiKeySecurityTestState) *Handler {
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch {
				case strings.Contains(sql, "INSERT INTO api_keys"):
					state.keyMutated = true
					*dest[0].(*int64) = 42
					return nil
				case strings.Contains(sql, "FROM users u") && strings.Contains(sql, "JOIN orgs o"):
					organizationRole, _ := auth.NormalizeOrganizationRole(state.actorRole)
					*dest[0].(*int64) = state.actorOrgID
					*dest[1].(*string) = organizationRole
					*dest[2].(*string) = state.actorRole
					*dest[3].(*string) = state.actorStatus
					*dest[4].(*string) = state.actorOrgStatus
					return nil
				case strings.Contains(sql, "FROM orgs"):
					*dest[0].(*string) = state.actorOrgStatus
					return nil
				case strings.Contains(sql, "FROM projects"):
					*dest[0].(*string) = "active"
					return nil
				case strings.Contains(sql, "FROM users owner_user"):
					if state.ownerStatus != "active" {
						return pgx.ErrNoRows
					}
					*dest[0].(*string) = state.ownerStatus
					return nil
				case strings.Contains(sql, "FROM project_members owner_membership"):
					if !state.ownerMembership {
						return pgx.ErrNoRows
					}
					*dest[0].(*string) = projectMemberDeveloper
					return nil
				case strings.Contains(sql, "FROM project_members"):
					if !state.actorMembership {
						return pgx.ErrNoRows
					}
					*dest[0].(*string) = projectMemberAdmin
					return nil
				case strings.Contains(sql, "FROM api_keys"):
					lowerSQL := strings.ToLower(sql)
					if strings.Contains(lowerSQL, "key_hash") || strings.Contains(lowerSQL, "key_prefix") {
						return errors.New("audit state query selected credential material")
					}
					return scanAPIKeySecurityRow(dest, state.keyState)
				default:
					return errors.New("unexpected transaction query: " + sql)
				}
			}}
		},
		ExecFn: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "pg_advisory_xact_lock"):
				state.actorLock = true
				return pgconn.NewCommandTag("SELECT 1"), nil
			case strings.Contains(sql, "UPDATE api_keys"):
				state.keyMutated = true
				if strings.Contains(sql, "status = $") {
					state.keyState.status = "disabled"
				}
				if strings.Contains(sql, "rpm_limit = $") {
					state.keyState.rpmLimit = 30
				}
				if strings.Contains(sql, "qps_limit = $") {
					state.keyState.qpsLimit = 5
				}
				if strings.Contains(sql, "allowed_ips = $") {
					state.keyState.allowedIPs = "203.0.113.10/32"
				}
				if strings.Contains(sql, "allowed_models = $") {
					state.keyState.allowedModels = "model-b"
				}
				if strings.Contains(sql, "policy_labels = $") {
					state.keyState.policyLabels = "restricted"
				}
				if strings.Contains(sql, "owner_user_id = $") {
					switch owner := args[0].(type) {
					case int64:
						state.keyState.ownerUserID = owner
					case nil:
						state.keyState.ownerUserID = 0
					default:
						return pgconn.CommandTag{}, errors.New("unexpected owner_user_id argument")
					}
					state.keyState.keyType = args[1].(string)
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				state.auditAttempted = true
				state.auditDetail = args[5].(string)
				state.auditBefore = args[11].(string)
				state.auditAfter = args[12].(string)
				return pgconn.NewCommandTag("INSERT 1"), nil
			default:
				return pgconn.CommandTag{}, errors.New("unexpected transaction statement: " + sql)
			}
		},
		CommitFn: func(context.Context) error {
			state.commitAttempted = true
			state.committed = true
			return nil
		},
		RollbackFn: func(context.Context) error {
			state.rolledBack = true
			return nil
		},
	}

	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
		QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch {
				case strings.Contains(sql, "FROM api_keys WHERE id"):
					return scanAPIKeySecurityRow(dest, state.keyState)
				case strings.Contains(sql, "SELECT id FROM projects"):
					*dest[0].(*int64) = 11
					return nil
				case strings.Contains(sql, "SELECT COUNT(*) FROM projects"):
					*dest[0].(*int) = 1
					return nil
				case strings.Contains(sql, "SELECT COUNT(*) FROM users"):
					*dest[0].(*int) = 1
					return nil
				case strings.Contains(sql, "FROM project_members"):
					*dest[0].(*int) = 1
					return nil
				default:
					return errors.New("unexpected pool query: " + sql)
				}
			}}
		},
		BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil },
	}
	return NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
}

func scanAPIKeySecurityRow(dest []any, row apiKeySecurityRow) error {
	if len(dest) == 4 {
		*dest[0].(*int64) = row.orgID
		*dest[1].(*int64) = row.projectID
		*dest[2].(*int64) = row.ownerUserID
		*dest[3].(*string) = row.keyType
		return nil
	}
	if len(dest) == 7 {
		*dest[0].(*int64) = row.orgID
		*dest[1].(*int64) = row.projectID
		*dest[2].(*int64) = row.ownerUserID
		*dest[3].(*string) = row.keyType
		*dest[4].(*string) = row.purpose
		*dest[5].(*string) = row.allowedIPs
		*dest[6].(*string) = row.allowedModels
		return nil
	}
	if len(dest) != 12 {
		return errors.New("unexpected API-key scan width")
	}
	*dest[0].(*int64) = row.orgID
	*dest[1].(*int64) = row.projectID
	*dest[2].(*int64) = row.ownerUserID
	*dest[3].(*string) = row.keyType
	*dest[4].(*string) = row.name
	*dest[5].(*string) = row.status
	*dest[6].(*int) = row.rpmLimit
	*dest[7].(*int) = row.qpsLimit
	*dest[8].(*string) = row.allowedIPs
	*dest[9].(*string) = row.allowedModels
	*dest[10].(*string) = row.policyLabels
	*dest[11].(*string) = row.purpose
	return nil
}
