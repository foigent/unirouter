package endpointprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestCatalogCoversEveryFixedProfileWithImmutableEvidence(t *testing.T) {
	profiles, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog failed: %v", err)
	}
	if len(profiles) != 4 {
		t.Fatalf("profiles=%d want 4", len(profiles))
	}
	wantRuntimeState := map[string]string{
		ProfileOpenAIResponsesText:   RuntimeContractOnly,
		ProfileAnthropicMessagesText: RuntimeContractOnly,
		ProfileCohereRerankV2:        RuntimeContractOnly,
		ProfileOpenAIImageGPT:        RuntimeRepositoryImageV1,
	}
	var schemaDigest string
	for _, profile := range profiles {
		if !artifactHashPattern.MatchString(profile.ManifestSHA256) ||
			!artifactHashPattern.MatchString(profile.SchemaSHA256) {
			t.Fatalf("profile lacks digests: %+v", profile)
		}
		if schemaDigest == "" {
			schemaDigest = profile.SchemaSHA256
		} else if profile.SchemaSHA256 != schemaDigest {
			t.Fatalf("schema digest drift: %s vs %s", profile.SchemaSHA256, schemaDigest)
		}
		if profile.Manifest.ConformanceLevel != "fixture_verified" ||
			profile.Manifest.NetworkVerified {
			t.Fatalf("profile overclaims evidence/runtime: %+v", profile.Manifest)
		}
		if want := wantRuntimeState[profile.Manifest.ProfileID]; profile.Manifest.RuntimeState != want {
			t.Fatalf(
				"profile %s runtime_state=%q want %q",
				profile.Manifest.ProfileID,
				profile.Manifest.RuntimeState,
				want,
			)
		}
	}
}

func TestOfflineConformanceMatchesGoldenDeterministically(t *testing.T) {
	first, err := RunOffline(context.Background(), NewRegistry())
	if err != nil {
		t.Fatalf("RunOffline failed: %v", err)
	}
	second, err := RunOffline(context.Background(), NewRegistry())
	if err != nil {
		t.Fatalf("second RunOffline failed: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("offline report is not deterministic\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
	if first.Summary.Profiles != 4 ||
		first.Summary.Cases != 41 ||
		first.Summary.Passed != 41 ||
		first.Summary.Failed != 0 {
		t.Fatalf("summary=%+v", first.Summary)
	}
	formatted, err := json.MarshalIndent(first, "", "  ")
	if err != nil {
		t.Fatalf("format report: %v", err)
	}
	formatted = append(formatted, '\n')
	golden, err := artifactFS.ReadFile("artifacts/reports/core.v1.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(formatted, golden) {
		t.Fatalf("golden report drifted\nactual=%s\ngolden=%s", formatted, golden)
	}
}

func TestOfflineConformanceResolvesManifestExactProfileVersion(t *testing.T) {
	v1 := openAIResponsesProfile{}
	v2Descriptor := v1.Descriptor()
	v2Descriptor.ProfileVersion = "2.0.0"
	registry := newRegistry(
		descriptorOverrideProfile{Profile: v1, descriptor: v2Descriptor},
		anthropicMessagesProfile{},
		cohereRerankProfile{},
		openAIImageProfile{},
	)
	_, err := RunOffline(context.Background(), registry)
	assertProtocolConfigurationError(t, err, "unknown_profile_version")
}
