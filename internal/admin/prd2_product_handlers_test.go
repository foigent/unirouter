package admin

import "testing"

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
