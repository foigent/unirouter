package runtimechange

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func (m *Manager) Resolve(ctx context.Context, modelName, selectionKey string) (routingpolicy.RuntimeAssignment, error) {
	state, err := m.resolveSlot(ctx, strings.TrimSpace(modelName))
	if err != nil {
		return routingpolicy.RuntimeAssignment{}, err
	}
	if state.canary.ReleaseRef != "" && canarySelected(modelName, selectionKey, state.canary.ReleaseRef, state.basisPoints) {
		return state.canary, nil
	}
	return state.stable, nil
}

func (m *Manager) Stable(ctx context.Context, modelName string) (routingpolicy.RuntimeAssignment, error) {
	state, err := m.resolveSlot(ctx, strings.TrimSpace(modelName))
	if err != nil {
		return routingpolicy.RuntimeAssignment{}, err
	}
	return state.stable, nil
}

// Invalidate exists for Router/controller convergence. The resolver performs
// authoritative reads and therefore has no stale process-local state.
func (m *Manager) Invalidate() {}

type resolvedSlot struct {
	stable, canary routingpolicy.RuntimeAssignment
	basisPoints    int
}

func (m *Manager) resolveSlot(ctx context.Context, modelName string) (resolvedSlot, error) {
	if modelName == "" {
		return resolvedSlot{}, fmt.Errorf("%w: model", ErrInvalid)
	}
	var (
		slotVersion, basisPoints                       int64
		stableRef, stableStage, canaryRef, canaryStage string
		stableJSON, canaryJSON                         []byte
	)
	err := m.db.QueryRow(ctx, `
SELECT s.slot_version, s.canary_basis_points,
       COALESCE(stable.release_ref,''), COALESCE(stable.stage,''), stable.candidate,
       COALESCE(canary.release_ref,''), COALESCE(canary.stage,''), canary.candidate
  FROM runtime_change_slots s
  LEFT JOIN runtime_change_releases stable ON stable.id=s.stable_release_id
  LEFT JOIN runtime_change_releases canary ON canary.id=s.canary_release_id
 WHERE s.model_name=$1`, modelName).Scan(
		&slotVersion, &basisPoints, &stableRef, &stableStage, &stableJSON,
		&canaryRef, &canaryStage, &canaryJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedSlot{}, nil
	}
	if err != nil {
		return resolvedSlot{}, fmt.Errorf("%w: resolve slot: %v", ErrUnavailable, err)
	}
	state := resolvedSlot{basisPoints: int(basisPoints)}
	if stableRef != "" {
		if stableStage != "active" {
			return resolvedSlot{}, fmt.Errorf("%w: stable stage", ErrUnavailable)
		}
		assignment, decodeErr := decodeAssignment(modelName, stableRef, stableStage, slotVersion, stableJSON)
		if decodeErr != nil {
			return resolvedSlot{}, decodeErr
		}
		state.stable = assignment
	}
	if canaryRef != "" {
		if canaryStage != "canary" || basisPoints < 1 || basisPoints > 5000 {
			return resolvedSlot{}, fmt.Errorf("%w: canary stage", ErrUnavailable)
		}
		assignment, decodeErr := decodeAssignment(modelName, canaryRef, canaryStage, slotVersion, canaryJSON)
		if decodeErr != nil {
			return resolvedSlot{}, decodeErr
		}
		state.canary = assignment
	} else if basisPoints != 0 {
		return resolvedSlot{}, fmt.Errorf("%w: canary slot", ErrUnavailable)
	}
	return state, nil
}

func decodeAssignment(modelName, ref, stage string, slotVersion int64, raw []byte) (routingpolicy.RuntimeAssignment, error) {
	var candidate Candidate
	if len(raw) == 0 || json.Unmarshal(raw, &candidate) != nil ||
		candidate.ContractVersion != CandidateVersion || candidate.ModelName != modelName {
		return routingpolicy.RuntimeAssignment{}, fmt.Errorf("%w: corrupt candidate", ErrUnavailable)
	}
	if _, err := routingpolicy.ValidateOverrides(candidate.Overrides); err != nil {
		return routingpolicy.RuntimeAssignment{}, fmt.Errorf("%w: invalid candidate: %v", ErrUnavailable, err)
	}
	return routingpolicy.RuntimeAssignment{
		ContractVersion: routingpolicy.RuntimeAssignmentVersion,
		Identity:        fmt.Sprintf("%s:%d:%s", modelName, slotVersion, ref),
		ReleaseRef:      ref, Stage: stage, Overrides: candidate.Overrides,
	}, nil
}

func canarySelected(modelName, selectionKey, ref string, basisPoints int) bool {
	if basisPoints <= 0 || strings.TrimSpace(selectionKey) == "" {
		return false
	}
	sum := sha256.Sum256([]byte(modelName + "|" + selectionKey + "|" + ref))
	return int(binary.BigEndian.Uint64(sum[:8])%10000) < basisPoints
}
