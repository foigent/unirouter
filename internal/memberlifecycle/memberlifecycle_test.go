package memberlifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestFixtureChainRevokesEffectiveAccessAndIsTerminal(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(NewMemoryStore())
	var reports []Report
	for _, event := range fixtures[0].value.Events {
		report, applyErr := service.Apply(mustMarshal(event))
		if applyErr != nil {
			t.Fatalf("%s: %v", event.EventID, applyErr)
		}
		reports = append(reports, report)
	}
	if len(reports[2].EffectiveMemberships) != 0 || len(reports[2].EffectiveCapabilities) != 0 ||
		!reports[2].Controls.DenyAdminSessions || !reports[2].Controls.RevokeLiveAdminSessions {
		t.Fatalf("suspend did not revoke effective access: %#v", reports[2])
	}
	if reports[4].LocalProjection.PersonalKeyHandling != "disabled_before_owner_delete" ||
		reports[4].RuntimeMutation != RuntimeMutationNone || reports[4].VendorAdapterAuthorized {
		t.Fatalf("deprovision boundary=%#v", reports[4])
	}
}

func TestReplayConflictAndCAS(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	first := fixtures[0].value.Events[0]
	service := NewService(NewMemoryStore())
	accepted, err := service.Apply(mustMarshal(first))
	if err != nil {
		t.Fatal(err)
	}
	pretty, _ := json.MarshalIndent(first, "", "  ")
	duplicate, err := service.Apply(pretty)
	if err != nil || duplicate.ReplayStatus != ReplayDuplicateNoop ||
		duplicate.InputSemanticHash != accepted.InputSemanticHash {
		t.Fatalf("duplicate=%#v err=%v", duplicate, err)
	}
	conflict := cloneEvent(first)
	conflict.SourceRef = redactedRef("conflict")
	if _, err := service.Apply(mustMarshal(conflict)); !errors.Is(err, ErrConflict) ||
		errorCode(err) != "event_identity_conflict" {
		t.Fatalf("conflict error=%v", err)
	}
	stale := cloneEvent(fixtures[0].value.Events[1])
	stale.Before.Capabilities[0].Status = "disabled"
	if _, err := service.Apply(mustMarshal(stale)); errorCode(err) != "subject_compare_and_swap_conflict" {
		t.Fatalf("stale error=%v", err)
	}
}

func TestStrictShapeVendorAndPrecisionBoundaries(t *testing.T) {
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	body := mustMarshal(fixtures[0].value.Events[0])
	duplicate := bytes.Replace(body, []byte(`"event_id":`), []byte(`"event_id":"duplicate","event_id":`), 1)
	if _, err := Evaluate(duplicate); errorCode(err) != "duplicate_json_key" {
		t.Fatalf("duplicate error=%v", err)
	}
	vendor := bytes.Replace(body, []byte(`"event_id":`), []byte(`"scim_vendor":"okta","event_id":`), 1)
	if _, err := Evaluate(vendor); errorCode(err) != "input_shape_invalid" {
		t.Fatalf("vendor error=%v", err)
	}
	missing := bytes.Replace(body, []byte(`"source_ref":"sha256:`+strings.Repeat("b", 64)+`",`), nil, 1)
	if _, err := Evaluate(missing); errorCode(err) != "input_shape_invalid" {
		t.Fatalf("missing error=%v", err)
	}
}

func TestConformanceGoldenIsExact(t *testing.T) {
	report, err := RunConformance()
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Fixtures != 1 || report.Summary.Cases != 17 ||
		report.Summary.Passed != 17 || report.Summary.Failed != 0 {
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
		t.Fatalf("golden drift\nactual=%s\ngolden=%s", actual, golden)
	}
}
