package memberlifecycle

import "encoding/json"

// Apply mutates only an isolated in-memory conformance ledger. It is not
// connected to users, memberships, sessions, keys, an IdP, or a database.
func (s *Service) Apply(body []byte) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, contractError(ErrInvalid, "offline_store_unavailable")
	}
	event, err := decodeEvent(body)
	if err != nil {
		return Report{}, err
	}
	semanticHash, err := canonicalHash(event)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "canonicalization_failed")
	}
	revision, _ := parsePositiveInt64(event.Revision)
	parent, _ := parseUnsignedInt64(event.ParentRevision)

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if existing, found := s.store.events[event.EventID]; found {
		if existing.semanticHash != semanticHash {
			return Report{}, contractError(ErrConflict, "event_identity_conflict")
		}
		report := cloneReport(existing.report)
		report.ReplayStatus = ReplayDuplicateNoop
		return report, nil
	}

	current, found := s.store.subjects[event.SubjectRef]
	if event.Operation == OperationCreate {
		if found {
			return Report{}, contractError(ErrConflict, "subject_already_exists")
		}
	} else {
		if !found || current.terminal {
			return Report{}, contractError(ErrConflict, "subject_state_conflict")
		}
		if current.revision != parent || event.Before == nil || !snapshotEqual(current.snapshot, *event.Before) {
			return Report{}, contractError(ErrConflict, "subject_compare_and_swap_conflict")
		}
	}

	report, err := evaluate(body, event)
	if err != nil {
		return Report{}, err
	}
	s.store.events[event.EventID] = storedEvent{semanticHash: semanticHash, report: cloneReport(report)}
	s.store.subjects[event.SubjectRef] = subjectState{
		revision: revision,
		snapshot: cloneSnapshot(event.After),
		terminal: event.Operation == OperationDeprovision,
	}
	return report, nil
}

func cloneSnapshot(snapshot StateSnapshot) StateSnapshot {
	body, _ := json.Marshal(snapshot)
	var cloned StateSnapshot
	_ = json.Unmarshal(body, &cloned)
	return cloned
}
