package modelrouting

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type evaluatedCandidate struct {
	selectionID int64
	canonical   canonicalCandidate
	legacy      legacyCandidate
	bindings    []evaluatedBinding
	priority    int
	weight      int
	status      string
}

type evaluatedBinding struct {
	selectionID int64
	canonical   canonicalBinding
	legacy      legacyBinding
}

func evaluateLegacy(value snapshot, policy routingpolicy.Config, selectionKey, hash string) ([]Route, []Exclusion, error) {
	if value.Slot.Present && value.Slot.Selected.ID != 0 {
		if value.Slot.Selected.CandidateVersion != legacyCandidateVersion {
			return nil, nil, fmt.Errorf("%w: legacy mode cannot interpret a v2 runtime slot", ErrManifestStale)
		}
		if err := validateLegacyAuthority(value, value.Slot.Selected, false); err != nil {
			return nil, nil, err
		}
	}
	return evaluateLegacyRoutes(value, policy, selectionKey, hash)
}

func evaluateLegacyForVerify(value snapshot, policy routingpolicy.Config, selectionKey, hash string) ([]Route, []Exclusion, error) {
	if err := validateLegacyCompatibility(value); err != nil {
		return nil, nil, err
	}
	return evaluateLegacyRoutes(value, policy, selectionKey, hash)
}

func evaluateLegacyRoutes(value snapshot, policy routingpolicy.Config, selectionKey, hash string) ([]Route, []Exclusion, error) {
	candidates, err := effectiveLegacyAuthorityCandidates(value)
	if err != nil {
		return nil, nil, err
	}
	selection := routingpolicy.SelectionSnapshot{Model: value.PublicModel, DBNow: value.DBNow}
	byID := make(map[int64]legacyCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
		keys := make([]routingpolicy.SelectionKey, 0, len(candidate.Bindings))
		for _, key := range candidate.Bindings {
			keys = append(keys, routingpolicy.SelectionKey{ID: key.ID, Status: key.Status, CooldownUntil: key.CooldownUntil, AllowedModels: key.AllowedModels})
		}
		selection.Channels = append(selection.Channels, routingpolicy.SelectionChannel{
			ID: candidate.ID, Name: candidate.Name, Provider: candidate.Provider,
			Priority: candidate.Priority, Weight: candidate.Weight, Status: candidate.Status,
			CircuitState: candidate.CircuitState, HalfOpenUntil: candidate.HalfOpenUntil, Keys: keys,
		})
	}
	evaluation := routingpolicy.EvaluateSelection(selection, policy, selectionKey)
	routes := make([]Route, 0, len(evaluation.Selected))
	canonicalByLegacy := canonicalByLegacyID(value.Canonical)
	for _, selected := range evaluation.Selected {
		candidate := byID[selected.ChannelID]
		key := legacyKeyByID(candidate.Bindings, selected.KeyID)
		canonical := canonicalByLegacy[candidate.ID]
		binding := canonicalBindingByLegacyID(canonical.Bindings, key.ID)
		routes = append(routes, Route{
			ConnectionName: candidate.Name,
			Provider:       candidate.Provider, BaseURL: candidate.BaseURL, AdapterType: candidate.AdapterType,
			AdapterConfig: slices.Clone(candidate.AdapterConfig), UpstreamModel: candidate.UpstreamModel,
			ProtocolProfileID: canonical.ProtocolProfileID, ProtocolProfileVersion: canonical.ProtocolProfileVersion,
			MaxContext: candidate.MaxContext, DefaultMaxOutput: candidate.DefaultMaxOutput,
			EncryptedSecret: key.Encrypted, Priority: candidate.Priority, Weight: candidate.Weight,
			SelectionReason: selected.Preview.Reason, RequiresProbe: selected.RequiresProbe,
			Evidence: evidenceFor(value, hash, selected.RequiresProbe, candidate, key, canonical, binding),
		})
	}
	exclusions := policyExclusions(evaluation.Exclusions)
	if len(routes) == 0 {
		return nil, exclusions, ErrNoRoute
	}
	return routes, exclusions, nil
}

func evaluateCompatibility(value snapshot, policy routingpolicy.Config, selectionKey, hash string) ([]Route, []Exclusion, error) {
	if err := validateSelectedManifest(value, legacyCandidateVersion); err != nil {
		return nil, nil, err
	}
	legacy, err := effectiveLegacyAuthorityCandidates(value)
	if err != nil {
		return nil, nil, err
	}
	legacyByID := make(map[int64]legacyCandidate, len(legacy))
	for _, candidate := range legacy {
		legacyByID[candidate.ID] = candidate
	}

	candidates := make([]evaluatedCandidate, 0, len(value.Canonical))
	exclusions := make([]Exclusion, 0)
	for _, candidate := range value.Canonical {
		legacyCandidate, mapped := legacyByID[candidate.LegacyChannelID]
		if !mapped || candidate.LegacyChannelID <= 0 || candidate.LegacyPublicModel != value.PublicModel {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: "legacy_provenance_missing"})
			continue
		}
		eligible, reason := canonicalEligibility(value, candidate, legacyCompatibilityOp, false)
		if !eligible {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: reason})
			continue
		}
		eligibleBindings := eligibleCanonicalBindings(value, candidate)
		bindings := make([]evaluatedBinding, 0, len(eligibleBindings))
		for _, binding := range eligibleBindings {
			if binding.LegacyKeyID <= 0 {
				continue
			}
			legacyKey := legacyKeyByID(legacyCandidate.Bindings, binding.LegacyKeyID)
			bindings = append(bindings, evaluatedBinding{selectionID: binding.LegacyKeyID, canonical: binding, legacy: legacyKey})
		}
		if len(bindings) == 0 {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: bindingExclusionReason(value, candidate)})
			continue
		}
		candidates = append(candidates, evaluatedCandidate{
			selectionID: candidate.LegacyChannelID, canonical: candidate, legacy: legacyCandidate,
			bindings: bindings, priority: legacyCandidate.Priority, weight: legacyCandidate.Weight,
			status: legacyCandidate.Status,
		})
	}
	return evaluateCanonicalCandidates(value, candidates, policy, selectionKey, hash, exclusions)
}

func evaluateCanonical(value snapshot, policy routingpolicy.Config, selectionKey, hash string) ([]Route, []Exclusion, error) {
	if err := validateSelectedManifest(value, canonicalCandidateVersion); err != nil {
		return nil, nil, err
	}
	candidates := make([]evaluatedCandidate, 0, len(value.Canonical))
	exclusions := make([]Exclusion, 0)
	for _, candidate := range value.Canonical {
		if candidate.Manifest == nil || candidate.Manifest.Admission != routingpolicy.AdmissionAllow ||
			candidate.Manifest.ModelOperationID != value.RequestedOperationID {
			continue
		}
		eligible, reason := canonicalEligibility(value, candidate, value.RequestedOperationID, true)
		if !eligible {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: reason})
			continue
		}
		eligibleBindings := eligibleCanonicalBindings(value, candidate)
		bindings := make([]evaluatedBinding, 0, len(eligibleBindings))
		for _, binding := range eligibleBindings {
			bindings = append(bindings, evaluatedBinding{selectionID: binding.ID, canonical: binding})
		}
		if len(bindings) == 0 {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: bindingExclusionReason(value, candidate)})
			continue
		}
		priority, weight := candidate.Priority, candidate.Weight
		if candidate.Manifest.Priority != nil {
			priority = *candidate.Manifest.Priority
		}
		if candidate.Manifest.Weight != nil {
			weight = *candidate.Manifest.Weight
		}
		candidates = append(candidates, evaluatedCandidate{
			selectionID: candidate.DeploymentID, canonical: candidate, bindings: bindings,
			priority: priority, weight: weight, status: "active",
		})
	}
	return evaluateCanonicalCandidates(value, candidates, policy, selectionKey, hash, exclusions)
}

// evaluateCanonicalV2Compatibility executes the sealed v2 manifest with the
// legacy provenance identities as selection salts. This is only for verify
// mode: it proves the post-cutover v2 slot against the exact sealed v1 parent
// without changing production selection until the process is explicitly
// restarted in canonical mode.
func evaluateCanonicalV2Compatibility(value snapshot, policy routingpolicy.Config, selectionKey, hash string) ([]Route, []Exclusion, error) {
	if err := validateSelectedManifest(value, canonicalCandidateVersion); err != nil {
		return nil, nil, err
	}
	legacy, err := effectiveLegacyAuthorityCandidates(value)
	if err != nil {
		return nil, nil, err
	}
	legacyByID := make(map[int64]legacyCandidate, len(legacy))
	for _, candidate := range legacy {
		legacyByID[candidate.ID] = candidate
	}
	candidates := make([]evaluatedCandidate, 0, len(value.Canonical))
	exclusions := make([]Exclusion, 0)
	for _, candidate := range value.Canonical {
		if candidate.Manifest == nil || candidate.Manifest.Admission != routingpolicy.AdmissionAllow ||
			candidate.Manifest.ModelOperationID != value.RequestedOperationID {
			continue
		}
		legacyCandidate, mapped := legacyByID[candidate.LegacyChannelID]
		if !mapped || candidate.LegacyChannelID <= 0 || candidate.LegacyPublicModel != value.PublicModel {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: "legacy_provenance_missing"})
			continue
		}
		eligible, reason := canonicalEligibility(value, candidate, value.RequestedOperationID, true)
		if !eligible {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: reason})
			continue
		}
		bindings := make([]evaluatedBinding, 0, len(candidate.Bindings))
		for _, binding := range eligibleCanonicalBindings(value, candidate) {
			if binding.LegacyKeyID <= 0 {
				continue
			}
			legacyKey := legacyKeyByID(legacyCandidate.Bindings, binding.LegacyKeyID)
			if legacyKey.ID == 0 {
				continue
			}
			bindings = append(bindings, evaluatedBinding{selectionID: binding.LegacyKeyID, canonical: binding, legacy: legacyKey})
		}
		if len(bindings) == 0 {
			exclusions = append(exclusions, Exclusion{DeploymentID: candidate.DeploymentID, LegacyChannelID: candidate.LegacyChannelID, Reason: bindingExclusionReason(value, candidate)})
			continue
		}
		priority, weight := candidate.Priority, candidate.Weight
		if candidate.Manifest.Priority != nil {
			priority = *candidate.Manifest.Priority
		}
		if candidate.Manifest.Weight != nil {
			weight = *candidate.Manifest.Weight
		}
		candidates = append(candidates, evaluatedCandidate{
			selectionID: candidate.LegacyChannelID, canonical: candidate, legacy: legacyCandidate,
			bindings: bindings, priority: priority, weight: weight, status: "active",
		})
	}
	return evaluateCanonicalCandidates(value, candidates, policy, selectionKey, hash, exclusions)
}

func evaluateCanonicalCandidates(value snapshot, candidates []evaluatedCandidate, policy routingpolicy.Config, selectionKey, hash string, exclusions []Exclusion) ([]Route, []Exclusion, error) {
	selection := routingpolicy.SelectionSnapshot{Model: value.PublicModel, DBNow: value.DBNow}
	byID := make(map[int64]evaluatedCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.selectionID] = candidate
		keys := make([]routingpolicy.SelectionKey, 0, len(candidate.bindings))
		for _, binding := range candidate.bindings {
			keys = append(keys, routingpolicy.SelectionKey{
				ID: binding.selectionID, Status: binding.canonical.Status,
				CooldownUntil: binding.canonical.CooldownUntil,
			})
		}
		selection.Channels = append(selection.Channels, routingpolicy.SelectionChannel{
			ID: candidate.selectionID, Name: candidate.canonical.Name, Provider: candidate.canonical.Provider,
			Priority: candidate.priority, Weight: candidate.weight, Status: candidate.status,
			CircuitState: candidate.canonical.CircuitState, HalfOpenUntil: candidate.canonical.HalfOpenUntil,
			Keys: keys,
		})
	}
	evaluation := routingpolicy.EvaluateSelection(selection, policy, selectionKey)
	for _, item := range evaluation.Exclusions {
		candidate := byID[item.ChannelID]
		exclusions = append(exclusions, Exclusion{DeploymentID: candidate.canonical.DeploymentID, LegacyChannelID: candidate.canonical.LegacyChannelID, Reason: item.Reason})
	}
	routes := make([]Route, 0, len(evaluation.Selected))
	for _, selected := range evaluation.Selected {
		candidate := byID[selected.ChannelID]
		binding := evaluatedBindingByID(candidate.bindings, selected.KeyID)
		route := Route{
			ConnectionName: candidate.canonical.Name,
			Provider:       candidate.canonical.Provider, BaseURL: candidate.canonical.BaseURL,
			AdapterType: candidate.canonical.AdapterType, AdapterConfig: slices.Clone(candidate.canonical.AdapterConfig),
			ProtocolProfileID: candidate.canonical.ProtocolProfileID, ProtocolProfileVersion: candidate.canonical.ProtocolProfileVersion,
			PublicPath:              candidate.canonical.PublicPath,
			ExecutorContractVersion: candidate.canonical.ExecutorContractVersion,
			ProductionApprovalID:    candidate.canonical.ProductionApprovalID,
			MaxContext:              candidate.canonical.MaxContext, DefaultMaxOutput: candidate.canonical.DefaultMaxOutput,
			UpstreamModel: candidate.canonical.UpstreamModel, EncryptedSecret: binding.canonical.Encrypted,
			Priority: candidate.priority, Weight: candidate.weight,
			SelectionReason: selected.Preview.Reason, RequiresProbe: selected.RequiresProbe,
		}
		baseEvidence := evidenceFor(
			value, hash, selected.RequiresProbe, candidate.legacy, binding.legacy,
			candidate.canonical, binding.canonical,
		)
		if modelqualification.IsQualificationRequired(candidate.canonical.OperationID) {
			public := executionevidence.EvidenceV2{
				EvidenceV1:                 baseEvidence,
				PublicExecutionBindingID:   candidate.canonical.PublicExecutionBindingID,
				DeliveryContractVersion:    candidate.canonical.DeliveryContractVersion,
				ChargeContractVersion:      candidate.canonical.ChargeContractVersion,
				IdempotencyContractVersion: candidate.canonical.IdempotencyContractVersion,
			}
			public.ContractVersion = executionevidence.ContractVersionV2
			route.PublicEvidence = &public
		} else {
			route.Evidence = baseEvidence
		}
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		return nil, exclusions, ErrNoRoute
	}
	return routes, exclusions, nil
}

func canonicalEligibility(value snapshot, candidate canonicalCandidate, operation string, production bool) (bool, string) {
	if candidate.ConnectionState == "retired" || candidate.DeploymentState == "retired" {
		return false, "retired"
	}
	if candidate.ConnectionState != "enabled" || candidate.DeploymentState != "enabled" {
		return false, "administratively_disabled"
	}
	if candidate.MaxContext <= 0 || candidate.DefaultMaxOutput <= 0 ||
		value.PublicContextLimit <= 0 || value.PublicOutputLimit <= 0 {
		return false, "execution_limits_missing"
	}
	if candidate.Manifest != nil {
		if candidate.ConnectionRevision != candidate.Manifest.ExpectedConnectionRevision ||
			candidate.DeploymentRevision != candidate.Manifest.ExpectedDeploymentRevision ||
			candidate.OperationRevision != candidate.Manifest.ExpectedOperationRevision ||
			candidate.ProtocolProfileID != candidate.Manifest.ProtocolProfileID ||
			candidate.ProtocolProfileVersion != candidate.Manifest.ProtocolProfileVersion ||
			candidate.PublicPath != candidate.Manifest.PublicPath ||
			candidate.ExecutorContractVersion != candidate.Manifest.ExecutorContractVersion {
			return false, "stale_revision"
		}
	}
	if !candidate.OperationPresent || candidate.OperationID != operation || candidate.OperationSupportState != "enabled" {
		return false, "unsupported_operation"
	}
	if candidate.ProtocolProfileID == "" || candidate.ProtocolProfileVersion == "" {
		return false, "protocol_profile_missing"
	}
	if modelqualification.IsQualificationRequired(operation) {
		binding := modelqualification.Binding{
			OperationID: operation, PublicPath: candidate.PublicPath,
			ProtocolProfileID:       candidate.ProtocolProfileID,
			ProtocolProfileVersion:  candidate.ProtocolProfileVersion,
			ExecutorContractVersion: candidate.ExecutorContractVersion,
		}
		if modelqualification.ValidateCompiledBinding(binding) != nil {
			return false, "execution_support_binding_mismatch"
		}
		if !candidate.ProductionQualified || candidate.ProductionApprovalID <= 0 ||
			candidate.QualificationState != "production_qualified" {
			return false, "production_qualification_missing"
		}
		if value.Mode != ModeCanonical {
			return false, "public_execution_requires_canonical_authority"
		}
		if candidate.Manifest == nil || candidate.Manifest.PublicExecutionBindingID <= 0 ||
			candidate.Manifest.DeliveryContractVersion == "" ||
			candidate.Manifest.ChargeContractVersion == "" ||
			candidate.Manifest.IdempotencyContractVersion == "" {
			return false, "public_execution_binding_missing"
		}
		compiled, err := modelqualification.CompiledPublicBinding(operation)
		if err != nil || candidate.Manifest.PublicPath != compiled.PublicPath ||
			candidate.Manifest.ProtocolProfileID != compiled.ProfileID ||
			candidate.Manifest.ProtocolProfileVersion != compiled.ProfileVersion ||
			candidate.Manifest.ExecutorContractVersion != compiled.ExecutorContractVersion ||
			candidate.Manifest.DeliveryContractVersion != compiled.DeliveryContractVersion ||
			candidate.Manifest.ChargeContractVersion != compiled.ChargeContractVersion ||
			candidate.Manifest.IdempotencyContractVersion != compiled.IdempotencyContractVersion {
			return false, "public_execution_contract_mismatch"
		}
		if !candidate.PublicExecutionQualified || candidate.PublicQualificationState != "public_executable" ||
			candidate.PublicExecutionBindingID != candidate.Manifest.PublicExecutionBindingID ||
			candidate.DeliveryContractVersion != candidate.Manifest.DeliveryContractVersion ||
			candidate.ChargeContractVersion != candidate.Manifest.ChargeContractVersion ||
			candidate.IdempotencyContractVersion != candidate.Manifest.IdempotencyContractVersion {
			return false, "public_execution_qualification_stale"
		}
	}
	if production {
		if !operationEvidenceAdmitsProduction(operation, candidate.OperationEvidenceState) ||
			candidate.OperationVerifiedAt == nil {
			return false, "unverified_operation"
		}
	}
	for _, required := range value.RequiredCapabilities {
		capability, exists := candidate.Capabilities[required]
		if !exists || capability.SupportState != "enabled" || capability.EvidenceState != "production_approved" || capability.VerifiedAt == nil {
			return false, "unsupported_capability:" + required
		}
	}
	return true, ""
}

// New public protocol families remain explicit-only until their complete
// execution, billing and error-envelope contracts have passed production
// approval. A successful provider probe proves connectivity, not that those
// contracts are safe to expose to customer traffic.
func operationEvidenceAdmitsProduction(operation, evidenceState string) bool {
	switch modeloperation.ID(operation) {
	case modeloperation.IDResponses, modeloperation.IDMessages:
		return evidenceState == "production_approved"
	default:
		switch evidenceState {
		case "live_probed", "conformance_verified", "production_approved":
			return true
		default:
			return false
		}
	}
}

func eligibleCanonicalBindings(value snapshot, candidate canonicalCandidate) []canonicalBinding {
	active := activeCanonicalBindings(candidate.Bindings, value.DBNow)
	result := make([]canonicalBinding, 0, len(active))
	for _, binding := range active {
		probe := binding.Probe
		if binding.Encrypted == "" || binding.LinkedAt.IsZero() || !probe.Present || !probe.Success ||
			probe.CredentialBindingID != binding.ID || probe.CredentialStateRevision != binding.StateRevision ||
			probe.UpstreamModel != candidate.UpstreamModel || probe.ConnectionRevision != candidate.ConnectionRevision ||
			probe.DeploymentRevision != candidate.DeploymentRevision || probe.ProfileID != candidate.ProtocolProfileID ||
			probe.ProfileVersion != candidate.ProtocolProfileVersion || probe.TestedAt.Before(binding.LinkedAt) {
			continue
		}
		if modelqualification.IsQualificationRequired(candidate.OperationID) {
			origin, err := modelqualification.NormalizeHTTPSOrigin(candidate.BaseURL)
			if err != nil || probe.OperationRevision != candidate.OperationRevision ||
				probe.PublicPath != candidate.PublicPath ||
				probe.ExecutorContractVersion != candidate.ExecutorContractVersion ||
				probe.HTTPSOrigin != origin || probe.SourceKind != modelqualification.LiveProbeSourceKind {
				continue
			}
		}
		result = append(result, binding)
	}
	return result
}

func bindingExclusionReason(value snapshot, candidate canonicalCandidate) string {
	if len(candidate.Bindings) == 0 {
		return "explicit_binding_missing"
	}
	for _, binding := range activeCanonicalBindings(candidate.Bindings, value.DBNow) {
		if binding.Encrypted == "" {
			return "credential_secret_missing"
		}
		return "binding_probe_missing"
	}
	return "credential_unavailable"
}

func validateSelectedManifest(value snapshot, expectedVersion string) error {
	selected := value.Slot.Selected
	if !value.Slot.Present || selected.ID <= 0 {
		return fmt.Errorf("%w: runtime slot or release missing", ErrManifestStale)
	}
	if selected.CandidateVersion != expectedVersion || selected.PublicModelID != value.PublicModelID {
		return fmt.Errorf("%w: release identity", ErrManifestStale)
	}
	if (expectedVersion == canonicalCandidateVersion && value.Slot.PublicModelID != value.PublicModelID) ||
		(expectedVersion == legacyCandidateVersion && value.Slot.PublicModelID != 0) {
		return fmt.Errorf("%w: slot authority identity", ErrManifestStale)
	}
	if selected.RouteItemsSealed == nil || selected.RouteItemsHash == "" || selected.RouteItemsCount != len(value.Manifest) {
		return ErrManifestUnsealed
	}
	if selected.Stage != "active" && selected.Stage != "canary" {
		return fmt.Errorf("%w: release stage", ErrManifestStale)
	}
	if got := manifestDigest(value.Manifest); got != selected.RouteItemsHash {
		return fmt.Errorf("%w: route item hash", ErrManifestStale)
	}
	for _, item := range value.Manifest {
		if item.SourceCandidateVersion != expectedVersion {
			return fmt.Errorf("%w: route item version", ErrManifestStale)
		}
		if expectedVersion == canonicalCandidateVersion {
			if modelqualification.IsQualificationRequired(item.ModelOperationID) {
				if modelqualification.ValidateCompiledBinding(modelqualification.Binding{
					OperationID: item.ModelOperationID, PublicPath: item.PublicPath,
					ProtocolProfileID:       item.ProtocolProfileID,
					ProtocolProfileVersion:  item.ProtocolProfileVersion,
					ExecutorContractVersion: item.ExecutorContractVersion,
				}) != nil {
					return fmt.Errorf("%w: route execution support binding", ErrManifestStale)
				}
			} else if item.PublicPath != "" || item.ExecutorContractVersion != "" ||
				item.PublicExecutionBindingID != 0 || item.DeliveryContractVersion != "" ||
				item.ChargeContractVersion != "" || item.IdempotencyContractVersion != "" {
				return fmt.Errorf("%w: unexpected route execution support binding", ErrManifestStale)
			}
		}
	}
	return nil
}

func validateLegacyCompatibility(value snapshot) error {
	release := value.LegacyRelease
	if release.ID <= 0 {
		return fmt.Errorf("%w: sealed v1 compatibility release missing", ErrManifestStale)
	}
	allowRolledBack := value.Slot.Selected.CandidateVersion == canonicalCandidateVersion
	if err := validateLegacyAuthority(value, release, allowRolledBack); err != nil {
		return err
	}
	if release.PublicModelID != value.PublicModelID || release.RouteItemsSealed == nil ||
		release.RouteItemsHash == "" || release.RouteItemsCount != len(value.LegacyManifest) {
		return ErrManifestUnsealed
	}
	if manifestDigest(value.LegacyManifest) != release.RouteItemsHash {
		return fmt.Errorf("%w: legacy route item hash", ErrManifestStale)
	}
	for _, item := range value.LegacyManifest {
		if item.SourceCandidateVersion != legacyCandidateVersion || item.ModelOperationID != legacyCompatibilityOp ||
			item.ExpectedOperationRevision <= 0 || item.ProtocolProfileID != "legacy.channel_model_probe" ||
			item.ProtocolProfileVersion != "v1" || item.PublicPath != "" || item.ExecutorContractVersion != "" {
			return fmt.Errorf("%w: legacy compatibility route item", ErrManifestStale)
		}
	}
	return nil
}

func validateLegacyAuthority(value snapshot, selected releaseSnapshot, allowRolledBack bool) error {
	if selected.ID <= 0 {
		return nil
	}
	validStage := selected.Stage == "active" || selected.Stage == "canary" || (allowRolledBack && selected.Stage == "rolled_back")
	if selected.CandidateVersion != legacyCandidateVersion || !validStage {
		return fmt.Errorf("%w: legacy release identity", ErrManifestStale)
	}
	var candidate struct {
		ContractVersion string           `json:"contract_version"`
		ModelName       string           `json:"model_name"`
		Overrides       []legacyOverride `json:"overrides"`
	}
	if len(selected.CandidateJSON) == 0 || json.Unmarshal(selected.CandidateJSON, &candidate) != nil ||
		candidate.ContractVersion != legacyCandidateVersion || candidate.ModelName != value.PublicModel ||
		legacyCandidateDigest(candidate.ContractVersion, candidate.ModelName, candidate.Overrides) != selected.CandidateHash {
		return fmt.Errorf("%w: corrupt legacy candidate", ErrManifestStale)
	}
	normalized, err := normalizeLegacyOverrides(candidate.Overrides)
	if err != nil || !reflect.DeepEqual(normalized, value.LegacyOverrides) {
		return fmt.Errorf("%w: invalid legacy overrides", ErrManifestStale)
	}
	return nil
}

func effectiveLegacyAuthorityCandidates(value snapshot) ([]legacyCandidate, error) {
	result := cloneLegacyCandidates(value.Legacy)
	if value.LegacyRelease.ID == 0 && (!value.Slot.Present || value.Slot.Selected.ID == 0) {
		return result, nil
	}
	byID := make(map[int64]int, len(result))
	for index := range result {
		byID[result[index].ID] = index
	}
	for _, override := range value.LegacyOverrides {
		position, exists := byID[override.ChannelID]
		if !exists {
			if override.Admission == routingpolicy.AdmissionDeny {
				continue
			}
			return nil, fmt.Errorf("%w: legacy channel absent", ErrManifestStale)
		}
		candidate := &result[position]
		if candidate.RoutingRevision != override.ExpectedRoutingEpoch {
			return nil, fmt.Errorf("%w: legacy routing revision", ErrManifestStale)
		}
		switch override.Admission {
		case routingpolicy.AdmissionAllow:
			if candidate.Status != "disabled" {
				candidate.Status = "active"
			}
		case routingpolicy.AdmissionDeny:
			candidate.Status = "disabled"
		case routingpolicy.AdmissionInherit:
		default:
			return nil, fmt.Errorf("%w: legacy admission", ErrManifestStale)
		}
		if override.Priority != nil {
			candidate.Priority = *override.Priority
		}
		if override.Weight != nil {
			candidate.Weight = *override.Weight
		}
	}
	return result, nil
}

func normalizeLegacyOverrides(input []legacyOverride) ([]legacyOverride, error) {
	converted := make([]routingpolicy.ChannelOverride, 0, len(input))
	for _, item := range input {
		converted = append(converted, routingpolicy.ChannelOverride{
			ChannelID: item.ChannelID, ExpectedRoutingEpoch: item.ExpectedRoutingEpoch,
			Admission: item.Admission, Priority: item.Priority, Weight: item.Weight,
		})
	}
	normalized, err := routingpolicy.ValidateOverrides(converted)
	if err != nil {
		return nil, err
	}
	result := make([]legacyOverride, 0, len(normalized))
	for _, item := range normalized {
		result = append(result, legacyOverride{
			ChannelID: item.ChannelID, ExpectedRoutingEpoch: item.ExpectedRoutingEpoch,
			Admission: item.Admission, Priority: item.Priority, Weight: item.Weight,
		})
	}
	return result, nil
}

func compareVerifyPlans(value snapshot, legacyRoutes, canonicalRoutes []Route) []Drift {
	drifts := compareProjectionInventory(value)
	if len(legacyRoutes) != len(canonicalRoutes) {
		drifts = append(drifts, Drift{Kind: "candidate_set", Detail: "eligible candidate count differs"})
		return drifts
	}
	for index := range legacyRoutes {
		legacy, canonical := legacyRoutes[index], canonicalRoutes[index]
		channelID := legacy.Evidence.LegacyChannelID
		switch {
		case channelID != canonical.Evidence.LegacyChannelID:
			drifts = append(drifts, Drift{Kind: "candidate_order", LegacyChannelID: channelID, Detail: "ordered candidate identity differs"})
		case legacy.UpstreamModel != canonical.UpstreamModel || legacy.BaseURL != canonical.BaseURL:
			drifts = append(drifts, Drift{Kind: "upstream", LegacyChannelID: channelID, Detail: "upstream identity differs"})
		case legacy.AdapterType != canonical.AdapterType || adapterDigest(legacy.AdapterConfig) != adapterDigest(canonical.AdapterConfig):
			drifts = append(drifts, Drift{Kind: "adapter", LegacyChannelID: channelID, Detail: "adapter profile differs"})
		case legacy.Priority != canonical.Priority || legacy.Weight != canonical.Weight:
			drifts = append(drifts, Drift{Kind: "routing_weight", LegacyChannelID: channelID, Detail: "priority or weight differs"})
		case legacy.MaxContext != canonical.MaxContext || legacy.DefaultMaxOutput != canonical.DefaultMaxOutput:
			drifts = append(drifts, Drift{Kind: "execution_limits", LegacyChannelID: channelID, Detail: "context or output limit differs"})
		case legacy.Evidence.LegacyChannelKeyID != canonical.Evidence.LegacyChannelKeyID:
			drifts = append(drifts, Drift{Kind: "credential_selection", LegacyChannelID: channelID, Detail: "selected credential provenance differs"})
		}
	}
	return drifts
}

func compareProjectionInventory(value snapshot) []Drift {
	drifts := make([]Drift, 0)
	canonical := canonicalByLegacyID(value.Canonical)
	seen := make(map[int64]struct{}, len(value.Legacy))
	for _, legacy := range value.Legacy {
		seen[legacy.ID] = struct{}{}
		candidate, exists := canonical[legacy.ID]
		if !exists || candidate.LegacyPublicModel != value.PublicModel {
			drifts = append(drifts, Drift{Kind: "candidate_mapping", LegacyChannelID: legacy.ID, Detail: "canonical deployment is missing"})
			continue
		}
		expectedState := "enabled"
		if legacy.Status == "disabled" {
			expectedState = "disabled"
		}
		switch {
		case candidate.ConnectionState != expectedState:
			drifts = append(drifts, Drift{Kind: "administrative_state", LegacyChannelID: legacy.ID, Detail: "connection administrative state differs"})
		case candidate.ConnectionRevision != legacy.RoutingRevision || candidate.DeploymentRevision != legacy.RoutingRevision:
			drifts = append(drifts, Drift{Kind: "configuration_revision", LegacyChannelID: legacy.ID, Detail: "configuration revision differs"})
		case candidate.Provider != legacy.Provider || candidate.BaseURL != legacy.BaseURL || candidate.UpstreamModel != legacy.UpstreamModel:
			drifts = append(drifts, Drift{Kind: "upstream", LegacyChannelID: legacy.ID, Detail: "provider or upstream differs"})
		case candidate.AdapterType != legacy.AdapterType || candidate.AdapterConfigDigest != legacy.AdapterConfigDigest:
			drifts = append(drifts, Drift{Kind: "adapter", LegacyChannelID: legacy.ID, Detail: "adapter profile differs"})
		case candidate.ProtocolProfileID == "" || candidate.ProtocolProfileVersion == "":
			drifts = append(drifts, Drift{Kind: "protocol_profile", LegacyChannelID: legacy.ID, Detail: "explicit protocol profile is missing"})
		case candidate.Priority != legacy.Priority || candidate.Weight != legacy.Weight:
			drifts = append(drifts, Drift{Kind: "routing_weight", LegacyChannelID: legacy.ID, Detail: "priority or weight differs"})
		case candidate.MaxContext != legacy.MaxContext || candidate.DefaultMaxOutput != legacy.DefaultMaxOutput:
			drifts = append(drifts, Drift{Kind: "execution_limits", LegacyChannelID: legacy.ID, Detail: "context or output limit differs"})
		case candidate.CircuitState != legacy.CircuitState || candidate.CircuitRevision != legacy.CircuitRevision ||
			candidate.FailCount != legacy.FailCount || candidate.DegradationThreshold != legacy.DegradationThreshold ||
			!sameTime(candidate.HalfOpenUntil, legacy.HalfOpenUntil) || !sameTime(candidate.HalfOpenProbeUntil, legacy.HalfOpenProbeUntil) ||
			candidate.HalfOpenProbeToken != legacy.HalfOpenProbeToken:
			drifts = append(drifts, Drift{Kind: "breaker", LegacyChannelID: legacy.ID, Detail: "circuit breaker state differs"})
		}
		for _, binding := range candidate.Bindings {
			if binding.Probe.Present && (candidate.ProtocolProfileID != binding.Probe.ProfileID || candidate.ProtocolProfileVersion != binding.Probe.ProfileVersion) {
				drifts = append(drifts, Drift{Kind: "protocol_profile", LegacyChannelID: legacy.ID, Detail: "operation and credential probe protocol profiles differ"})
				break
			}
		}
		drifts = append(drifts, compareCredentialInventory(value, legacy, candidate)...)
	}
	for _, candidate := range value.Canonical {
		if candidate.LegacyChannelID <= 0 || candidate.LegacyPublicModel != value.PublicModel {
			drifts = append(drifts, Drift{Kind: "candidate_mapping", LegacyChannelID: candidate.LegacyChannelID, Detail: "canonical-only deployment is not comparable in verify mode"})
			continue
		}
		if _, exists := seen[candidate.LegacyChannelID]; !exists {
			drifts = append(drifts, Drift{Kind: "candidate_mapping", LegacyChannelID: candidate.LegacyChannelID, Detail: "canonical deployment has no legacy candidate"})
		}
	}
	return drifts
}

func compareCredentialInventory(value snapshot, legacy legacyCandidate, canonical canonicalCandidate) []Drift {
	drifts := make([]Drift, 0)
	wanted := make(map[int64]legacyBinding, len(legacy.Bindings))
	for _, key := range legacy.Bindings {
		wanted[key.ID] = key
	}
	actual := make(map[int64]canonicalBinding)
	for _, binding := range canonical.Bindings {
		if binding.LegacyKeyID <= 0 {
			drifts = append(drifts, Drift{Kind: "credential_mapping", LegacyChannelID: legacy.ID, Detail: "canonical binding lacks legacy provenance"})
			continue
		}
		actual[binding.LegacyKeyID] = binding
	}
	if len(wanted) != len(actual) {
		drifts = append(drifts, Drift{Kind: "credential_mapping", LegacyChannelID: legacy.ID, Detail: "credential binding set differs"})
		return drifts
	}
	for id, key := range wanted {
		binding, exists := actual[id]
		if !exists || binding.Status != key.Status || binding.StateRevision != key.StateRevision ||
			!sameTime(binding.CooldownUntil, key.CooldownUntil) ||
			subtle.ConstantTimeCompare([]byte(binding.Encrypted), []byte(key.Encrypted)) != 1 {
			drifts = append(drifts, Drift{Kind: "credential_mapping", LegacyChannelID: legacy.ID, Detail: "credential state or secret binding differs"})
			break
		}
	}
	legacyEligible := make(map[int64]struct{})
	for _, key := range legacy.Bindings {
		if key.Status == "active" && (key.CooldownUntil == nil || !key.CooldownUntil.After(value.DBNow)) &&
			(key.AllowedModels == "" || modellist.Contains(key.AllowedModels, value.PublicModel)) {
			legacyEligible[key.ID] = struct{}{}
		}
	}
	canonicalEligible := make(map[int64]struct{})
	for _, binding := range eligibleCanonicalBindings(value, canonical) {
		if binding.LegacyKeyID > 0 {
			canonicalEligible[binding.LegacyKeyID] = struct{}{}
		}
	}
	if !reflect.DeepEqual(legacyEligible, canonicalEligible) {
		drifts = append(drifts, Drift{Kind: "credential_eligibility", LegacyChannelID: legacy.ID, Detail: "eligible credential set or probe authority differs"})
	}
	return drifts
}

func evidenceFor(
	value snapshot,
	hash string,
	requiresProbe bool,
	legacy legacyCandidate,
	legacyKey legacyBinding,
	candidate canonicalCandidate,
	binding canonicalBinding,
) RouteEvidence {
	selected := value.Slot.Selected
	legacyExecution := value.Mode == ModeLegacy ||
		(value.Mode == ModeVerify && selected.CandidateVersion == legacyCandidateVersion)
	operationID := candidate.OperationID
	operationRevision := candidate.OperationRevision
	operationEvidenceState := candidate.OperationEvidenceState
	operationEvidenceRef := candidate.OperationEvidenceRef
	operationVerifiedAt := candidate.OperationVerifiedAt
	protocolProfileID := candidate.ProtocolProfileID
	protocolProfileVersion := candidate.ProtocolProfileVersion
	capabilityEvidenceHash := capabilityEvidenceDigest(value.RequiredCapabilities, candidate.Capabilities)
	capabilityEvidence := capabilityEvidenceEntries(value.RequiredCapabilities, candidate.Capabilities)
	executionSource := executionevidence.ContractSourceCanonicalProfile
	if legacyExecution {
		executionSource = executionevidence.ContractSourceLegacyAdapter
		operationID = value.RequestedOperationID
		operationRevision = 0
		operationEvidenceState = ""
		operationEvidenceRef = ""
		operationVerifiedAt = nil
		protocolProfileID = ""
		protocolProfileVersion = ""
		capabilityEvidenceHash = ""
		capabilityEvidence = nil
	}
	if operationID == "" {
		operationID = value.RequestedOperationID
	}
	if value.Mode == ModeLegacy {
		return RouteEvidence{
			ContractVersion: executionevidence.ContractVersion, AuthorityMode: value.Mode,
			ExecutionContractSource: executionevidence.ContractSourceLegacyAdapter,
			ModelOperationID:        operationID, RequiresProbe: requiresProbe,
			LegacyChannelID: legacy.ID, LegacyChannelKeyID: legacyKey.ID,
			LegacyRoutingRevision: legacy.RoutingRevision, LegacyCircuitRevision: legacy.CircuitRevision,
			LegacyCredentialStateRevision: legacyKey.StateRevision,
		}
	}
	return RouteEvidence{
		ContractVersion: executionevidence.ContractVersion, ExecutionContractSource: executionSource,
		PublicModelID: value.PublicModelID, DeploymentID: candidate.DeploymentID,
		ConnectionID: candidate.ConnectionID, CredentialBindingID: binding.ID,
		ConnectionRevision: candidate.ConnectionRevision, DeploymentRevision: candidate.DeploymentRevision,
		CircuitRevision:         candidate.CircuitRevision,
		CredentialStateRevision: binding.StateRevision, CredentialLinkedAt: binding.LinkedAt,
		OperationRevision: operationRevision, OperationEvidenceState: operationEvidenceState,
		OperationEvidenceRef: operationEvidenceRef,
		OperationVerifiedAt:  operationVerifiedAt,
		ModelOperationID:     operationID, ProtocolProfileID: protocolProfileID,
		ProtocolProfileVersion: protocolProfileVersion,
		CapabilityEvidenceHash: capabilityEvidenceHash,
		CapabilityEvidence:     capabilityEvidence,
		CredentialProbeID:      binding.Probe.ID, CredentialProbeTestedAt: binding.Probe.TestedAt,
		RuntimeReleaseID: selected.ID, RuntimeReleaseRef: selected.Ref,
		RuntimeStage: selected.Stage, SlotVersion: value.Slot.Version, SnapshotHash: hash,
		AuthorityMode: value.Mode, LegacyChannelID: legacy.ID, LegacyChannelKeyID: legacyKey.ID,
		LegacyRoutingRevision: legacy.RoutingRevision, LegacyCircuitRevision: legacy.CircuitRevision,
		LegacyCredentialStateRevision: legacyKey.StateRevision, RequiresProbe: requiresProbe,
	}
}

func cloneLegacyCandidates(input []legacyCandidate) []legacyCandidate {
	result := slices.Clone(input)
	for index := range result {
		result[index].AdapterConfig = slices.Clone(result[index].AdapterConfig)
		result[index].Bindings = slices.Clone(result[index].Bindings)
	}
	return result
}

func activeCanonicalBindings(input []canonicalBinding, now time.Time) []canonicalBinding {
	result := make([]canonicalBinding, 0, len(input))
	for _, binding := range input {
		if binding.Status == "active" && (binding.CooldownUntil == nil || !binding.CooldownUntil.After(now)) {
			result = append(result, binding)
		}
	}
	return result
}

func canonicalByLegacyID(input []canonicalCandidate) map[int64]canonicalCandidate {
	result := make(map[int64]canonicalCandidate, len(input))
	for _, candidate := range input {
		if candidate.LegacyChannelID > 0 {
			if _, exists := result[candidate.LegacyChannelID]; exists {
				// A zero value forces the inventory comparison to report a mapping
				// drift instead of picking one ambiguous deployment.
				result[candidate.LegacyChannelID] = canonicalCandidate{}
			} else {
				result[candidate.LegacyChannelID] = candidate
			}
		}
	}
	return result
}

func legacyKeyByID(input []legacyBinding, id int64) legacyBinding {
	for _, item := range input {
		if item.ID == id {
			return item
		}
	}
	return legacyBinding{}
}

func canonicalBindingByLegacyID(input []canonicalBinding, id int64) canonicalBinding {
	for _, item := range input {
		if item.LegacyKeyID == id {
			return item
		}
	}
	return canonicalBinding{}
}

func evaluatedBindingByID(input []evaluatedBinding, id int64) evaluatedBinding {
	for _, item := range input {
		if item.selectionID == id {
			return item
		}
	}
	return evaluatedBinding{}
}

func policyExclusions(input []routingpolicy.PreviewCandidate) []Exclusion {
	result := make([]Exclusion, 0, len(input))
	for _, item := range input {
		result = append(result, Exclusion{LegacyChannelID: item.ChannelID, Reason: item.Reason})
	}
	return result
}

func sameTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
