package modelrouting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// Verify implements runtimechange.CutoverParityVerifier without importing the
// runtimechange package. The caller owns the surrounding write transaction;
// every read below therefore observes the exact locks and MVCC snapshot used
// by the subsequent slot CAS.
func (p *Planner) Verify(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID, legacyReleaseID, deploymentReleaseID int64,
) (string, string, error) {
	if p == nil || tx == nil || orgID <= 0 || publicModelID <= 0 ||
		legacyReleaseID <= 0 || deploymentReleaseID <= 0 || legacyReleaseID == deploymentReleaseID {
		return "", "", ErrInvalidRequest
	}
	var isolation string
	if err := tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		return "", "", fmt.Errorf("%w: read cutover transaction isolation: %v", ErrUnavailable, err)
	}
	if isolation != "repeatable read" && isolation != "serializable" {
		return "", "", fmt.Errorf("%w: cutover verifier requires repeatable-read transaction", ErrUnavailable)
	}
	value, err := loadCutoverSnapshot(ctx, tx, orgID, publicModelID, legacyReleaseID, deploymentReleaseID)
	if err != nil {
		return "", "", err
	}
	return verifyCutoverSnapshot(value, p.policy)
}

type cutoverOperationSnapshot struct {
	OperationID string
	Canonical   []canonicalCandidate
}

type cutoverSnapshot struct {
	DBNow                time.Time
	CustomerOrgID        int64
	PublicModelID        int64
	PublicModel          string
	PublicModelLifecycle string
	PublicContextLimit   int
	PublicOutputLimit    int
	SlotStableReleaseID  int64
	SlotPublicModelID    int64
	SlotVersion          int64
	LegacyRelease        releaseSnapshot
	LegacyManifest       []manifestItem
	LegacyOverrides      []legacyOverride
	Legacy               []legacyCandidate
	DeploymentRelease    releaseSnapshot
	DeploymentManifest   []manifestItem
	Operations           []cutoverOperationSnapshot
}

func loadCutoverSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID, legacyReleaseID, deploymentReleaseID int64,
) (cutoverSnapshot, error) {
	value := cutoverSnapshot{CustomerOrgID: orgID, PublicModelID: publicModelID}
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&value.DBNow); err != nil {
		return cutoverSnapshot{}, fmt.Errorf("%w: read cutover database clock: %v", ErrUnavailable, err)
	}
	value.DBNow = value.DBNow.UTC()
	if err := tx.QueryRow(ctx, `
SELECT model_name,lifecycle,context_length,output_limit
  FROM model_registry
 WHERE id=$1 AND org_id=$2`, publicModelID, orgID).Scan(
		&value.PublicModel, &value.PublicModelLifecycle,
		&value.PublicContextLimit, &value.PublicOutputLimit,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cutoverSnapshot{}, fmt.Errorf("%w: cutover public model", ErrManifestStale)
		}
		return cutoverSnapshot{}, fmt.Errorf("%w: read cutover public model: %v", ErrUnavailable, err)
	}
	if value.PublicModelLifecycle == "retired" || value.PublicContextLimit <= 0 || value.PublicOutputLimit <= 0 {
		return cutoverSnapshot{}, fmt.Errorf("%w: cutover public model is not executable", ErrManifestStale)
	}
	var stableID, canaryID, slotPublicModelID *int64
	if err := tx.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,public_model_id,slot_version
  FROM runtime_change_slots
 WHERE model_name=$1`, value.PublicModel).Scan(
		&stableID, &canaryID, &slotPublicModelID, &value.SlotVersion,
	); err != nil {
		return cutoverSnapshot{}, fmt.Errorf("%w: read cutover runtime slot: %v", ErrUnavailable, err)
	}
	if stableID == nil || canaryID != nil || (*stableID != legacyReleaseID && *stableID != deploymentReleaseID) {
		return cutoverSnapshot{}, fmt.Errorf("%w: cutover runtime slot identity", ErrManifestStale)
	}
	value.SlotStableReleaseID = *stableID
	if slotPublicModelID != nil {
		value.SlotPublicModelID = *slotPublicModelID
	}
	var err error
	value.LegacyRelease, err = loadRelease(ctx, tx, legacyReleaseID)
	if err != nil {
		return cutoverSnapshot{}, err
	}
	value.DeploymentRelease, err = loadRelease(ctx, tx, deploymentReleaseID)
	if err != nil {
		return cutoverSnapshot{}, err
	}
	value.LegacyManifest, err = loadManifest(ctx, tx, legacyReleaseID)
	if err != nil {
		return cutoverSnapshot{}, err
	}
	value.DeploymentManifest, err = loadManifest(ctx, tx, deploymentReleaseID)
	if err != nil {
		return cutoverSnapshot{}, err
	}
	value.LegacyOverrides, err = decodeLegacyOverrides(value.LegacyRelease.CandidateJSON, value.PublicModel)
	if err != nil {
		return cutoverSnapshot{}, err
	}
	value.Legacy, err = loadLegacyCandidates(ctx, tx, value.PublicModel)
	if err != nil {
		return cutoverSnapshot{}, err
	}
	operationIDs := make([]string, 0)
	seenOperations := make(map[string]struct{})
	for _, item := range value.DeploymentManifest {
		if item.ModelOperationID == legacyCompatibilityOp {
			return cutoverSnapshot{}, fmt.Errorf("%w: v2 manifest contains legacy operation", ErrManifestStale)
		}
		if _, exists := modeloperation.LookupID(modeloperation.ID(item.ModelOperationID)); !exists {
			return cutoverSnapshot{}, fmt.Errorf("%w: v2 manifest contains unknown operation", ErrManifestStale)
		}
		if _, exists := seenOperations[item.ModelOperationID]; !exists {
			seenOperations[item.ModelOperationID] = struct{}{}
			operationIDs = append(operationIDs, item.ModelOperationID)
		}
	}
	sort.Strings(operationIDs)
	if err := validateLegacyCutoverOperationSet(operationIDs); err != nil {
		return cutoverSnapshot{}, err
	}
	for _, operationID := range operationIDs {
		candidates, loadErr := loadCanonicalCandidates(
			ctx, tx, orgID, publicModelID, operationID, deploymentReleaseID, value.DeploymentManifest,
		)
		if loadErr != nil {
			return cutoverSnapshot{}, loadErr
		}
		if loadErr = loadAllCapabilities(
			ctx, tx, orgID, publicModelID, operationID,
			value.DeploymentRelease.ExecutionProfileID,
			value.DeploymentRelease.ExecutionProfileVersion,
			candidates,
		); loadErr != nil {
			return cutoverSnapshot{}, loadErr
		}
		value.Operations = append(value.Operations, cutoverOperationSnapshot{OperationID: operationID, Canonical: candidates})
	}
	return value, nil
}

func validateLegacyCutoverOperationSet(targetOperations []string) error {
	if !slices.Equal(targetOperations, legacyGatewayOperationSet()) {
		return &ParityError{Drifts: []Drift{{
			Kind:   "operation_set",
			Detail: "v2 manifest operation set must exactly preserve the established v1 Gateway operation set",
		}}}
	}
	return nil
}

func loadAllCapabilities(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID int64,
	operationID, executionProfileID, executionProfileVersion string,
	candidates []canonicalCandidate,
) error {
	return loadCapabilityEvidence(
		ctx, tx, orgID, publicModelID, operationID,
		executionProfileID, executionProfileVersion, nil, candidates,
	)
}

type cutoverCredentialProof struct {
	BindingID         int64         `json:"binding_id"`
	LegacyKeyID       int64         `json:"legacy_key_id"`
	Status            string        `json:"status"`
	StateRevision     int64         `json:"state_revision"`
	CooldownUntil     *time.Time    `json:"cooldown_until,omitempty"`
	LinkedAt          time.Time     `json:"linked_at"`
	CurrentlyEligible bool          `json:"currently_eligible"`
	Probe             probeSnapshot `json:"probe"`
}

type cutoverCandidateProof struct {
	OperationID             string                   `json:"operation_id"`
	LegacyChannelID         int64                    `json:"legacy_channel_id"`
	DeploymentID            int64                    `json:"deployment_id"`
	ConnectionID            int64                    `json:"connection_id"`
	Provider                string                   `json:"provider"`
	BaseURL                 string                   `json:"base_url"`
	AdapterType             string                   `json:"adapter_type"`
	AdapterConfigDigest     string                   `json:"adapter_config_digest"`
	UpstreamModel           string                   `json:"upstream_model"`
	ConnectionState         string                   `json:"connection_state"`
	DeploymentState         string                   `json:"deployment_state"`
	ConnectionRevision      int64                    `json:"connection_revision"`
	DeploymentRevision      int64                    `json:"deployment_revision"`
	OperationRevision       int64                    `json:"operation_revision"`
	OperationSupportState   string                   `json:"operation_support_state"`
	OperationEvidenceState  string                   `json:"operation_evidence_state"`
	OperationEvidenceRef    string                   `json:"operation_evidence_ref"`
	OperationVerifiedAt     *time.Time               `json:"operation_verified_at,omitempty"`
	ProtocolProfileID       string                   `json:"protocol_profile_id"`
	ProtocolProfileVersion  string                   `json:"protocol_profile_version"`
	PublicPath              string                   `json:"public_path,omitempty"`
	ExecutorContractVersion string                   `json:"executor_contract_version,omitempty"`
	ProductionApprovalID    int64                    `json:"production_approval_id,omitempty"`
	ProductionQualified     bool                     `json:"production_qualified,omitempty"`
	QualificationState      string                   `json:"qualification_state,omitempty"`
	Priority                int                      `json:"priority"`
	Weight                  int                      `json:"weight"`
	MaxContext              int                      `json:"max_context"`
	DefaultMaxOutput        int                      `json:"default_max_output"`
	CircuitState            string                   `json:"circuit_state"`
	CircuitRevision         int64                    `json:"circuit_revision"`
	HalfOpenUntil           *time.Time               `json:"half_open_until,omitempty"`
	HalfOpenProbeUntil      *time.Time               `json:"half_open_probe_until,omitempty"`
	HalfOpenProbeToken      string                   `json:"half_open_probe_token"`
	FailCount               int                      `json:"fail_count"`
	DegradationThreshold    int                      `json:"degradation_threshold"`
	Capabilities            []capabilitySnapshot     `json:"capabilities"`
	Credentials             []cutoverCredentialProof `json:"credentials"`
}

type cutoverPlanProof struct {
	DBNow                   time.Time               `json:"db_now"`
	CustomerOrgID           int64                   `json:"customer_org_id"`
	PublicModelID           int64                   `json:"public_model_id"`
	PublicModel             string                  `json:"public_model"`
	PublicContextLimit      int                     `json:"public_context_limit"`
	PublicOutputLimit       int                     `json:"public_output_limit"`
	SlotStableReleaseID     int64                   `json:"slot_stable_release_id"`
	SlotPublicModelID       int64                   `json:"slot_public_model_id"`
	SlotVersion             int64                   `json:"slot_version"`
	LegacyReleaseID         int64                   `json:"legacy_release_id"`
	LegacyReleaseRevision   int64                   `json:"legacy_release_revision"`
	LegacyReleaseStage      string                  `json:"legacy_release_stage"`
	LegacyCandidateHash     string                  `json:"legacy_candidate_hash"`
	LegacySealedAt          *time.Time              `json:"legacy_sealed_at,omitempty"`
	LegacyReleaseHash       string                  `json:"legacy_release_hash"`
	DeploymentReleaseID     int64                   `json:"deployment_release_id"`
	DeploymentRevision      int64                   `json:"deployment_release_revision"`
	DeploymentStage         string                  `json:"deployment_release_stage"`
	DeploymentCandidateHash string                  `json:"deployment_candidate_hash"`
	DeploymentSealedAt      *time.Time              `json:"deployment_sealed_at,omitempty"`
	DeploymentReleaseHash   string                  `json:"deployment_release_hash"`
	Candidates              []cutoverCandidateProof `json:"candidates"`
}

func verifyCutoverSnapshot(value cutoverSnapshot, policy routingpolicy.Config) (string, string, error) {
	if err := validateCutoverReleasePair(value); err != nil {
		return "", "", err
	}
	legacyProof := newCutoverPlanProof(value)
	canonicalProof := newCutoverPlanProof(value)
	for _, operation := range value.Operations {
		legacyCandidates, canonicalCandidates, err := buildCutoverOperationProofs(value, operation, policy)
		if err != nil {
			return "", "", err
		}
		legacyProof.Candidates = append(legacyProof.Candidates, legacyCandidates...)
		canonicalProof.Candidates = append(canonicalProof.Candidates, canonicalCandidates...)
	}
	if len(legacyProof.Candidates) == 0 || len(legacyProof.Candidates) != len(canonicalProof.Candidates) {
		return "", "", &ParityError{Drifts: []Drift{{Kind: "candidate_set", Detail: "cutover candidate set is empty or differs"}}}
	}
	sortCutoverProofs(legacyProof.Candidates)
	sortCutoverProofs(canonicalProof.Candidates)
	legacyHash, err := hashCutoverProof(legacyProof)
	if err != nil {
		return "", "", err
	}
	canonicalHash, err := hashCutoverProof(canonicalProof)
	if err != nil {
		return "", "", err
	}
	if legacyHash != canonicalHash {
		return legacyHash, canonicalHash, &ParityError{Drifts: []Drift{{Kind: "execution_plan", Detail: "full cutover execution plans differ"}}}
	}
	return legacyHash, canonicalHash, nil
}

func validateCutoverReleasePair(value cutoverSnapshot) error {
	if value.CustomerOrgID <= 0 || value.PublicModelID <= 0 || value.PublicModel == "" ||
		value.DBNow.IsZero() || value.PublicContextLimit <= 0 || value.PublicOutputLimit <= 0 ||
		value.LegacyRelease.ID <= 0 || value.DeploymentRelease.ID <= 0 || len(value.Operations) == 0 {
		return ErrInvalidRequest
	}
	if value.LegacyRelease.CandidateVersion != legacyCandidateVersion ||
		value.LegacyRelease.PublicModelID != value.PublicModelID ||
		value.DeploymentRelease.CandidateVersion != canonicalCandidateVersion ||
		value.DeploymentRelease.PublicModelID != value.PublicModelID ||
		value.DeploymentRelease.ParentStableReleaseID != value.LegacyRelease.ID {
		return fmt.Errorf("%w: cutover release identity", ErrManifestStale)
	}
	forward := value.SlotStableReleaseID == value.LegacyRelease.ID && value.SlotPublicModelID == 0 &&
		value.LegacyRelease.Stage == "active" && value.DeploymentRelease.Stage == "validated"
	reverse := value.SlotStableReleaseID == value.DeploymentRelease.ID && value.SlotPublicModelID == value.PublicModelID &&
		value.LegacyRelease.Stage == "rolled_back" && value.DeploymentRelease.Stage == "active"
	if !forward && !reverse {
		return fmt.Errorf("%w: cutover release stages or slot identity", ErrManifestStale)
	}
	legacyValue := snapshot{
		PublicModelID: value.PublicModelID, PublicModel: value.PublicModel,
		LegacyRelease: value.LegacyRelease, LegacyManifest: value.LegacyManifest,
		LegacyOverrides: value.LegacyOverrides,
	}
	if err := validateLegacyAuthority(legacyValue, value.LegacyRelease, reverse); err != nil {
		return err
	}
	if value.LegacyRelease.RouteItemsSealed == nil || value.LegacyRelease.RouteItemsCount != len(value.LegacyManifest) ||
		value.LegacyRelease.RouteItemsHash == "" || manifestDigest(value.LegacyManifest) != value.LegacyRelease.RouteItemsHash {
		return ErrManifestUnsealed
	}
	for _, item := range value.LegacyManifest {
		if item.SourceCandidateVersion != legacyCandidateVersion || item.ModelOperationID != legacyCompatibilityOp ||
			item.ExpectedOperationRevision <= 0 || item.ProtocolProfileID != "legacy.channel_model_probe" ||
			item.ProtocolProfileVersion != "v1" || item.PublicPath != "" || item.ExecutorContractVersion != "" {
			return fmt.Errorf("%w: cutover legacy compatibility manifest", ErrManifestStale)
		}
	}
	if value.DeploymentRelease.RouteItemsSealed == nil || value.DeploymentRelease.RouteItemsCount != len(value.DeploymentManifest) ||
		value.DeploymentRelease.RouteItemsHash == "" || manifestDigest(value.DeploymentManifest) != value.DeploymentRelease.RouteItemsHash {
		return ErrManifestUnsealed
	}
	for _, item := range value.DeploymentManifest {
		if item.SourceCandidateVersion != canonicalCandidateVersion || item.ModelOperationID == legacyCompatibilityOp ||
			item.Admission != routingpolicy.AdmissionAllow || item.ExpectedOperationRevision <= 0 ||
			item.ProtocolProfileID == "" || item.ProtocolProfileVersion == "" {
			return fmt.Errorf("%w: cutover canonical manifest", ErrManifestStale)
		}
	}
	return validateDeploymentCandidateDocument(value)
}

type deploymentRouteDocument struct {
	DeploymentID               int64  `json:"deployment_id"`
	ModelOperationID           string `json:"model_operation_id"`
	ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
	ExpectedOperationRevision  int64  `json:"expected_operation_revision"`
	ProtocolProfileID          string `json:"protocol_profile_id"`
	ProtocolProfileVersion     string `json:"protocol_profile_version"`
	PublicPath                 string `json:"public_path,omitempty"`
	ExecutorContractVersion    string `json:"executor_contract_version,omitempty"`
	Admission                  string `json:"admission"`
	Priority                   *int   `json:"priority,omitempty"`
	Weight                     *int   `json:"weight,omitempty"`
}

func validateDeploymentCandidateDocument(value cutoverSnapshot) error {
	var document struct {
		ContractVersion  string                    `json:"contract_version"`
		PublicModelID    int64                     `json:"public_model_id"`
		ModelName        string                    `json:"model_name"`
		DeploymentRoutes []deploymentRouteDocument `json:"deployment_routes"`
	}
	if len(value.DeploymentRelease.CandidateJSON) == 0 || json.Unmarshal(value.DeploymentRelease.CandidateJSON, &document) != nil ||
		document.ContractVersion != canonicalCandidateVersion || document.PublicModelID != value.PublicModelID ||
		document.ModelName != value.PublicModel || len(document.DeploymentRoutes) != len(value.DeploymentManifest) {
		return fmt.Errorf("%w: corrupt canonical candidate", ErrManifestStale)
	}
	expected := make([]deploymentRouteDocument, 0, len(value.DeploymentManifest))
	for _, item := range value.DeploymentManifest {
		expected = append(expected, deploymentRouteDocument{
			DeploymentID: item.DeploymentID, ModelOperationID: item.ModelOperationID,
			ExpectedConnectionRevision: item.ExpectedConnectionRevision,
			ExpectedDeploymentRevision: item.ExpectedDeploymentRevision,
			ExpectedOperationRevision:  item.ExpectedOperationRevision,
			ProtocolProfileID:          item.ProtocolProfileID, ProtocolProfileVersion: item.ProtocolProfileVersion,
			PublicPath: item.PublicPath, ExecutorContractVersion: item.ExecutorContractVersion,
			Admission: item.Admission, Priority: item.Priority, Weight: item.Weight,
		})
	}
	if !reflect.DeepEqual(document.DeploymentRoutes, expected) {
		return fmt.Errorf("%w: canonical candidate and manifest differ", ErrManifestStale)
	}
	body, _ := json.Marshal(document)
	if digestBytes(body) != value.DeploymentRelease.CandidateHash {
		return fmt.Errorf("%w: canonical candidate hash", ErrManifestStale)
	}
	return nil
}

func newCutoverPlanProof(value cutoverSnapshot) cutoverPlanProof {
	return cutoverPlanProof{
		DBNow: value.DBNow.UTC(), CustomerOrgID: value.CustomerOrgID,
		PublicModelID: value.PublicModelID, PublicModel: value.PublicModel,
		PublicContextLimit: value.PublicContextLimit, PublicOutputLimit: value.PublicOutputLimit,
		SlotStableReleaseID: value.SlotStableReleaseID, SlotPublicModelID: value.SlotPublicModelID,
		SlotVersion: value.SlotVersion, LegacyReleaseID: value.LegacyRelease.ID,
		LegacyReleaseRevision: value.LegacyRelease.Revision,
		LegacyReleaseStage:    value.LegacyRelease.Stage, LegacyCandidateHash: value.LegacyRelease.CandidateHash,
		LegacySealedAt: value.LegacyRelease.RouteItemsSealed, LegacyReleaseHash: value.LegacyRelease.RouteItemsHash,
		DeploymentReleaseID: value.DeploymentRelease.ID, DeploymentRevision: value.DeploymentRelease.Revision,
		DeploymentStage: value.DeploymentRelease.Stage, DeploymentCandidateHash: value.DeploymentRelease.CandidateHash,
		DeploymentSealedAt:    value.DeploymentRelease.RouteItemsSealed,
		DeploymentReleaseHash: value.DeploymentRelease.RouteItemsHash,
	}
}

func buildCutoverOperationProofs(
	value cutoverSnapshot,
	operation cutoverOperationSnapshot,
	policy routingpolicy.Config,
) ([]cutoverCandidateProof, []cutoverCandidateProof, error) {
	snapshotValue := snapshot{
		Mode: ModeVerify, DBNow: value.DBNow, CustomerOrgID: value.CustomerOrgID,
		PublicModelID: value.PublicModelID, PublicModel: value.PublicModel,
		PublicModelLifecycle: value.PublicModelLifecycle,
		PublicContextLimit:   value.PublicContextLimit, PublicOutputLimit: value.PublicOutputLimit,
		RequestedOperationID: operation.OperationID,
		LegacyRelease:        value.LegacyRelease, LegacyManifest: value.LegacyManifest,
		LegacyOverrides: value.LegacyOverrides, Legacy: value.Legacy,
		Manifest: value.DeploymentManifest, Canonical: operation.Canonical,
	}
	selected := value.DeploymentRelease
	// The release document and actual forward/reverse stages were validated
	// above. Selection evaluation needs the same sealed v2 manifest shape used
	// after the CAS, so only the local proof copy is normalized to active.
	selected.Stage = "active"
	selected.Selected = true
	snapshotValue.Slot = slotSnapshot{
		Present: true, PublicModelID: value.PublicModelID, Version: value.SlotVersion,
		Stable: selected, Selected: selected,
	}
	selectionKey := fmt.Sprintf("cutover:%d:%d:%s", value.CustomerOrgID, value.PublicModelID, operation.OperationID)
	legacyRoutes, _, legacyErr := evaluateLegacyRoutes(snapshotValue, policy, selectionKey, "cutover-proof")
	canonicalRoutes, _, canonicalErr := evaluateCanonicalV2Compatibility(snapshotValue, policy, selectionKey, "cutover-proof")
	if legacyErr != nil && !errors.Is(legacyErr, ErrNoRoute) {
		return nil, nil, legacyErr
	}
	if canonicalErr != nil && !errors.Is(canonicalErr, ErrNoRoute) {
		return nil, nil, canonicalErr
	}
	if legacyErr != nil || canonicalErr != nil || len(legacyRoutes) == 0 || len(canonicalRoutes) == 0 {
		return nil, nil, &ParityError{Drifts: []Drift{{Kind: "route_availability", Detail: "cutover requires at least one executable route on both authorities"}}}
	}
	if drifts := compareVerifyPlans(snapshotValue, legacyRoutes, canonicalRoutes); len(drifts) != 0 {
		return nil, nil, &ParityError{Drifts: drifts}
	}
	effectiveLegacy, err := effectiveLegacyAuthorityCandidates(snapshotValue)
	if err != nil {
		return nil, nil, err
	}
	drifts := compareProjectionInventory(snapshotValue)
	if len(drifts) != 0 {
		return nil, nil, &ParityError{Drifts: drifts}
	}
	legacyByID := make(map[int64]legacyCandidate, len(effectiveLegacy))
	for _, candidate := range effectiveLegacy {
		if candidate.Status == "active" {
			legacyByID[candidate.ID] = candidate
		}
	}
	canonicalByLegacy := make(map[int64]canonicalCandidate)
	for _, candidate := range operation.Canonical {
		if candidate.Manifest == nil || candidate.Manifest.ModelOperationID != operation.OperationID ||
			candidate.Manifest.Admission != routingpolicy.AdmissionAllow {
			continue
		}
		legacyCandidate, exists := legacyByID[candidate.LegacyChannelID]
		if !exists || candidate.LegacyChannelID <= 0 {
			return nil, nil, &ParityError{Drifts: []Drift{{Kind: "candidate_set", LegacyChannelID: candidate.LegacyChannelID, Detail: "v2 manifest introduces a route absent from v1"}}}
		}
		eligible, reason := canonicalEligibility(snapshotValue, candidate, operation.OperationID, true)
		if !eligible {
			return nil, nil, &ParityError{Drifts: []Drift{{Kind: reason, LegacyChannelID: candidate.LegacyChannelID, Detail: "canonical cutover candidate is not executable"}}}
		}
		if len(eligibleCanonicalBindings(snapshotValue, candidate)) == 0 {
			return nil, nil, &ParityError{Drifts: []Drift{{Kind: "credential_eligibility", LegacyChannelID: candidate.LegacyChannelID, Detail: "canonical cutover candidate has no current probed binding"}}}
		}
		priority, weight := candidate.Priority, candidate.Weight
		if candidate.Manifest.Priority != nil {
			priority = *candidate.Manifest.Priority
		}
		if candidate.Manifest.Weight != nil {
			weight = *candidate.Manifest.Weight
		}
		if priority != legacyCandidate.Priority || weight != legacyCandidate.Weight {
			return nil, nil, &ParityError{Drifts: []Drift{{Kind: "routing_weight", LegacyChannelID: candidate.LegacyChannelID, Detail: "v2 priority or weight differs from effective v1 authority"}}}
		}
		if _, duplicate := canonicalByLegacy[candidate.LegacyChannelID]; duplicate {
			return nil, nil, &ParityError{Drifts: []Drift{{Kind: "candidate_mapping", LegacyChannelID: candidate.LegacyChannelID, Detail: "multiple deployments map one legacy channel"}}}
		}
		canonicalByLegacy[candidate.LegacyChannelID] = candidate
	}
	if len(canonicalByLegacy) != len(legacyByID) {
		return nil, nil, &ParityError{Drifts: []Drift{{Kind: "candidate_set", Detail: "v2 manifest does not contain the complete effective v1 candidate set"}}}
	}
	legacyProofs := make([]cutoverCandidateProof, 0, len(legacyByID))
	canonicalProofs := make([]cutoverCandidateProof, 0, len(legacyByID))
	for legacyID, legacyCandidate := range legacyByID {
		canonicalCandidate, exists := canonicalByLegacy[legacyID]
		if !exists {
			return nil, nil, &ParityError{Drifts: []Drift{{Kind: "candidate_set", LegacyChannelID: legacyID, Detail: "v2 manifest omits an effective v1 candidate"}}}
		}
		legacyProofs = append(legacyProofs, legacyCutoverCandidateProof(operation.OperationID, legacyCandidate, canonicalCandidate, snapshotValue))
		canonicalProofs = append(canonicalProofs, canonicalCutoverCandidateProof(operation.OperationID, canonicalCandidate, snapshotValue))
	}
	return legacyProofs, canonicalProofs, nil
}

func legacyCutoverCandidateProof(
	operationID string,
	legacy legacyCandidate,
	canonical canonicalCandidate,
	value snapshot,
) cutoverCandidateProof {
	proof := commonCutoverCandidateProof(operationID, canonical)
	proof.Provider, proof.BaseURL, proof.AdapterType = legacy.Provider, legacy.BaseURL, legacy.AdapterType
	proof.AdapterConfigDigest, proof.UpstreamModel = legacy.AdapterConfigDigest, legacy.UpstreamModel
	proof.ConnectionState, proof.DeploymentState = legacyAdministrativeState(legacy.Status), legacyAdministrativeState(legacy.Status)
	proof.ConnectionRevision, proof.DeploymentRevision = legacy.RoutingRevision, legacy.RoutingRevision
	proof.Priority, proof.Weight = legacy.Priority, legacy.Weight
	proof.MaxContext, proof.DefaultMaxOutput = legacy.MaxContext, legacy.DefaultMaxOutput
	proof.CircuitState, proof.CircuitRevision = legacy.CircuitState, legacy.CircuitRevision
	proof.HalfOpenUntil, proof.HalfOpenProbeUntil = legacy.HalfOpenUntil, legacy.HalfOpenProbeUntil
	proof.HalfOpenProbeToken, proof.FailCount = legacy.HalfOpenProbeToken, legacy.FailCount
	proof.DegradationThreshold = legacy.DegradationThreshold
	bindingsByLegacy := make(map[int64]canonicalBinding, len(canonical.Bindings))
	for _, binding := range canonical.Bindings {
		bindingsByLegacy[binding.LegacyKeyID] = binding
	}
	for _, key := range legacy.Bindings {
		binding := bindingsByLegacy[key.ID]
		proof.Credentials = append(proof.Credentials, cutoverCredentialProof{
			BindingID: binding.ID, LegacyKeyID: key.ID, Status: key.Status,
			StateRevision: key.StateRevision, CooldownUntil: key.CooldownUntil,
			LinkedAt: binding.LinkedAt, CurrentlyEligible: legacyBindingEligible(value, key), Probe: binding.Probe,
		})
	}
	sortCutoverCredentials(proof.Credentials)
	return proof
}

func canonicalCutoverCandidateProof(
	operationID string,
	candidate canonicalCandidate,
	value snapshot,
) cutoverCandidateProof {
	proof := commonCutoverCandidateProof(operationID, candidate)
	proof.Provider, proof.BaseURL, proof.AdapterType = candidate.Provider, candidate.BaseURL, candidate.AdapterType
	proof.AdapterConfigDigest, proof.UpstreamModel = candidate.AdapterConfigDigest, candidate.UpstreamModel
	proof.ConnectionState, proof.DeploymentState = candidate.ConnectionState, candidate.DeploymentState
	proof.ConnectionRevision, proof.DeploymentRevision = candidate.ConnectionRevision, candidate.DeploymentRevision
	proof.Priority, proof.Weight = candidate.Priority, candidate.Weight
	if candidate.Manifest != nil {
		if candidate.Manifest.Priority != nil {
			proof.Priority = *candidate.Manifest.Priority
		}
		if candidate.Manifest.Weight != nil {
			proof.Weight = *candidate.Manifest.Weight
		}
	}
	proof.MaxContext, proof.DefaultMaxOutput = candidate.MaxContext, candidate.DefaultMaxOutput
	proof.CircuitState, proof.CircuitRevision = candidate.CircuitState, candidate.CircuitRevision
	proof.HalfOpenUntil, proof.HalfOpenProbeUntil = candidate.HalfOpenUntil, candidate.HalfOpenProbeUntil
	proof.HalfOpenProbeToken, proof.FailCount = candidate.HalfOpenProbeToken, candidate.FailCount
	proof.DegradationThreshold = candidate.DegradationThreshold
	eligible := make(map[int64]struct{})
	for _, binding := range eligibleCanonicalBindings(value, candidate) {
		eligible[binding.ID] = struct{}{}
	}
	for _, binding := range candidate.Bindings {
		_, currentlyEligible := eligible[binding.ID]
		proof.Credentials = append(proof.Credentials, cutoverCredentialProof{
			BindingID: binding.ID, LegacyKeyID: binding.LegacyKeyID, Status: binding.Status,
			StateRevision: binding.StateRevision, CooldownUntil: binding.CooldownUntil,
			LinkedAt: binding.LinkedAt, CurrentlyEligible: currentlyEligible, Probe: binding.Probe,
		})
	}
	sortCutoverCredentials(proof.Credentials)
	return proof
}

func commonCutoverCandidateProof(operationID string, candidate canonicalCandidate) cutoverCandidateProof {
	proof := cutoverCandidateProof{
		OperationID: operationID, LegacyChannelID: candidate.LegacyChannelID,
		DeploymentID: candidate.DeploymentID, ConnectionID: candidate.ConnectionID,
		OperationRevision:       candidate.OperationRevision,
		OperationSupportState:   candidate.OperationSupportState,
		OperationEvidenceState:  candidate.OperationEvidenceState,
		OperationEvidenceRef:    candidate.OperationEvidenceRef,
		OperationVerifiedAt:     candidate.OperationVerifiedAt,
		ProtocolProfileID:       candidate.ProtocolProfileID,
		ProtocolProfileVersion:  candidate.ProtocolProfileVersion,
		PublicPath:              candidate.PublicPath,
		ExecutorContractVersion: candidate.ExecutorContractVersion,
		ProductionApprovalID:    candidate.ProductionApprovalID,
		ProductionQualified:     candidate.ProductionQualified,
		QualificationState:      candidate.QualificationState,
	}
	capabilityIDs := make([]string, 0, len(candidate.Capabilities))
	for id := range candidate.Capabilities {
		capabilityIDs = append(capabilityIDs, id)
	}
	sort.Strings(capabilityIDs)
	for _, id := range capabilityIDs {
		proof.Capabilities = append(proof.Capabilities, candidate.Capabilities[id])
	}
	return proof
}

func legacyAdministrativeState(status string) string {
	if status == "disabled" {
		return "disabled"
	}
	return "enabled"
}

func legacyBindingEligible(value snapshot, binding legacyBinding) bool {
	if binding.Status != "active" || (binding.CooldownUntil != nil && binding.CooldownUntil.After(value.DBNow)) {
		return false
	}
	return binding.AllowedModels == "" || modellist.Contains(binding.AllowedModels, value.PublicModel)
}

func sortCutoverCredentials(input []cutoverCredentialProof) {
	sort.Slice(input, func(left, right int) bool {
		if input[left].LegacyKeyID != input[right].LegacyKeyID {
			return input[left].LegacyKeyID < input[right].LegacyKeyID
		}
		return input[left].BindingID < input[right].BindingID
	})
}

func sortCutoverProofs(input []cutoverCandidateProof) {
	sort.Slice(input, func(left, right int) bool {
		if input[left].OperationID != input[right].OperationID {
			return input[left].OperationID < input[right].OperationID
		}
		if input[left].Priority != input[right].Priority {
			return input[left].Priority < input[right].Priority
		}
		return input[left].LegacyChannelID < input[right].LegacyChannelID
	})
}

func hashCutoverProof(value cutoverPlanProof) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: marshal cutover plan proof: %v", ErrUnavailable, err)
	}
	return digestBytes(body), nil
}
