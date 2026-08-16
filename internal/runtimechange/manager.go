package runtimechange

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/providerpack"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type Manager struct {
	db      db.DBQuerier
	builder SnapshotBuilder
	policy  routingpolicy.Config
	now     func() time.Time
}

func NewManager(database db.DBQuerier, builder SnapshotBuilder, policy routingpolicy.Config) (*Manager, error) {
	if database == nil || builder == nil {
		return nil, fmt.Errorf("runtime change dependencies are required")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Manager{db: database, builder: builder, policy: policy, now: time.Now}, nil
}

func (m *Manager) CreateDraft(ctx context.Context, request DraftRequest, actorUserID int64) (Release, error) {
	request, err := NormalizeDraft(request)
	if err != nil {
		return Release{}, err
	}
	if actorUserID <= 0 {
		return Release{}, fmt.Errorf("%w: actor", ErrInvalid)
	}
	if request.ReleaseRef == "" {
		request.ReleaseRef, err = automaticReleaseRef(request.ModelName, m.now())
		if err != nil {
			return Release{}, fmt.Errorf("%w: generate release_ref: %v", ErrUnavailable, err)
		}
	}
	if err := m.validateChannelPins(ctx, m.db, request.ModelName, request.Overrides); err != nil {
		return Release{}, err
	}
	baseline, parentID, err := m.stableCandidate(ctx, request.ModelName)
	if err != nil {
		return Release{}, err
	}
	candidateOverrides := mergeOverrides(baseline.Overrides, request.Overrides)
	candidate := Candidate{ContractVersion: CandidateVersion, ModelName: request.ModelName, Overrides: candidateOverrides}
	pins, report, baselineHash, candidateHash, err := m.evaluateCandidate(ctx, request.ReleaseRef, request.Reason, actorUserID, baseline, candidate)
	if err != nil {
		return Release{}, err
	}
	candidateJSON, _ := json.Marshal(candidate)
	pinsJSON, _ := json.Marshal(pins)
	reportJSON, _ := json.Marshal(report)
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Release{}, fmt.Errorf("%w: begin create: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	var release Release
	err = tx.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
    candidate, provider_pack_pins, simulation_report, overall_risk, stage,
    version, parent_stable_release_id, canary_basis_points, reason, created_by)
VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9,'draft',1,$10,0,$11,$12)
RETURNING id, created_at, updated_at`,
		request.ReleaseRef, request.ModelName, CandidateVersion, candidateHash, baselineHash,
		string(candidateJSON), string(pinsJSON), string(reportJSON), report.OverallRisk,
		parentID, request.Reason, actorUserID,
	).Scan(&release.ID, &release.CreatedAt, &release.UpdatedAt)
	if err != nil {
		return Release{}, classifyWriteError(err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO runtime_change_events (
    release_id, sequence, action, from_stage, to_stage, actor_user_id,
    expected_version, resulting_version, detail)
VALUES ($1,1,'create','none','draft',$2,0,1,$3::jsonb)`,
		release.ID, actorUserID, `{"runtime_mutation":"none"}`)
	if err != nil {
		return Release{}, fmt.Errorf("%w: create event: %v", ErrUnavailable, err)
	}
	created, err := scanRelease(tx.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE release_ref=$1`, request.ReleaseRef))
	if err != nil {
		return Release{}, fmt.Errorf("%w: read created release before commit: %v", ErrUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("%w: commit create: %v", ErrUnavailable, err)
	}
	return created, nil
}

func (m *Manager) List(ctx context.Context, modelName string, limit int) ([]Release, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	modelName = strings.TrimSpace(modelName)
	rows, err := m.db.Query(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases
 WHERE ($1 = '' OR model_name = $1)
 ORDER BY updated_at DESC, id DESC LIMIT $2`, modelName, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list releases: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	result := make([]Release, 0)
	for rows.Next() {
		release, scanErr := scanRelease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, release)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list releases: %v", ErrUnavailable, err)
	}
	return result, nil
}

func (m *Manager) Get(ctx context.Context, ref string) (Release, error) {
	row := m.db.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE release_ref=$1`, strings.TrimSpace(strings.ToLower(ref)))
	return scanRelease(row)
}

func (m *Manager) Slot(ctx context.Context, modelName string) (Slot, error) {
	var slot Slot
	err := m.db.QueryRow(ctx, `
SELECT s.model_name, COALESCE(stable.release_ref,''), COALESCE(canary.release_ref,''),
       s.canary_basis_points, s.slot_version
  FROM runtime_change_slots s
  LEFT JOIN runtime_change_releases stable ON stable.id=s.stable_release_id
  LEFT JOIN runtime_change_releases canary ON canary.id=s.canary_release_id
 WHERE s.model_name=$1`, strings.TrimSpace(modelName)).Scan(
		&slot.ModelName, &slot.StableReleaseRef, &slot.CanaryReleaseRef,
		&slot.CanaryBasisPoints, &slot.SlotVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Slot{ModelName: strings.TrimSpace(modelName), RuntimeMutation: "none"}, nil
	}
	if err != nil {
		return Slot{}, fmt.Errorf("%w: read slot: %v", ErrUnavailable, err)
	}
	if slot.StableReleaseRef == "" && slot.CanaryReleaseRef == "" {
		slot.RuntimeMutation = "none"
	} else {
		slot.RuntimeMutation = "bounded_route_assignment"
	}
	return slot, nil
}

func (m *Manager) stableCandidate(ctx context.Context, modelName string) (Candidate, *int64, error) {
	var id *int64
	var raw []byte
	err := m.db.QueryRow(ctx, `
SELECT s.stable_release_id, stable.candidate
  FROM runtime_change_slots s
  LEFT JOIN runtime_change_releases stable ON stable.id=s.stable_release_id AND stable.stage='active'
 WHERE s.model_name=$1`, modelName).Scan(&id, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{ContractVersion: CandidateVersion, ModelName: modelName}, nil, nil
	}
	if err != nil {
		return Candidate{}, nil, fmt.Errorf("%w: read stable release: %v", ErrUnavailable, err)
	}
	if id == nil || len(raw) == 0 {
		if id != nil {
			return Candidate{}, nil, fmt.Errorf("%w: invalid stable release", ErrUnavailable)
		}
		return Candidate{ContractVersion: CandidateVersion, ModelName: modelName}, nil, nil
	}
	var candidate Candidate
	if json.Unmarshal(raw, &candidate) != nil || candidate.ContractVersion != CandidateVersion || candidate.ModelName != modelName {
		return Candidate{}, nil, fmt.Errorf("%w: corrupt stable candidate", ErrUnavailable)
	}
	return candidate, id, nil
}

func (m *Manager) evaluateCandidate(
	ctx context.Context,
	releaseRef, reason string,
	actorUserID int64,
	baseline, candidate Candidate,
) ([]governancesim.ProviderPackSnapshot, governancesim.Report, string, string, error) {
	baselineSnapshot, candidateSnapshot, err := m.builder.BuildRuntimeChangeSnapshots(ctx, candidate.ModelName, baseline.Overrides, candidate.Overrides)
	if err != nil {
		return nil, governancesim.Report{}, "", "", fmt.Errorf("%w: build governed snapshots: %v", ErrUnavailable, err)
	}
	packs, err := governancesim.CurrentProviderPackSnapshots()
	if err != nil {
		return nil, governancesim.Report{}, "", "", fmt.Errorf("%w: provider packs: %v", ErrUnavailable, err)
	}
	now := m.now().UTC().Truncate(time.Microsecond)
	request := governancesim.SimulationRequest{
		ContractVersion: governancesim.ContractVersion,
		SimulationID:    releaseRef + ":validation",
		CreatedAt:       formatMicros(now),
		SelectionKey:    "runtime-change:" + releaseRef,
		EvaluationMode:  governancesim.ModeShadow,
		Baseline:        governancesim.Bundle{Policy: m.policy, RouteSnapshot: baselineSnapshot, ProviderPacks: packs},
		Candidate:       governancesim.Bundle{Policy: m.policy, RouteSnapshot: candidateSnapshot, ProviderPacks: packs},
		Release: governancesim.ReleaseIntent{
			ContractVersion: governancesim.ReleaseVersion,
			ReleaseID:       releaseRef, Revision: "1", ParentRevision: "0",
			Action: governancesim.ActionAdvance, FromStage: governancesim.StageDraft,
			ToStage: governancesim.StageValidated, ActorRef: digest([]byte("user:" + strconv.FormatInt(actorUserID, 10))),
			OccurredAt: formatMicros(now), Reason: reason,
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, governancesim.Report{}, "", "", fmt.Errorf("%w: encode simulation", ErrInvalid)
	}
	report, err := governancesim.Evaluate(body)
	if err != nil {
		return nil, governancesim.Report{}, "", "", fmt.Errorf("%w: simulation: %v", ErrInvalid, err)
	}
	return packs, report, hashJSON(baseline), hashJSON(candidate), nil
}

func (m *Manager) validateChannelPins(ctx context.Context, store interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, modelName string, overrides []routingpolicy.ChannelOverride) error {
	for _, override := range overrides {
		var provider, adapterType, models, channelStatus string
		var epoch int64
		err := store.QueryRow(ctx, `
SELECT provider, COALESCE(adapter_type,'openai_compatible'), models, routing_epoch, status
  FROM channels WHERE id=$1`, override.ChannelID).Scan(&provider, &adapterType, &models, &epoch, &channelStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: channel %d not found", ErrInvalid, override.ChannelID)
		}
		if err != nil {
			return fmt.Errorf("%w: read channel pin: %v", ErrUnavailable, err)
		}
		if epoch != override.ExpectedRoutingEpoch {
			return fmt.Errorf("%w: channel %d identity changed", ErrConflict, override.ChannelID)
		}
		if override.Admission == routingpolicy.AdmissionDeny {
			// A deny override carries no traffic. It may intentionally retire a
			// channel that no longer declares this model or whose old adapter pack
			// is gone, but its current epoch is still pinned to prevent ID reuse.
			continue
		}
		if !modelInCSV(models, modelName) {
			return fmt.Errorf("%w: channel %d identity changed", ErrConflict, override.ChannelID)
		}
		if override.Admission == routingpolicy.AdmissionAllow && channelStatus == "disabled" {
			return fmt.Errorf("%w: channel %d is emergency disabled", ErrConflict, override.ChannelID)
		}
		_, _, found, packErr := providerpack.FindConformance(provider, adapterType)
		if packErr != nil {
			return fmt.Errorf("%w: provider pack catalog: %v", ErrUnavailable, packErr)
		}
		if !found {
			return fmt.Errorf("%w: channel %d has no offline adapter conformance fixture", ErrInvalid, override.ChannelID)
		}
	}
	return nil
}

func mergeOverrides(baseline, patch []routingpolicy.ChannelOverride) []routingpolicy.ChannelOverride {
	byID := make(map[int64]routingpolicy.ChannelOverride, len(baseline)+len(patch))
	for _, item := range baseline {
		byID[item.ChannelID] = item
	}
	for _, item := range patch {
		byID[item.ChannelID] = item
	}
	result := make([]routingpolicy.ChannelOverride, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	slices.SortFunc(result, func(left, right routingpolicy.ChannelOverride) int {
		switch {
		case left.ChannelID < right.ChannelID:
			return -1
		case left.ChannelID > right.ChannelID:
			return 1
		default:
			return 0
		}
	})
	return result
}

func scanRelease(row interface{ Scan(...interface{}) error }) (Release, error) {
	var release Release
	var candidateJSON, pinsJSON, reportJSON []byte
	err := row.Scan(
		&release.ID, &release.ReleaseRef, &release.ModelName, &release.CandidateVersion,
		&release.CandidateHash, &release.BaselineHash, &candidateJSON, &pinsJSON,
		&reportJSON, &release.OverallRisk, &release.Stage, &release.Version,
		&release.ParentStableReleaseID, &release.CanaryBasisPoints, &release.Reason,
		&release.CreatedBy, &release.ValidatedBy, &release.ActivatedBy, &release.CreatedAt,
		&release.ValidatedAt, &release.ActivatedAt, &release.RolledBackAt, &release.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, fmt.Errorf("%w: scan release: %v", ErrUnavailable, err)
	}
	if json.Unmarshal(candidateJSON, &release.Candidate) != nil ||
		json.Unmarshal(pinsJSON, &release.ProviderPackPins) != nil ||
		json.Unmarshal(reportJSON, &release.SimulationReport) != nil {
		return Release{}, fmt.Errorf("%w: corrupt release", ErrUnavailable)
	}
	return release, nil
}

func modelInCSV(models, wanted string) bool {
	return modellist.Contains(models, wanted)
}

func automaticReleaseRef(modelName string, now time.Time) (string, error) {
	var entropy [12]byte
	if _, err := cryptorand.Read(entropy[:]); err != nil {
		return "", err
	}
	slug := releaseRefSlug(modelName)
	return "rcr_" + slug + "_" + now.UTC().Format("20060102t150405") + "_" + hex.EncodeToString(entropy[:]), nil
}

func releaseRefSlug(value string) string {
	var builder strings.Builder
	previousSeparator := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			builder.WriteRune(character)
			previousSeparator = false
			continue
		}
		if builder.Len() > 0 && !previousSeparator {
			builder.WriteByte('_')
			previousSeparator = true
		}
	}
	slug := strings.Trim(builder.String(), "_")
	if slug == "" {
		slug = "model"
	}
	if len(slug) > 18 {
		slug = strings.TrimRight(slug[:18], "_")
	}
	return slug
}

func hashJSON(value any) string {
	body, _ := json.Marshal(value)
	return digest(body)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func formatMicros(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}

func classifyWriteError(err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate") || strings.Contains(message, "unique") {
		return fmt.Errorf("%w: release_ref exists", ErrConflict)
	}
	return fmt.Errorf("%w: write release: %v", ErrUnavailable, err)
}
