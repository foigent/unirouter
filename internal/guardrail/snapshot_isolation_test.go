package guardrail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestTenantRefreshFailureRetainsOnlyThatTenantSnapshot(t *testing.T) {
	options := DefaultOptions()
	options.SnapshotMaxAge = 20 * time.Millisecond
	options.RefreshInterval = time.Millisecond
	options.MaxActivePoliciesPerOrg = 2
	service, err := NewService(nil, nil, []byte("0123456789abcdef0123456789abcdef"), options)
	if err != nil {
		t.Fatal(err)
	}

	orgA, orgB := int64(11), int64(22)
	platform := testSnapshotPolicy(1, nil, "platform", ActionAudit)
	oldA := testSnapshotPolicy(2, &orgA, "org-a-old", ActionAudit)
	oldB := testSnapshotPolicy(3, &orgB, "org-b-old", ActionAudit)
	old := time.Now().Add(-time.Second)
	if err := service.publishPolicyRefresh(old, []Policy{platform, oldA, oldB}, nil, true); err != nil {
		t.Fatal(err)
	}

	invalidA := testSnapshotPolicy(4, &orgA, "org-a-invalid", ActionAudit)
	invalidA.DetectorRef = "external:not-registered"
	newB := testSnapshotPolicy(5, &orgB, "org-b-new", ActionBlock)
	err = service.publishPolicyRefresh(time.Now(), []Policy{platform, invalidA, newB}, nil, true)
	if err != nil {
		t.Fatalf("tenant-local validation failure became globally fatal: %v", err)
	}

	resultA := service.Pin(Subject{OrgID: orgA, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if resultA.Decision != DecisionBlock || resultA.BlockedBy != "guardrail:snapshot:tenant" {
		t.Fatalf("invalid tenant did not retain its stale fail-closed snapshot: %+v", resultA)
	}
	resultB := service.Pin(Subject{OrgID: orgB, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if resultB.Decision != DecisionBlock || resultB.BlockedBy != newB.Ref || resultB.PolicySetVersion == "unavailable" {
		t.Fatalf("healthy tenant was not independently refreshed: %+v", resultB)
	}
	resultC := service.Pin(Subject{OrgID: 33, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "safe"}},
	)
	if resultC.Decision != DecisionAllow || resultC.Degraded {
		t.Fatalf("tenant without configured policy inherited another tenant failure: %+v", resultC)
	}
}

func TestTenantPolicyExcessDoesNotInvalidateHealthyTenant(t *testing.T) {
	options := DefaultOptions()
	options.MaxActivePoliciesPerOrg = 2
	service, err := NewService(nil, nil, []byte("0123456789abcdef0123456789abcdef"), options)
	if err != nil {
		t.Fatal(err)
	}

	orgA, orgB := int64(41), int64(42)
	policies := []Policy{
		testSnapshotPolicy(1, nil, "platform", ActionAudit),
		testSnapshotPolicy(2, &orgA, "org-a-1", ActionAudit),
		testSnapshotPolicy(3, &orgA, "org-a-2", ActionAudit),
		testSnapshotPolicy(4, &orgA, "org-a-3", ActionAudit),
		testSnapshotPolicy(5, &orgB, "org-b", ActionBlock),
	}
	err = service.publishPolicyRefresh(time.Now(), policies, map[int64]struct{}{orgA: {}}, true)
	if err != nil {
		t.Fatalf("tenant-local policy excess became globally fatal: %v", err)
	}

	resultA := service.Pin(Subject{OrgID: orgA, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if resultA.Decision != DecisionBlock || resultA.BlockedBy != "guardrail:snapshot:tenant" {
		t.Fatalf("over-limit tenant did not fail closed locally: %+v", resultA)
	}
	resultB := service.Pin(Subject{OrgID: orgB, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if resultB.Decision != DecisionBlock || resultB.BlockedBy != policies[4].Ref {
		t.Fatalf("over-limit tenant invalidated healthy tenant: %+v", resultB)
	}
}

func TestTenantCacheExcessFailsClosedOnlyForOmittedTenant(t *testing.T) {
	options := DefaultOptions()
	options.MaxActivePoliciesPerOrg = 2
	options.MaxActivePlatformPolicies = 2
	options.MaxCachedPolicies = 4
	options.MaxCachedTenantScopes = 1
	service, err := NewService(nil, nil, []byte("0123456789abcdef0123456789abcdef"), options)
	if err != nil {
		t.Fatal(err)
	}

	orgA, orgB := int64(45), int64(46)
	policyA := testSnapshotPolicy(1, &orgA, "org-a", ActionBlock)
	policyB := testSnapshotPolicy(2, &orgB, "org-b", ActionBlock)
	if err := service.publishPolicyRefresh(time.Now(), []Policy{policyA, policyB}, nil, true); err != nil {
		t.Fatalf("tenant cache excess became globally fatal: %v", err)
	}

	resultA := service.Pin(Subject{OrgID: orgA, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if resultA.Decision != DecisionBlock || resultA.BlockedBy != policyA.Ref {
		t.Fatalf("tenant inside the cache bound was not independently published: %+v", resultA)
	}
	resultB := service.Pin(Subject{OrgID: orgB, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "safe"}},
	)
	if resultB.Decision != DecisionBlock || resultB.BlockedBy != "guardrail:snapshot:tenant" {
		t.Fatalf("omitted tenant did not fail closed locally: %+v", resultB)
	}
}

func TestPublishedTenantSnapshotDoesNotAliasMutableInput(t *testing.T) {
	service, err := NewService(nil, nil, []byte("0123456789abcdef0123456789abcdef"), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	orgID := int64(47)
	policy := testSnapshotPolicy(1, &orgID, "immutable-tenant", ActionBlock)
	if err := service.publishPolicyRefresh(time.Now(), []Policy{policy}, nil, true); err != nil {
		t.Fatal(err)
	}

	orgID = 999
	policy.Config[0] = '['
	result := service.Pin(Subject{OrgID: 47, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if result.Decision != DecisionBlock || result.BlockedBy != "guardrail:immutable-tenant" {
		t.Fatalf("published snapshot aliased mutable policy input: %+v", result)
	}
}

func TestInvalidPlatformSnapshotFailsClosedGloballyWhileTenantRefreshPublishes(t *testing.T) {
	options := DefaultOptions()
	options.SnapshotMaxAge = 20 * time.Millisecond
	options.RefreshInterval = time.Millisecond
	service, err := NewService(nil, nil, []byte("0123456789abcdef0123456789abcdef"), options)
	if err != nil {
		t.Fatal(err)
	}

	orgB := int64(52)
	oldPlatform := testSnapshotPolicy(1, nil, "platform-old", ActionAudit)
	oldB := testSnapshotPolicy(2, &orgB, "org-b-old", ActionAudit)
	if err := service.publishPolicyRefresh(time.Now().Add(-time.Second), []Policy{oldPlatform, oldB}, nil, true); err != nil {
		t.Fatal(err)
	}
	invalidPlatform := testSnapshotPolicy(3, nil, "platform-invalid", ActionBlock)
	invalidPlatform.DetectorRef = "external:not-registered"
	newB := testSnapshotPolicy(4, &orgB, "org-b-new", ActionAudit)
	err = service.publishPolicyRefresh(time.Now(), []Policy{invalidPlatform, newB}, nil, true)
	if err == nil || !strings.Contains(err.Error(), "platform") {
		t.Fatalf("invalid platform snapshot was not reported: %v", err)
	}

	pinned := service.Pin(Subject{OrgID: orgB, Model: "gpt"})
	if got := pinned.policies; len(got) != 2 || got[1].Ref != newB.Ref {
		t.Fatalf("tenant copy-on-write refresh was not published beside retained platform scope: %+v", got)
	}
	result := pinned.Evaluate(t.Context(), StageInput, []Text{{Content: "safe"}})
	if result.Decision != DecisionBlock || result.BlockedBy != "guardrail:snapshot:platform" {
		t.Fatalf("stale mandatory platform scope did not fail closed globally: %+v", result)
	}
}

func TestInitialRefreshAllowsHealthyTenantWhenAnotherTenantIsInvalid(t *testing.T) {
	badOrgID, goodOrgID := int64(61), int64(62)
	bad := testSnapshotPolicy(1, &badOrgID, "bad-tenant", ActionAudit)
	bad.DetectorRef = "external:not-registered"
	good := testSnapshotPolicy(2, &goodOrgID, "good-tenant", ActionBlock)
	database := &testutil.MockDB{QueryFn: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(query, "HAVING COUNT(*)") {
			return &testutil.MockRows{}, nil
		}
		return &testutil.MockRows{Rows: []func(...any) error{
			scanSnapshotPolicyRow(bad),
			scanSnapshotPolicyRow(good),
		}}, nil
	}}
	service, err := NewService(database, nil, []byte("0123456789abcdef0123456789abcdef"), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("initial refresh was globally blocked by one invalid tenant: %v", err)
	}

	badResult := service.Pin(Subject{OrgID: badOrgID, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "safe"}},
	)
	if badResult.Decision != DecisionBlock || badResult.BlockedBy != "guardrail:snapshot:tenant" {
		t.Fatalf("invalid tenant did not fail closed locally: %+v", badResult)
	}
	goodResult := service.Pin(Subject{OrgID: goodOrgID, Model: "gpt"}).Evaluate(
		t.Context(), StageInput, []Text{{Content: "alice@example.com"}},
	)
	if goodResult.Decision != DecisionBlock || goodResult.BlockedBy != "guardrail:2" {
		t.Fatalf("healthy tenant was not published during initial refresh: %+v", goodResult)
	}
}

func scanSnapshotPolicyRow(policy Policy) func(...any) error {
	return func(dest ...any) error {
		*dest[0].(*int64) = policy.ID
		*dest[1].(**int64) = policy.OrgID
		*dest[2].(**int64) = policy.ProjectID
		*dest[3].(**int64) = policy.APIKeyID
		*dest[4].(*string) = policy.Name
		*dest[5].(*string) = policy.Model
		*dest[6].(*string) = policy.Stage
		*dest[7].(*string) = policy.DetectorRef
		*dest[8].(*string) = policy.Action
		*dest[9].(*string) = policy.Mode
		*dest[10].(*string) = policy.FailureMode
		*dest[11].(*bool) = policy.Mandatory
		*dest[12].(*json.RawMessage) = append(json.RawMessage(nil), policy.Config...)
		*dest[13].(*int64) = policy.Version
		*dest[14].(*time.Time) = time.Now().UTC()
		*dest[15].(*time.Time) = time.Now().UTC()
		return nil
	}
}

func testSnapshotPolicy(id int64, orgID *int64, name, action string) Policy {
	return Policy{
		ID: id, Ref: "guardrail:" + strings.TrimPrefix(name, "guardrail:"), Name: name, Version: 1,
		OrgID: orgID, Stage: StageInput, DetectorRef: DetectorPII, Action: action,
		Mode: ModeEnforce, FailureMode: FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	}
}
