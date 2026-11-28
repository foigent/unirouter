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
	if len(profiles) != 29 {
		t.Fatalf("profiles=%d want 29", len(profiles))
	}
	wantRuntimeState := map[string]string{
		ProfileOpenAIResponsesText:         RuntimeContractOnly,
		ProfileOpenAIResponsesTextV2:       RuntimeContractOnly,
		ProfileOpenAIResponsesToolsV3:      RuntimeContractOnly,
		ProfileAnthropicMessagesText:       RuntimeContractOnly,
		ProfileAnthropicMessagesTextV2:     RuntimeContractOnly,
		ProfileAnthropicMessagesTextV3:     RuntimeContractOnly,
		ProfileCohereRerankV2:              RuntimeContractOnly,
		ProfileMiniMaxH3Image:              RuntimeRepositoryImageV1,
		ProfileDashScopeQwenImage:          RuntimeRepositoryImageV1,
		ProfileDashScopeWanVideo:           RuntimeRepositoryAsyncMediaV1,
		ProfileMiniMaxH3Video:              RuntimeRepositoryAsyncMediaV1,
		ProfileGLMCogVideo:                 RuntimeRepositoryAsyncMediaV1,
		ProfileGLMImage:                    RuntimeRepositoryImageV1,
		ProfileDeepSeekOCR:                 RuntimeRepositoryAsyncMediaV1,
		ProfileGLMOCR:                      RuntimeRepositoryAsyncMediaV1,
		ProfileDeepSeekASR:                 RuntimeRepositoryAsyncMediaV1,
		ProfileDeepSeekTTS:                 RuntimeRepositoryAsyncMediaV1,
		ProfileMiniMaxASR:                  RuntimeRepositoryAsyncMediaV1,
		ProfileMiniMaxTTS:                  RuntimeRepositoryAsyncMediaV1,
		ProfileGLMASR:                      RuntimeRepositoryAsyncMediaV1,
		ProfileGLMTTS:                      RuntimeRepositoryAsyncMediaV1,
		ProfileOpenAITranscribe:            RuntimeRepositoryAsyncMediaV1,
		ProfileMiMoASR:                     RuntimeRepositoryAsyncMediaV1,
		ProfileMiMoTTS:                     RuntimeRepositoryAsyncMediaV1,
		ProfileDashScopeASR:                RuntimeRepositoryAsyncMediaV1,
		ProfileDashScopeTTS:                RuntimeRepositoryAsyncMediaV1,
		ProfileOpenAIImageGPT:              RuntimeRepositoryImageV1,
		ProfileOpenAIResponsesInlineJPEG:   RuntimeContractOnly,
		ProfileAnthropicMessagesInlineJPEG: RuntimeContractOnly,
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
	for _, profile := range first.Profiles {
		for _, result := range profile.Cases {
			if !result.Passed {
				t.Fatalf("offline case %s/%s failed: %s", profile.ProfileID, result.CaseID, result.Message)
			}
		}
	}
	if first.Summary.Profiles != 29 ||
		first.Summary.Cases != 195 ||
		first.Summary.Passed != 195 ||
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
	registry := NewRegistry()
	profiles := make([]Profile, 0, len(registry.order))
	for _, identity := range registry.order {
		profile := registry.profiles[identity]
		if identity.profileID == v2Descriptor.ProfileID && identity.profileVersion == v1.Descriptor().ProfileVersion {
			profile = descriptorOverrideProfile{Profile: v1, descriptor: v2Descriptor}
		}
		profiles = append(profiles, profile)
	}
	registry = newRegistry(profiles...)
	_, err := RunOffline(context.Background(), registry)
	assertProtocolConfigurationError(t, err, "unknown_profile_version")
}
