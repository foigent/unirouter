package modelrouting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

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
	if !snapshot.CapabilitiesResolved {
		profile, ok := modelprofile.Lookup(snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion)
		if !ok {
			return Plan{}, fmt.Errorf("%w: unknown model execution profile %s@%s", ErrManifestStale, snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion)
		}
		requirements, deriveErr := ResolveCapabilityRequirements(profile, request)
		if deriveErr != nil {
			return Plan{}, deriveErr
		}
		snapshot.CapabilityRequirements = requirements
		snapshot.CapabilitiesResolved = true
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
	profile, ok := modelprofile.Lookup(snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion)
	if !ok {
		return Plan{}, fmt.Errorf("%w: unknown model execution profile %s@%s", ErrManifestStale, snapshot.ExecutionProfileID, snapshot.ExecutionProfileVersion)
	}
	if !snapshot.CapabilitiesResolved {
		return Plan{}, fmt.Errorf("%w: invocation capabilities were not resolved", ErrManifestStale)
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
		len(request.SelectionKey) > 256 || len(request.InvocationCapabilities) > 64 ||
		strings.ContainsAny(request.PublicModel+request.ModelOperationID+request.SelectionKey, "\x00\r\n") {
		return Request{}, ErrInvalidRequest
	}
	if _, exists := modeloperation.LookupID(modeloperation.ID(request.ModelOperationID)); !exists {
		return Request{}, fmt.Errorf("%w: unknown model operation %q", ErrInvalidRequest, request.ModelOperationID)
	}
	if len(request.InvocationCapabilities) != 0 && request.DeriveInvocationCapabilities != nil {
		return Request{}, fmt.Errorf("%w: invocation capabilities have multiple authorities", ErrInvalidRequest)
	}
	return request, nil
}

// ResolveCapabilityRequirements is the sole normalization authority for a
// Profile's always-required capabilities and the typed requirements derived
// from one invocation.
func ResolveCapabilityRequirements(profile modelprofile.Profile, request Request) ([]CapabilityRequirement, error) {
	invocation := request.InvocationCapabilities
	if request.DeriveInvocationCapabilities != nil {
		derived, err := request.DeriveInvocationCapabilities(profile)
		if err != nil {
			return nil, err
		}
		invocation = derived
	}
	if len(invocation) > 64 || len(profile.AlwaysRequiredCapabilities) > 64 {
		return nil, fmt.Errorf("%w: too many capability requirements", ErrInvalidRequest)
	}

	type normalizedRequirement struct {
		origins            map[string]struct{}
		functionToolChoice *FunctionToolChoiceConstraint
	}
	requirementsByCapability := make(map[string]*normalizedRequirement, len(invocation)+len(profile.AlwaysRequiredCapabilities))
	add := func(rawCapability, rawOrigin string, invocationOrigin bool, rawConstraint *FunctionToolChoiceConstraint) error {
		capability := strings.TrimSpace(rawCapability)
		origin := strings.TrimSpace(rawOrigin)
		if capability == "" || capability != rawCapability || len(capability) > 80 || strings.ContainsAny(capability, "\x00\r\n") {
			return fmt.Errorf("%w: required capability", ErrInvalidRequest)
		}
		if invocationOrigin && origin != CapabilityOriginExplicitRequest && origin != CapabilityOriginProviderDefault {
			return fmt.Errorf("%w: capability origin %q", ErrInvalidRequest, origin)
		}
		if !invocationOrigin && origin != CapabilityOriginProfileAlwaysRequired {
			return fmt.Errorf("%w: always-required capability origin", ErrInvalidRequest)
		}
		if profile.ID != modelprofile.UnverifiedProfileID && !slices.Contains(profile.Capabilities, capability) {
			return fmt.Errorf("%w: capability %q is not supported by %s@%s", ErrInvalidRequest, capability, profile.ID, profile.Version)
		}
		constraint, constraintErr := normalizeFunctionToolChoiceConstraint(capability, rawConstraint)
		if constraintErr != nil {
			return constraintErr
		}
		normalized := requirementsByCapability[capability]
		if normalized == nil {
			normalized = &normalizedRequirement{origins: make(map[string]struct{}), functionToolChoice: constraint}
			requirementsByCapability[capability] = normalized
		} else if !equalFunctionToolChoiceConstraint(normalized.functionToolChoice, constraint) {
			return fmt.Errorf("%w: conflicting capability constraint for %s", ErrInvalidRequest, capability)
		}
		normalized.origins[origin] = struct{}{}
		return nil
	}
	for _, requirement := range invocation {
		if len(requirement.Origins) == 0 || len(requirement.Origins) > 4 {
			return nil, fmt.Errorf("%w: capability requirement origins", ErrInvalidRequest)
		}
		for _, origin := range requirement.Origins {
			if err := add(requirement.CapabilityID, origin, true, requirement.FunctionToolChoice); err != nil {
				return nil, err
			}
		}
	}
	for _, capability := range profile.AlwaysRequiredCapabilities {
		if err := add(capability, CapabilityOriginProfileAlwaysRequired, false, nil); err != nil {
			return nil, err
		}
	}

	capabilityIDs := make([]string, 0, len(requirementsByCapability))
	for capability := range requirementsByCapability {
		capabilityIDs = append(capabilityIDs, capability)
	}
	slices.Sort(capabilityIDs)
	requirements := make([]CapabilityRequirement, 0, len(capabilityIDs))
	toolChoiceCount := 0
	for _, capability := range capabilityIDs {
		normalized := requirementsByCapability[capability]
		origins := make([]string, 0, len(normalized.origins))
		for origin := range normalized.origins {
			origins = append(origins, origin)
		}
		slices.Sort(origins)
		if normalized.functionToolChoice != nil {
			toolChoiceCount++
		}
		requirements = append(requirements, CapabilityRequirement{
			CapabilityID: capability, Origins: origins,
			FunctionToolChoice: cloneFunctionToolChoiceConstraint(normalized.functionToolChoice),
		})
	}
	if toolChoiceCount > 1 {
		return nil, fmt.Errorf("%w: multiple function tool choice constraints", ErrInvalidRequest)
	}
	return requirements, nil
}

func normalizeFunctionToolChoiceConstraint(capability string, raw *FunctionToolChoiceConstraint) (*FunctionToolChoiceConstraint, error) {
	isChoiceCapability := capability == modelprofile.CapabilityToolChoiceAuto ||
		capability == modelprofile.CapabilityToolChoiceReq || capability == modelprofile.CapabilityToolChoiceNamed
	if raw == nil {
		if isChoiceCapability {
			return nil, fmt.Errorf("%w: function tool choice constraint is required for %s", ErrInvalidRequest, capability)
		}
		return nil, nil
	}
	isNoneConstraint := capability == modelprofile.CapabilityFunctionTools
	if !isChoiceCapability && !isNoneConstraint {
		return nil, fmt.Errorf("%w: function tool choice constraint is invalid for %s", ErrInvalidRequest, capability)
	}
	if (!isNoneConstraint && len(raw.DeclaredFunctionNames) == 0) || len(raw.DeclaredFunctionNames) > 128 {
		return nil, fmt.Errorf("%w: declared function names", ErrInvalidRequest)
	}
	names := slices.Clone(raw.DeclaredFunctionNames)
	for _, name := range names {
		if name == "" || name != strings.TrimSpace(name) || len(name) > 128 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("%w: declared function name", ErrInvalidRequest)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	selected := strings.TrimSpace(raw.SelectedFunctionName)
	if raw.SelectedFunctionName != selected || len(selected) > 128 || strings.IndexFunc(selected, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("%w: selected function name", ErrInvalidRequest)
	}
	if capability == modelprofile.CapabilityToolChoiceNamed {
		if selected == "" || !slices.Contains(names, selected) {
			return nil, fmt.Errorf("%w: selected function must be declared", ErrInvalidRequest)
		}
	} else if selected != "" {
		return nil, fmt.Errorf("%w: selected function is valid only for named tool choice", ErrInvalidRequest)
	}
	return &FunctionToolChoiceConstraint{DeclaredFunctionNames: names, SelectedFunctionName: selected}, nil
}

func equalFunctionToolChoiceConstraint(left, right *FunctionToolChoiceConstraint) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.SelectedFunctionName == right.SelectedFunctionName && slices.Equal(left.DeclaredFunctionNames, right.DeclaredFunctionNames)
}

func cloneFunctionToolChoiceConstraint(value *FunctionToolChoiceConstraint) *FunctionToolChoiceConstraint {
	if value == nil {
		return nil
	}
	return &FunctionToolChoiceConstraint{
		DeclaredFunctionNames: slices.Clone(value.DeclaredFunctionNames),
		SelectedFunctionName:  value.SelectedFunctionName,
	}
}

func capabilityIDs(requirements []CapabilityRequirement) []string {
	ids := make([]string, len(requirements))
	for index := range requirements {
		ids[index] = requirements[index].CapabilityID
	}
	return ids
}

func cloneCapabilityRequirements(requirements []CapabilityRequirement) []CapabilityRequirement {
	cloned := make([]CapabilityRequirement, len(requirements))
	for index := range requirements {
		cloned[index] = CapabilityRequirement{
			CapabilityID:       requirements[index].CapabilityID,
			Origins:            slices.Clone(requirements[index].Origins),
			FunctionToolChoice: cloneFunctionToolChoiceConstraint(requirements[index].FunctionToolChoice),
		}
	}
	return cloned
}

func planMetadata(value snapshot, profile modelprofile.Profile, hash string) Plan {
	selected := value.Slot.Selected
	return Plan{
		AuthorityMode:             value.Mode,
		PublicModelID:             value.PublicModelID,
		PublicModel:               value.PublicModel,
		ModelOperationID:          value.RequestedOperationID,
		CapabilityRequirements:    cloneCapabilityRequirements(value.CapabilityRequirements),
		RequiredCapabilities:      capabilityIDs(value.CapabilityRequirements),
		PublicContextLimit:        value.PublicContextLimit,
		PublicOutputLimit:         value.PublicOutputLimit,
		RequestDefaultOutputLimit: modelprofile.DefaultRequestOutputLimit(profile, value.PublicOutputLimit),
		ExecutionProfileID:        value.ExecutionProfileID, ExecutionProfileVersion: value.ExecutionProfileVersion,
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
