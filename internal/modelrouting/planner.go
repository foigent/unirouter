package modelrouting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type snapshotLoader interface {
	Load(context.Context, pgx.Tx, Request) (snapshot, error)
}

type Planner struct {
	database TxStarter
	policy   routingpolicy.Config
	loader   snapshotLoader
}

func NewPlanner(database TxStarter, policy routingpolicy.Config) (*Planner, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: database", ErrInvalidRequest)
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("%w: routing policy: %v", ErrInvalidRequest, err)
	}
	return &Planner{database: database, policy: policy, loader: sqlSnapshotLoader{}}, nil
}

func (p *Planner) Plan(ctx context.Context, request Request) (Plan, error) {
	if p == nil || p.database == nil || p.loader == nil {
		return Plan{}, ErrUnavailable
	}
	request, err := normalizeRequest(request)
	if err != nil {
		return Plan{}, err
	}
	tx, err := p.database.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("%w: begin route snapshot: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	snapshot, err := p.loader.Load(ctx, tx, request)
	if err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, fmt.Errorf("%w: commit route snapshot: %v", ErrUnavailable, err)
	}

	return buildPlan(snapshot, request, p.policy)
}

// buildPlan evaluates an already materialized immutable snapshot. Keeping this
// pure step shared prevents the startup authority gate from reimplementing the
// production planner's parity and manifest semantics.
func buildPlan(snapshot snapshot, request Request, policy routingpolicy.Config) (Plan, error) {
	snapshot.Mode = request.Mode
	snapshot.RequestedOperationID = request.ModelOperationID
	snapshot.RequiredCapabilities = slices.Clone(request.RequiredCapabilities)
	profile, ok := modelprofile.Lookup(snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion)
	if !ok {
		return Plan{}, fmt.Errorf("%w: unknown model execution profile %s@%s", ErrManifestStale, snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion)
	}
	if profile.ID != modelprofile.UnverifiedProfileID {
		if err := modelprofile.ValidateEffectiveLimits(profile, snapshot.PublicContextLimit, snapshot.PublicOutputLimit); err != nil {
			return Plan{}, fmt.Errorf("%w: model execution profile limits: %v", ErrManifestStale, err)
		}
	}
	hash, err := snapshotDigest(snapshot)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: hash route snapshot: %v", ErrUnavailable, err)
	}
	base := planMetadata(snapshot, profile, hash)
	if modelqualification.IsQualificationRequired(request.ModelOperationID) && request.Mode != ModeCanonical {
		base.Exclusions = []Exclusion{{Reason: "public_execution_requires_canonical_authority"}}
		return base, ErrNoRoute
	}

	switch request.Mode {
	case ModeLegacy:
		routes, exclusions, buildErr := evaluateLegacy(snapshot, policy, request.SelectionKey, hash)
		base.Routes, base.Exclusions = routes, exclusions
		if buildErr != nil {
			return base, buildErr
		}
	case ModeVerify:
		legacyRoutes, legacyExclusions, legacyErr := evaluateLegacyForVerify(snapshot, policy, request.SelectionKey, hash)
		if legacyErr != nil && !errors.Is(legacyErr, ErrNoRoute) {
			return base, legacyErr
		}
		var canonicalRoutes []Route
		var canonicalExclusions []Exclusion
		var canonicalErr error
		switch snapshot.Slot.Selected.CandidateVersion {
		case legacyCandidateVersion:
			canonicalRoutes, canonicalExclusions, canonicalErr = evaluateCompatibility(snapshot, policy, request.SelectionKey, hash)
		case canonicalCandidateVersion:
			canonicalRoutes, canonicalExclusions, canonicalErr = evaluateCanonicalV2Compatibility(snapshot, policy, request.SelectionKey, hash)
		default:
			return base, fmt.Errorf("%w: selected runtime release", ErrManifestStale)
		}
		if canonicalErr != nil && !errors.Is(canonicalErr, ErrNoRoute) {
			return base, canonicalErr
		}
		drifts := compareVerifyPlans(snapshot, legacyRoutes, canonicalRoutes)
		if (legacyErr == nil) != (canonicalErr == nil) {
			drifts = append(drifts, Drift{Kind: "route_availability", Detail: "legacy and canonical route availability differs"})
		}
		if len(drifts) != 0 {
			base.Exclusions = append(legacyExclusions, canonicalExclusions...)
			base.Routes = nil
			return base, &ParityError{Drifts: drifts}
		}
		base.Routes, base.Exclusions = canonicalRoutes, canonicalExclusions
		if canonicalErr != nil {
			return base, canonicalErr
		}
	case ModeCanonical:
		routes, exclusions, buildErr := evaluateCanonical(snapshot, policy, request.SelectionKey, hash)
		base.Routes, base.Exclusions = routes, exclusions
		if buildErr != nil {
			return base, buildErr
		}
	default:
		return Plan{}, ErrInvalidMode
	}
	if len(base.Routes) == 0 {
		return base, ErrNoRoute
	}
	return base, nil
}

func normalizeRequest(request Request) (Request, error) {
	switch request.Mode {
	case ModeLegacy, ModeVerify, ModeCanonical:
	default:
		return Request{}, fmt.Errorf("%w: %q", ErrInvalidMode, request.Mode)
	}
	request.PublicModel = strings.TrimSpace(request.PublicModel)
	request.ModelOperationID = strings.TrimSpace(request.ModelOperationID)
	request.SelectionKey = strings.TrimSpace(request.SelectionKey)
	if request.CustomerOrgID <= 0 || request.PublicModel == "" || len(request.PublicModel) > 160 ||
		request.ModelOperationID == "" || len(request.ModelOperationID) > 80 || request.SelectionKey == "" ||
		len(request.SelectionKey) > 256 || len(request.RequiredCapabilities) > 64 ||
		strings.ContainsAny(request.PublicModel+request.ModelOperationID+request.SelectionKey, "\x00\r\n") {
		return Request{}, ErrInvalidRequest
	}
	if _, exists := modeloperation.LookupID(modeloperation.ID(request.ModelOperationID)); !exists {
		return Request{}, fmt.Errorf("%w: unknown model operation %q", ErrInvalidRequest, request.ModelOperationID)
	}
	capabilities := make([]string, 0, len(request.RequiredCapabilities))
	seen := make(map[string]struct{}, len(request.RequiredCapabilities))
	for _, raw := range request.RequiredCapabilities {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > 80 || strings.ContainsAny(value, "\x00\r\n") {
			return Request{}, fmt.Errorf("%w: required capability", ErrInvalidRequest)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		capabilities = append(capabilities, value)
	}
	slices.Sort(capabilities)
	request.RequiredCapabilities = capabilities
	return request, nil
}

func planMetadata(value snapshot, profile modelprofile.Profile, hash string) Plan {
	selected := value.Slot.Selected
	return Plan{
		AuthorityMode:        value.Mode,
		PublicModelID:        value.PublicModelID,
		PublicModel:          value.PublicModel,
		ModelOperationID:     value.RequestedOperationID,
		RequiredCapabilities: slices.Clone(value.RequiredCapabilities),
		PublicContextLimit:   value.PublicContextLimit,
		PublicOutputLimit:    value.PublicOutputLimit,
		ExecutionProfileID:   value.ExecutionProfileID, ExecutionProfileVersion: value.ExecutionProfileVersion,
		TokenizerProfileID: profile.TokenizerProfileID, TokenizerProfileVersion: profile.TokenizerProfileVersion,
		ReasoningProfile: profile.ReasoningProfile, CacheUsageProfile: profile.CacheUsageProfile,
		ModelProtocolProfile: profile.ProtocolProfile,
		SnapshotHash:         hash,
		DatabaseTime:         value.DBNow,
		RuntimeReleaseID:     selected.ID,
		RuntimeReleaseRef:    selected.Ref,
		RuntimeStage:         selected.Stage,
		SlotVersion:          value.Slot.Version,
	}
}
