package governancesim

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestConformanceArtifactsAndGoldenReportAreExact(t *testing.T) {
	report, err := RunConformance()
	if err != nil {
		t.Fatalf("RunConformance failed: %v", err)
	}
	if report.Summary.Fixtures != 1 ||
		report.Summary.Cases != 17 ||
		report.Summary.Passed != report.Summary.Cases ||
		report.Summary.Failed != 0 {
		t.Fatalf("summary=%+v cases=%+v", report.Summary, report.Cases)
	}
	actual, err := MarshalConformanceReport(report)
	if err != nil {
		t.Fatalf("MarshalConformanceReport failed: %v", err)
	}
	golden, err := GoldenReportBytes()
	if err != nil {
		t.Fatalf("GoldenReportBytes failed: %v", err)
	}
	if !bytes.Equal(actual, golden) {
		t.Fatalf("golden report drift\nactual=%s\ngolden=%s", actual, golden)
	}
}

func TestConformanceArtifactsAreStrictJSON(t *testing.T) {
	schema, fixtures, report, err := ArtifactDigests()
	if err != nil {
		t.Fatalf("ArtifactDigests failed: %v", err)
	}
	if schema.SHA256 == "" || report.SHA256 == "" || len(fixtures) != 1 {
		t.Fatalf("identities schema=%+v fixtures=%+v report=%+v", schema, fixtures, report)
	}
	for _, identity := range append([]ArtifactIdentity{schema, report}, fixtures...) {
		body, readErr := conformanceArtifacts.ReadFile("artifacts/" + identity.Path)
		if readErr != nil {
			t.Fatalf("read %s: %v", identity.Path, readErr)
		}
		var decoded any
		if decodeErr := json.Unmarshal(body, &decoded); decodeErr != nil {
			t.Fatalf("parse %s: %v", identity.Path, decodeErr)
		}
	}
}
