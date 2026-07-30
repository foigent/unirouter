package governancesim

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

var (
	portableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	packIDPattern     = regexp.MustCompile(`^unirouter\.[a-z0-9.-]+$`)
	providerPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	versionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	unsignedPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	utcMicrosPattern  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$`)
)

var stageOrder = []string{
	StageDraft,
	StageValidated,
	StageShadow,
	StageCanary,
	StageExternalGate,
}

// Evaluate performs a zero-persistence, zero-network governance simulation.
func Evaluate(body []byte) (Report, error) {
	request, err := decodeRequest(body)
	if err != nil {
		return Report{}, err
	}
	return evaluate(body, request)
}

func decodeRequest(body []byte) (SimulationRequest, error) {
	if len(body) == 0 || len(body) > MaxInputBytes {
		return SimulationRequest{}, contractError(ErrInvalid, "input_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return SimulationRequest{}, contractError(ErrInvalid, "duplicate_json_key")
		}
		return SimulationRequest{}, contractError(ErrInvalid, "input_json_invalid")
	}
	var request SimulationRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return SimulationRequest{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return SimulationRequest{}, contractError(ErrInvalid, "trailing_json")
	}
	if !hasExactJSONShape(body, request) {
		return SimulationRequest{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	if err := validateRequest(request); err != nil {
		return SimulationRequest{}, err
	}
	return request, nil
}

func evaluate(body []byte, request SimulationRequest) (Report, error) {
	semanticHash, err := canonicalHash(request)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "canonicalization_failed")
	}
	baseline, err := evaluateBundle(request.Baseline, request.SelectionKey)
	if err != nil {
		return Report{}, err
	}
	candidate, err := evaluateBundle(request.Candidate, request.SelectionKey)
	if err != nil {
		return Report{}, err
	}
	routeChanges := compareRoutes(request.Baseline, baseline, request.Candidate, candidate)
	packChanges := compareProviderPacks(request.Baseline.ProviderPacks, request.Candidate.ProviderPacks)
	summary, overall := summarizeChanges(routeChanges, packChanges)
	release := releaseReceipt(request, semanticHash, overall)
	return Report{
		ReportVersion:       ReportVersion,
		ContractVersion:     ContractVersion,
		SimulationID:        request.SimulationID,
		InputPayloadHash:    hash(body),
		InputSemanticHash:   semanticHash,
		EvaluationMode:      request.EvaluationMode,
		Baseline:            baseline,
		Candidate:           candidate,
		RouteChanges:        routeChanges,
		ProviderPackChanges: packChanges,
		Summary:             summary,
		OverallRisk:         overall,
		Release:             release,
		ReplayStatus:        ReplayAccepted,
		RuntimeMutation:     RuntimeMutationNone,
	}, nil
}

func evaluateBundle(bundle Bundle, selectionKey string) (RouteOutcome, error) {
	dbNow, _ := parseUTCMicros(bundle.RouteSnapshot.DBNow)
	selection := routingpolicy.SelectionSnapshot{
		Model:    bundle.RouteSnapshot.Model,
		DBNow:    dbNow,
		Version:  bundle.RouteSnapshot.SnapshotID,
		Source:   bundle.RouteSnapshot.Source,
		Degraded: bundle.RouteSnapshot.Degraded,
		Channels: make([]routingpolicy.SelectionChannel, 0, len(bundle.RouteSnapshot.Channels)),
	}
	for _, channel := range bundle.RouteSnapshot.Channels {
		channelID, _ := parsePositiveInt64(channel.ID)
		var halfOpenUntil *time.Time
		if channel.HalfOpenUntil != "" {
			parsed, _ := parseUTCMicros(channel.HalfOpenUntil)
			halfOpenUntil = &parsed
		}
		keys := make([]routingpolicy.SelectionKey, 0, len(channel.Keys))
		for _, key := range channel.Keys {
			keyID, _ := parsePositiveInt64(key.ID)
			var cooldownUntil *time.Time
			if key.CooldownUntil != "" {
				parsed, _ := parseUTCMicros(key.CooldownUntil)
				cooldownUntil = &parsed
			}
			keys = append(keys, routingpolicy.SelectionKey{
				ID:            keyID,
				Status:        key.Status,
				CooldownUntil: cooldownUntil,
				AllowedModels: key.AllowedModels,
			})
		}
		selection.Channels = append(selection.Channels, routingpolicy.SelectionChannel{
			ID:            channelID,
			Name:          channel.Name,
			Provider:      channel.Provider,
			Priority:      channel.Priority,
			Weight:        channel.Weight,
			Status:        channel.Status,
			CircuitState:  channel.CircuitState,
			HalfOpenUntil: halfOpenUntil,
			Keys:          keys,
		})
	}
	result := routingpolicy.EvaluateSelection(selection, bundle.Policy, selectionKey)
	snapshotHash, err := canonicalHash(bundle.RouteSnapshot)
	if err != nil {
		return RouteOutcome{}, contractError(ErrInvalid, "snapshot_canonicalization_failed")
	}
	outcome := RouteOutcome{
		SnapshotID:       bundle.RouteSnapshot.SnapshotID,
		SnapshotHash:     snapshotHash,
		PolicyVersion:    routingpolicy.Version,
		SelectionVersion: routingpolicy.SelectionVersion,
		Candidates:       make([]RouteCandidate, 0, len(result.Selected)),
		Exclusions:       make([]RouteCandidate, 0, len(result.Exclusions)),
		ExcludedCount:    result.ExcludedCount,
		ExclusionReasons: result.ExclusionReasons,
	}
	for _, item := range result.Selected {
		outcome.Candidates = append(outcome.Candidates, RouteCandidate{
			ChannelID:        strconv.FormatInt(item.Preview.ChannelID, 10),
			ChannelName:      item.Preview.ChannelName,
			Provider:         item.Preview.Provider,
			KeyID:            strconv.FormatInt(item.KeyID, 10),
			Priority:         item.Preview.Priority,
			Weight:           item.Preview.Weight,
			Order:            item.Preview.Order,
			CircuitState:     item.Preview.CircuitState,
			EligibleKeyCount: item.Preview.EligibleKeyCount,
			Reason:           item.Preview.Reason,
			RequiresProbe:    item.Preview.RequiresProbe,
		})
	}
	for _, item := range result.Exclusions {
		outcome.Exclusions = append(outcome.Exclusions, RouteCandidate{
			ChannelID:        strconv.FormatInt(item.ChannelID, 10),
			ChannelName:      item.ChannelName,
			Provider:         item.Provider,
			Priority:         item.Priority,
			Weight:           item.Weight,
			Order:            item.Order,
			CircuitState:     item.CircuitState,
			EligibleKeyCount: item.EligibleKeyCount,
			Reason:           item.Reason,
			RequiresProbe:    item.RequiresProbe,
		})
	}
	return outcome, nil
}

func compareRoutes(
	baselineBundle Bundle,
	baseline RouteOutcome,
	candidateBundle Bundle,
	candidate RouteOutcome,
) []RouteChange {
	changes := make([]RouteChange, 0, 4)
	baselinePolicy, _ := canonicalHash(baselineBundle.Policy)
	candidatePolicy, _ := canonicalHash(candidateBundle.Policy)
	if baselinePolicy != candidatePolicy {
		changes = append(changes, RouteChange{
			Field: "policy/config", Baseline: baselinePolicy, Candidate: candidatePolicy,
			Risk: RiskReviewRequired, Reason: "selection_policy_changed",
		})
	}
	if baseline.SnapshotHash != candidate.SnapshotHash {
		changes = append(changes, RouteChange{
			Field: "route/snapshot", Baseline: baseline.SnapshotHash, Candidate: candidate.SnapshotHash,
			Risk: RiskReviewRequired, Reason: "credential_free_route_snapshot_changed",
		})
	}
	baselineOrder := routeOrder(baseline.Candidates)
	candidateOrder := routeOrder(candidate.Candidates)
	if baselineOrder != candidateOrder {
		risk, reason := RiskReviewRequired, "deterministic_candidate_order_changed"
		if len(baseline.Candidates) > 0 && len(candidate.Candidates) == 0 {
			risk, reason = RiskBreaking, "candidate_routes_exhausted"
		}
		changes = append(changes, RouteChange{
			Field: "route/candidate_order", Baseline: baselineOrder, Candidate: candidateOrder,
			Risk: risk, Reason: reason,
		})
	}
	baselineExclusions := exclusionOrder(baseline.Exclusions)
	candidateExclusions := exclusionOrder(candidate.Exclusions)
	if baselineExclusions != candidateExclusions {
		changes = append(changes, RouteChange{
			Field: "route/exclusions", Baseline: baselineExclusions, Candidate: candidateExclusions,
			Risk: RiskReviewRequired, Reason: "eligibility_explanations_changed",
		})
	}
	return changes
}

func compareProviderPacks(baseline, candidate []ProviderPackSnapshot) []ProviderPackChange {
	baselineByID := make(map[string]ProviderPackSnapshot, len(baseline))
	candidateByID := make(map[string]ProviderPackSnapshot, len(candidate))
	ids := make([]string, 0, len(baseline)+len(candidate))
	for _, pack := range baseline {
		baselineByID[pack.PackID] = pack
		ids = append(ids, pack.PackID)
	}
	for _, pack := range candidate {
		candidateByID[pack.PackID] = pack
		if _, exists := baselineByID[pack.PackID]; !exists {
			ids = append(ids, pack.PackID)
		}
	}
	sort.Strings(ids)
	changes := make([]ProviderPackChange, 0, len(ids))
	for _, id := range ids {
		before, beforeExists := baselineByID[id]
		after, afterExists := candidateByID[id]
		change := ProviderPackChange{PackID: id, Risk: RiskUnchanged}
		if beforeExists {
			change.BaselineVersion = before.PackVersion
		}
		if afterExists {
			change.CandidateVersion = after.PackVersion
		}
		switch {
		case !beforeExists:
			change.Risk = RiskReviewRequired
			change.Reasons = []string{"pack_added_requires_independent_conformance"}
		case !afterExists:
			change.Risk = RiskBreaking
			change.Reasons = []string{"pack_removed"}
		default:
			change.Risk, change.Reasons = comparePack(before, after)
		}
		changes = append(changes, change)
	}
	return changes
}

func comparePack(before, after ProviderPackSnapshot) (string, []string) {
	beforeHash, _ := canonicalHash(before)
	afterHash, _ := canonicalHash(after)
	if beforeHash == afterHash {
		return RiskUnchanged, []string{"no_change"}
	}
	reasons := make([]string, 0)
	breaking := false
	review := false
	beforeVersion, _ := parseVersion(before.PackVersion)
	afterVersion, _ := parseVersion(after.PackVersion)
	switch {
	case compareVersion(afterVersion, beforeVersion) < 0:
		breaking = true
		reasons = append(reasons, "version_downgrade")
	case afterVersion[0] != beforeVersion[0]:
		breaking = true
		reasons = append(reasons, "major_version_changed")
	case after.PackVersion == before.PackVersion:
		breaking = true
		reasons = append(reasons, "same_version_content_drift")
	}
	for _, changed := range []struct {
		value  bool
		reason string
	}{
		{before.SchemaVersion != after.SchemaVersion, "schema_version_changed"},
		{before.SchemaSHA256 != after.SchemaSHA256, "schema_digest_changed"},
		{before.Provider != after.Provider, "provider_changed"},
		{before.AdapterType != after.AdapterType, "adapter_type_changed"},
		{before.UsageSignature != after.UsageSignature, "usage_semantics_changed"},
		{before.ErrorSignature != after.ErrorSignature, "error_semantics_changed"},
	} {
		if changed.value {
			breaking = true
			reasons = append(reasons, changed.reason)
		}
	}
	for _, set := range []struct {
		before []string
		after  []string
		name   string
	}{
		{before.Endpoints, after.Endpoints, "endpoint"},
		{before.Modalities, after.Modalities, "modality"},
		{before.AuthProfiles, after.AuthProfiles, "auth_profile"},
	} {
		removed, added := setDiff(set.before, set.after)
		if len(removed) > 0 {
			breaking = true
			reasons = append(reasons, set.name+"_removed")
		}
		if len(added) > 0 {
			review = true
			reasons = append(reasons, set.name+"_added")
		}
	}
	fixtureRisk, fixtureReasons := compareFixtures(before.Fixtures, after.Fixtures)
	if fixtureRisk == RiskBreaking {
		breaking = true
	} else if fixtureRisk == RiskReviewRequired {
		review = true
	}
	reasons = append(reasons, fixtureReasons...)
	if before.NetworkVerified != after.NetworkVerified {
		review = true
		reasons = append(reasons, "network_evidence_state_changed")
	}
	if before.ManifestSHA256 != after.ManifestSHA256 && !breaking && !review {
		reasons = append(reasons, "versioned_manifest_digest_changed")
	}
	sort.Strings(reasons)
	switch {
	case breaking:
		return RiskBreaking, reasons
	case review:
		return RiskReviewRequired, reasons
	default:
		return RiskCompatible, reasons
	}
}

func compareFixtures(before, after []FixtureSnapshot) (string, []string) {
	beforeByID := make(map[string]string, len(before))
	afterByID := make(map[string]string, len(after))
	for _, fixture := range before {
		beforeByID[fixture.FixtureID] = fixture.SHA256
	}
	for _, fixture := range after {
		afterByID[fixture.FixtureID] = fixture.SHA256
	}
	breaking, review := false, false
	reasons := make([]string, 0)
	for id, digest := range beforeByID {
		next, exists := afterByID[id]
		if !exists {
			breaking = true
			reasons = append(reasons, "fixture_removed")
		} else if next != digest {
			breaking = true
			reasons = append(reasons, "fixture_digest_changed")
		}
	}
	for id := range afterByID {
		if _, exists := beforeByID[id]; !exists {
			review = true
			reasons = append(reasons, "fixture_added")
		}
	}
	reasons = uniqueSorted(reasons)
	if breaking {
		return RiskBreaking, reasons
	}
	if review {
		return RiskReviewRequired, reasons
	}
	return RiskUnchanged, reasons
}

func summarizeChanges(routes []RouteChange, packs []ProviderPackChange) (DiffSummary, string) {
	var summary DiffSummary
	overall := RiskUnchanged
	add := func(risk string) {
		switch risk {
		case RiskUnchanged:
			summary.Unchanged++
		case RiskCompatible:
			summary.Compatible++
		case RiskReviewRequired:
			summary.ReviewRequired++
		case RiskBreaking:
			summary.Breaking++
		}
		if riskRank(risk) > riskRank(overall) {
			overall = risk
		}
	}
	for _, change := range routes {
		add(change.Risk)
	}
	for _, change := range packs {
		add(change.Risk)
	}
	return summary, overall
}

func releaseReceipt(request SimulationRequest, semanticHash, overallRisk string) ReleaseReceipt {
	decision := "simulated_only"
	if overallRisk == RiskBreaking && request.Release.Action == ActionAdvance {
		decision = "blocked_breaking_change"
	}
	return ReleaseReceipt{
		ContractVersion:            ReleaseVersion,
		ReleaseID:                  request.Release.ReleaseID,
		Revision:                   request.Release.Revision,
		Action:                     request.Release.Action,
		FromStage:                  request.Release.FromStage,
		SimulatedStage:             request.Release.ToStage,
		RollbackToRevision:         request.Release.RollbackToRevision,
		AuditEventID:               hash([]byte(semanticHash + "|release")),
		Decision:                   decision,
		PromotionGate:              "external_explicit_authority_required",
		RuntimePromotionAuthorized: false,
		RuntimeMutation:            RuntimeMutationNone,
	}
}

func validateRequest(request SimulationRequest) error {
	if request.ContractVersion != ContractVersion {
		return contractError(ErrInvalid, "contract_version_unsupported")
	}
	if !validPortableID(request.SimulationID, 160) || len(request.SelectionKey) == 0 ||
		len(request.SelectionKey) > 256 || !utf8.ValidString(request.SelectionKey) {
		return contractError(ErrInvalid, "simulation_identity_invalid")
	}
	if _, err := parseUTCMicros(request.CreatedAt); err != nil {
		return contractError(ErrInvalid, "created_at_invalid")
	}
	switch request.EvaluationMode {
	case ModeShadow, ModeCanary, ModeEnforced:
	default:
		return contractError(ErrInvalid, "evaluation_mode_invalid")
	}
	if err := validateBundle(request.Baseline); err != nil {
		return err
	}
	if err := validateBundle(request.Candidate); err != nil {
		return err
	}
	if request.Baseline.RouteSnapshot.Model != request.Candidate.RouteSnapshot.Model {
		return contractError(ErrInvalid, "route_model_mismatch")
	}
	return validateRelease(request.Release)
}

func validateBundle(bundle Bundle) error {
	if err := bundle.Policy.Validate(); err != nil {
		return contractError(ErrInvalid, "policy_invalid")
	}
	if err := validateRouteSnapshot(bundle.RouteSnapshot); err != nil {
		return err
	}
	if len(bundle.ProviderPacks) == 0 || len(bundle.ProviderPacks) > 64 {
		return contractError(ErrInvalid, "provider_pack_count_invalid")
	}
	lastID := ""
	for _, pack := range bundle.ProviderPacks {
		if err := validatePack(pack); err != nil {
			return err
		}
		if pack.PackID <= lastID {
			return contractError(ErrInvalid, "provider_pack_order_invalid")
		}
		lastID = pack.PackID
	}
	return nil
}

func validateRouteSnapshot(snapshot RouteSnapshot) error {
	if !validPortableID(snapshot.SnapshotID, 160) || strings.TrimSpace(snapshot.Model) == "" ||
		len(snapshot.Model) > 200 || !utf8.ValidString(snapshot.Model) {
		return contractError(ErrInvalid, "route_snapshot_identity_invalid")
	}
	if _, err := parseUTCMicros(snapshot.DBNow); err != nil {
		return contractError(ErrInvalid, "route_snapshot_time_invalid")
	}
	switch snapshot.Source {
	case "fixture", "synthetic", "sanitized_export":
	default:
		return contractError(ErrInvalid, "route_snapshot_source_invalid")
	}
	if len(snapshot.Channels) == 0 || len(snapshot.Channels) > 128 {
		return contractError(ErrInvalid, "route_channel_count_invalid")
	}
	var previousID int64
	for _, channel := range snapshot.Channels {
		id, err := parsePositiveInt64(channel.ID)
		if err != nil || id <= previousID {
			return contractError(ErrInvalid, "route_channel_order_invalid")
		}
		previousID = id
		if strings.TrimSpace(channel.Name) == "" || len(channel.Name) > 128 ||
			!providerPattern.MatchString(channel.Provider) ||
			channel.Priority < 0 || channel.Priority > 1_000_000 ||
			channel.Weight < 1 || channel.Weight > 1_000_000 {
			return contractError(ErrInvalid, "route_channel_invalid")
		}
		if channel.Status != "active" && channel.Status != "disabled" {
			return contractError(ErrInvalid, "route_channel_status_invalid")
		}
		switch channel.CircuitState {
		case "healthy", "half_open", "open":
		default:
			return contractError(ErrInvalid, "route_circuit_state_invalid")
		}
		if channel.HalfOpenUntil != "" {
			if _, err := parseUTCMicros(channel.HalfOpenUntil); err != nil {
				return contractError(ErrInvalid, "route_half_open_time_invalid")
			}
		}
		var previousKeyID int64
		for _, key := range channel.Keys {
			keyID, err := parsePositiveInt64(key.ID)
			if err != nil || keyID <= previousKeyID {
				return contractError(ErrInvalid, "route_key_order_invalid")
			}
			previousKeyID = keyID
			if key.Status != "active" && key.Status != "disabled" {
				return contractError(ErrInvalid, "route_key_status_invalid")
			}
			if len(key.AllowedModels) > 2000 || !utf8.ValidString(key.AllowedModels) {
				return contractError(ErrInvalid, "route_key_models_invalid")
			}
			if key.CooldownUntil != "" {
				if _, err := parseUTCMicros(key.CooldownUntil); err != nil {
					return contractError(ErrInvalid, "route_key_cooldown_invalid")
				}
			}
		}
	}
	return nil
}

func validatePack(pack ProviderPackSnapshot) error {
	if pack.SchemaVersion != "unirouter.provider-pack.v1" ||
		!digestPattern.MatchString(pack.SchemaSHA256) ||
		!packIDPattern.MatchString(pack.PackID) ||
		!versionPattern.MatchString(pack.PackVersion) ||
		!digestPattern.MatchString(pack.ManifestSHA256) ||
		!providerPattern.MatchString(pack.Provider) ||
		!validPortableID(pack.AdapterType, 100) ||
		strings.TrimSpace(pack.UsageSignature) == "" ||
		strings.TrimSpace(pack.ErrorSignature) == "" {
		return contractError(ErrInvalid, "provider_pack_identity_invalid")
	}
	for _, values := range [][]string{pack.Endpoints, pack.Modalities, pack.AuthProfiles} {
		if len(values) == 0 || !sortedUniqueNonempty(values) {
			return contractError(ErrInvalid, "provider_pack_set_invalid")
		}
	}
	if len(pack.Fixtures) == 0 || len(pack.Fixtures) > 128 {
		return contractError(ErrInvalid, "provider_pack_fixture_count_invalid")
	}
	lastID := ""
	for _, fixture := range pack.Fixtures {
		if !validPortableID(fixture.FixtureID, 160) ||
			!digestPattern.MatchString(fixture.SHA256) ||
			fixture.FixtureID <= lastID {
			return contractError(ErrInvalid, "provider_pack_fixture_invalid")
		}
		lastID = fixture.FixtureID
	}
	return nil
}

func validateRelease(intent ReleaseIntent) error {
	if intent.ContractVersion != ReleaseVersion ||
		!validPortableID(intent.ReleaseID, 160) ||
		!digestPattern.MatchString(intent.ActorRef) ||
		strings.TrimSpace(intent.Reason) == "" || len(intent.Reason) > 500 {
		return contractError(ErrInvalid, "release_identity_invalid")
	}
	revision, err := parsePositiveInt64(intent.Revision)
	if err != nil {
		return contractError(ErrInvalid, "release_revision_invalid")
	}
	parent, err := parseUnsignedInt64(intent.ParentRevision)
	if err != nil || parent != revision-1 {
		return contractError(ErrInvalid, "release_parent_revision_invalid")
	}
	if _, err := parseUTCMicros(intent.OccurredAt); err != nil {
		return contractError(ErrInvalid, "release_time_invalid")
	}
	from := stageIndex(intent.FromStage)
	to := stageIndex(intent.ToStage)
	if from < 0 || to < 0 {
		return contractError(ErrInvalid, "release_stage_invalid")
	}
	switch intent.Action {
	case ActionAdvance:
		if to != from+1 || intent.RollbackToRevision != "" {
			return contractError(ErrInvalid, "release_advance_invalid")
		}
	case ActionRollback:
		target, targetErr := parsePositiveInt64(intent.RollbackToRevision)
		if targetErr != nil || target >= revision || to >= from {
			return contractError(ErrInvalid, "release_rollback_invalid")
		}
	default:
		return contractError(ErrInvalid, "release_action_invalid")
	}
	return nil
}

func routeOrder(items []RouteCandidate) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.ChannelID+":"+item.KeyID)
	}
	return strings.Join(parts, ",")
}

func exclusionOrder(items []RouteCandidate) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.ChannelID+":"+item.Reason)
	}
	return strings.Join(parts, ",")
}

func setDiff(before, after []string) (removed, added []string) {
	beforeSet := make(map[string]struct{}, len(before))
	afterSet := make(map[string]struct{}, len(after))
	for _, value := range before {
		beforeSet[value] = struct{}{}
	}
	for _, value := range after {
		afterSet[value] = struct{}{}
	}
	for _, value := range before {
		if _, exists := afterSet[value]; !exists {
			removed = append(removed, value)
		}
	}
	for _, value := range after {
		if _, exists := beforeSet[value]; !exists {
			added = append(added, value)
		}
	}
	return removed, added
}

func sortedUniqueNonempty(values []string) bool {
	previous := ""
	for _, value := range values {
		if strings.TrimSpace(value) == "" || value <= previous || len(value) > 256 || !utf8.ValidString(value) {
			return false
		}
		previous = value
	}
	return true
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func parseVersion(value string) ([3]uint64, error) {
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return [3]uint64{}, fmt.Errorf("invalid version")
	}
	var result [3]uint64
	for index := range result {
		parsed, err := strconv.ParseUint(match[index+1], 10, 32)
		if err != nil {
			return [3]uint64{}, err
		}
		result[index] = parsed
	}
	return result, nil
}

func compareVersion(left, right [3]uint64) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func stageIndex(stage string) int {
	for index, value := range stageOrder {
		if value == stage {
			return index
		}
	}
	return -1
}

func riskRank(risk string) int {
	switch risk {
	case RiskCompatible:
		return 1
	case RiskReviewRequired:
		return 2
	case RiskBreaking:
		return 3
	default:
		return 0
	}
}

func validPortableID(value string, max int) bool {
	return len(value) > 0 && len(value) <= max && utf8.ValidString(value) && portableIDPattern.MatchString(value)
}

func parsePositiveInt64(value string) (int64, error) {
	parsed, err := parseUnsignedInt64(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("positive int64 required")
	}
	return parsed, nil
}

func parseUnsignedInt64(value string) (int64, error) {
	if !unsignedPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid decimal")
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseUTCMicros(value string) (time.Time, error) {
	if !utcMicrosPattern.MatchString(value) {
		return time.Time{}, fmt.Errorf("six-digit UTC required")
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func canonicalHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hash(body), nil
}

func hash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hasExactJSONShape(body []byte, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var provided, expected any
	if json.Unmarshal(body, &provided) != nil || json.Unmarshal(encoded, &expected) != nil {
		return false
	}
	return sameJSONShape(provided, expected)
}

func sameJSONShape(provided, expected any) bool {
	switch expectedValue := expected.(type) {
	case map[string]any:
		providedValue, ok := provided.(map[string]any)
		if !ok || len(providedValue) != len(expectedValue) {
			return false
		}
		for key, expectedChild := range expectedValue {
			providedChild, exists := providedValue[key]
			if !exists || !sameJSONShape(providedChild, expectedChild) {
				return false
			}
		}
		return true
	case []any:
		providedValue, ok := provided.([]any)
		if !ok || len(providedValue) != len(expectedValue) {
			return false
		}
		for index := range expectedValue {
			if !sameJSONShape(providedValue[index], expectedValue[index]) {
				return false
			}
		}
		return true
	default:
		switch provided.(type) {
		case map[string]any, []any:
			return false
		default:
			return true
		}
	}
}
