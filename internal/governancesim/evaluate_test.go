package governancesim

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestEvaluateDeterministicRouteAndProviderDiff(t *testing.T) {
	request := validRequest()
	body := mustJSON(t, request)
	first, err := Evaluate(body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Evaluate(body)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes := mustJSON(t, first)
	secondBytes := mustJSON(t, second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("report bytes drifted:\n%s\n%s", firstBytes, secondBytes)
	}
	if first.RuntimeMutation != RuntimeMutationNone ||
		first.Release.RuntimePromotionAuthorized ||
		first.Release.PromotionGate != "external_explicit_authority_required" {
		t.Fatalf("runtime boundary failed: %#v", first.Release)
	}
	if first.OverallRisk != RiskReviewRequired || first.Summary.ReviewRequired == 0 {
		t.Fatalf("expected review-required diff: %#v", first)
	}
	if len(first.Baseline.Candidates) != 2 || len(first.Candidate.Candidates) != 1 ||
		first.Candidate.Exclusions[0].Reason != "channel_inactive" {
		t.Fatalf("route simulator did not use expected selection semantics: %#v %#v", first.Baseline, first.Candidate)
	}
	if len(first.ProviderPackChanges) != 1 ||
		first.ProviderPackChanges[0].Risk != RiskReviewRequired ||
		!contains(first.ProviderPackChanges[0].Reasons, "endpoint_added") {
		t.Fatalf("provider firewall did not classify additive change: %#v", first.ProviderPackChanges)
	}
}

func TestProviderChangeFirewallFailsClosedOnSemanticDrift(t *testing.T) {
	for name, mutate := range map[string]func(*SimulationRequest){
		"same-version-drift": func(request *SimulationRequest) {
			request.Candidate.ProviderPacks[0].PackVersion = request.Baseline.ProviderPacks[0].PackVersion
		},
		"endpoint-removal": func(request *SimulationRequest) {
			request.Candidate.ProviderPacks[0].Endpoints = []string{"/v1/embeddings"}
		},
		"fixture-digest-change": func(request *SimulationRequest) {
			request.Candidate.ProviderPacks[0].Fixtures[0].SHA256 = "sha256:" + strings.Repeat("9", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := validRequest()
			mutate(&request)
			report, err := Evaluate(mustJSON(t, request))
			if err != nil {
				t.Fatal(err)
			}
			if report.ProviderPackChanges[0].Risk != RiskBreaking ||
				report.OverallRisk != RiskBreaking ||
				report.Release.Decision != "blocked_breaking_change" {
				t.Fatalf("change did not fail closed: %#v", report)
			}
		})
	}
}

func TestRouteExhaustionIsBreaking(t *testing.T) {
	request := validRequest()
	for index := range request.Candidate.RouteSnapshot.Channels {
		request.Candidate.RouteSnapshot.Channels[index].Status = "disabled"
	}
	report, err := Evaluate(mustJSON(t, request))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Candidate.Candidates) != 0 ||
		report.OverallRisk != RiskBreaking ||
		report.Release.Decision != "blocked_breaking_change" {
		t.Fatalf("route exhaustion was not blocked: %#v", report)
	}
}

func TestStrictJSONAndPrecisionBoundaries(t *testing.T) {
	request := validRequest()
	body := mustJSON(t, request)
	duplicate := bytes.Replace(body, []byte(`"contract_version":"`), []byte(`"contract_version":"unirouter.governance-simulation.v1","contract_version":"`), 1)
	if _, err := Evaluate(duplicate); testErrorCode(err) != "duplicate_json_key" {
		t.Fatalf("duplicate key error=%v", err)
	}
	unknown := bytes.Replace(body, []byte(`"simulation_id":`), []byte(`"credential":"forbidden","simulation_id":`), 1)
	if _, err := Evaluate(unknown); testErrorCode(err) != "input_shape_invalid" {
		t.Fatalf("unknown credential field error=%v", err)
	}
	missingRequired := bytes.Replace(body, []byte(`"max_retry_count":1,`), nil, 1)
	if _, err := Evaluate(missingRequired); testErrorCode(err) != "input_shape_invalid" {
		t.Fatalf("missing required field error=%v", err)
	}
	subMicro := cloneRequestForTest(t, request)
	subMicro.CreatedAt = "2026-07-31T08:00:00.0000001Z"
	if _, err := Evaluate(mustJSON(t, subMicro)); testErrorCode(err) != "created_at_invalid" {
		t.Fatalf("sub-microsecond timestamp error=%v", err)
	}
	unsafeID := cloneRequestForTest(t, request)
	unsafeID.Baseline.RouteSnapshot.Channels[0].ID = "9223372036854775808"
	if _, err := Evaluate(mustJSON(t, unsafeID)); testErrorCode(err) != "route_channel_order_invalid" {
		t.Fatalf("int64 overflow error=%v", err)
	}
}

func TestMemoryServiceDoesNotAdvanceBlockedBreakingChange(t *testing.T) {
	service := NewService(NewMemoryStore())
	blocked := validRequest()
	blocked.SimulationID = "sim-b8-blocked"
	for index := range blocked.Candidate.RouteSnapshot.Channels {
		blocked.Candidate.RouteSnapshot.Channels[index].Status = "disabled"
	}
	report, err := service.Apply(mustJSON(t, blocked))
	if err != nil || report.Release.Decision != "blocked_breaking_change" {
		t.Fatalf("blocked change: %#v %v", report, err)
	}

	allowed := validRequest()
	allowed.SimulationID = "sim-b8-after-blocked"
	if _, err := service.Apply(mustJSON(t, allowed)); err != nil {
		t.Fatalf("blocked evidence advanced offline release state: %v", err)
	}
}

func TestMemoryServiceReplayProgressionRollbackAndCAS(t *testing.T) {
	service := NewService(NewMemoryStore())
	firstRequest := validRequest()
	first, err := service.Apply(mustJSON(t, firstRequest))
	if err != nil || first.ReplayStatus != ReplayAccepted {
		t.Fatalf("first apply: %#v %v", first, err)
	}
	pretty, _ := json.MarshalIndent(firstRequest, "", "  ")
	duplicate, err := service.Apply(pretty)
	if err != nil || duplicate.ReplayStatus != ReplayDuplicateNoop ||
		duplicate.InputPayloadHash != first.InputPayloadHash {
		t.Fatalf("semantic replay mismatch: %#v %v", duplicate, err)
	}
	conflict := cloneRequestForTest(t, firstRequest)
	conflict.SelectionKey = "different"
	if _, err := service.Apply(mustJSON(t, conflict)); !errors.Is(err, ErrConflict) ||
		testErrorCode(err) != "simulation_identity_conflict" {
		t.Fatalf("identity conflict error=%v", err)
	}

	second := nextAdvance(t, firstRequest, "sim-b8-002", StageValidated, StageShadow)
	if _, err := service.Apply(mustJSON(t, second)); err != nil {
		t.Fatalf("validated to shadow: %v", err)
	}
	third := nextAdvance(t, second, "sim-b8-003", StageShadow, StageCanary)
	if _, err := service.Apply(mustJSON(t, third)); err != nil {
		t.Fatalf("shadow to canary: %v", err)
	}
	rollback := cloneRequestForTest(t, third)
	rollback.SimulationID = "sim-b8-004"
	rollback.Release.Revision = "4"
	rollback.Release.ParentRevision = "3"
	rollback.Release.Action = ActionRollback
	rollback.Release.FromStage = StageCanary
	rollback.Release.ToStage = StageShadow
	rollback.Release.RollbackToRevision = "2"
	report, err := service.Apply(mustJSON(t, rollback))
	if err != nil || report.Release.SimulatedStage != StageShadow ||
		report.Release.RuntimeMutation != RuntimeMutationNone {
		t.Fatalf("rollback: %#v %v", report, err)
	}
	stale := nextAdvance(t, third, "sim-b8-005", StageCanary, StageExternalGate)
	stale.Release.Revision = "5"
	stale.Release.ParentRevision = "4"
	if _, err := service.Apply(mustJSON(t, stale)); testErrorCode(err) != "release_compare_and_swap_conflict" {
		t.Fatalf("stale stage error=%v", err)
	}
}

func validRequest() SimulationRequest {
	policy := routingpolicy.Default()
	baselinePack := ProviderPackSnapshot{
		SchemaVersion:   "unirouter.provider-pack.v1",
		SchemaSHA256:    "sha256:f2252611cc0bb84c98ef39a1bba50284aeeb09ec782046f0a15847135896377e",
		PackID:          "unirouter.openai.core",
		PackVersion:     "1.0.0",
		ManifestSHA256:  "sha256:aca97cdb6199acab6cb5107ba4213eec88bb873c415d0085d4c4176c96a0ced9",
		Provider:        "openai",
		AdapterType:     "openai_compatible",
		NetworkVerified: false,
		Endpoints:       []string{"/v1/chat/completions", "/v1/embeddings"},
		Modalities:      []string{"embeddings", "text"},
		AuthProfiles:    []string{"bearer:Authorization"},
		UsageSignature:  "authority=provider_reported;missing=missing_not_zero;cache=",
		ErrorSignature:  "classifier=unirouter.http-failure.v1;unknown=unknown_not_retryable_without_status_evidence",
		Fixtures: []FixtureSnapshot{
			{FixtureID: "openai-chat-success.v1", SHA256: "sha256:6b95c320fb75b9e74e30dc2a5e07a282ad83e20329a19e4271d8fb79ac87a861"},
			{FixtureID: "openai-embeddings-success.v1", SHA256: "sha256:5ba8db14c310d1ca60e953dc27806e2b71ea5dcaee3210b3ecd5849dc9d6b150"},
		},
	}
	candidatePack := baselinePack
	candidatePack.PackVersion = "1.1.0"
	candidatePack.ManifestSHA256 = "sha256:" + strings.Repeat("1", 64)
	candidatePack.Endpoints = []string{"/v1/chat/completions", "/v1/embeddings", "/v1/responses"}
	candidatePack.Fixtures = append(append([]FixtureSnapshot(nil), baselinePack.Fixtures...),
		FixtureSnapshot{FixtureID: "openai-responses-text.v1", SHA256: "sha256:" + strings.Repeat("2", 64)})

	baselineRoute := RouteSnapshot{
		SnapshotID: "route-baseline-v1",
		Model:      "gpt-4.1",
		DBNow:      "2026-07-31T08:00:00.000000Z",
		Source:     "fixture",
		Channels: []ChannelSnapshot{
			{ID: "1", Name: "primary", Provider: "openai", Priority: 10, Weight: 100, Status: "active", CircuitState: "healthy", Keys: []KeySnapshot{{ID: "11", Status: "active"}}},
			{ID: "2", Name: "secondary", Provider: "azure", Priority: 20, Weight: 100, Status: "active", CircuitState: "healthy", Keys: []KeySnapshot{{ID: "22", Status: "active"}}},
		},
	}
	candidateRoute := baselineRoute
	candidateRoute.SnapshotID = "route-candidate-v1"
	candidateRoute.Channels = append([]ChannelSnapshot(nil), baselineRoute.Channels...)
	candidateRoute.Channels[0].Status = "disabled"
	return SimulationRequest{
		ContractVersion: ContractVersion,
		SimulationID:    "sim-b8-001",
		CreatedAt:       "2026-07-31T08:00:00.000000Z",
		SelectionKey:    "fixture-request-001",
		EvaluationMode:  ModeShadow,
		Baseline: Bundle{
			Policy: policy, RouteSnapshot: baselineRoute,
			ProviderPacks: []ProviderPackSnapshot{baselinePack},
		},
		Candidate: Bundle{
			Policy: policy, RouteSnapshot: candidateRoute,
			ProviderPacks: []ProviderPackSnapshot{candidatePack},
		},
		Release: ReleaseIntent{
			ContractVersion: ReleaseVersion,
			ReleaseID:       "release-b8-001",
			Revision:        "1",
			ParentRevision:  "0",
			Action:          ActionAdvance,
			FromStage:       StageDraft,
			ToStage:         StageValidated,
			ActorRef:        "sha256:" + strings.Repeat("a", 64),
			OccurredAt:      "2026-07-31T08:00:00.000000Z",
			Reason:          "offline fixture validation",
		},
	}
}

func nextAdvance(t *testing.T, previous SimulationRequest, simulationID, from, to string) SimulationRequest {
	t.Helper()
	next := cloneRequestForTest(t, previous)
	next.SimulationID = simulationID
	next.Release.Revision = nextRevision(previous.Release.Revision)
	next.Release.ParentRevision = previous.Release.Revision
	next.Release.Action = ActionAdvance
	next.Release.FromStage = from
	next.Release.ToStage = to
	next.Release.RollbackToRevision = ""
	return next
}

func cloneRequestForTest(t *testing.T, request SimulationRequest) SimulationRequest {
	t.Helper()
	body := mustJSON(t, request)
	var cloned SimulationRequest
	if err := json.Unmarshal(body, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func testErrorCode(err error) string {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	return ""
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
