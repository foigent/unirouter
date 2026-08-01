package agenttrust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
)

func TestFixtureEvaluatesAndVerifiesSignedReceipt(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(body)
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != DecisionShadowOnly || !report.ReceiptVerification.CryptographicallyValid {
		t.Fatalf("unexpected fixture report: %+v", report)
	}
	if report.ReceiptVerification.RuntimeTrusted || report.RuntimeEnforcementAuthorized ||
		report.RuntimeMutation != RuntimeMutationNone || report.SynchronousRuntimeDependency {
		t.Fatalf("fixture crossed runtime trust boundary: %+v", report)
	}
	if len(report.EffectiveAuthorityTree) != 3 || report.EffectiveAuthorityTree[2].State != NodeRevoked {
		t.Fatalf("unexpected authority tree: %+v", report.EffectiveAuthorityTree)
	}
}

func TestDynamicReceiptReplayAndIdentityConflict(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	var request ReviewRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.ReviewID = "agent-trust-dynamic-001"
	request.SignedReceipt = SignedDecisionReceipt{}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := BuildReceiptPayload(request)
	if err != nil {
		t.Fatal(err)
	}
	request.SignedReceipt, err = SignDecisionReceipt(payload, "test-key", "7", privateKey, "fixture_local")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(publicKey, privateKey.Public().(ed25519.PublicKey)) {
		t.Fatal("generated signing key pair mismatch")
	}
	encoded, _ := json.Marshal(request)
	service := NewService(NewMemoryStore())
	accepted, err := service.Apply(encoded)
	if err != nil || accepted.ReplayStatus != ReplayAccepted {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	pretty, _ := json.MarshalIndent(request, "", "  ")
	duplicate, err := service.Apply(pretty)
	if err != nil || duplicate.ReplayStatus != ReplayDuplicateNoop {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}

	changed := cloneRequest(request)
	changed.ProvenanceRef = "sha256:8888888888888888888888888888888888888888888888888888888888888888"
	changed.SignedReceipt = SignedDecisionReceipt{}
	changedPayload, err := BuildReceiptPayload(changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.SignedReceipt, err = SignDecisionReceipt(changedPayload, "test-key", "7", privateKey, "fixture_local")
	if err != nil {
		t.Fatal(err)
	}
	changedBody, _ := json.Marshal(changed)
	if _, err := service.Apply(changedBody); !errors.Is(err, ErrConflict) || errorCode(err) != "review_identity_conflict" {
		t.Fatalf("expected review identity conflict, got %v", err)
	}
}

func TestAuthorityReviewUsesCurrentAgentEndpointBoundary(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatal(err)
	}
	var request ReviewRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	request.Authority.Nodes[1].Limits.AllowedEndpoints = []string{"/v1/tools/execute"}
	encoded, _ := json.Marshal(request)
	if _, err := Evaluate(encoded); errorCode(err) != "authority_endpoint_invalid" {
		t.Fatalf("expected current model-gateway endpoint rejection, got %v", err)
	}
}

func TestConformanceGoldenIsExact(t *testing.T) {
	report, err := RunConformance()
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Failed != 0 {
		t.Fatalf("summary=%+v cases=%+v", report.Summary, report.Cases)
	}
	actual, err := MarshalConformanceReport(report)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := GoldenReportBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, golden) {
		t.Fatalf("golden report mismatch\nactual:\n%s\ngolden:\n%s", actual, golden)
	}
}
