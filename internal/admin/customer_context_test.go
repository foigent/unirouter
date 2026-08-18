package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func customerActorContext(role string, identityOrgID int64, customer customercontext.Snapshot) context.Context {
	ctx := context.WithValue(context.Background(), middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, role)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, identityOrgID)
	return context.WithValue(ctx, middleware.CtxCustomerContext, customer)
}

func TestResolveActorOrganizationUsesCanonicalCustomer(t *testing.T) {
	ready := customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: 22, OrganizationName: "Acme"}

	t.Run("platform defaults to customer instead of identity org", func(t *testing.T) {
		ctx := customerActorContext(auth.RolePlatformAdmin, 1, ready)
		orgID, err := resolveActorOrganization(ctx, 0)
		if err != nil || orgID != 22 {
			t.Fatalf("resolveActorOrganization() = %d, %v; want 22, nil", orgID, err)
		}
		if _, err := resolveActorOrganization(ctx, 1); err == nil {
			t.Fatal("platform identity organization must not be accepted as customer scope")
		}
	})

	t.Run("customer identity must match canonical customer", func(t *testing.T) {
		matching := customerActorContext(auth.RoleOrgAdmin, 22, ready)
		orgID, err := resolveActorOrganization(matching, 0)
		if err != nil || orgID != 22 {
			t.Fatalf("resolveActorOrganization() = %d, %v; want 22, nil", orgID, err)
		}
		mismatched := customerActorContext(auth.RoleOrgAdmin, 23, ready)
		if _, err := resolveActorOrganization(mismatched, 0); err == nil {
			t.Fatal("non-customer identity must fail closed")
		}
	})

	t.Run("non-ready customer state fails closed", func(t *testing.T) {
		ctx := customerActorContext(auth.RolePlatformAdmin, 1, customercontext.Snapshot{State: customercontext.StateAmbiguous})
		if _, err := resolveActorOrganization(ctx, 0); err == nil {
			t.Fatal("ambiguous customer context must fail closed")
		}
	})
}

func TestActorOrgConstraintBindsPlatformToCanonicalCustomer(t *testing.T) {
	ready := customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: 22, OrganizationName: "Acme"}
	ctx := customerActorContext(auth.RolePlatformAdmin, 1, ready)
	orgID, constrained := actorOrgConstraint(ctx)
	if !constrained || orgID != 22 {
		t.Fatalf("actorOrgConstraint() = %d, %v; want 22, true", orgID, constrained)
	}

	legacyCtx := context.WithValue(context.Background(), middleware.CtxUserRole, auth.RolePlatformAdmin)
	legacyCtx = context.WithValue(legacyCtx, middleware.CtxOrganizationRole, auth.RolePlatformAdmin)
	if _, constrained := actorOrgConstraint(legacyCtx); constrained {
		t.Fatal("direct-handler compatibility context must remain unconstrained")
	}
}

func TestActorIdentityScopeReflectsCurrentAuthority(t *testing.T) {
	ready := customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: 22, OrganizationName: "Acme"}
	if got := actorIdentityScope(customerActorContext(auth.RolePlatformAdmin, 1, ready), auth.RolePlatformAdmin); got != "deployment_operator" {
		t.Fatalf("ready platform identity scope = %q", got)
	}
	if got := actorIdentityScope(customerActorContext(auth.RolePlatformAdmin, 22, ready), auth.RolePlatformAdmin); got != "deployment_operator_and_customer" {
		t.Fatalf("explicit dual identity scope = %q", got)
	}
	notInitialized := customercontext.Snapshot{State: customercontext.StateNotInitialized}
	if got := actorIdentityScope(customerActorContext(auth.RolePlatformAdmin, 1, notInitialized), auth.RolePlatformAdmin); got != "deployment_operator" {
		t.Fatalf("pre-bootstrap platform identity scope = %q", got)
	}
	if got := actorIdentityScope(customerActorContext(auth.RoleOrgAdmin, 22, ready), auth.RoleOrgAdmin); got != "customer_identity" {
		t.Fatalf("customer identity scope = %q", got)
	}
}

func TestProjectHelpersFailClosedWhenCustomerContextIsNotReady(t *testing.T) {
	handler := NewHandler(nil, "secret", nil, nil)
	ctx := customerActorContext(auth.RolePlatformAdmin, 1, customercontext.Snapshot{State: customercontext.StateAmbiguous})
	checks := map[string]bool{
		"read project":             handler.canReadProject(ctx, 7),
		"write project":            handler.canWriteProject(ctx, 7),
		"manage members":           handler.canManageProjectMembers(ctx, 7),
		"create project key":       handler.canCreateProjectKey(ctx, 7),
		"read project usage":       handler.canReadProjectUsage(ctx, 7),
		"read credential metadata": handler.canReadProjectCredentialMetadata(ctx, 7),
	}
	for name, allowed := range checks {
		if allowed {
			t.Fatalf("%s unexpectedly allowed without a ready customer context", name)
		}
	}
}

func TestModelGovernancePathScopeBindsPlatformToCanonicalCustomer(t *testing.T) {
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	})

	request := httptest.NewRequest(http.MethodPut, "/api/v1/model-sets/42/items", nil).WithContext(ctx)
	scope, err := resolveModelGovernancePathScope(request)
	if err != nil {
		t.Fatalf("resolve canonical path scope: %v", err)
	}
	if !scope.constrained || scope.orgID != 7 {
		t.Fatalf("expected canonical customer scope, got %#v", scope)
	}

	foreign := httptest.NewRequest(http.MethodPut, "/api/v1/model-sets/42/items?org_id=8", nil).WithContext(ctx)
	if _, err := resolveModelGovernancePathScope(foreign); !errors.Is(err, errActorOrganizationForbidden) {
		t.Fatalf("expected foreign organization to fail closed, got %v", err)
	}

	unavailable := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{State: customercontext.StateAmbiguous})
	request = httptest.NewRequest(http.MethodPut, "/api/v1/model-sets/42/items", nil).WithContext(unavailable)
	if _, err := resolveModelGovernancePathScope(request); !errors.Is(err, errActorOrganizationForbidden) {
		t.Fatalf("expected ambiguous customer context to fail closed, got %v", err)
	}
}

func TestModelAuthorizationScopeBindsPlatformToCanonicalCustomer(t *testing.T) {
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-authorizations", nil).WithContext(ctx)
	if orgID, err := resolveModelAuthorizationOrg(request, 0); err != nil || orgID != 7 {
		t.Fatalf("resolve canonical model authorization scope = %d, %v; want 7, nil", orgID, err)
	}
	if _, err := resolveModelAuthorizationOrg(request, 8); err == nil {
		t.Fatal("foreign organization must fail closed")
	}

	unavailable := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{State: customercontext.StateUnavailable})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/model-authorizations", nil).WithContext(unavailable)
	if _, err := resolveModelAuthorizationOrg(request, 0); err == nil {
		t.Fatal("unavailable customer context must fail closed")
	}
}

func TestAccessAndWorkloadScopesBindPlatformToCanonicalCustomer(t *testing.T) {
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/access/legacy-preview", nil).WithContext(ctx)
	if orgID, err := resolveAccessProvisioningOrg(request, 0); err != nil || orgID != 7 {
		t.Fatalf("resolve access provisioning scope = %d, %v; want 7, nil", orgID, err)
	}
	if orgID, err := resolveWorkloadApplicationOrg(request, 0); err != nil || orgID != 7 {
		t.Fatalf("resolve workload application scope = %d, %v; want 7, nil", orgID, err)
	}
	if _, err := resolveAccessProvisioningOrg(request, 8); err == nil {
		t.Fatal("foreign access provisioning organization must fail closed")
	}
	if _, err := resolveWorkloadApplicationOrg(request, 8); err == nil {
		t.Fatal("foreign workload organization must fail closed")
	}

	unavailable := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{State: customercontext.StateInactive, OrganizationID: 7})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/workload-applications", nil).WithContext(unavailable)
	if _, err := resolveAccessProvisioningOrg(request, 0); err == nil {
		t.Fatal("inactive customer must block access provisioning scope")
	}
	if _, err := resolveWorkloadApplicationOrg(request, 0); err == nil {
		t.Fatal("inactive customer must block workload scope")
	}
}

func TestAccessActorScopeUsesCanonicalCustomerInsteadOfIdentityOrganization(t *testing.T) {
	ready := customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	}
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, ready)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(42))
	scope := accessActorScope(ctx)
	if scope.UserID != 42 || scope.OrgID != 7 || scope.OrganizationRole != auth.RolePlatformAdmin || !scope.EnforceOrgBoundary {
		t.Fatalf("unexpected access actor scope: %#v", scope)
	}

	unavailable := customerActorContext(
		auth.RolePlatformAdmin,
		99,
		customercontext.Snapshot{State: customercontext.StateAmbiguous},
	)
	unavailable = context.WithValue(unavailable, middleware.CtxUserID, int64(42))
	if scope := accessActorScope(unavailable); scope.OrgID != 0 || !scope.EnforceOrgBoundary {
		t.Fatalf("ambiguous customer context must produce an invalid access scope: %#v", scope)
	}
}

func TestPlatformAPIKeyLookupIsBoundToCanonicalCustomer(t *testing.T) {
	ready := customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	}
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, ready)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(42))
	clause, args, ok := actorScopedAPIKeyLookupClause(ctx, apiKeyObjectWrite, "ak", 2)
	if !ok || clause != " AND ak.org_id = $2" || len(args) != 1 || args[0] != int64(7) {
		t.Fatalf("unexpected canonical API-key lookup scope: clause=%q args=%#v ok=%t", clause, args, ok)
	}

	unavailable := customerActorContext(
		auth.RolePlatformAdmin,
		99,
		customercontext.Snapshot{State: customercontext.StateUnavailable},
	)
	unavailable = context.WithValue(unavailable, middleware.CtxUserID, int64(42))
	if clause, args, ok := actorScopedAPIKeyLookupClause(unavailable, apiKeyObjectRead, "ak", 2); ok || clause != "" || args != nil {
		t.Fatalf("unavailable customer context must fail closed: clause=%q args=%#v ok=%t", clause, args, ok)
	}
}

func TestProjectCommitRequirementIsBoundToCanonicalCustomer(t *testing.T) {
	ready := customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	}
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, ready)
	requirement := committingActorRequirement{
		Project: &committingActorProjectRequirement{ProjectID: 42},
	}
	bound, err := constrainProjectCommitRequirementToCustomer(ctx, requirement)
	if err != nil || bound.TargetOrgID != 7 {
		t.Fatalf("canonical project commit scope = %#v, %v; want org 7", bound, err)
	}

	requirement.TargetOrgID = 8
	if _, err := constrainProjectCommitRequirementToCustomer(ctx, requirement); !errors.Is(err, errCommittingActorScopeNotFound) {
		t.Fatalf("foreign project commit target must fail closed, got %v", err)
	}

	unavailable := customerActorContext(
		auth.RolePlatformAdmin,
		99,
		customercontext.Snapshot{State: customercontext.StateAmbiguous},
	)
	requirement.TargetOrgID = 0
	if _, err := constrainProjectCommitRequirementToCustomer(unavailable, requirement); !errors.Is(err, errCommittingActorScopeNotFound) {
		t.Fatalf("ambiguous project commit context must fail closed, got %v", err)
	}

	organizationOnly := committingActorRequirement{TargetOrgID: 99}
	unchanged, err := constrainProjectCommitRequirementToCustomer(ctx, organizationOnly)
	if err != nil || unchanged.TargetOrgID != 99 {
		t.Fatalf("organization-only requirement must retain its explicit contract: %#v, %v", unchanged, err)
	}
}

func TestUserMutationTargetScopeUsesCanonicalCustomer(t *testing.T) {
	ready := customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	}
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, ready)
	if orgID, unconstrained, ok := userMutationTargetScope(ctx); !ok || unconstrained || orgID != 7 {
		t.Fatalf("canonical user mutation scope = %d, %t, %t; want 7, false, true", orgID, unconstrained, ok)
	}

	unavailable := customerActorContext(
		auth.RolePlatformAdmin,
		99,
		customercontext.Snapshot{State: customercontext.StateInactive, OrganizationID: 7},
	)
	if orgID, unconstrained, ok := userMutationTargetScope(unavailable); ok || unconstrained || orgID != 0 {
		t.Fatalf("inactive customer user mutation scope must fail closed: %d, %t, %t", orgID, unconstrained, ok)
	}

	legacy := context.WithValue(context.Background(), middleware.CtxUserRole, auth.RolePlatformAdmin)
	legacy = context.WithValue(legacy, middleware.CtxOrganizationRole, auth.RolePlatformAdmin)
	legacy = context.WithValue(legacy, middleware.CtxOrgID, int64(99))
	if orgID, unconstrained, ok := userMutationTargetScope(legacy); !ok || !unconstrained || orgID != 99 {
		t.Fatalf("legacy platform user mutation scope = %d, %t, %t; want 99, true, true", orgID, unconstrained, ok)
	}
}

func TestCustomerDomainHelpersDoNotUsePlatformIdentityOrganization(t *testing.T) {
	ctx := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{
		State: customercontext.StateReady, OrganizationID: 7, OrganizationName: "Customer",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/data-exchanges", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	if orgID, ok := dataExchangeOrgID(recorder, request); !ok || orgID != 7 {
		t.Fatalf("data exchange scope = %d, %t; want 7, true", orgID, ok)
	}

	handler := NewHandler(nil, "secret", nil, nil)
	selector := policySelector{OrgID: int64Pointer(8)}
	if err := handler.canonicalizePolicySelector(ctx, &selector); !errors.Is(err, errLimitPolicyForbidden) {
		t.Fatalf("foreign customer policy selector should fail closed, got %v", err)
	}
	global := policySelector{}
	if err := handler.canonicalizePolicySelector(ctx, &global); err != nil {
		t.Fatalf("deployment-global policy should remain available, got %v", err)
	}

	unavailable := customerActorContext(auth.RolePlatformAdmin, 99, customercontext.Snapshot{State: customercontext.StateAmbiguous})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/data-exchanges", nil).WithContext(unavailable)
	recorder = httptest.NewRecorder()
	if _, ok := dataExchangeOrgID(recorder, request); ok || recorder.Code != http.StatusForbidden {
		t.Fatalf("ambiguous customer data exchange should fail closed, status=%d ok=%t", recorder.Code, ok)
	}
}

func TestListOrganizationsReturnsOnlyCanonicalCustomer(t *testing.T) {
	handler := NewHandler(nil, "secret", nil, nil)
	ready := customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: 22, OrganizationName: "Acme"}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	request = request.WithContext(customerActorContext(auth.RolePlatformAdmin, 1, ready))
	recorder := httptest.NewRecorder()

	handler.ListOrganizations(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 1 || len(response.Data) != 1 || response.Data[0].ID != 22 || response.Data[0].Name != "Acme" {
		t.Fatalf("unexpected organizations response: %+v", response)
	}
}

func TestListOrganizationsReturnsEmptyForNonReadyCustomerContext(t *testing.T) {
	handler := NewHandler(nil, "secret", nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	request = request.WithContext(customerActorContext(
		auth.RolePlatformAdmin,
		1,
		customercontext.Snapshot{State: customercontext.StateNotInitialized},
	))
	recorder := httptest.NewRecorder()

	handler.ListOrganizations(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"data\":[],\"total\":0}\n" {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestListOrganizationsRejectsMismatchedCustomerIdentity(t *testing.T) {
	handler := NewHandler(nil, "secret", nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	request = request.WithContext(customerActorContext(
		auth.RoleOrgAdmin,
		23,
		customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: 22, OrganizationName: "Acme"},
	))
	recorder := httptest.NewRecorder()

	handler.ListOrganizations(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestListUsersCustomerScopeExcludesDeploymentOperators(t *testing.T) {
	var countQuery, listQuery string
	database := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			countQuery = sql
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			listQuery = sql
			return &testutil.MockRows{}, nil
		},
	}
	handler := NewHandler(database, "secret", nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users?auth_provider=local", nil)
	request = request.WithContext(customerActorContext(
		auth.RolePlatformAdmin,
		99,
		customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: 22, OrganizationName: "Acme"},
	))
	recorder := httptest.NewRecorder()

	handler.ListUsers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	for label, sql := range map[string]string{"count": countQuery, "list": listQuery} {
		if !strings.Contains(sql, "u.org_id = $1") || !strings.Contains(sql, "organization_role, ''), u.role) <> 'platform_admin'") {
			t.Fatalf("%s query does not enforce canonical customer member scope: %s", label, sql)
		}
	}
}
