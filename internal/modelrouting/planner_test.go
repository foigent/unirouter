package modelrouting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type fakeTx struct {
	pgx.Tx
	commits   int
	rollbacks int
}

func (tx *fakeTx) Commit(context.Context) error {
	tx.commits++
	return nil
}

func (tx *fakeTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type fakeStarter struct {
	tx      *fakeTx
	options []pgx.TxOptions
}

func (starter *fakeStarter) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	starter.options = append(starter.options, options)
	return starter.tx, nil
}

type fixtureLoader struct {
	value snapshot
	calls int
	tx    pgx.Tx
}

func (loader *fixtureLoader) Load(_ context.Context, tx pgx.Tx, _ Request) (snapshot, error) {
	loader.calls++
	loader.tx = tx
	return loader.value, nil
}

func TestParseModeIsStrictAndCallerOwnsDefault(t *testing.T) {
	for _, value := range []string{"legacy", " verify ", "CANONICAL"} {
		if _, err := ParseMode(value); err != nil {
			t.Fatalf("ParseMode(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "auto", "fallback", "legacy,canonical"} {
		if _, err := ParseMode(value); !errors.Is(err, ErrInvalidMode) {
			t.Fatalf("ParseMode(%q) error = %v", value, err)
		}
	}
}

func TestPlannerUsesOneReadOnlyRepeatableReadSnapshot(t *testing.T) {
	value := verifyFixture()
	starter := &fakeStarter{tx: &fakeTx{}}
	loader := &fixtureLoader{value: value}
	planner := &Planner{database: starter, policy: routingpolicy.Default(), loader: loader}
	plan, err := planner.Plan(context.Background(), requestFor(ModeVerify, "same-snapshot"))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Routes) != 2 || loader.calls != 1 || loader.tx != starter.tx {
		t.Fatalf("planner did not use exactly one paired snapshot: plan=%#v calls=%d", plan, loader.calls)
	}
	if len(starter.options) != 1 || starter.options[0].IsoLevel != pgx.RepeatableRead || starter.options[0].AccessMode != pgx.ReadOnly {
		t.Fatalf("unexpected transaction options: %#v", starter.options)
	}
	if starter.tx.commits != 1 {
		t.Fatalf("snapshot transaction commits = %d, want 1", starter.tx.commits)
	}
}

func TestVerifyReturnsCanonicalRoutesOnlyWhenPlansMatch(t *testing.T) {
	plan, err := planFixture(t, verifyFixture(), requestFor(ModeVerify, "parity-ok"))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Routes) != 2 {
		t.Fatalf("route count = %d, want 2", len(plan.Routes))
	}
	for _, route := range plan.Routes {
		if route.Evidence.AuthorityMode != ModeVerify || route.Evidence.DeploymentID == 0 ||
			route.Evidence.ConnectionID == 0 || route.Evidence.CredentialBindingID == 0 ||
			route.Evidence.LegacyRoutingRevision != 3 || route.Evidence.LegacyCircuitRevision != 4 ||
			route.Evidence.LegacyCredentialStateRevision != 2 || route.ConnectionName == "" ||
			route.EncryptedSecret == "" || route.Evidence.SnapshotHash != plan.SnapshotHash {
			t.Fatalf("verify did not return a complete canonical route: %#v", route)
		}
	}
}

func TestVerifySupportsSealedCanonicalSlotAgainstRolledBackLegacyParent(t *testing.T) {
	value := canonicalFixture()
	value.LegacyRelease.Stage = "rolled_back"
	plan, err := planFixture(t, value, requestFor(ModeVerify, "post-cutover-verify"))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Routes) != 2 || plan.RuntimeReleaseID != value.Slot.Selected.ID {
		t.Fatalf("post-cutover verify did not return the selected v2 plan: %#v", plan)
	}
	for _, route := range plan.Routes {
		if route.Evidence.AuthorityMode != ModeVerify || route.Evidence.ModelOperationID != "openai.chat_completions.v1" {
			t.Fatalf("unexpected post-cutover route evidence: %#v", route.Evidence)
		}
	}
}

func TestLegacyUnsealedCandidateStillRoutesButVerifyFailsClosed(t *testing.T) {
	value := verifyFixture()
	value.Slot.Selected.RouteItemsHash = ""
	value.Slot.Selected.RouteItemsCount = 0
	value.Slot.Selected.RouteItemsSealed = nil
	value.Slot.Stable = value.Slot.Selected
	value.LegacyRelease = value.Slot.Selected
	value.Manifest = nil
	value.LegacyManifest = nil
	plan, err := planFixture(t, value, requestFor(ModeLegacy, "legacy-unsealed"))
	if err != nil || len(plan.Routes) == 0 {
		t.Fatalf("legacy authority unexpectedly depended on compatibility seal: plan=%#v err=%v", plan, err)
	}
	if plan, err = planFixture(t, value, requestFor(ModeVerify, "verify-unsealed")); !errors.Is(err, ErrManifestUnsealed) || len(plan.Routes) != 0 {
		t.Fatalf("verify accepted an unsealed v1 compatibility release: plan=%#v err=%v", plan, err)
	}
}

func TestVerifyDriftAlwaysReturnsZeroRoutes(t *testing.T) {
	tests := []struct {
		name string
		edit func(*snapshot)
	}{
		{name: "candidate_set", edit: func(value *snapshot) { value.Canonical = value.Canonical[:1] }},
		{name: "upstream", edit: func(value *snapshot) { value.Canonical[0].UpstreamModel = "other-upstream" }},
		{name: "adapter_config", edit: func(value *snapshot) {
			value.Canonical[0].AdapterConfig = []byte(`{"profile":"different"}`)
			value.Canonical[0].AdapterConfigDigest = adapterDigest(value.Canonical[0].AdapterConfig)
		}},
		{name: "priority_weight", edit: func(value *snapshot) { value.Canonical[0].Weight++ }},
		{name: "breaker", edit: func(value *snapshot) { value.Canonical[0].CircuitRevision++ }},
		{name: "credential_mapping", edit: func(value *snapshot) { value.Canonical[0].Bindings[0].LegacyKeyID = 999 }},
		{name: "credential_secret", edit: func(value *snapshot) { value.Canonical[0].Bindings[0].Encrypted = "cipher:other" }},
		{name: "protocol_profile", edit: func(value *snapshot) { value.Canonical[0].Bindings[0].Probe.ProfileVersion = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := verifyFixture()
			test.edit(&value)
			plan, err := planFixture(t, value, requestFor(ModeVerify, "drift"))
			if !errors.Is(err, ErrParity) {
				t.Fatalf("Plan() error = %v, want parity error", err)
			}
			if len(plan.Routes) != 0 {
				t.Fatalf("parity failure leaked %d routes", len(plan.Routes))
			}
		})
	}
}

func TestVerifyUsesLegacyProvenanceSaltAndCanonicalUsesCanonicalSalt(t *testing.T) {
	value := verifyFixture()
	key := selectionKeyWithDifferentFirst(t, value)
	legacyFirst := selectedFirst(value, key, true)
	canonicalFirst := selectedFirst(value, key, false)
	if legacyFirst == canonicalFirst {
		t.Fatalf("fixture did not distinguish salts: %d", legacyFirst)
	}

	verifyPlan, err := planFixture(t, value, requestFor(ModeVerify, key))
	if err != nil {
		t.Fatalf("verify Plan() error = %v", err)
	}
	if verifyPlan.Routes[0].Evidence.LegacyChannelID != legacyFirst {
		t.Fatalf("verify selected legacy channel %d, want %d", verifyPlan.Routes[0].Evidence.LegacyChannelID, legacyFirst)
	}

	canonical := canonicalFixture()
	canonicalPlan, err := planFixture(t, canonical, requestFor(ModeCanonical, key))
	if err != nil {
		t.Fatalf("canonical Plan() error = %v", err)
	}
	if canonicalPlan.Routes[0].Evidence.DeploymentID != canonicalFirst {
		t.Fatalf("canonical selected deployment %d, want %d", canonicalPlan.Routes[0].Evidence.DeploymentID, canonicalFirst)
	}
}

func TestCanonicalFailsClosedForRetiredStaleProbeBindingAndCapability(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		edit   func(*snapshot)
	}{
		{name: "retired", reason: "retired", edit: func(value *snapshot) { value.Canonical[0].DeploymentState = "retired" }},
		{name: "stale_revision", reason: "stale_revision", edit: func(value *snapshot) { value.Canonical[0].ConnectionRevision++ }},
		{name: "missing_route_limit", reason: "execution_limits_missing", edit: func(value *snapshot) { value.Canonical[0].MaxContext = 0 }},
		{name: "no_probe", reason: "binding_probe_missing", edit: func(value *snapshot) { value.Canonical[0].Bindings[0].Probe.Present = false }},
		{name: "probe_profile", reason: "binding_probe_missing", edit: func(value *snapshot) { value.Canonical[0].Bindings[0].Probe.ProfileVersion = "wrong" }},
		{name: "empty_secret", reason: "credential_secret_missing", edit: func(value *snapshot) { value.Canonical[0].Bindings[0].Encrypted = "" }},
		{name: "no_binding", reason: "explicit_binding_missing", edit: func(value *snapshot) { value.Canonical[0].Bindings = nil }},
		{name: "capability_unknown", reason: "unsupported_capability:tools", edit: func(value *snapshot) { delete(value.Canonical[0].Capabilities, "tools") }},
		{name: "capability_unapproved", reason: "unsupported_capability:tools", edit: func(value *snapshot) {
			item := value.Canonical[0].Capabilities["tools"]
			item.EvidenceState = "live_probed"
			value.Canonical[0].Capabilities["tools"] = item
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := canonicalFixture()
			value.Canonical = value.Canonical[:1]
			test.edit(&value)
			plan, err := planFixture(t, value, requestFor(ModeCanonical, "fail-closed"))
			if !errors.Is(err, ErrNoRoute) {
				t.Fatalf("Plan() error = %v, want no route", err)
			}
			if len(plan.Routes) != 0 || !slices.ContainsFunc(plan.Exclusions, func(item Exclusion) bool { return item.Reason == test.reason }) {
				t.Fatalf("unexpected fail-closed plan: %#v", plan)
			}
		})
	}
}

func TestNewProtocolOperationsRequireProductionApprovedEvidence(t *testing.T) {
	for _, operation := range []string{"openai.responses.v1", "anthropic.messages.v1"} {
		value := publicSnapshotFixture(t, operation, false)
		candidate := value.Canonical[0]
		candidate.OperationEvidenceState = "live_probed"
		candidate.ProductionQualified = false
		candidate.QualificationState = ""
		candidate.ProductionApprovalID = 0
		if eligible, reason := canonicalEligibility(value, candidate, operation, true); eligible || reason != "production_qualification_missing" {
			t.Fatalf("operation %s with live probe eligibility=(%t,%q), want fail closed", operation, eligible, reason)
		}
		candidate.OperationEvidenceState = "production_approved"
		candidate.ProductionQualified = true
		candidate.QualificationState = "production_qualified"
		candidate.ProductionApprovalID = 9
		if eligible, reason := canonicalEligibility(value, candidate, operation, true); eligible || reason != "public_execution_binding_missing" {
			t.Fatalf("operation %s with only M75 approval eligibility=(%t,%q), want M77 fail closed", operation, eligible, reason)
		}
		qualified := publicSnapshotFixture(t, operation, true)
		if eligible, reason := canonicalEligibility(qualified, qualified.Canonical[0], operation, true); !eligible || reason != "" {
			t.Fatalf("operation %s with exact M77 authority eligibility=(%t,%q), want admitted", operation, eligible, reason)
		}
	}

	candidate := canonicalFixture().Canonical[0]
	candidate.OperationEvidenceState = "live_probed"
	if eligible, reason := canonicalEligibility(canonicalFixture(), candidate, candidate.OperationID, true); !eligible || reason != "" {
		t.Fatalf("established chat operation eligibility=(%t,%q), want live probe admitted", eligible, reason)
	}
}

func TestPublicPlannerReturnsOnlyExplicitEvidenceV2FromExactM77Item(t *testing.T) {
	for _, operation := range []string{"openai.responses.v1", "anthropic.messages.v1"} {
		t.Run(operation, func(t *testing.T) {
			old := publicSnapshotFixture(t, operation, false)
			request := requestFor(ModeCanonical, "m75-public-fenced")
			request.ModelOperationID = operation
			plan, err := planFixture(t, old, request)
			if !errors.Is(err, ErrNoRoute) || len(plan.Routes) != 0 ||
				!slices.ContainsFunc(plan.Exclusions, func(item Exclusion) bool {
					return item.Reason == "public_execution_binding_missing"
				}) {
				t.Fatalf("old M75 public item routed: plan=%#v err=%v", plan, err)
			}

			value := publicSnapshotFixture(t, operation, true)
			request.SelectionKey = "m77-public-route"
			plan, err = planFixture(t, value, request)
			if err != nil || len(plan.Routes) != 1 {
				t.Fatalf("M77 public route result: plan=%#v err=%v", plan, err)
			}
			route := plan.Routes[0]
			if route.PublicEvidence == nil || route.Evidence.ContractVersion != "" {
				t.Fatalf("public route did not preserve the explicit evidence union: %#v", route)
			}
			normalized, err := executionevidence.NormalizeV2(*route.PublicEvidence)
			if err != nil {
				t.Fatal(err)
			}
			item := value.Manifest[0]
			if normalized.PublicExecutionBindingID != item.PublicExecutionBindingID ||
				normalized.DeliveryContractVersion != item.DeliveryContractVersion ||
				normalized.ChargeContractVersion != item.ChargeContractVersion ||
				normalized.IdempotencyContractVersion != item.IdempotencyContractVersion ||
				normalized.CredentialBindingID != value.Canonical[0].Bindings[0].ID ||
				normalized.CredentialProbeID != value.Canonical[0].Bindings[0].Probe.ID {
				t.Fatalf("public EvidenceV2 lost structural or current credential authority: %#v", normalized)
			}

			staleProbe := publicSnapshotFixture(t, operation, true)
			staleProbe.Canonical[0].Bindings[0].Probe.OperationRevision--
			request.SelectionKey = "m77-stale-operation-probe"
			plan, err = planFixture(t, staleProbe, request)
			if !errors.Is(err, ErrNoRoute) || len(plan.Routes) != 0 ||
				!slices.ContainsFunc(plan.Exclusions, func(item Exclusion) bool {
					return item.Reason == "binding_probe_missing"
				}) {
				t.Fatalf("stale public operation probe routed: plan=%#v err=%v", plan, err)
			}
		})
	}
}

func TestCanonicalProbeAuthorityIsPerBindingAndLinkEpoch(t *testing.T) {
	value := canonicalFixture()
	value.Canonical = value.Canonical[:1]
	candidate := &value.Canonical[0]
	second := candidate.Bindings[0]
	second.ID++
	second.LegacyKeyID++
	second.Encrypted = "cipher:second"
	second.Probe = probeSnapshot{}
	candidate.Bindings = append(candidate.Bindings, second)
	plan, err := planFixture(t, value, requestFor(ModeCanonical, "per-binding-probe"))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	for _, route := range plan.Routes {
		if route.Evidence.CredentialBindingID == second.ID {
			t.Fatal("a successful probe for binding A authorized unprobed binding B")
		}
	}

	value = canonicalFixture()
	value.Canonical = value.Canonical[:1]
	binding := &value.Canonical[0].Bindings[0]
	binding.StateRevision++
	plan, err = planFixture(t, value, requestFor(ModeCanonical, "rotated-binding"))
	if !errors.Is(err, ErrNoRoute) || len(plan.Routes) != 0 {
		t.Fatalf("a probe from before the current credential state was reused: plan=%#v err=%v", plan, err)
	}

	value = canonicalFixture()
	value.Canonical = value.Canonical[:1]
	binding = &value.Canonical[0].Bindings[0]
	binding.LinkedAt = binding.Probe.TestedAt.Add(time.Nanosecond)
	plan, err = planFixture(t, value, requestFor(ModeCanonical, "relinked-binding"))
	if !errors.Is(err, ErrNoRoute) || len(plan.Routes) != 0 {
		t.Fatalf("a probe from before the current link epoch was reused: plan=%#v err=%v", plan, err)
	}
}

func TestCanonicalEvidenceBindsOperationProtocolAndCapabilities(t *testing.T) {
	value := canonicalFixture()
	expectedConnectionNames := make(map[int64]string, len(value.Canonical))
	for index := range value.Canonical {
		value.Legacy[index].Name = fmt.Sprintf("legacy-channel-%d", index)
		value.Canonical[index].Name = fmt.Sprintf("canonical-connection-%d", index)
		expectedConnectionNames[value.Canonical[index].DeploymentID] = value.Canonical[index].Name
	}
	plan, err := planFixture(t, value, requestFor(ModeCanonical, "evidence"))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	for _, route := range plan.Routes {
		evidence := route.Evidence
		if evidence.ModelOperationID != "openai.chat_completions.v1" || evidence.OperationRevision != 7 ||
			evidence.CircuitRevision != 4 ||
			evidence.OperationEvidenceState != "production_approved" || evidence.OperationEvidenceRef != "fixture://operation/chat" ||
			evidence.OperationVerifiedAt == nil ||
			evidence.ProtocolProfileID != "openai.chat.text" || evidence.ProtocolProfileVersion != "v1" ||
			evidence.CapabilityEvidenceHash == "" || len(evidence.CapabilityEvidence) != 1 ||
			evidence.CapabilityEvidence[0].EvidenceRef != "fixture://capability/tools" ||
			route.ConnectionName != expectedConnectionNames[evidence.DeploymentID] || route.ProtocolProfileID != evidence.ProtocolProfileID ||
			evidence.CredentialLinkedAt.IsZero() || evidence.CredentialProbeID <= 0 ||
			evidence.CredentialProbeTestedAt.IsZero() || route.MaxContext <= 0 || route.DefaultMaxOutput <= 0 ||
			evidence.LegacyChannelID != 0 || evidence.LegacyChannelKeyID != 0 ||
			evidence.LegacyRoutingRevision != 0 || evidence.LegacyCredentialStateRevision != 0 {
			t.Fatalf("incomplete evidence: %#v", evidence)
		}
	}
	if plan.PublicContextLimit != 128000 || plan.PublicOutputLimit != 8192 {
		t.Fatalf("public execution limits missing from plan: %#v", plan)
	}
}

func TestSnapshotHashNeverDependsOnEncryptedSecrets(t *testing.T) {
	value := canonicalFixture()
	value.Mode = ModeCanonical
	first, err := snapshotDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	value.Legacy[0].Bindings[0].Encrypted = "entirely-different-legacy-ciphertext"
	value.Canonical[0].Bindings[0].Encrypted = "entirely-different-canonical-ciphertext"
	second, err := snapshotDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("secret changed safe snapshot hash: %q != %q", first, second)
	}
	value.Canonical[0].Bindings[0].StateRevision++
	third, _ := snapshotDigest(value)
	if third == second {
		t.Fatal("credential state revision was omitted from snapshot hash")
	}
	value.Canonical[0].Bindings[0].LinkedAt = value.Canonical[0].Bindings[0].LinkedAt.Add(time.Nanosecond)
	fourth, _ := snapshotDigest(value)
	if fourth == third {
		t.Fatal("credential link epoch was omitted from snapshot hash")
	}
	capability := value.Canonical[0].Capabilities["tools"]
	capability.Revision++
	value.Canonical[0].Capabilities["tools"] = capability
	fifth, _ := snapshotDigest(value)
	if fifth == fourth {
		t.Fatal("capability evidence revision was omitted from snapshot hash")
	}
	value.Canonical[0].OperationEvidenceRef = "fixture://operation/replaced-proof"
	sixth, _ := snapshotDigest(value)
	if sixth == fifth {
		t.Fatal("operation evidence reference was omitted from snapshot hash")
	}
	capability = value.Canonical[0].Capabilities["tools"]
	capability.EvidenceRef = "fixture://capability/replaced-proof"
	value.Canonical[0].Capabilities["tools"] = capability
	seventh, _ := snapshotDigest(value)
	if seventh == sixth {
		t.Fatal("capability evidence reference was omitted from snapshot hash")
	}
}

func TestPublicSnapshotHashFreezesTheExactM77Bundle(t *testing.T) {
	base := publicSnapshotFixture(t, "openai.responses.v1", true)
	want, err := snapshotDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*snapshot)
	}{
		{name: "binding", edit: func(value *snapshot) { value.Canonical[0].PublicExecutionBindingID++ }},
		{name: "delivery", edit: func(value *snapshot) { value.Canonical[0].DeliveryContractVersion += ".drift" }},
		{name: "charge", edit: func(value *snapshot) { value.Canonical[0].ChargeContractVersion += ".drift" }},
		{name: "idempotency", edit: func(value *snapshot) { value.Canonical[0].IdempotencyContractVersion += ".drift" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Canonical = slices.Clone(base.Canonical)
			test.edit(&value)
			got, digestErr := snapshotDigest(value)
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			if got == want {
				t.Fatalf("%s was omitted from the public plan snapshot", test.name)
			}
		})
	}
}

func TestAdapterDigestPreservesAdjacentLargeIntegers(t *testing.T) {
	left := adapterDigest([]byte(`{"revision":9007199254740992}`))
	right := adapterDigest([]byte(`{"revision":9007199254740993}`))
	if left == right {
		t.Fatal("adapter digest rounded adjacent 64-bit JSON integers")
	}
}

func TestUnknownOrInternalOperationIsRejected(t *testing.T) {
	for _, operation := range []string{"unknown.operation.v1", legacyCompatibilityOp} {
		request := requestFor(ModeLegacy, "invalid-operation")
		request.ModelOperationID = operation
		if _, err := planFixture(t, verifyFixture(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("operation %q error = %v, want invalid request", operation, err)
		}
	}
}

func TestUnsealedOrMutatedManifestNeverRoutes(t *testing.T) {
	value := canonicalFixture()
	value.Slot.Selected.RouteItemsSealed = nil
	if plan, err := planFixture(t, value, requestFor(ModeCanonical, "unsealed")); !errors.Is(err, ErrManifestUnsealed) || len(plan.Routes) != 0 {
		t.Fatalf("unsealed manifest result = %#v, %v", plan, err)
	}
	value = canonicalFixture()
	value.Manifest[0].Weight = intPointer(999)
	if plan, err := planFixture(t, value, requestFor(ModeCanonical, "mutated")); !errors.Is(err, ErrManifestStale) || len(plan.Routes) != 0 {
		t.Fatalf("mutated manifest result = %#v, %v", plan, err)
	}
	value = canonicalFixture()
	value.Manifest[0].ProtocolProfileVersion = "tampered"
	if plan, err := planFixture(t, value, requestFor(ModeCanonical, "mutated-profile")); !errors.Is(err, ErrManifestStale) || len(plan.Routes) != 0 {
		t.Fatalf("mutated manifest profile result = %#v, %v", plan, err)
	}
}

func planFixture(t *testing.T, value snapshot, request Request) (Plan, error) {
	t.Helper()
	starter := &fakeStarter{tx: &fakeTx{}}
	loader := &fixtureLoader{value: value}
	planner := &Planner{database: starter, policy: routingpolicy.Default(), loader: loader}
	return planner.Plan(context.Background(), request)
}

func requestFor(mode Mode, key string) Request {
	return Request{
		Mode: mode, CustomerOrgID: 41, PublicModel: "company-chat",
		ModelOperationID: "openai.chat_completions.v1", RequiredCapabilities: []string{"tools"},
		SelectionKey: key,
	}
}

func verifyFixture() snapshot {
	now := time.Date(2026, 8, 20, 12, 0, 0, 123000000, time.UTC)
	value := snapshot{
		DBNow: now, CustomerOrgID: 41, PublicModelID: 71, PublicModel: "company-chat",
		PublicModelLifecycle: "production", RequestedOperationID: "openai.chat_completions.v1",
		RequiredCapabilities: []string{"tools"}, PublicContextLimit: 128000, PublicOutputLimit: 8192,
	}
	for index, id := range []int64{11, 22} {
		legacyKeyID := id*10 + 1
		deploymentID := int64(1000 + index + 1)
		bindingID := int64(3000 + index + 1)
		adapterConfig := []byte(fmt.Sprintf(`{"profile":"p%d"}`, index+1))
		legacy := legacyCandidate{
			ID: id, Name: fmt.Sprintf("endpoint-%d", id), Provider: "provider", BaseURL: fmt.Sprintf("https://provider-%d.invalid/v1", id),
			AdapterType: "openai_compatible", AdapterConfig: adapterConfig, AdapterConfigDigest: adapterDigest(adapterConfig),
			UpstreamModel: fmt.Sprintf("upstream-%d", id), Priority: 10, Weight: 100 + index*100,
			MaxContext: 128000, DefaultMaxOutput: 8192,
			Status: "active", CircuitState: "healthy", DegradationThreshold: 5,
			RoutingRevision: 3, CircuitRevision: 4,
			Bindings: []legacyBinding{{ID: legacyKeyID, Encrypted: fmt.Sprintf("cipher:%d", legacyKeyID), Status: "active", StateRevision: 2}},
		}
		capVerified := now.Add(-time.Minute)
		canonical := canonicalCandidate{
			DeploymentID: deploymentID, PublicModelID: 71, ConnectionID: int64(2000 + index + 1),
			LegacyChannelID: id, LegacyPublicModel: "company-chat", Name: legacy.Name,
			Provider: legacy.Provider, BaseURL: legacy.BaseURL, AdapterType: legacy.AdapterType,
			AdapterConfig: slices.Clone(adapterConfig), AdapterConfigDigest: legacy.AdapterConfigDigest,
			ConnectionState: "enabled", ConnectionRevision: 3, UpstreamModel: legacy.UpstreamModel,
			DeploymentState: "enabled", DeploymentRevision: 3, Priority: legacy.Priority, Weight: legacy.Weight,
			MaxContext: legacy.MaxContext, DefaultMaxOutput: legacy.DefaultMaxOutput,
			CircuitState: "healthy", CircuitRevision: 4, DegradationThreshold: 5,
			OperationPresent: true, OperationID: legacyCompatibilityOp,
			ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1",
			OperationSupportState: "enabled", OperationEvidenceState: "declared",
			OperationEvidenceRef: "fixture://operation/legacy", OperationRevision: 7,
			Bindings: []canonicalBinding{{
				ID: bindingID, Encrypted: legacy.Bindings[0].Encrypted, Status: "active",
				StateRevision: 2, LegacyKeyID: legacyKeyID, LinkedAt: now.Add(-2 * time.Minute),
				Probe: probeSnapshot{Present: true, ID: int64(4000 + index), Success: true,
					ProfileID: "legacy.channel_model_probe", ProfileVersion: "v1", UpstreamModel: legacy.UpstreamModel,
					ConnectionRevision: 3, DeploymentRevision: 3, CredentialBindingID: bindingID,
					CredentialStateRevision: 2, TestedAt: now.Add(-time.Minute)},
			}},
			Capabilities: map[string]capabilitySnapshot{"tools": {ID: "tools", SupportState: "enabled", EvidenceState: "production_approved", EvidenceRef: "fixture://capability/tools", VerifiedAt: &capVerified, Revision: 5}},
		}
		value.Legacy = append(value.Legacy, legacy)
		value.Canonical = append(value.Canonical, canonical)
	}
	value.Manifest = []manifestItem{
		{Ordinal: 1, DeploymentID: value.Canonical[0].DeploymentID, ModelOperationID: legacyCompatibilityOp,
			ExpectedConnectionRevision: 3, ExpectedDeploymentRevision: 3, ExpectedOperationRevision: 7,
			ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1",
			Admission: routingpolicy.AdmissionAllow, SourceCandidateVersion: legacyCandidateVersion},
		{Ordinal: 2, DeploymentID: value.Canonical[1].DeploymentID, ModelOperationID: legacyCompatibilityOp,
			ExpectedConnectionRevision: 3, ExpectedDeploymentRevision: 3, ExpectedOperationRevision: 7,
			ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1",
			Admission: routingpolicy.AdmissionAllow, SourceCandidateVersion: legacyCandidateVersion},
	}
	for index := range value.Canonical {
		item := value.Manifest[index]
		value.Canonical[index].Manifest = &item
	}
	sealed := now.Add(-30 * time.Second)
	overrides := []legacyOverride{
		{ChannelID: 11, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow},
		{ChannelID: 22, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow},
	}
	candidateJSON := legacyCandidateJSON("company-chat", overrides)
	value.LegacyOverrides = overrides
	value.Slot = slotSnapshot{Present: true, Version: 9, Selected: releaseSnapshot{
		ID: 501, Ref: "rcr_legacy_verify", CandidateVersion: legacyCandidateVersion,
		CandidateHash: legacyCandidateDigest(legacyCandidateVersion, "company-chat", overrides),
		CandidateJSON: candidateJSON, RouteItemsHash: manifestDigest(value.Manifest),
		RouteItemsCount: len(value.Manifest), RouteItemsSealed: &sealed, Stage: "active", Revision: 4, PublicModelID: 71,
	}}
	value.Slot.Stable = value.Slot.Selected
	value.LegacyRelease = value.Slot.Selected
	value.LegacyManifest = slices.Clone(value.Manifest)
	return value
}

func canonicalFixture() snapshot {
	value := verifyFixture()
	now := value.DBNow
	value.Manifest = nil
	for index := range value.Canonical {
		candidate := &value.Canonical[index]
		verified := now.Add(-time.Minute)
		candidate.OperationID = "openai.chat_completions.v1"
		candidate.ProtocolProfileID = "openai.chat.text"
		candidate.ProtocolProfileVersion = "v1"
		candidate.OperationEvidenceState = "production_approved"
		candidate.OperationEvidenceRef = "fixture://operation/chat"
		candidate.OperationVerifiedAt = &verified
		for bindingIndex := range candidate.Bindings {
			candidate.Bindings[bindingIndex].Probe.ProfileID = candidate.ProtocolProfileID
			candidate.Bindings[bindingIndex].Probe.ProfileVersion = candidate.ProtocolProfileVersion
		}
		item := manifestItem{
			Ordinal: index + 1, DeploymentID: candidate.DeploymentID,
			ModelOperationID: candidate.OperationID, ExpectedConnectionRevision: candidate.ConnectionRevision,
			ExpectedDeploymentRevision: candidate.DeploymentRevision, ExpectedOperationRevision: candidate.OperationRevision,
			ProtocolProfileID: candidate.ProtocolProfileID, ProtocolProfileVersion: candidate.ProtocolProfileVersion,
			Admission: routingpolicy.AdmissionAllow, SourceCandidateVersion: canonicalCandidateVersion,
		}
		value.Manifest = append(value.Manifest, item)
	}
	for index := range value.Canonical {
		item := value.Manifest[index]
		value.Canonical[index].Manifest = &item
	}
	sealed := now.Add(-30 * time.Second)
	value.Slot.PublicModelID = value.PublicModelID
	value.Slot.Selected = releaseSnapshot{
		ID: 601, Ref: "rcr_canonical_v2", CandidateVersion: canonicalCandidateVersion,
		CandidateHash: "sha256:canonical", RouteItemsHash: manifestDigest(value.Manifest),
		RouteItemsCount: len(value.Manifest), RouteItemsSealed: &sealed, Stage: "active", Revision: 5, PublicModelID: value.PublicModelID,
		ParentStableReleaseID: value.LegacyRelease.ID,
	}
	value.Slot.Stable = value.Slot.Selected
	return value
}

func publicSnapshotFixture(t *testing.T, operation string, withM77 bool) snapshot {
	t.Helper()
	value := canonicalFixture()
	value.Mode = ModeCanonical
	value.Canonical = value.Canonical[:1]
	candidate := &value.Canonical[0]
	compiled, err := modelqualification.CompiledPublicBinding(operation)
	if err != nil {
		t.Fatal(err)
	}
	candidate.BaseURL = "https://provider.invalid"
	candidate.OperationID = operation
	candidate.ProtocolProfileID = compiled.ProfileID
	candidate.ProtocolProfileVersion = compiled.ProfileVersion
	candidate.PublicPath = compiled.PublicPath
	candidate.ExecutorContractVersion = compiled.ExecutorContractVersion
	candidate.OperationEvidenceState = "production_approved"
	candidate.ProductionQualified = true
	candidate.QualificationState = "production_qualified"
	candidate.ProductionApprovalID = 9
	for index := range candidate.Bindings {
		probe := &candidate.Bindings[index].Probe
		probe.ProfileID = compiled.ProfileID
		probe.ProfileVersion = compiled.ProfileVersion
		probe.OperationRevision = candidate.OperationRevision
		probe.PublicPath = compiled.PublicPath
		probe.ExecutorContractVersion = compiled.ExecutorContractVersion
		probe.HTTPSOrigin = candidate.BaseURL
		probe.SourceKind = modelqualification.LiveProbeSourceKind
	}
	item := manifestItem{
		Ordinal: 1, DeploymentID: candidate.DeploymentID, ModelOperationID: operation,
		ExpectedConnectionRevision: candidate.ConnectionRevision,
		ExpectedDeploymentRevision: candidate.DeploymentRevision,
		ExpectedOperationRevision:  candidate.OperationRevision,
		ProtocolProfileID:          compiled.ProfileID,
		ProtocolProfileVersion:     compiled.ProfileVersion,
		PublicPath:                 compiled.PublicPath,
		ExecutorContractVersion:    compiled.ExecutorContractVersion,
		Admission:                  routingpolicy.AdmissionAllow,
		SourceCandidateVersion:     canonicalCandidateVersion,
	}
	if withM77 {
		item.PublicExecutionBindingID = 77
		item.DeliveryContractVersion = compiled.DeliveryContractVersion
		item.ChargeContractVersion = compiled.ChargeContractVersion
		item.IdempotencyContractVersion = compiled.IdempotencyContractVersion
		candidate.PublicExecutionBindingID = item.PublicExecutionBindingID
		candidate.DeliveryContractVersion = item.DeliveryContractVersion
		candidate.ChargeContractVersion = item.ChargeContractVersion
		candidate.IdempotencyContractVersion = item.IdempotencyContractVersion
		candidate.PublicExecutionQualified = true
		candidate.PublicQualificationState = "public_executable"
	}
	value.Manifest = []manifestItem{item}
	candidate.Manifest = &value.Manifest[0]
	value.Slot.Selected.RouteItemsHash = manifestDigest(value.Manifest)
	value.Slot.Selected.RouteItemsCount = 1
	value.Slot.Stable = value.Slot.Selected
	return value
}

func selectionKeyWithDifferentFirst(t *testing.T, value snapshot) string {
	t.Helper()
	for index := 0; index < 10000; index++ {
		key := fmt.Sprintf("salt-%d", index)
		if selectedFirst(value, key, true) != selectedFirst(value, key, false) {
			return key
		}
	}
	t.Fatal("could not find a salt discriminator")
	return ""
}

func selectedFirst(value snapshot, key string, legacyIDs bool) int64 {
	selection := routingpolicy.SelectionSnapshot{Model: value.PublicModel, DBNow: value.DBNow}
	for _, candidate := range value.Canonical {
		id := candidate.DeploymentID
		keyID := candidate.Bindings[0].ID
		if legacyIDs {
			id = candidate.LegacyChannelID
			keyID = candidate.Bindings[0].LegacyKeyID
		}
		selection.Channels = append(selection.Channels, routingpolicy.SelectionChannel{
			ID: id, Name: candidate.Name, Provider: candidate.Provider, Priority: candidate.Priority,
			Weight: candidate.Weight, Status: "active", CircuitState: "healthy",
			Keys: []routingpolicy.SelectionKey{{ID: keyID, Status: "active"}},
		})
	}
	return routingpolicy.EvaluateSelection(selection, routingpolicy.Default(), key).Selected[0].ChannelID
}

func intPointer(value int) *int { return &value }

func legacyCandidateJSON(model string, overrides []legacyOverride) []byte {
	body, _ := json.Marshal(struct {
		ContractVersion string           `json:"contract_version"`
		ModelName       string           `json:"model_name"`
		Overrides       []legacyOverride `json:"overrides"`
	}{legacyCandidateVersion, model, overrides})
	return body
}
