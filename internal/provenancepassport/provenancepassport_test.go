package provenancepassport

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

func TestEvaluateBuildsExactPassportWithoutRuntimeMutation(t *testing.T) {
	body, err := SampleRequestBytes()
	if err != nil {
		t.Fatalf("sample request: %v", err)
	}
	report, err := Evaluate(body)
	if err != nil {
		t.Fatalf("evaluate passport: %v", err)
	}
	if report.Source.SourceHash != "sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27" ||
		report.Source.EnvelopeHash != "sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2" ||
		report.Source.IdempotencyKey != "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e" {
		t.Fatalf("reference identity drifted: %#v", report.Source)
	}
	if len(report.FieldGroups.Direct) != 9 ||
		len(report.FieldGroups.Derived) != 2 ||
		len(report.FieldGroups.Declared) != 2 ||
		len(report.FieldGroups.Missing) != 4 ||
		len(report.CaptureGaps) != 4 {
		t.Fatalf("passport coverage drifted: groups=%#v gaps=%d", report.FieldGroups, len(report.CaptureGaps))
	}
	if report.RuntimeBoundary.RuntimeMutation != RuntimeMutationNone ||
		report.RuntimeBoundary.SecondConnectorRegistered ||
		report.RuntimeBoundary.SecondConnectorRuntimeOwned {
		t.Fatalf("runtime boundary drifted: %#v", report.RuntimeBoundary)
	}
}

func TestFixtureContainsExactCurrentSourceBytesAndProjectionSeed(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(fixtures) != 1 {
		t.Fatalf("fixtures=%d want=1", len(fixtures))
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(fixtures[0].value.Request.SourceSnapshotBase64)
	if err != nil {
		t.Fatalf("decode fixture source: %v", err)
	}
	current, err := os.ReadFile(filepath.Join(
		"..", "..", "contracts", "completed-execution", "v1", "fixtures", "valid-settled.json",
	))
	if err != nil {
		t.Fatalf("read current Source fixture: %v", err)
	}
	if !bytes.Equal(decoded, current) {
		t.Fatal("passport fixture does not preserve the exact current Source fixture bytes")
	}
	if got := eventconnector.HashSourceBytes(current); got != fixtures[0].value.Expected.SourceHash {
		t.Fatalf("Source fixture hash=%s want=%s", got, fixtures[0].value.Expected.SourceHash)
	}
	otlp, err := os.ReadFile(filepath.Join(
		"..", "telemetryexport", "testdata", "chat-provider-otlp.json",
	))
	if err != nil {
		t.Fatalf("read OpenInference golden: %v", err)
	}
	if got := eventconnector.SHA256Reference(otlp); got != OpenInferenceGoldenDigest {
		t.Fatalf("OpenInference golden digest=%s want=%s", got, OpenInferenceGoldenDigest)
	}
}

func TestPinnedProtocolAndSecondConnectorEvidenceRemainExact(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "contracts", "foigent", "event-connector-protocol.pin.json",
	))
	if err != nil {
		t.Fatalf("read product protocol pin: %v", err)
	}
	var pin struct {
		ProtocolVersion string `json:"protocol_version"`
		ReleaseDigest   string `json:"release_digest"`
		Authority       struct {
			Tag string `json:"tag"`
		} `json:"authority"`
		Components map[string]string `json:"components"`
	}
	if err := json.Unmarshal(payload, &pin); err != nil {
		t.Fatalf("decode product protocol pin: %v", err)
	}
	expected := map[string]string{
		"source_projection_contract":    ReferenceMappingDigest,
		"reference_connector_manifest":  "sha256:a3bc97bd5d5c7aab6452de9518be0b00f62776877deb431a91c175da5287de79",
		"v1_to_v2_compatibility_matrix": "sha256:bdc2527ca947bf7b6a9c1feb2d5a97a9426b5b3ec602690a2d7b23d3f85ace52",
		"conformance_contract":          "sha256:f820936448590144156c2e499134eb4b3142ca0265275b6e9bdf1e5cd7855e14",
		"vector_bundle":                 "sha256:9e50bb4570dcc1ff07157ad6198641e937357b641ae052cddcb3420b52ad11ed",
	}
	if pin.ProtocolVersion != ProductProtocolVersion ||
		pin.Authority.Tag != "v"+ProductProtocolVersion ||
		pin.ReleaseDigest != ProductProtocolDigest {
		t.Fatalf("protocol identity drifted: %#v", pin)
	}
	for name, want := range expected {
		if got := pin.Components[name]; got != want {
			t.Fatalf("protocol component %s=%s want=%s", name, got, want)
		}
	}
}

func TestProductRuntimeRegistryStillContainsOnlyReferenceConnector(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "contracts", "foigent", "connector-registry.pin.v1.json",
	))
	if err != nil {
		t.Fatalf("read runtime registry pin: %v", err)
	}
	registry, connectorErr := eventconnector.DecodeRegistryPin(payload)
	if connectorErr != nil {
		t.Fatalf("decode runtime registry pin: %v", connectorErr)
	}
	ids := registry.ConnectorIDs()
	if len(ids) != 1 || ids[0] != "unirouter.completedfeed.reference-v1" {
		t.Fatalf("runtime registry connector IDs=%v", ids)
	}
	encoded := string(payload)
	if strings.Contains(encoded, LiteLLMConnectorID) {
		t.Fatal("conformance-only LiteLLM Connector leaked into the product runtime registry")
	}
	workflow, err := os.ReadFile(filepath.Join(
		"..", "..", ".github", "workflows", "foigent-protocol-conformance.yml",
	))
	if err != nil {
		t.Fatalf("read protocol conformance workflow: %v", err)
	}
	// This licensed CI carrier adds only community/license files to the original
	// authority source. Runtime provenance keeps ProductProtocolCommit unchanged.
	const conformanceActionCommit = "e01527350ab38d02c2bfdefe090ff72d6e48e749"
	if !bytes.Contains(
		workflow,
		[]byte("uses: foigent/event-connector-spec/actions/source-neutral-v2@"+conformanceActionCommit),
	) {
		t.Fatal("product CI no longer pins the exact licensed conformance carrier commit")
	}
}

func TestConformanceAndGoldenReportAreByteExact(t *testing.T) {
	report, err := RunConformance()
	if err != nil {
		t.Fatalf("run conformance: %v", err)
	}
	if report.Summary.Failed != 0 || report.Summary.Passed != report.Summary.Cases {
		t.Fatalf("conformance failed: %#v", report.Summary)
	}
	actual, err := MarshalConformanceReport(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	golden, err := GoldenReportBytes()
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(actual, golden) {
		t.Fatalf("conformance report differs from golden\nactual=%s\nwant=%s", actual, golden)
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
	if len(service.store.passports) != 1 {
		t.Fatalf("stored passports=%d want=1", len(service.store.passports))
	}
}
