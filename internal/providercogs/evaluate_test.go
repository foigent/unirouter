package providercogs

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

const (
	testSourceInstance = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testOrgRef         = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testProjectRef     = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestEvaluateSeparatesSupplierCostFromCustomerRevenue(t *testing.T) {
	body := mustImportBody(t, closedFixture())
	report, err := Evaluate(body)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if report.CustomerLedgerMutation != CustomerLedgerMutationNone ||
		report.ReplayStatus != ReplayAccepted {
		t.Fatalf("unsafe report boundary: %+v", report)
	}
	if report.Summary.Pages != 2 ||
		report.Summary.UniqueSupplierLines != 3 ||
		report.Summary.DuplicateNoopLines != 1 ||
		report.Summary.SourceAttempts != 3 ||
		report.Summary.ExactRequestMatches != 1 ||
		report.Summary.AggregateUnattributed != 1 ||
		report.Summary.SupplierOnly != 1 ||
		report.Summary.SourceOnly != 1 ||
		report.Summary.SupplierEvidencePending != 1 ||
		report.Summary.SupplierTotalMinor != "470" ||
		report.Summary.MatchedSupplierMinor != "120" ||
		report.Summary.UnattributedMinor != "350" {
		t.Fatalf("summary=%+v", report.Summary)
	}
	exact := findMatch(t, report, StatusExactRequestMatched, "line-request")
	if exact.ProviderCost == nil || exact.ProviderCost.ValueMinor != "120" ||
		exact.ProviderEstimate == nil || exact.ProviderEstimate.ValueMinor != "100" ||
		exact.ProviderVariance != "20" ||
		exact.CustomerRevenue == nil || exact.CustomerRevenue.ValueMinor != "5000" ||
		exact.CustomerDebtAction != "none" {
		t.Fatalf("exact match=%+v", exact)
	}
	aggregate := findMatch(t, report, StatusAggregateUnattributed, "line-daily")
	if aggregate.RequestID != "" || aggregate.CustomerRevenue != nil ||
		aggregate.CustomerDebtAction != "none" {
		t.Fatalf("aggregate cost was fabricated into customer attribution: %+v", aggregate)
	}
	if !hashRefPattern.MatchString(report.ImportPayloadHash) ||
		!hashRefPattern.MatchString(report.ImportSemanticHash) ||
		!hashRefPattern.MatchString(report.SourceSnapshotHash) {
		t.Fatalf(
			"report hashes=%s/%s/%s",
			report.ImportPayloadHash,
			report.ImportSemanticHash,
			report.SourceSnapshotHash,
		)
	}
}

func TestEvaluateIsDeterministicAndExactDecimal(t *testing.T) {
	body := mustImportBody(t, closedFixture())
	first, err := Evaluate(body)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	second, err := Evaluate(body)
	if err != nil {
		t.Fatalf("second Evaluate failed: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("report drift\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
	request := closedFixture()
	request.Pages[0].Lines[0].AmountMinor = "9223372036854775807"
	request.Pages[1].Lines[0].AmountMinor = "9223372036854775807"
	request.Pages[1].Lines[1].AmountMinor = "9223372036854775807"
	request.Pages[1].Lines = request.Pages[1].Lines[:2]
	if _, err := Evaluate(mustImportBody(t, request)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sum overflow error=%v", err)
	}
	request = closedFixture()
	request.Pages[0].Lines[0].AmountMinor = "1.0"
	request.Pages[1].Lines[0].AmountMinor = "1.0"
	if _, err := Evaluate(mustImportBody(t, request)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-integer amount error=%v", err)
	}
}

func TestMemoryImportReplaysAsNoopAndConflictsOnByteDrift(t *testing.T) {
	service := NewService(NewMemoryStore())
	body := mustImportBody(t, closedFixture())
	first, err := service.Import(body)
	if err != nil || first.ReplayStatus != ReplayAccepted {
		t.Fatalf("first import=%+v err=%v", first, err)
	}
	second, err := service.Import(body)
	if err != nil || second.ReplayStatus != ReplayDuplicateNoop {
		t.Fatalf("duplicate import=%+v err=%v", second, err)
	}
	compacted, err := json.Marshal(closedFixture())
	if err != nil {
		t.Fatalf("marshal compacted import: %v", err)
	}
	third, err := service.Import(compacted)
	if err != nil || third.ReplayStatus != ReplayDuplicateNoop ||
		third.ImportPayloadHash != first.ImportPayloadHash {
		t.Fatalf("semantic duplicate import=%+v err=%v", third, err)
	}
	mutated := closedFixture()
	mutated.Pages[0].Lines[0].AmountMinor = "121"
	if _, err := service.Import(mustImportBody(t, mutated)); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay error=%v", err)
	}
	fourth, err := service.Import(body)
	if err != nil || fourth.ReplayStatus != ReplayDuplicateNoop ||
		fourth.Summary.SupplierTotalMinor != first.Summary.SupplierTotalMinor {
		t.Fatalf("conflict mutated stored evidence: %+v err=%v", fourth, err)
	}
}

func TestLineDuplicateNoopAndConflictAreDistinct(t *testing.T) {
	request := closedFixture()
	request.Pages[1].Lines[1] = request.Pages[0].Lines[0]
	if report, err := Evaluate(mustImportBody(t, request)); err != nil ||
		report.Summary.DuplicateNoopLines != 2 {
		t.Fatalf("duplicate noop report=%+v err=%v", report, err)
	}
	request = closedFixture()
	request.Pages[1].Lines[1].LineID = request.Pages[0].Lines[0].LineID
	if _, err := Evaluate(mustImportBody(t, request)); !errors.Is(err, ErrConflict) {
		t.Fatalf("line conflict error=%v", err)
	}
}

func TestAmbiguousAndProjectMismatchedEvidenceNeverAutoAttributes(t *testing.T) {
	request := closedFixture()
	duplicate := request.SourceSnapshot.Attempts[0]
	duplicate.RequestID = "req-duplicate"
	duplicate.AttemptID = "attempt-duplicate"
	request.SourceSnapshot.Attempts = append(request.SourceSnapshot.Attempts, duplicate)
	report, err := Evaluate(mustImportBody(t, request))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	match := findMatch(t, report, StatusAmbiguous, "line-request")
	if match.RequestID != "" || match.CustomerRevenue != nil {
		t.Fatalf("ambiguous supplier line was attributed: %+v", match)
	}

	request = closedFixture()
	request.Pages[0].Lines[0].ProjectRef =
		"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	request.Pages[1].Lines[0].ProjectRef =
		"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	report, err = Evaluate(mustImportBody(t, request))
	if err != nil {
		t.Fatalf("Evaluate project mismatch failed: %v", err)
	}
	match = findMatch(t, report, StatusSupplierOnly, "line-request")
	if match.RequestID != "" || match.CustomerRevenue != nil {
		t.Fatalf("cross-project supplier line was attributed: %+v", match)
	}
}

func TestProvisionalCoverageRequiresDeclaredDelayAndDoesNotClaimMissing(t *testing.T) {
	request := closedFixture()
	request.Coverage.State = CoverageProvisional
	request.Coverage.Gaps = []Gap{{
		Code:       GapProviderDataDelayed,
		Provenance: "declared",
		Detail:     "Provider reports may arrive after the preview window.",
	}}
	report, err := Evaluate(mustImportBody(t, request))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if report.Summary.SourceOnly != 0 || report.Summary.SupplierEvidencePending != 2 {
		t.Fatalf("provisional coverage overclaimed missing evidence: %+v", report.Summary)
	}
	request.Coverage.Gaps = []Gap{}
	if _, err := Evaluate(mustImportBody(t, request)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing delay gap error=%v", err)
	}
}

func TestInvalidCursorTimeIdentityAndNestedDuplicateFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ImportRequest)
	}{
		{
			name: "cursor chain",
			mutate: func(request *ImportRequest) {
				request.Pages[1].CursorIn = "wrong"
			},
		},
		{
			name: "noncanonical time",
			mutate: func(request *ImportRequest) {
				request.Coverage.StartAt = "2026-07-31T00:00:00Z"
			},
		},
		{
			name: "source instance",
			mutate: func(request *ImportRequest) {
				request.SourceSnapshot.Attempts[0].SourceInstance =
					"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
			},
		},
		{
			name: "customer currency",
			mutate: func(request *ImportRequest) {
				request.SourceSnapshot.Attempts[0].CustomerCurrency = "USD"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := closedFixture()
			test.mutate(&request)
			if _, err := Evaluate(mustImportBody(t, request)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	body := mustImportBody(t, closedFixture())
	body = []byte(strings.Replace(
		string(body),
		`"contract_version": "unirouter.provider-cogs-offline.v1"`,
		`"contract_version": "unirouter.provider-cogs-offline.v1", "contract_version": "duplicate"`,
		1,
	))
	if _, err := Evaluate(body); !errors.Is(err, ErrInvalid) ||
		safeErrorCode(err) != "duplicate_json_key" {
		t.Fatalf("nested duplicate error=%v code=%s", err, safeErrorCode(err))
	}
	body = mustImportBody(t, closedFixture())
	body = []byte(strings.Replace(string(body), `"period_start": "",`, "", 1))
	if _, err := Evaluate(body); !errors.Is(err, ErrInvalid) ||
		safeErrorCode(err) != "import_shape_invalid" {
		t.Fatalf("missing explicit field error=%v code=%s", err, safeErrorCode(err))
	}
}

func TestErrorsDoNotLeakSupplierOrCustomerEvidence(t *testing.T) {
	request := closedFixture()
	request.Pages[0].Lines[0].AmountMinor = "private-supplier-amount"
	body := mustImportBody(t, request)
	_, err := Evaluate(body)
	if err == nil {
		t.Fatal("invalid amount accepted")
	}
	for _, sensitive := range []string{"private-supplier-amount", "upstream-one", "req-one"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("error leaked %q: %v", sensitive, err)
		}
	}
}

func TestSignedArithmeticBoundaries(t *testing.T) {
	if value, err := checkedSubtract(math.MinInt64, math.MinInt64); err != nil || value != 0 {
		t.Fatalf("min-min=%d err=%v", value, err)
	}
	if _, err := checkedSubtract(0, math.MinInt64); err == nil {
		t.Fatal("expected subtraction overflow")
	}
	if _, err := parseSigned("-0"); err == nil {
		t.Fatal("-0 must not be canonical")
	}
}

func closedFixture() ImportRequest {
	requestLine := Line{
		LineID:            "line-request",
		Revision:          "1",
		LineKind:          LineCharge,
		Granularity:       GranularityRequest,
		EvidenceLevel:     EvidenceExactRequestUsage,
		ObservedAt:        "2026-07-31T00:10:00.000000Z",
		ProjectRef:        testProjectRef,
		UpstreamRequestID: "upstream-one",
		LineItem:          "Text model usage",
		AmountMinor:       "120",
		Finality:          FinalityFinal,
	}
	return ImportRequest{
		ContractVersion: ContractVersion,
		ImportID:        "import-20260731-001",
		Provider:        "openai",
		SourceInstance:  testSourceInstance,
		Currency:        "USD",
		AmountScale:     "1000000",
		Coverage: Coverage{
			StartAt: "2026-07-31T00:00:00.000000Z",
			EndAt:   "2026-08-01T00:00:00.000000Z",
			State:   CoverageClosed,
			Gaps: []Gap{{
				Code:       GapRequestAttributionUnavailable,
				Provenance: "declared",
				Detail:     "Aggregate supplier rows cannot be split into request costs.",
			}},
		},
		Pages: []Page{
			{
				PageID:    "page-0",
				PageIndex: "0",
				CursorIn:  "",
				CursorOut: "cursor-1",
				IsLast:    false,
				Lines:     []Line{requestLine},
			},
			{
				PageID:    "page-1",
				PageIndex: "1",
				CursorIn:  "cursor-1",
				CursorOut: "",
				IsLast:    true,
				Lines: []Line{
					requestLine,
					{
						LineID:        "line-daily",
						Revision:      "1",
						LineKind:      LineCharge,
						Granularity:   GranularityProjectBucket,
						EvidenceLevel: EvidenceAggregateMatched,
						ObservedAt:    "2026-08-01T01:00:00.000000Z",
						PeriodStart:   "2026-07-31T00:00:00.000000Z",
						PeriodEnd:     "2026-08-01T00:00:00.000000Z",
						ProjectRef:    testProjectRef,
						LineItem:      "Daily project costs",
						AmountMinor:   "300",
						Finality:      FinalityFinal,
					},
					{
						LineID:            "line-supplier-only",
						Revision:          "1",
						LineKind:          LineCharge,
						Granularity:       GranularityRequest,
						EvidenceLevel:     EvidenceExactRequestUsage,
						ObservedAt:        "2026-07-31T00:30:00.000000Z",
						ProjectRef:        testProjectRef,
						UpstreamRequestID: "upstream-missing",
						LineItem:          "Unmatched request cost",
						AmountMinor:       "50",
						Finality:          FinalityFinal,
					},
				},
			},
		},
		SourceSnapshot: SourceSnapshot{
			SchemaVersion: SourceSnapshotVersion,
			SnapshotID:    "snapshot-20260731-001",
			CreatedAt:     "2026-08-01T01:30:00.000000Z",
			Attempts: []Attempt{
				attemptFixture("req-one", "attempt-one", "upstream-one", "2026-07-31T00:09:00.000000Z", "100", "5000"),
				attemptFixture("req-source-only", "attempt-source-only", "upstream-source-only", "2026-07-31T10:00:00.000000Z", "80", "4000"),
				attemptFixture("req-future", "attempt-future", "upstream-future", "2026-08-02T00:00:00.000000Z", "60", "3000"),
			},
		},
	}
}

func attemptFixture(requestID, attemptID, upstreamID, occurredAt, estimate, revenue string) Attempt {
	return Attempt{
		OrgRef:                testOrgRef,
		ProjectRef:            testProjectRef,
		RequestID:             requestID,
		AttemptID:             attemptID,
		Provider:              "openai",
		SourceInstance:        testSourceInstance,
		UpstreamRequestID:     upstreamID,
		OccurredAt:            occurredAt,
		ProviderEstimateMinor: estimate,
		ProviderCurrency:      "USD",
		ProviderAmountScale:   "1000000",
		CustomerRevenueMicros: revenue,
		CustomerCurrency:      "CNY",
		CustomerAmountScale:   "1000000",
	}
}

func mustImportBody(t *testing.T, request ImportRequest) []byte {
	t.Helper()
	body, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatalf("marshal import: %v", err)
	}
	return body
}

func findMatch(t *testing.T, report Report, status, lineID string) Match {
	t.Helper()
	for _, match := range report.Matches {
		if match.Status == status && match.LineID == lineID {
			return match
		}
	}
	t.Fatalf("match status=%s line=%s not found: %+v", status, lineID, report.Matches)
	return Match{}
}
