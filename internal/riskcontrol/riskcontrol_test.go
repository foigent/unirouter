package riskcontrol

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/uniroute/unirouter/internal/agenttrust"
	"github.com/uniroute/unirouter/internal/provenancepassport"
)

func TestEvaluateBuildsBoundedAdviceAndHumanReviewedProposal(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatalf("sample request: %v", err)
	}
	report, err := Evaluate(body)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if report.Advice != AdviceShadowOnly {
		t.Fatalf("advice=%s want=%s", report.Advice, AdviceShadowOnly)
	}
	if report.Dimensions != (RiskDimensions{
		Authority: RiskElevated, Change: RiskElevated,
		Provenance: RiskKnownGap, Financial: RiskLow,
	}) {
		t.Fatalf("dimensions=%#v", report.Dimensions)
	}
	if report.Assertion.Scope != AssertionScope ||
		report.Assertion.AuthorityEffect != "downscope_only" ||
		report.Assertion.SignatureState != "not_issued_offline" ||
		report.Assertion.ExpiresAt != "2026-07-31T08:10:00.000000Z" ||
		report.Assertion.RuntimeEnforcementAuthorized {
		t.Fatalf("assertion boundary=%#v", report.Assertion)
	}
	if report.Proposal.State != ProposalSimulatedReviewRequired ||
		!report.Proposal.HumanReviewRequired ||
		report.Proposal.SelfPromoted ||
		report.Proposal.RuntimePromotionAuthorized ||
		report.Proposal.PromotionGate != PromotionGate {
		t.Fatalf("proposal boundary=%#v", report.Proposal)
	}
	if report.RuntimeBoundary.RuntimeMutation != RuntimeMutationNone ||
		report.RuntimeBoundary.RemoteRiskDependency ||
		report.RuntimeBoundary.ProviderOrShadowTraffic ||
		report.RuntimeBoundary.RuntimePolicyRead {
		t.Fatalf("runtime boundary=%#v", report.RuntimeBoundary)
	}
}

func TestEvaluatePreservesExactB10AndB11EvidenceBoundaries(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatalf("sample request: %v", err)
	}
	var request ReviewRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	report, err := Evaluate(body)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if report.Evidence.AgentTrustDecision != agenttrust.DecisionShadowOnly ||
		!report.Evidence.ReceiptCryptographicallyValid ||
		report.Evidence.ReceiptRuntimeTrusted {
		t.Fatalf("Agent Trust boundary=%#v", report.Evidence)
	}
	if report.Evidence.SourceHash !=
		"sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27" ||
		report.Evidence.EnvelopeHash !=
			"sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2" ||
		report.Evidence.CaptureGapCount != 4 {
		t.Fatalf("Provenance boundary=%#v", report.Evidence)
	}
	if report.Evidence.SourceScope.OrgRef != "101" ||
		report.Evidence.SourceScope.ProjectRef != "202" ||
		report.Evidence.SourceScope.SourceInstance != "unirouter-development" ||
		report.Evidence.SourceScope.ScopeHash !=
			request.EvidenceBinding.PassportSourceScopeHash ||
		report.Evidence.SourceScope.TenantBindingStatus != ScopeBindingUnverified {
		t.Fatalf("Source scope boundary=%#v", report.Evidence.SourceScope)
	}
	passportBody, _ := json.Marshal(request.ProvenancePassport)
	passport, err := provenancepassport.Evaluate(passportBody)
	if err != nil {
		t.Fatalf("evaluate nested passport: %v", err)
	}
	if !passport.Source.ExactBytesPreserved ||
		passport.Compatibility.FeedWireUnchanged != true ||
		passport.Compatibility.CursorBehaviorUnchanged != true {
		t.Fatalf("Source compatibility drifted=%#v %#v", passport.Source, passport.Compatibility)
	}
}

func TestConformanceDependenciesPinExactB8B10B11Artifacts(t *testing.T) {
	dependencies, err := dependencyArtifacts()
	if err != nil {
		t.Fatalf("dependencies: %v", err)
	}
	if len(dependencies) != 9 {
		t.Fatalf("dependencies=%d want=9", len(dependencies))
	}
	expected := map[string]string{
		"internal/governancesim/artifacts/governance-simulation.v1.schema.json":    "sha256:02e00f49039d813b7ef2a739c2c0f32200a7f9df68e0bd0356a84c1927d4e123",
		"internal/agenttrust/artifacts/agent-trust-review.v1.schema.json":          "sha256:1e375c8febfd93e9972cdaab88c15c1aecf08537fc56ee1a2dfc5e6b037c6a37",
		"internal/provenancepassport/artifacts/provenance-passport.v1.schema.json": "sha256:4b577521cc5b7ce2a2e5ff6f38e21c34e367e7bde04153da399f02304fafe4db",
	}
	for _, dependency := range dependencies {
		if want, exists := expected[dependency.Path]; exists && dependency.SHA256 != want {
			t.Fatalf("%s=%s want=%s", dependency.Path, dependency.SHA256, want)
		}
		delete(expected, dependency.Path)
	}
	if len(expected) != 0 {
		t.Fatalf("missing dependency identities=%v actual=%v", expected, dependencies)
	}
}

func TestConformanceAndGoldenReportAreByteExact(t *testing.T) {
	report, err := RunConformance()
	if err != nil {
		t.Fatalf("run conformance: %v", err)
	}
	if report.Summary.Failed != 0 ||
		report.Summary.Passed != report.Summary.Cases {
		t.Fatalf("conformance failed=%#v cases=%#v", report.Summary, report.Cases)
	}
	actual, err := MarshalConformanceReport(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	golden, err := GoldenReportBytes()
	if err != nil {
		t.Fatalf("golden report: %v", err)
	}
	if !bytes.Equal(actual, golden) {
		t.Fatalf("conformance report differs from golden")
	}
}

func TestStrictContractRejectsUnknownContentAndBindingTamper(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatalf("sample request: %v", err)
	}
	unknown := bytes.Replace(
		body,
		[]byte(`"review_id":`),
		[]byte(`"raw_prompt":"forbidden","review_id":`),
		1,
	)
	if _, err := Evaluate(unknown); errorCode(err) != "input_shape_invalid" {
		t.Fatalf("unknown content error=%v", err)
	}
	var request ReviewRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.EvidenceBinding.PassportEnvelopeHash =
		"sha256:" + strings.Repeat("0", 64)
	if _, err := Evaluate(mustMarshal(request)); errorCode(err) !=
		"passport_envelope_hash_mismatch" {
		t.Fatalf("binding tamper error=%v", err)
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.EvidenceBinding.PassportSourceScopeHash =
		"sha256:" + strings.Repeat("0", 64)
	if _, err := Evaluate(mustMarshal(request)); errorCode(err) !=
		"passport_source_scope_hash_mismatch" {
		t.Fatalf("Source scope binding tamper error=%v", err)
	}
}

func TestMemoryReplayIsConcurrentAndFailClosed(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatalf("sample request: %v", err)
	}
	service := NewService(NewMemoryStore())
	const workers = 12
	var wait sync.WaitGroup
	wait.Add(workers)
	errs := make(chan error, workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer wait.Done()
			_, applyErr := service.Apply(body)
			errs <- applyErr
		}()
	}
	wait.Wait()
	close(errs)
	for applyErr := range errs {
		if applyErr != nil {
			t.Fatalf("concurrent duplicate failed: %v", applyErr)
		}
	}
	if len(service.store.reviews) != 1 {
		t.Fatalf("stored reviews=%d want=1", len(service.store.reviews))
	}

	var conflict ReviewRequest
	if err := json.Unmarshal(body, &conflict); err != nil {
		t.Fatal(err)
	}
	conflict.RiskEpoch = "2"
	if _, err := service.Apply(mustMarshal(conflict)); errorCode(err) !=
		"review_identity_conflict" {
		t.Fatalf("semantic conflict error=%v", err)
	}
}
