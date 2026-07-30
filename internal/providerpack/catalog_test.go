package providerpack

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/uniroute/unirouter/internal/adapters"
)

func TestCatalogHasThreeStrictFixtureVerifiedCorePacks(t *testing.T) {
	packs, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog failed: %v", err)
	}
	if len(packs) != 3 {
		t.Fatalf("packs=%d want 3", len(packs))
	}
	wantAdapters := map[string]bool{
		adapters.TypeOpenAICompatible: false,
		adapters.TypeAnthropicNative:  false,
		adapters.TypeAzureOpenAI:      false,
	}
	var schemaDigest string
	for _, pack := range packs {
		if pack.Manifest.ConformanceLevel != "fixture_verified" || pack.Manifest.NetworkVerified {
			t.Fatalf("pack overclaims conformance: %+v", pack)
		}
		if !digestPattern.MatchString(pack.ManifestSHA256) || !digestPattern.MatchString(pack.SchemaSHA256) {
			t.Fatalf("pack lacks immutable digests: %+v", pack)
		}
		if schemaDigest == "" {
			schemaDigest = pack.SchemaSHA256
		} else if pack.SchemaSHA256 != schemaDigest {
			t.Fatalf("pack schema digests differ: %s vs %s", pack.SchemaSHA256, schemaDigest)
		}
		if !adapters.DefaultRegistry().Has(pack.Manifest.AdapterType) {
			t.Fatalf("pack adapter is not registered: %s", pack.Manifest.AdapterType)
		}
		if _, ok := wantAdapters[pack.Manifest.AdapterType]; !ok {
			t.Fatalf("unexpected core adapter %s", pack.Manifest.AdapterType)
		}
		wantAdapters[pack.Manifest.AdapterType] = true
	}
	for adapterType, found := range wantAdapters {
		if !found {
			t.Fatalf("missing pack for %s", adapterType)
		}
	}
	if got, err := GoldenReportDigest(); err != nil {
		t.Fatalf("GoldenReportDigest failed: %v", err)
	} else if got != "sha256:803111ecad2b365508bcac0afae2d92185d7d12de3190db31653c8eb72b33488" {
		t.Fatalf("golden report digest=%s", got)
	}
}

func TestManifestSemanticValidationRejectsUnprovedOrUnsupportedClaims(t *testing.T) {
	packs, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog failed: %v", err)
	}
	baseline := packs[0].Manifest
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{
			name: "unregistered adapter",
			mutate: func(manifest *Manifest) {
				manifest.AdapterType = "vendor_magic"
			},
		},
		{
			name: "modality without endpoint",
			mutate: func(manifest *Manifest) {
				manifest.Modalities = []string{"embeddings"}
			},
		},
		{
			name: "unsupported auth profile",
			mutate: func(manifest *Manifest) {
				manifest.AuthProfiles = []AuthProfile{{Type: "query_secret", WireHeader: "api-key"}}
			},
		},
		{
			name: "auth profile wire mismatch",
			mutate: func(manifest *Manifest) {
				manifest.AuthProfiles = []AuthProfile{{Type: "api_key", WireHeader: "Authorization"}}
			},
		},
		{
			name: "unsupported cache claim",
			mutate: func(manifest *Manifest) {
				manifest.UsageProfile.CacheClasses = []string{"cache_write"}
			},
		},
		{
			name: "nonportable fixture path",
			mutate: func(manifest *Manifest) {
				manifest.Fixtures[0].Path = "fixtures/../fixture.json"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, marshalErr := json.Marshal(baseline)
			if marshalErr != nil {
				t.Fatalf("marshal baseline: %v", marshalErr)
			}
			var manifest Manifest
			if unmarshalErr := json.Unmarshal(raw, &manifest); unmarshalErr != nil {
				t.Fatalf("unmarshal baseline: %v", unmarshalErr)
			}
			test.mutate(&manifest)
			if validateErr := validateManifest(manifest); validateErr == nil {
				t.Fatal("validateManifest accepted unsupported claim")
			}
		})
	}
}

func TestOfflineConformancePassesEveryPackAndCaseDeterministically(t *testing.T) {
	first, err := RunOffline(context.Background(), adapters.DefaultRegistry())
	if err != nil {
		t.Fatalf("RunOffline failed: %v", err)
	}
	second, err := RunOffline(context.Background(), adapters.DefaultRegistry())
	if err != nil {
		t.Fatalf("second RunOffline failed: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("offline report is not deterministic\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
	if first.Summary.Packs != 3 || first.Summary.Cases != 20 ||
		first.Summary.Passed != 20 || first.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", first.Summary)
	}
	if len(first.Packs[0].Fixtures) != 1 ||
		len(first.Packs[1].Fixtures) != 2 ||
		len(first.Packs[2].Fixtures) != 2 {
		t.Fatalf("unexpected endpoint fixture coverage: %+v", first.Packs)
	}
	for _, pack := range first.Packs {
		for _, result := range pack.Cases {
			if !result.Passed {
				t.Fatalf("%s/%s failed: %s", pack.PackID, result.CaseID, result.Message)
			}
		}
	}

	formatted, err := json.MarshalIndent(first, "", "  ")
	if err != nil {
		t.Fatalf("format report: %v", err)
	}
	formatted = append(formatted, '\n')
	golden, err := artifactFS.ReadFile("artifacts/reports/core.v1.json")
	if err != nil {
		t.Fatalf("read golden report: %v", err)
	}
	if !bytes.Equal(formatted, golden) {
		t.Fatalf("golden report drifted\nactual=%s\ngolden=%s", formatted, golden)
	}
}

func TestFindUsesProviderAndAdapterWithoutGuessingCompatibility(t *testing.T) {
	if pack, found, err := Find("azure", adapters.TypeAzureOpenAI); err != nil || !found || pack.Manifest.PackID != "unirouter.azure-openai.core" {
		t.Fatalf("azure pack=%+v found=%t err=%v", pack, found, err)
	}
	if pack, found, err := Find("kimi", adapters.TypeOpenAICompatible); err != nil || found || pack.Manifest.PackID != "" {
		t.Fatalf("compatible vendor inherited an OpenAI claim: pack=%+v found=%t err=%v", pack, found, err)
	}
	if pack, found, err := Find("vendor", "vendor_magic"); err != nil || found || pack.Manifest.PackID != "" {
		t.Fatalf("unknown pack=%+v found=%t err=%v", pack, found, err)
	}
}
