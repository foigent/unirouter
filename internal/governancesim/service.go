package governancesim

import (
	"encoding/json"
	"strconv"
)

// Apply records only an in-memory simulation/release evidence chain. It has no
// production routing, policy or provider-pack promotion path.
func (s *Service) Apply(body []byte) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, contractError(ErrInvalid, "offline_store_unavailable")
	}
	request, err := decodeRequest(body)
	if err != nil {
		return Report{}, err
	}
	semanticHash, err := canonicalHash(request)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "canonicalization_failed")
	}
	revision, _ := parsePositiveInt64(request.Release.Revision)
	parent, _ := parseUnsignedInt64(request.Release.ParentRevision)

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if existing, found := s.store.simulations[request.SimulationID]; found {
		if existing.semanticHash != semanticHash {
			return Report{}, contractError(ErrConflict, "simulation_identity_conflict")
		}
		report := cloneReport(existing.report)
		report.ReplayStatus = ReplayDuplicateNoop
		return report, nil
	}

	current, found := s.store.releases[request.Release.ReleaseID]
	if !found {
		if revision != 1 || parent != 0 || request.Release.FromStage != StageDraft {
			return Report{}, contractError(ErrConflict, "release_initial_revision_conflict")
		}
		current = releaseState{
			revision: 0,
			stage:    StageDraft,
			history:  map[int64]string{0: StageDraft},
		}
	}
	if current.revision != parent || current.stage != request.Release.FromStage {
		return Report{}, contractError(ErrConflict, "release_compare_and_swap_conflict")
	}
	if request.Release.Action == ActionRollback {
		target, _ := parsePositiveInt64(request.Release.RollbackToRevision)
		if current.history[target] != request.Release.ToStage {
			return Report{}, contractError(ErrConflict, "release_rollback_target_conflict")
		}
	}

	report, err := evaluate(body, request)
	if err != nil {
		return Report{}, err
	}
	s.store.simulations[request.SimulationID] = storedSimulation{
		semanticHash: semanticHash,
		report:       cloneReport(report),
	}
	if report.Release.Decision == "blocked_breaking_change" {
		return report, nil
	}
	current.revision = revision
	current.stage = request.Release.ToStage
	current.history[revision] = request.Release.ToStage
	s.store.releases[request.Release.ReleaseID] = current
	return report, nil
}

func cloneReport(report Report) Report {
	body, err := json.Marshal(report)
	if err != nil {
		return report
	}
	var cloned Report
	if err := json.Unmarshal(body, &cloned); err != nil {
		return report
	}
	return cloned
}

func nextRevision(value string) string {
	parsed, _ := parsePositiveInt64(value)
	return strconv.FormatInt(parsed+1, 10)
}
