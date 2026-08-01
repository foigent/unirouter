package secretref

import (
	"context"
	"sort"
	"sync"
	"time"
)

// FixtureBackend is a deterministic in-memory implementation for contract,
// rotation and outage tests. It is not a production secret store.
type FixtureBackend struct {
	mu        sync.RWMutex
	clock     func() time.Time
	available bool
	records   map[Reference]*fixtureRecord
}

type fixtureRecord struct {
	revision   uint64
	generation uint64
	active     string
	versions   map[string]*fixtureVersion
	audit      []AuditReceipt
}

type fixtureVersion struct {
	metadata VersionMetadata
	material []byte
}

func NewFixtureBackend(clock func() time.Time) *FixtureBackend {
	if clock == nil {
		clock = time.Now
	}
	return &FixtureBackend{
		clock:     clock,
		available: true,
		records:   make(map[Reference]*fixtureRecord),
	}
}

var _ Backend = (*FixtureBackend)(nil)

func (b *FixtureBackend) Name() string {
	return "fixture"
}

// SetAvailable is a fixture-only outage switch. It never changes lifecycle
// revision or active generation.
func (b *FixtureBackend) SetAvailable(available bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.available = available
}

func (b *FixtureBackend) Stage(
	ctx context.Context,
	ref Reference,
	version string,
	expectedRevision uint64,
	material []byte,
) (AuditReceipt, error) {
	if err := b.validateInput(ctx, ref, version); err != nil {
		return AuditReceipt{}, err
	}
	if err := validateMaterial(material); err != nil {
		return AuditReceipt{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return AuditReceipt{}, ErrBackendUnavailable
	}
	record := b.records[ref]
	if record == nil {
		record = &fixtureRecord{versions: make(map[string]*fixtureVersion)}
		b.records[ref] = record
	}
	if record.revision != expectedRevision {
		return AuditReceipt{}, ErrRevisionConflict
	}
	if _, exists := record.versions[version]; exists {
		return AuditReceipt{}, ErrVersionExists
	}

	now := b.now()
	record.versions[version] = &fixtureVersion{
		metadata: VersionMetadata{
			Version:  version,
			State:    VersionStaged,
			StagedAt: now,
		},
		material: append([]byte(nil), material...),
	}
	record.revision++
	receipt := AuditReceipt{
		Action:     ActionStage,
		Reference:  ref,
		Version:    version,
		Revision:   record.revision,
		Generation: record.generation,
		OccurredAt: now,
	}
	record.audit = append(record.audit, receipt)
	return receipt, nil
}

func (b *FixtureBackend) Activate(
	ctx context.Context,
	ref Reference,
	version string,
	expectedRevision uint64,
) (Binding, AuditReceipt, error) {
	return b.promote(ctx, ActionActivate, ref, version, expectedRevision)
}

func (b *FixtureBackend) Rollback(
	ctx context.Context,
	ref Reference,
	version string,
	expectedRevision uint64,
) (Binding, AuditReceipt, error) {
	return b.promote(ctx, ActionRollback, ref, version, expectedRevision)
}

func (b *FixtureBackend) promote(
	ctx context.Context,
	action Action,
	ref Reference,
	version string,
	expectedRevision uint64,
) (Binding, AuditReceipt, error) {
	if err := b.validateInput(ctx, ref, version); err != nil {
		return Binding{}, AuditReceipt{}, err
	}
	if action != ActionActivate && action != ActionRollback {
		return Binding{}, AuditReceipt{}, ErrInvalidTransition
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return Binding{}, AuditReceipt{}, ErrBackendUnavailable
	}
	record := b.records[ref]
	if record == nil {
		return Binding{}, AuditReceipt{}, ErrVersionNotFound
	}
	if record.revision != expectedRevision {
		return Binding{}, AuditReceipt{}, ErrRevisionConflict
	}
	target := record.versions[version]
	if target == nil {
		return Binding{}, AuditReceipt{}, ErrVersionNotFound
	}
	switch action {
	case ActionActivate:
		if target.metadata.State != VersionStaged && target.metadata.State != VersionRetired {
			return Binding{}, AuditReceipt{}, ErrInvalidTransition
		}
	case ActionRollback:
		if target.metadata.State != VersionRetired {
			return Binding{}, AuditReceipt{}, ErrInvalidTransition
		}
	}
	if record.active == version {
		return Binding{}, AuditReceipt{}, ErrInvalidTransition
	}

	now := b.now()
	previous := record.active
	if previous != "" {
		current := record.versions[previous]
		current.metadata.State = VersionRetired
		current.metadata.Retired = timePtr(now)
	}
	target.metadata.State = VersionActive
	target.metadata.Activated = timePtr(now)
	target.metadata.Retired = nil
	record.active = version
	record.revision++
	record.generation++

	binding := Binding{
		Reference:  ref,
		Version:    version,
		Generation: record.generation,
	}
	receipt := AuditReceipt{
		Action:          action,
		Reference:       ref,
		Version:         version,
		PreviousVersion: previous,
		Revision:        record.revision,
		Generation:      record.generation,
		OccurredAt:      now,
	}
	record.audit = append(record.audit, receipt)
	return binding, receipt, nil
}

func (b *FixtureBackend) Revoke(
	ctx context.Context,
	ref Reference,
	version string,
	expectedRevision uint64,
) (AuditReceipt, error) {
	if err := b.validateInput(ctx, ref, version); err != nil {
		return AuditReceipt{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.available {
		return AuditReceipt{}, ErrBackendUnavailable
	}
	record := b.records[ref]
	if record == nil {
		return AuditReceipt{}, ErrVersionNotFound
	}
	if record.revision != expectedRevision {
		return AuditReceipt{}, ErrRevisionConflict
	}
	target := record.versions[version]
	if target == nil {
		return AuditReceipt{}, ErrVersionNotFound
	}
	if target.metadata.State == VersionActive || target.metadata.State == VersionRevoked {
		return AuditReceipt{}, ErrInvalidTransition
	}

	now := b.now()
	target.metadata.State = VersionRevoked
	target.metadata.Revoked = timePtr(now)
	record.revision++
	receipt := AuditReceipt{
		Action:     ActionRevoke,
		Reference:  ref,
		Version:    version,
		Revision:   record.revision,
		Generation: record.generation,
		OccurredAt: now,
	}
	record.audit = append(record.audit, receipt)
	return receipt, nil
}

func (b *FixtureBackend) Pin(ctx context.Context, ref Reference) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	if err := ref.Validate(); err != nil {
		return Binding{}, err
	}
	if ref.Backend != b.Name() {
		return Binding{}, ErrBackendNotFound
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.available {
		return Binding{}, ErrBackendUnavailable
	}
	record := b.records[ref]
	if record == nil || record.active == "" {
		return Binding{}, ErrNoActiveVersion
	}
	return Binding{
		Reference:  ref,
		Version:    record.active,
		Generation: record.generation,
	}, nil
}

func (b *FixtureBackend) WithMaterial(
	ctx context.Context,
	binding Binding,
	consume func([]byte) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := binding.Validate(); err != nil {
		return err
	}
	if binding.Reference.Backend != b.Name() {
		return ErrBackendNotFound
	}
	if consume == nil {
		return ErrConsumerRejected
	}

	// Keep the read lock through the deliberately short callback so activation
	// cannot race between binding validation and material injection.
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.available {
		return ErrBackendUnavailable
	}
	record := b.records[binding.Reference]
	if record == nil ||
		record.active != binding.Version ||
		record.generation != binding.Generation {
		return ErrStaleBinding
	}
	version := record.versions[binding.Version]
	if version == nil || version.metadata.State != VersionActive {
		return ErrStaleBinding
	}

	material := append([]byte(nil), version.material...)
	defer wipe(material)
	if !consumeSafely(consume, material) {
		// Discard error and panic text: a consumer could accidentally include
		// the secret in either.
		return ErrConsumerRejected
	}
	return nil
}

func (b *FixtureBackend) Snapshot(ctx context.Context, ref Reference) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := ref.Validate(); err != nil {
		return Snapshot{}, err
	}
	if ref.Backend != b.Name() {
		return Snapshot{}, ErrBackendNotFound
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.available {
		return Snapshot{}, ErrBackendUnavailable
	}
	record := b.records[ref]
	if record == nil {
		return Snapshot{
			Reference: ref,
			Versions:  make([]VersionMetadata, 0),
		}, nil
	}

	snapshot := Snapshot{
		Reference:  ref,
		Revision:   record.revision,
		Generation: record.generation,
		Versions:   make([]VersionMetadata, 0, len(record.versions)),
	}
	if record.active != "" {
		active := Binding{
			Reference:  ref,
			Version:    record.active,
			Generation: record.generation,
		}
		snapshot.Active = &active
	}
	for _, version := range record.versions {
		snapshot.Versions = append(snapshot.Versions, cloneMetadata(version.metadata))
	}
	sort.Slice(snapshot.Versions, func(i, j int) bool {
		return snapshot.Versions[i].Version < snapshot.Versions[j].Version
	})
	return snapshot, nil
}

func (b *FixtureBackend) Audit(ctx context.Context, ref Reference) ([]AuditReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if ref.Backend != b.Name() {
		return nil, ErrBackendNotFound
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.available {
		return nil, ErrBackendUnavailable
	}
	record := b.records[ref]
	if record == nil {
		return make([]AuditReceipt, 0), nil
	}
	return append([]AuditReceipt(nil), record.audit...), nil
}

func (b *FixtureBackend) now() time.Time {
	return b.clock().UTC()
}

func (b *FixtureBackend) validateInput(ctx context.Context, ref Reference, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	if ref.Backend != b.Name() {
		return ErrBackendNotFound
	}
	return validateVersion(version)
}

func timePtr(value time.Time) *time.Time {
	copyValue := value
	return &copyValue
}

func cloneMetadata(value VersionMetadata) VersionMetadata {
	clone := value
	if value.Activated != nil {
		clone.Activated = timePtr(*value.Activated)
	}
	if value.Retired != nil {
		clone.Retired = timePtr(*value.Retired)
	}
	if value.Revoked != nil {
		clone.Revoked = timePtr(*value.Revoked)
	}
	return clone
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func consumeSafely(consume func([]byte) error, material []byte) (accepted bool) {
	defer func() {
		if recover() != nil {
			accepted = false
		}
	}()
	return consume(material) == nil
}
