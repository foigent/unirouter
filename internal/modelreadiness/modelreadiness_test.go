package modelreadiness

import "testing"

func TestSummarizeKeepsFourFactsIndependent(t *testing.T) {
	ready := Ready("ready", nil)
	optional := Optional("optional", nil)
	blocked := Blocked("credential_missing", nil, "", "")
	platform := PlatformModel{
		Registration: Blocked("model_unregistered", nil, "", ""),
		Lifecycle:    ready, Catalog: Blocked("catalog_hidden", nil, "", ""),
		PriceAuthority: ready, BaseRoute: ready,
	}
	project := ProjectCaller{
		Project: ready, Alias: optional, ModelSet: ready, Wallet: ready,
		ProjectBudget: optional, Application: optional, Grant: ready,
		Credential: blocked, ModelAuthorization: ready, LimitPolicy: ready,
	}
	summary := Summarize(platform, project)
	if summary.Registered.Value == nil || *summary.Registered.Value ||
		summary.CatalogVisible.Value == nil || *summary.CatalogVisible.Value ||
		summary.Routable.Value == nil || !*summary.Routable.Value ||
		summary.CurrentProjectCallerCallable.Value == nil || *summary.CurrentProjectCallerCallable.Value {
		t.Fatalf("facts were collapsed: %#v", summary)
	}
}

func TestSummarizePreservesUnavailable(t *testing.T) {
	ready := Ready("ready", nil)
	platform := PlatformModel{Registration: ready, Catalog: ready, Lifecycle: ready, PriceAuthority: ready, BaseRoute: ready}
	project := ProjectCaller{Project: ready, ModelSet: ready, Wallet: ready, Application: Optional("self", nil),
		Grant: ready, Credential: ready, ModelAuthorization: Unavailable("policy_store_unavailable"), LimitPolicy: ready}
	summary := Summarize(platform, project)
	if summary.CurrentProjectCallerCallable.State != StateUnavailable || summary.CurrentProjectCallerCallable.Value != nil {
		t.Fatalf("unavailable evidence was misrepresented: %#v", summary.CurrentProjectCallerCallable)
	}
}

func TestSummarizePreservesOptionalFactsWithoutCallingThemBlocked(t *testing.T) {
	optional := Optional("model_governance_not_licensed_optional", nil)
	summary := Summarize(PlatformModel{Registration: optional, Catalog: optional}, ProjectCaller{})
	if summary.Registered.State != StateNotApplicable || summary.Registered.Value != nil ||
		summary.CatalogVisible.State != StateNotApplicable || summary.CatalogVisible.Value != nil {
		t.Fatalf("optional facts were misrepresented: %#v", summary)
	}
}

func TestCallableFactUsesEveryRuntimeAuthorityWithoutCollapsingOptionalFacts(t *testing.T) {
	ready := Ready("ready", nil)
	optional := Optional("optional", nil)
	basePlatform := PlatformModel{
		Registration: Ready("registered", nil), Lifecycle: ready,
		Catalog: Ready("catalog", nil), PriceAuthority: ready,
		Channel: ready, ProviderCredential: ready, BaseRoute: ready,
		RuntimeSlot: optional,
	}
	baseProject := ProjectCaller{
		Project: ready, Alias: optional, ModelSet: optional, Wallet: ready,
		ProjectBudget: Warning("monitoring_only", nil, "", ""),
		Application:   Optional("self_not_applicable", nil), Grant: ready,
		Credential: ready, ModelAuthorization: ready, LimitPolicy: ready,
	}
	readySummary := Summarize(basePlatform, baseProject)
	if readySummary.CurrentProjectCallerCallable.Value == nil || !*readySummary.CurrentProjectCallerCallable.Value {
		t.Fatalf("optional axes incorrectly blocked a callable subject: %#v", readySummary)
	}

	tests := []struct {
		name    string
		mutate  func(*PlatformModel, *ProjectCaller)
		state   State
		reason  string
		hasBool bool
	}{
		{"lifecycle", func(p *PlatformModel, _ *ProjectCaller) { p.Lifecycle = Blocked("retired", nil, "", "") }, StateBlocked, "retired", true},
		{"gateway license", func(p *PlatformModel, _ *ProjectCaller) {
			p.GatewayLicense = Blocked("license_error.invalid", nil, "/license", "platform_admin")
		}, StateBlocked, "license_error.invalid", true},
		{"price", func(p *PlatformModel, _ *ProjectCaller) { p.PriceAuthority = Blocked("price_missing", nil, "", "") }, StateBlocked, "price_missing", true},
		{"base route", func(p *PlatformModel, _ *ProjectCaller) { p.BaseRoute = Blocked("route_missing", nil, "", "") }, StateBlocked, "route_missing", true},
		{"project", func(_ *PlatformModel, p *ProjectCaller) { p.Project = Blocked("project_inactive", nil, "", "") }, StateBlocked, "project_inactive", true},
		{"alias", func(_ *PlatformModel, p *ProjectCaller) { p.Alias = Blocked("alias_inactive", nil, "", "") }, StateBlocked, "alias_inactive", true},
		{"model set", func(_ *PlatformModel, p *ProjectCaller) { p.ModelSet = Blocked("model_denied", nil, "", "") }, StateBlocked, "model_denied", true},
		{"wallet", func(_ *PlatformModel, p *ProjectCaller) { p.Wallet = Blocked("wallet_empty", nil, "", "") }, StateBlocked, "wallet_empty", true},
		{"application", func(_ *PlatformModel, p *ProjectCaller) { p.Application = Blocked("application_inactive", nil, "", "") }, StateBlocked, "application_inactive", true},
		{"grant", func(_ *PlatformModel, p *ProjectCaller) { p.Grant = Blocked("grant_missing", nil, "", "") }, StateBlocked, "grant_missing", true},
		{"credential", func(_ *PlatformModel, p *ProjectCaller) { p.Credential = Blocked("credential_missing", nil, "", "") }, StateBlocked, "credential_missing", true},
		{"model authorization", func(_ *PlatformModel, p *ProjectCaller) { p.ModelAuthorization = Blocked("explicit_deny", nil, "", "") }, StateBlocked, "explicit_deny", true},
		{"limit policy unavailable", func(_ *PlatformModel, p *ProjectCaller) { p.LimitPolicy = Unavailable("limit_store_unavailable") }, StateUnavailable, "caller_evidence_unavailable", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			platform, project := basePlatform, baseProject
			test.mutate(&platform, &project)
			fact := Summarize(platform, project).CurrentProjectCallerCallable
			if fact.State != test.state || fact.ReasonCode != test.reason {
				t.Fatalf("unexpected fact: %#v", fact)
			}
			if test.hasBool {
				if fact.Value == nil || *fact.Value {
					t.Fatalf("blocked fact must be explicit false: %#v", fact)
				}
			} else if fact.Value != nil {
				t.Fatalf("unavailable fact must not invent false: %#v", fact)
			}
		})
	}
}

func TestRegistrationCatalogAndRoutingFactsRemainIndependent(t *testing.T) {
	ready := Ready("ready", nil)
	optional := Optional("optional", nil)
	project := ProjectCaller{
		Project: ready, Alias: optional, ModelSet: optional, Wallet: ready,
		Application: optional, Grant: ready, Credential: ready,
		ModelAuthorization: ready, LimitPolicy: ready,
	}
	platform := PlatformModel{
		Registration: Blocked("unregistered", nil, "", ""),
		Lifecycle:    ready, Catalog: Blocked("catalog_hidden", nil, "", ""),
		PriceAuthority: ready, BaseRoute: ready,
	}
	summary := Summarize(platform, project)
	if summary.Registered.Value == nil || *summary.Registered.Value ||
		summary.CatalogVisible.Value == nil || *summary.CatalogVisible.Value ||
		summary.Routable.Value == nil || !*summary.Routable.Value ||
		summary.CurrentProjectCallerCallable.Value == nil || !*summary.CurrentProjectCallerCallable.Value {
		t.Fatalf("independent facts were collapsed: %#v", summary)
	}
}
