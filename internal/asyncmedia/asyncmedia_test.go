package asyncmedia

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func TestFixturesFreezeOnlyPreGateProductSemantics(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(fixtures) != 2 {
		t.Fatalf("fixtures=%d want=2", len(fixtures))
	}
	seen := map[string]bool{}
	for _, loaded := range fixtures {
		plan := loaded.validated.Fixture.Plan
		seen[plan.OperationClass] = true
		if plan.RuntimeState != RuntimeStateDecisionGated ||
			plan.DecisionGates != (DecisionGates{
				MediaProvider: DecisionPending, ObjectStore: DecisionPending,
				Retention: DecisionPending, ChargeTrigger: DecisionPending,
			}) {
			t.Fatalf("decision gate drifted: %#v", plan)
		}
		if plan.RuntimeBoundary != (RuntimeBoundary{
			RuntimeMutation:      RuntimeMutationNone,
			ExternalDecisionGate: "P7-D03",
		}) {
			t.Fatalf("runtime boundary drifted: %#v", plan.RuntimeBoundary)
		}
		if plan.Privacy != (PrivacyBoundary{MetadataOnly: true}) {
			t.Fatalf("privacy boundary drifted: %#v", plan.Privacy)
		}
		if plan.ChargeState != ChargeUnresolved {
			t.Fatalf("charge state=%s", plan.ChargeState)
		}
	}
	if !seen[OperationTextToImage] || !seen[OperationImageToVideo] {
		t.Fatalf("fixture coverage=%v", seen)
	}
}

func TestIdempotencyBindsTenantIntentAndImmutableObjectMetadata(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	var video JobPlan
	for _, fixture := range fixtures {
		if fixture.validated.Fixture.Plan.OperationClass == OperationImageToVideo {
			video = fixture.validated.Fixture.Plan
		}
	}
	baseline, err := ComputeIdempotencyKey(video)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*JobPlan){
		"tenant": func(plan *JobPlan) {
			plan.TenantScope.ProjectRef = "203"
			plan.InputObjects[0].TenantScope.ProjectRef = "203"
		},
		"intent": func(plan *JobPlan) {
			plan.IntentSHA256 = "sha256:" + strings.Repeat("4", 64)
		},
		"object-content": func(plan *JobPlan) {
			plan.InputObjects[0].ContentSHA256 =
				"sha256:" + strings.Repeat("5", 64)
		},
		"object-size": func(plan *JobPlan) {
			plan.InputObjects[0].SizeBytes = "4097"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := video
			changed.InputObjects = append([]MediaObjectRef(nil), video.InputObjects...)
			mutate(&changed)
			got, hashErr := ComputeIdempotencyKey(changed)
			if hashErr != nil {
				t.Fatal(hashErr)
			}
			if got == baseline {
				t.Fatalf("%s did not change idempotency identity", name)
			}
		})
	}
}

func TestStrictFixtureRejectsContentAndRuntimeExpansion(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	image := fixtures[0]
	if image.validated.Fixture.Plan.OperationClass != OperationTextToImage {
		image = fixtures[1]
	}
	tests := []struct {
		name string
		body []byte
		code string
	}{
		{
			name: "raw prompt",
			body: bytes.Replace(
				image.raw,
				[]byte(`"plan_id":`),
				[]byte(`"raw_prompt":"forbidden","plan_id":`),
				1,
			),
			code: "input_shape_invalid",
		},
		{
			name: "duplicate",
			body: bytes.Replace(
				image.raw,
				[]byte(`"fixture_id":`),
				[]byte(`"fixture_id":"duplicate","fixture_id":`),
				1,
			),
			code: "duplicate_json_key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, validateErr := ValidateFixture(test.body); errorCode(validateErr) !=
				test.code {
				t.Fatalf("error=%v want=%s", validateErr, test.code)
			}
		})
	}

	expanded := cloneFixture(image.validated.Fixture)
	expanded.Plan.RuntimeBoundary.ProviderNetworkCall = true
	if _, err := ValidateFixture(mustMarshal(expanded)); errorCode(err) !=
		"runtime_boundary_violated" {
		t.Fatalf("runtime expansion error=%v", err)
	}
}

func TestDependenciesPinExactDecisionGatedImageEvidence(t *testing.T) {
	dependencies, err := authoritativeDependencies()
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 4 {
		t.Fatalf("dependencies=%d want=4", len(dependencies))
	}
	expected := map[string]string{
		"internal/endpointprotocol/artifacts/endpoint-profile.v1.schema.json":    "sha256:e1546a5a7638e400f89b43ea6684e2879a072eb13f0a74a312979aea356752cc",
		"internal/endpointprotocol/artifacts/fixtures/openai-gpt-image-v1.json":  "sha256:3b802f15539ff4fe0309ca93a779a3639da5910b0f28df3087aa24520fa16755",
		"internal/endpointprotocol/artifacts/manifests/openai-gpt-image.v1.json": "sha256:792d54b5341ae13425afd24ffa864c068b705043f2b09f29c6c717963b9febd7",
		"internal/endpointprotocol/artifacts/reports/core.v1.json":               "sha256:ab3d4ec66077922ce921f1def0ccfa501ba8bf5866190c3581fa5c71a91b5759",
	}
	for _, dependency := range dependencies {
		if expected[dependency.Path] != dependency.SHA256 {
			t.Fatalf("dependency=%#v", dependency)
		}
		delete(expected, dependency.Path)
	}
	if len(expected) != 0 {
		t.Fatalf("missing dependencies=%v", expected)
	}
	catalog, err := endpointprotocol.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range catalog {
		if profile.Manifest.ProfileID == endpointprotocol.ProfileOpenAIImageGPT &&
			profile.Manifest.RuntimeState != endpointprotocol.RuntimeDecisionGate {
			t.Fatalf("image profile runtime state=%s", profile.Manifest.RuntimeState)
		}
	}
}

func TestConformanceAndGoldenReportAreByteExact(t *testing.T) {
	report, err := RunConformance()
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != (struct {
		Fixtures int `json:"fixtures"`
		Cases    int `json:"cases"`
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
	}{Fixtures: 2, Cases: 55, Passed: 55}) {
		t.Fatalf("summary=%#v", report.Summary)
	}
	classes := make(map[string]struct{})
	for _, testCase := range report.Cases {
		for _, class := range testCase.CaseClasses {
			classes[class] = struct{}{}
		}
	}
	if len(classes) != 36 {
		t.Fatalf("classes=%d want=36", len(classes))
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
		t.Fatalf("conformance report differs from golden")
	}
}

func TestFixtureJSONRoundTripDoesNotInventNullOrContent(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		body, marshalErr := json.Marshal(fixture.validated.Fixture)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		lower := strings.ToLower(string(body))
		for _, forbidden := range []string{
			`"raw_prompt"`, `"raw_media"`, `"storage_locator"`,
			`"signed_url"`, `"credential_value"`, `:null`,
		} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s includes forbidden content %q", fixture.identity.Path, forbidden)
			}
		}
	}
}
