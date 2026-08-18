package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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

func TestUsageAnalyticsEntityLabels(t *testing.T) {
	cases := []struct {
		groupKey string
		id       int64
		name     string
		detail   string
		want     string
	}{
		{groupKey: "project", id: 37, name: "Default Project", detail: "default", want: "默认项目（项目 #37）"},
		{groupKey: "owner", id: 34, name: "uiaccept", want: "uiaccept（用户 #34）"},
		{groupKey: "key", id: 61, name: "个人凭证", detail: "ur_live", want: "个人凭证 · ur_live***（凭证 #61）"},
	}
	for _, tc := range cases {
		if got := usageAnalyticsEntityLabel(tc.groupKey, tc.id, tc.name, tc.detail); got != tc.want {
			t.Fatalf("%s label: got %q want %q", tc.groupKey, got, tc.want)
		}
	}
}

func TestHydrateUsageAnalyticsLabelsUsesOneOrganizationScopedLookup(t *testing.T) {
	queryCalls := 0
	db := &testutil.MockDB{QueryFn: func(_ context.Context, query string, args ...any) (pgx.Rows, error) {
		queryCalls++
		if !strings.Contains(query, "FROM users") || !strings.Contains(query, "org_id=$1") {
			t.Fatalf("unexpected label query: %s", query)
		}
		if len(args) != 2 || args[0] != int64(7) {
			t.Fatalf("unexpected label args: %#v", args)
		}
		ids, ok := args[1].([]int64)
		if !ok || len(ids) != 1 || ids[0] != 34 {
			t.Fatalf("unexpected label ids: %#v", args[1])
		}
		return &testutil.MockRows{Rows: []func(...any) error{
			func(dest ...any) error {
				*dest[0].(*int64) = 34
				*dest[1].(*string) = "uiaccept"
				*dest[2].(*string) = ""
				return nil
			},
		}}, nil
	}}
	h := newTestHandler(db)
	groups := []map[string]interface{}{{"key": "34"}, {"key": "0"}}

	if err := h.hydrateUsageAnalyticsLabels(context.Background(), "owner", 7, groups); err != nil {
		t.Fatalf("hydrate labels: %v", err)
	}
	if queryCalls != 1 || groups[0]["label"] != "uiaccept（用户 #34）" || groups[1]["label"] != "未归属" {
		t.Fatalf("unexpected hydrated groups: %#v calls=%d", groups, queryCalls)
	}
}

func TestNotificationAttentionLevel(t *testing.T) {
	cases := []struct {
		critical int64
		warning  int64
		want     string
	}{
		{0, 0, "none"},
		{0, 2, "warning"},
		{1, 0, "critical"},
		{1, 2, "critical"},
	}
	for _, tc := range cases {
		if got := notificationAttentionLevel(tc.critical, tc.warning); got != tc.want {
			t.Fatalf("critical=%d warning=%d: got %s want %s", tc.critical, tc.warning, got, tc.want)
		}
	}
}

func TestInternalStatusReturnsNotificationSummaryNotRuntimeHealth(t *testing.T) {
	queryCalls := 0
	db := &testutil.MockDB{QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
		queryCalls++
		if !strings.Contains(query, "FROM notification_events") || strings.Contains(query, "LIMIT 20") {
			t.Fatalf("unexpected internal status query: %s", query)
		}
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = 1
			*dest[1].(*int64) = 2
			return nil
		}}
	}}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/internal-status", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.InternalStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if queryCalls != 1 || response["summary_kind"] != "notification_events" || response["attention_level"] != "critical" {
		t.Fatalf("unexpected notification summary: %#v calls=%d", response, queryCalls)
	}
	if _, exists := response["status"]; exists {
		t.Fatalf("notification aggregation must not be exposed as runtime status: %#v", response)
	}
	if _, exists := response["incidents"]; exists {
		t.Fatalf("internal status must link to notification center instead of duplicating event rows: %#v", response)
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
	if !strings.Contains(joined, "EXISTS") || !strings.Contains(joined, "project_members") {
		t.Fatalf("scope=my must require live project membership, got %q", joined)
	}
	if len(args) != 5 || args[1] != int64(42) || args[2] != int64(42) || args[4] != "personal" {
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
