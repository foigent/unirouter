package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestUsageAnalyticsGroup(t *testing.T) {
	group, expr, ok := usageAnalyticsGroup("overall")
	if !ok {
		t.Fatal("expected overall group to be valid")
	}
	if group.Key != "overall" || group.Label != "总体" || expr != "'全部请求'" {
		t.Fatalf("unexpected overall group: %#v expr=%q", group, expr)
	}

	group, expr, ok = usageAnalyticsGroup("alias")
	if !ok {
		t.Fatal("expected alias group to be valid")
	}
	if group.Key != "alias" || expr == "" {
		t.Fatalf("unexpected alias group: %#v expr=%q", group, expr)
	}

	group, expr, ok = usageAnalyticsGroup("owner")
	if !ok {
		t.Fatal("expected owner group to be valid")
	}
	if group.Key != "owner" || !strings.Contains(expr, "owner_user_id") {
		t.Fatalf("unexpected owner group: %#v expr=%q", group, expr)
	}

	group, expr, ok = usageAnalyticsGroup("key_type")
	if !ok {
		t.Fatal("expected key_type group to be valid")
	}
	if group.Key != "key_type" || !strings.Contains(expr, "key_type") {
		t.Fatalf("unexpected key_type group: %#v expr=%q", group, expr)
	}

	if _, _, ok := usageAnalyticsGroup("raw_sql"); ok {
		t.Fatal("unexpected invalid group_by to pass whitelist")
	}
}

func TestInternalStatusLevel(t *testing.T) {
	cases := []struct {
		critical int64
		warning  int64
		want     string
	}{
		{0, 0, "operational"},
		{0, 2, "warning"},
		{1, 0, "degraded"},
		{1, 2, "degraded"},
	}
	for _, tc := range cases {
		if got := internalStatusLevel(tc.critical, tc.warning); got != tc.want {
			t.Fatalf("critical=%d warning=%d: got %s want %s", tc.critical, tc.warning, got, tc.want)
		}
	}
}

func TestUsageAnalyticsUpstreamSourceSQLIncludesNull(t *testing.T) {
	summaryQuery := usageAnalyticsUpstreamSourceSQL()
	if summaryQuery != "COALESCE(usage_source, '') IN ('upstream', 'api', '')" {
		t.Fatalf("unexpected upstream source SQL: %s", summaryQuery)
	}
}

func TestAnalyticsConditionsMyScopeAllowsProjectScopedRoleWithoutProjectID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest("GET", "/usage/analytics?scope=my&key_type=personal", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(42))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(7))
	req = req.WithContext(ctx)

	conditions, args, err := h.analyticsConditions(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(conditions, " AND ")
	if !strings.Contains(joined, "org_id") || !strings.Contains(joined, "owner_user_id") || !strings.Contains(joined, "key_type") {
		t.Fatalf("expected org, owner, and key_type conditions, got %q", joined)
	}
	if len(args) != 3 || args[1] != int64(42) || args[2] != "personal" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestAnalyticsConditionsRejectsConflictingMyScopeOwner(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest("GET", "/usage/analytics?scope=my&owner_user_id=99", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(42))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(7))
	req = req.WithContext(ctx)

	if _, _, err := h.analyticsConditions(req); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected conflicting owner error, got %v", err)
	}
}
