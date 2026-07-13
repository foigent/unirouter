package guardrail

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/filter"
)

type detectorFunc struct {
	version string
	detect  func(Context, Text, json.RawMessage) ([]Finding, error)
}

type streamDetectorFunc struct {
	detectorFunc
	capability StreamCapability
}

func (d streamDetectorFunc) StreamCapability() StreamCapability { return d.capability }

func (d detectorFunc) Version() string { return d.version }
func (d detectorFunc) Detect(ctx Context, text Text, config json.RawMessage) ([]Finding, error) {
	return d.detect(ctx, text, config)
}

func newGuardrailTestService(t *testing.T, sensitive *filter.DFAFilter) *Service {
	t.Helper()
	options := DefaultOptions()
	options.ExternalDeadline = 10 * time.Millisecond
	service, err := NewService(nil, sensitive, []byte("0123456789abcdef0123456789abcdef"), options)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestP6F11StreamCapabilitiesKeepAsyncDetectorsShadowOnly(t *testing.T) {
	service := newGuardrailTestService(t, filter.NewDFAFilter([]string{"blocked"}))
	if err := service.RegisterDetector("external:async-fixture", detectorFunc{
		version: "fixture.v1", detect: func(Context, Text, json.RawMessage) ([]Finding, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterDetector("external:sync-fixture", streamDetectorFunc{
		detectorFunc: detectorFunc{version: "fixture.v1", detect: func(Context, Text, json.RawMessage) ([]Finding, error) { return nil, nil }},
		capability:   StreamCapability{Synchronous: true, LookbehindBytes: 64},
	}); err != nil {
		t.Fatal(err)
	}
	capabilities := service.DetectorStreamCapabilities()
	if capabilities[DetectorPII] != (StreamCapability{Synchronous: true, LookbehindBytes: 512}) ||
		capabilities["external:async-fixture"].Synchronous ||
		capabilities["external:sync-fixture"] != (StreamCapability{Synchronous: true, LookbehindBytes: 64}) ||
		!capabilities[DetectorSensitiveWords].Synchronous {
		t.Fatalf("unexpected fixed registry capabilities: %+v", capabilities)
	}
	policy := Policy{
		ID: 1, Ref: "guardrail:p6f11", Name: "stream external", Version: 1,
		Stage: StageStreamOutput, DetectorRef: "external:async-fixture", Action: ActionBlock,
		Mode: ModeEnforce, FailureMode: FailureFailClosed,
	}
	if !errors.Is(service.ValidatePolicy(policy), ErrInvalidPolicy) {
		t.Fatal("asynchronous detector was accepted for synchronous stream enforcement")
	}
	policy.Mode = ModeShadow
	if err := service.ValidatePolicy(policy); err != nil {
		t.Fatalf("asynchronous stream shadow policy was rejected: %v", err)
	}
	policy.Mode = ModeEnforce
	policy.Action = ActionAudit
	if err := service.ValidatePolicy(policy); err != nil {
		t.Fatalf("post-disclosure stream audit policy was rejected: %v", err)
	}
	policy.DetectorRef = "external:sync-fixture"
	policy.Action = ActionBlock
	if err := service.ValidatePolicy(policy); err != nil {
		t.Fatalf("bounded synchronous detector was rejected: %v", err)
	}
}

func TestP6F11PinnedStreamFailsClosedWhenSensitiveCapabilityOutgrowsBound(t *testing.T) {
	sensitive := filter.NewDFAFilter([]string{"short"})
	service := newGuardrailTestService(t, sensitive)
	installPolicies(service, Policy{
		ID: 2, Ref: "guardrail:long-sensitive", Name: "long sensitive", Version: 1,
		Stage: StageStreamOutput, DetectorRef: DetectorSensitiveWords,
		Action: ActionBlock, Mode: ModeEnforce, FailureMode: FailureFailClosed,
	})
	sensitive.UpdateWords([]string{strings.Repeat("x", MaxStreamLookbehindBytes+2)})
	pinned := service.Pin(Subject{OrgID: 7, Model: "gpt-4o", Endpoint: "chat"})
	profile := pinned.StreamProfile()
	if !profile.Enforced || profile.LookbehindBytes != MaxStreamLookbehindBytes {
		t.Fatalf("invalid detector capability widened the platform bound: %+v", profile)
	}
	result := pinned.EvaluateStreamEnforced(t.Context(), []Text{{Path: "choices[0].content", Content: "safe"}})
	if result.Decision != DecisionBlock || !result.Degraded || len(result.Receipts) == 0 || result.Receipts[0].Outcome != OutcomeUnavailable {
		t.Fatalf("runtime capability loss was not failed closed: %+v", result)
	}
}

func installPolicies(service *Service, policies ...Policy) {
	service.snapshot.Store(&snapshot{policies: policies, loadedAt: time.Now().UTC(), version: policySetVersion(policies)})
}

func TestPIIRedactionTransformsExactTextWithoutRawEvidence(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	policy := Policy{
		ID: 1, Ref: "guardrail:1", Name: "redact email", Version: 3,
		Stage: StageInput, DetectorRef: DetectorPII, Action: ActionRedact,
		Mode: ModeEnforce, FailureMode: FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	}
	installPolicies(service, policy)
	pinned := service.Pin(Subject{OrgID: 7, APIKeyID: 9, Model: "gpt"})
	result := pinned.Evaluate(t.Context(), StageInput, []Text{{Path: "messages[0].content", Content: "contact alice@example.com now"}})
	if result.Decision != DecisionAllow || !result.Transformed || result.Texts[0].Content != "contact [REDACTED] now" {
		t.Fatalf("unexpected result: %+v", result)
	}
	evidence, err := json.Marshal(SafeReceiptMetadata(result))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(evidence), "alice@example.com") || !strings.Contains(string(evidence), `"outcome":"transform"`) {
		t.Fatalf("unsafe or incomplete evidence: %s", evidence)
	}
	if len(result.Receipts) != 1 || len(result.Receipts[0].Fingerprints) != 1 || len(result.Receipts[0].Fingerprints[0]) != 24 {
		t.Fatalf("missing irreversible bounded fingerprint: %+v", result.Receipts)
	}
}

func TestPlatformAndTenantPoliciesStackWithoutWeakening(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	orgID := int64(7)
	global := Policy{
		ID: 1, Ref: "guardrail:1", Name: "global block", Version: 1,
		Stage: StageInput, DetectorRef: DetectorPII, Action: ActionBlock,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true,
		Config: json.RawMessage(`{"entities":["secret"]}`),
	}
	tenant := Policy{
		ID: 2, Ref: "guardrail:2", Name: "tenant audit", Version: 1, OrgID: &orgID,
		Stage: StageInput, DetectorRef: DetectorPII, Action: ActionAudit,
		Mode: ModeEnforce, FailureMode: FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	}
	installPolicies(service, global, tenant)
	result := service.Pin(Subject{OrgID: orgID, APIKeyID: 3, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "token_abcdefghijkl and alice@example.com"}})
	if result.Decision != DecisionBlock || result.BlockedBy != global.Ref {
		t.Fatalf("tenant policy weakened mandatory global block: %+v", result)
	}
}

func TestExternalFailureModesAndNoRetry(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	calls := 0
	err := service.RegisterDetector("external:fixture", detectorFunc{
		version: "fixture.v1",
		detect: func(ctx Context, _ Text, _ json.RawMessage) ([]Finding, error) {
			calls++
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mandatory := Policy{
		ID: 1, Ref: "guardrail:1", Name: "mandatory external", Version: 1,
		Stage: StageInput, DetectorRef: "external:fixture", Action: ActionBlock,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true,
	}
	installPolicies(service, mandatory)
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "hello"}})
	if result.Decision != DecisionBlock || !result.Degraded || calls != 1 || result.Receipts[0].Outcome != OutcomeUnavailable {
		t.Fatalf("mandatory detector failure did not fail closed once: result=%+v calls=%d", result, calls)
	}

	calls = 0
	mandatory.Mandatory = false
	mandatory.FailureMode = FailureDegradedAllow
	installPolicies(service, mandatory)
	result = service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "hello"}})
	if result.Decision != DecisionAllow || !result.Degraded || calls != 1 {
		t.Fatalf("advisory detector failure did not degrade once: result=%+v calls=%d", result, calls)
	}
}

func TestDetectorInputBoundsUsePolicyFailureModeWithoutPartialScanning(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	policy := Policy{
		ID: 1, Ref: "guardrail:1", Name: "bounded PII", Version: 1,
		Stage: StageInput, DetectorRef: DetectorPII, Action: ActionBlock,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true,
	}
	installPolicies(service, policy)
	oversize := strings.Repeat("x", service.options.LocalDetectorMaxBytes+1)
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: oversize}})
	if result.Decision != DecisionBlock || !result.Degraded || result.Receipts[0].Outcome != OutcomeIndeterminate {
		t.Fatalf("fail-closed oversize detector input was not blocked: %+v", result)
	}

	policy.Mandatory = false
	policy.FailureMode = FailureDegradedAllow
	installPolicies(service, policy)
	result = service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: oversize}})
	if result.Decision != DecisionAllow || !result.Degraded || result.Receipts[0].Outcome != OutcomeIndeterminate {
		t.Fatalf("degraded-allow oversize detector input did not surface a gap: %+v", result)
	}

	calls := 0
	if err := service.RegisterDetector("external:bounded-fixture", detectorFunc{
		version: "fixture.v1",
		detect: func(Context, Text, json.RawMessage) ([]Finding, error) {
			calls++
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	policy.DetectorRef = "external:bounded-fixture"
	policy.FailureMode = FailureFailClosed
	installPolicies(service, policy)
	externalOversize := strings.Repeat("x", service.options.ExternalDetectorMaxBytes+1)
	result = service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: externalOversize}})
	if result.Decision != DecisionBlock || result.Receipts[0].Outcome != OutcomeIndeterminate || calls != 0 {
		t.Fatalf("oversize external input was partially dispatched: result=%+v calls=%d", result, calls)
	}
	tooManyTexts := make([]Text, service.options.MaxDetectorTexts+1)
	policy.DetectorRef = DetectorPII
	installPolicies(service, policy)
	result = service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, tooManyTexts)
	if result.Decision != DecisionBlock || result.Receipts[0].Outcome != OutcomeIndeterminate {
		t.Fatalf("high-cardinality text input bypassed the detector bound: %+v", result)
	}
}

func TestNoInspectableTextIsSkippedAndUsesPolicyFailureMode(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	policy := Policy{
		ID: 1, Ref: "guardrail:1", Name: "structured output", Version: 1,
		Stage: StageNonStreamOutput, DetectorRef: DetectorPII, Action: ActionBlock,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true,
	}
	installPolicies(service, policy)
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageNonStreamOutput, nil)
	if result.Decision != DecisionBlock || !result.Degraded || result.Receipts[0].Outcome != OutcomeSkipped {
		t.Fatalf("uninspectable mandatory output was reported as safe: %+v", result)
	}
	policy.Mandatory = false
	policy.FailureMode = FailureDegradedAllow
	installPolicies(service, policy)
	result = service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageNonStreamOutput, nil)
	if result.Decision != DecisionAllow || !result.Degraded || result.Receipts[0].Outcome != OutcomeSkipped {
		t.Fatalf("uninspectable advisory output lost degraded evidence: %+v", result)
	}
}

func TestNilPinnedGuardrailFailsClosedWithoutPanic(t *testing.T) {
	var pinned *Pinned
	result := pinned.Evaluate(t.Context(), StageInput, []Text{{Content: "hello"}})
	if result.Decision != DecisionBlock || result.PolicySetVersion != "unavailable" || !result.Degraded {
		t.Fatalf("nil guardrail pin did not fail closed: %+v", result)
	}
}

func TestEquivalentPoliciesReuseOnePinnedDetectorResult(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	calls := 0
	if err := service.RegisterDetector("external:cached-fixture", detectorFunc{
		version: "fixture.v1",
		detect: func(Context, Text, json.RawMessage) ([]Finding, error) {
			calls++
			return []Finding{{Category: "policy", Start: 0, End: 5, ConfidenceBucket: "high"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	first := Policy{ID: 1, Ref: "guardrail:1", Name: "first", Version: 1, Stage: StageInput, DetectorRef: "external:cached-fixture", Action: ActionAudit, Mode: ModeEnforce, FailureMode: FailureDegradedAllow}
	second := first
	second.ID, second.Ref, second.Name = 2, "guardrail:2", "second"
	installPolicies(service, first, second)
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Path: "messages[0].content", Content: "hello"}})
	if result.Decision != DecisionAllow || len(result.Receipts) != 2 || calls != 1 {
		t.Fatalf("equivalent pinned policies repeated detector work: result=%+v calls=%d", result, calls)
	}
}

func TestExternalPoliciesShareOneEvaluationDeadline(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	deadlines := make([]time.Time, 0, 2)
	if err := service.RegisterDetector("external:shared-deadline", detectorFunc{
		version: "fixture.v1",
		detect: func(ctx Context, _ Text, _ json.RawMessage) ([]Finding, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("external detector call has no deadline")
			}
			deadlines = append(deadlines, deadline)
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	first := Policy{ID: 1, Ref: "guardrail:1", Name: "first", Version: 1, Stage: StageInput, DetectorRef: "external:shared-deadline", Action: ActionAudit, Mode: ModeEnforce, FailureMode: FailureDegradedAllow, Config: json.RawMessage(`{"threshold":1}`)}
	second := first
	second.ID, second.Ref, second.Name, second.Config = 2, "guardrail:2", "second", json.RawMessage(`{"threshold":2}`)
	installPolicies(service, first, second)
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "hello"}})
	if result.Decision != DecisionAllow || len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("external policy chain renewed its deadline: result=%+v deadlines=%+v", result, deadlines)
	}
}

func TestExternalDetectorConcurrencySaturationFailsWithoutExtraDispatch(t *testing.T) {
	options := DefaultOptions()
	options.ExternalDeadline = 100 * time.Millisecond
	options.ExternalConcurrency = 1
	service, err := NewService(nil, nil, []byte("0123456789abcdef0123456789abcdef"), options)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	if err := service.RegisterDetector("external:saturated-fixture", detectorFunc{
		version: "fixture.v1",
		detect: func(ctx Context, _ Text, _ json.RawMessage) ([]Finding, error) {
			calls.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	policy := Policy{ID: 1, Ref: "guardrail:1", Name: "mandatory", Version: 1, Stage: StageInput, DetectorRef: "external:saturated-fixture", Action: ActionBlock, Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true}
	installPolicies(service, policy)
	firstResult := make(chan Result, 1)
	go func() {
		firstResult <- service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "first"}})
	}()
	<-entered
	second := service.Pin(Subject{OrgID: 1, APIKeyID: 2, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "second"}})
	if second.Decision != DecisionBlock || second.Receipts[0].Outcome != OutcomeUnavailable || calls.Load() != 1 {
		t.Fatalf("saturated detector dispatched extra work: result=%+v calls=%d", second, calls.Load())
	}
	close(release)
	if first := <-firstResult; first.Decision != DecisionAllow {
		t.Fatalf("first bounded detector call did not complete: %+v", first)
	}
}

func TestPinnedPolicyVersionIsImmutableAcrossRefresh(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	policy := Policy{
		ID: 1, Ref: "guardrail:1", Name: "audit", Version: 1,
		Stage: StageInput, DetectorRef: DetectorPII, Action: ActionAudit,
		Mode: ModeEnforce, FailureMode: FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	}
	installPolicies(service, policy)
	pinned := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"})
	originalVersion := pinned.version
	policy.Version = 2
	policy.Action = ActionBlock
	installPolicies(service, policy)
	oldResult := pinned.Evaluate(t.Context(), StageInput, []Text{{Content: "alice@example.com"}})
	newResult := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "alice@example.com"}})
	if oldResult.PolicySetVersion != originalVersion || oldResult.Decision != DecisionAllow || newResult.Decision != DecisionBlock || newResult.PolicySetVersion == originalVersion {
		t.Fatalf("pin changed across refresh: old=%+v new=%+v", oldResult, newResult)
	}
}

func TestInvalidAndIndeterminatePoliciesAreSafe(t *testing.T) {
	invalid := Policy{
		Ref: "guardrail:x", Name: "bad", Version: 1, Stage: StageInput,
		DetectorRef: DetectorSensitiveWords, Action: ActionRedact,
		Mode: ModeEnforce, FailureMode: FailureFailClosed,
	}
	if !errors.Is(invalid.Validate(), ErrInvalidPolicy) {
		t.Fatalf("expected incompatible sensitive redaction to fail validation")
	}
	credentialConfig := invalid
	credentialConfig.DetectorRef = DetectorPII
	credentialConfig.Action = ActionAudit
	credentialConfig.Config = json.RawMessage(`{"headers":[{"client_secret":"should-not-be-stored"}]}`)
	if !errors.Is(credentialConfig.Validate(), ErrInvalidPolicy) {
		t.Fatal("policy config accepted detector credentials")
	}
	configValidationService := newGuardrailTestService(t, nil)
	unknownPIIConfig := credentialConfig
	unknownPIIConfig.Config = json.RawMessage(`{"entity":["email"]}`)
	if !errors.Is(configValidationService.ValidatePolicy(unknownPIIConfig), ErrInvalidPolicy) {
		t.Fatal("PII policy accepted a misspelled configuration field")
	}

	service := newGuardrailTestService(t, filter.NewDFAFilter([]string{"blocked"}))
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt", Endpoint: "chat"}).Evaluate(t.Context(), StageInput, []Text{{Content: "blocked content"}})
	if result.Decision != DecisionBlock || result.Receipts[0].DetectorRef != DetectorSensitiveWords {
		t.Fatalf("sensitive-word compatibility did not run through kernel: %+v", result)
	}
}

func TestDetectorCannotSmuggleRawContentThroughEvidenceLabels(t *testing.T) {
	service := newGuardrailTestService(t, nil)
	err := service.RegisterDetector("external:unsafe-fixture", detectorFunc{
		version: "fixture.v1",
		detect: func(Context, Text, json.RawMessage) ([]Finding, error) {
			return []Finding{{Category: "token_abcdefghijkl", Start: 0, End: 5, ConfidenceBucket: "high"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{
		ID: 7, Ref: "guardrail:7", Name: "mandatory fixture", Version: 1,
		Stage: StageInput, DetectorRef: "external:unsafe-fixture", Action: ActionBlock,
		Mode: ModeEnforce, FailureMode: FailureFailClosed, Mandatory: true,
	}
	installPolicies(service, policy)
	result := service.Pin(Subject{OrgID: 1, APIKeyID: 1, Model: "gpt"}).Evaluate(t.Context(), StageInput, []Text{{Content: "alice"}})
	encoded, _ := json.Marshal(SafeReceiptMetadata(result))
	if result.Decision != DecisionBlock || result.Receipts[0].Outcome != OutcomeMatch ||
		result.Receipts[0].Categories["external_finding"] != 1 || strings.Contains(string(encoded), "token_abcdefghijkl") {
		t.Fatalf("unsafe detector label reached evidence: %s", encoded)
	}
}

func TestLocalPIISecretsDetectorLatencyEvidence(t *testing.T) {
	const iterations = 500
	content := strings.Repeat("ordinary request context ", 640) + " alice@example.com token_abcdefghijkl 4242 4242 4242 4242"
	detector := piiSecretsDetector{}
	durations := make([]time.Duration, 0, iterations)
	for range iterations {
		started := time.Now()
		findings, err := detector.Detect(t.Context(), Text{Path: "messages[0].content", Content: content}, nil)
		if err != nil || len(findings) != 3 {
			t.Fatalf("unexpected local detector result: findings=%+v err=%v", findings, err)
		}
		durations = append(durations, time.Since(started))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(iterations*95)/100-1]
	t.Logf("local deterministic detector fixture_bytes=%d iterations=%d p95=%s", len(content), iterations, p95)
}

func BenchmarkLocalPIISecretsDetector16KiB(b *testing.B) {
	content := strings.Repeat("ordinary request context ", 640) + " alice@example.com token_abcdefghijkl 4242 4242 4242 4242"
	detector := piiSecretsDetector{}
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	for range b.N {
		if _, err := detector.Detect(b.Context(), Text{Path: "messages[0].content", Content: content}, nil); err != nil {
			b.Fatal(err)
		}
	}
}
