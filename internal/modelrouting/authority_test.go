package modelrouting

import (
	"context"
	"errors"
	"testing"

	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestAuthorityLegacyReleaseProvesChatAndEmbeddingsAdapterExecutability(t *testing.T) {
	value := verifyFixture()
	for index := range value.Legacy {
		config := []byte(`{}`)
		value.Legacy[index].AdapterType = "anthropic_native"
		value.Legacy[index].AdapterConfig = config
		value.Legacy[index].AdapterConfigDigest = adapterDigest(config)
		value.Canonical[index].AdapterType = "anthropic_native"
		value.Canonical[index].AdapterConfig = config
		value.Canonical[index].AdapterConfigDigest = adapterDigest(config)
	}

	request := requestFor(ModeVerify, "authority-chat-adapter")
	request.RequiredCapabilities = nil
	chatPlan, err := buildPlan(value, request, routingpolicy.Default())
	if err != nil {
		t.Fatalf("build chat plan: %v", err)
	}
	if issues := authorityRouteOperationIssues(context.Background(), chatPlan); len(issues) != 0 {
		t.Fatalf("chat adapter issues = %v, want none", issues)
	}

	request.ModelOperationID = "openai.embeddings.v1"
	request.SelectionKey = "authority-embeddings-adapter"
	embeddingsPlan, err := buildPlan(value, request, routingpolicy.Default())
	if err != nil {
		t.Fatalf("build embeddings plan: %v", err)
	}
	issues := authorityRouteOperationIssues(context.Background(), embeddingsPlan)
	if len(issues) != 1 || issues[0] != "operation_adapter_unsupported" {
		t.Fatalf("embeddings adapter issues = %v", issues)
	}
}

func TestAuthorityRejectsStructuralExclusionEvenWhenAnotherRouteIsHealthy(t *testing.T) {
	value := canonicalFixture()
	value.Canonical[1].DeploymentRevision++
	request := requestFor(ModeCanonical, "authority-mixed-route")
	plan, err := buildPlan(value, request, routingpolicy.Default())
	if err != nil {
		t.Fatalf("buildPlan() error = %v", err)
	}
	if len(plan.Routes) != 1 {
		t.Fatalf("healthy route count = %d, want 1", len(plan.Routes))
	}
	ready, structural := authorityPlanReady(plan, nil)
	if ready || len(structural) != 1 || structural[0] != "stale_revision" {
		t.Fatalf("authority accepted partial manifest: ready=%v structural=%v plan=%#v", ready, structural, plan)
	}
}

func TestAuthorityAllowsOnlyExplicitOperationalUnavailability(t *testing.T) {
	operational := Plan{Exclusions: []Exclusion{{Reason: "administratively_disabled"}, {Reason: "key_cooldown"}}}
	ready, structural := authorityPlanReady(operational, ErrNoRoute)
	if !ready || len(structural) != 0 {
		t.Fatalf("operationally unavailable authority rejected: ready=%v structural=%v", ready, structural)
	}
	for _, reason := range []string{"binding_probe_missing", "stale_revision", "explicit_binding_missing", ""} {
		plan := Plan{Exclusions: []Exclusion{{Reason: reason}}}
		ready, structural = authorityPlanReady(plan, ErrNoRoute)
		if ready || len(structural) != 1 {
			t.Fatalf("structural reason %q accepted: ready=%v structural=%v", reason, ready, structural)
		}
	}
	ready, structural = authorityPlanReady(Plan{}, errors.New("boom"))
	if ready || len(structural) != 0 {
		t.Fatalf("unexpected error accepted: ready=%v structural=%v", ready, structural)
	}
}
