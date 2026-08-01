package providercogs

// Import evaluates and records only an immutable in-memory replay identity.
// It is intentionally not wired to HTTP or production persistence.
func (s *Service) Import(body []byte) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, contractError(ErrInvalid, "offline_store_unavailable")
	}
	request, err := decodeImport(body)
	if err != nil {
		return Report{}, err
	}
	semanticHash, hashErr := canonicalHash(request)
	if hashErr != nil {
		return Report{}, hashErr
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if existing, found := s.store.imports[request.ImportID]; found {
		if existing.semanticHash != semanticHash {
			return Report{}, contractError(ErrConflict, "import_identity_conflict")
		}
		report := cloneReport(existing.report)
		report.ReplayStatus = ReplayDuplicateNoop
		return report, nil
	}
	report, err := evaluate(body, request)
	if err != nil {
		return Report{}, err
	}
	s.store.imports[request.ImportID] = storedImport{
		semanticHash: semanticHash,
		report:       cloneReport(report),
	}
	return report, nil
}
