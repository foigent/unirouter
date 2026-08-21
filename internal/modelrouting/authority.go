package modelrouting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const AuthorityCheckContractVersion = "unirouter.model-route-authority-check.v1"

// AuthorityCheckRequest asks for a deployment-wide, read-only proof that all
// currently reachable model identities can be interpreted by one process
// authority mode. CustomerOrgID is explicit because on-prem installations
// have exactly one customer root and must never fall back to an arbitrary org.
type AuthorityCheckRequest struct {
	Mode          Mode  `json:"mode"`
	CustomerOrgID int64 `json:"customer_org_id"`
}

// AuthorityIssue is deliberately secret-free. It reports stable domain
// identities and classifications, never URLs, adapter JSON or credentials.
type AuthorityIssue struct {
	Code           string `json:"code"`
	Model          string `json:"model,omitempty"`
	ReleaseRef     string `json:"release_ref,omitempty"`
	OperationID    string `json:"operation_id,omitempty"`
	Classification string `json:"classification,omitempty"`
}

type AuthorityReleaseCheck struct {
	Model       string `json:"model"`
	ReleaseID   int64  `json:"release_id"`
	ReleaseRef  string `json:"release_ref"`
	Stage       string `json:"stage"`
	OperationID string `json:"operation_id"`
	Routable    bool   `json:"routable"`
	Ready       bool   `json:"ready"`
}

type AuthorityCheckReport struct {
	ContractVersion      string                  `json:"contract_version"`
	Mode                 Mode                    `json:"mode"`
	CustomerOrgID        int64                   `json:"customer_org_id"`
	DatabaseTime         time.Time               `json:"database_time"`
	Ready                bool                    `json:"ready"`
	DatabaseReady        bool                    `json:"database_ready"`
	ControlPlaneReady    bool                    `json:"control_plane_ready"`
	ControlPlaneContract string                  `json:"control_plane_contract,omitempty"`
	ModelsChecked        int                     `json:"models_checked"`
	ReleasesChecked      int                     `json:"releases_checked"`
	Checks               []AuthorityReleaseCheck `json:"checks"`
	BlockingIssues       []AuthorityIssue        `json:"blocking_issues"`
}

type authorityInventoryItem struct {
	ModelName     string
	PublicModelID int64
	Lifecycle     string
	SlotVersion   int64
	StableID      int64
	StableRef     string
	StableVersion string
	CanaryID      int64
	CanaryRef     string
	CanaryVersion string
}

type authorityRelease struct {
	ID               int64
	Ref              string
	CandidateVersion string
	Stage            string
}

// CheckAuthority proves route authority from one immutable database snapshot.
// The transaction is read-only by contract, so invoking this function cannot
// advance a sequence or repair a projection as a side effect.
func CheckAuthority(
	ctx context.Context,
	database TxStarter,
	policy routingpolicy.Config,
	request AuthorityCheckRequest,
) (AuthorityCheckReport, error) {
	report := AuthorityCheckReport{
		ContractVersion: AuthorityCheckContractVersion,
		Mode:            request.Mode, CustomerOrgID: request.CustomerOrgID,
		Checks:         make([]AuthorityReleaseCheck, 0),
		BlockingIssues: make([]AuthorityIssue, 0),
	}
	if database == nil || request.CustomerOrgID <= 0 {
		return report, fmt.Errorf("%w: database and positive customer org ID are required", ErrInvalidRequest)
	}
	if _, err := ParseMode(string(request.Mode)); err != nil {
		return report, err
	}
	if err := policy.Validate(); err != nil {
		return report, fmt.Errorf("%w: routing policy: %v", ErrInvalidRequest, err)
	}
	tx, err := database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, fmt.Errorf("%w: begin authority snapshot: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&report.DatabaseTime); err != nil {
		return report, fmt.Errorf("%w: read authority database clock: %v", ErrUnavailable, err)
	}
	report.DatabaseTime = report.DatabaseTime.UTC()

	rootIDs, err := loadAuthorityRootCustomers(ctx, tx)
	if err != nil {
		return report, err
	}
	if len(rootIDs) != 1 || rootIDs[0] != request.CustomerOrgID {
		report.BlockingIssues = append(report.BlockingIssues, AuthorityIssue{
			Code: "customer_root_mismatch", Classification: "customer_authority",
		})
	}

	inventory, err := loadAuthorityInventory(ctx, tx, request.CustomerOrgID)
	if err != nil {
		return report, err
	}
	report.ModelsChecked = len(inventory)
	if len(inventory) == 0 {
		report.BlockingIssues = append(report.BlockingIssues, AuthorityIssue{
			Code: "authority_inventory_empty", Classification: "runtime_assignment",
		})
	}
	for _, item := range inventory {
		if err := checkAuthorityInventoryItem(ctx, tx, policy, request, item, &report); err != nil {
			return report, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return report, fmt.Errorf("%w: commit authority snapshot: %v", ErrUnavailable, err)
	}
	report.DatabaseReady = len(report.BlockingIssues) == 0
	report.Ready = report.DatabaseReady
	if !report.Ready {
		return report, ErrAuthorityNotReady
	}
	return report, nil
}

func loadAuthorityRootCustomers(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	rows, err := tx.Query(ctx, `
SELECT id
  FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("%w: read customer root authority: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("%w: scan customer root authority: %v", ErrUnavailable, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate customer root authority: %v", ErrUnavailable, err)
	}
	return ids, nil
}

func loadAuthorityInventory(ctx context.Context, tx pgx.Tx, customerOrgID int64) ([]authorityInventoryItem, error) {
	rows, err := tx.Query(ctx, `
WITH legacy_models AS (
    SELECT DISTINCT BTRIM(configured.model) AS model_name
      FROM channels channel
      CROSS JOIN LATERAL regexp_split_to_table(
          COALESCE(channel.models,''),'[,，;；\r\n]+'
      ) configured(model)
     WHERE channel.status IN ('active','staged')
       AND BTRIM(configured.model)<>''
), current_slot_models AS (
    SELECT slot.model_name
      FROM runtime_change_slots slot
     WHERE slot.stable_release_id IS NOT NULL OR slot.canary_release_id IS NOT NULL
), authority_models AS (
    SELECT model_name FROM legacy_models
    UNION
    SELECT model_name FROM current_slot_models
)
SELECT authority.model_name,
       COALESCE(registry.id,0),COALESCE(registry.lifecycle,''),
       COALESCE(slot.slot_version,0),
       COALESCE(stable.id,0),COALESCE(stable.release_ref,''),COALESCE(stable.candidate_version,''),
       COALESCE(canary.id,0),COALESCE(canary.release_ref,''),COALESCE(canary.candidate_version,'')
  FROM authority_models authority
  LEFT JOIN model_registry registry
    ON registry.org_id=$1 AND registry.model_name=authority.model_name
  LEFT JOIN runtime_change_slots slot ON slot.model_name=authority.model_name
  LEFT JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
  LEFT JOIN runtime_change_releases canary ON canary.id=slot.canary_release_id
 ORDER BY authority.model_name`, customerOrgID)
	if err != nil {
		return nil, fmt.Errorf("%w: read authority inventory: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	items := make([]authorityInventoryItem, 0)
	for rows.Next() {
		var item authorityInventoryItem
		if err := rows.Scan(
			&item.ModelName, &item.PublicModelID, &item.Lifecycle, &item.SlotVersion,
			&item.StableID, &item.StableRef, &item.StableVersion,
			&item.CanaryID, &item.CanaryRef, &item.CanaryVersion,
		); err != nil {
			return nil, fmt.Errorf("%w: scan authority inventory: %v", ErrUnavailable, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate authority inventory: %v", ErrUnavailable, err)
	}
	return items, nil
}

func checkAuthorityInventoryItem(
	ctx context.Context,
	tx pgx.Tx,
	policy routingpolicy.Config,
	request AuthorityCheckRequest,
	item authorityInventoryItem,
	report *AuthorityCheckReport,
) error {
	if item.PublicModelID <= 0 {
		addAuthorityIssue(report, "model_registry_missing", item.ModelName, "", "", "model_identity")
		return nil
	}
	if item.Lifecycle == "retired" {
		addAuthorityIssue(report, "model_registry_retired", item.ModelName, "", "", "model_identity")
		return nil
	}
	if item.SlotVersion <= 0 {
		addAuthorityIssue(report, "runtime_slot_missing", item.ModelName, "", "", "runtime_assignment")
		return nil
	}
	if item.StableID <= 0 {
		addAuthorityIssue(report, "stable_release_missing", item.ModelName, "", "", "runtime_assignment")
		return nil
	}
	releases := []authorityRelease{{
		ID: item.StableID, Ref: item.StableRef,
		CandidateVersion: item.StableVersion, Stage: "active",
	}}
	if item.CanaryID > 0 {
		releases = append(releases, authorityRelease{
			ID: item.CanaryID, Ref: item.CanaryRef,
			CandidateVersion: item.CanaryVersion, Stage: "canary",
		})
	}
	for _, release := range releases {
		if err := checkAuthorityRelease(ctx, tx, policy, request, item, release, report); err != nil {
			return err
		}
	}
	return nil
}

func checkAuthorityRelease(
	ctx context.Context,
	tx pgx.Tx,
	policy routingpolicy.Config,
	request AuthorityCheckRequest,
	item authorityInventoryItem,
	release authorityRelease,
	report *AuthorityCheckReport,
) error {
	operations, err := authorityReleaseOperations(ctx, tx, release)
	if err != nil {
		return err
	}
	for _, operationID := range operations {
		routingRequest, err := normalizeRequest(Request{
			Mode: request.Mode, CustomerOrgID: request.CustomerOrgID,
			PublicModel: item.ModelName, ModelOperationID: operationID,
			SelectionKey: fmt.Sprintf("authority-check:%d:%s", release.ID, operationID),
		})
		if err != nil {
			addAuthorityIssue(report, "operation_identity_invalid", item.ModelName, release.Ref, operationID, "operation")
			continue
		}
		value, loadErr := loadSQLSnapshotForRelease(ctx, tx, routingRequest, release.ID)
		var plan Plan
		if loadErr == nil {
			plan, loadErr = buildPlan(value, routingRequest, policy)
		}
		if errors.Is(loadErr, ErrUnavailable) {
			return loadErr
		}
		var operationIssues []string
		if release.CandidateVersion == legacyCandidateVersion {
			operationIssues = authorityRouteOperationIssues(ctx, plan)
		}
		check := AuthorityReleaseCheck{
			Model: item.ModelName, ReleaseID: release.ID, ReleaseRef: release.Ref,
			Stage: release.Stage, OperationID: operationID,
			Routable: loadErr == nil && len(plan.Routes) > 0 && len(operationIssues) == 0,
		}
		planReady, structuralExclusions := authorityPlanReady(plan, loadErr)
		structuralExclusions = appendUniqueAuthorityReasons(structuralExclusions, operationIssues...)
		for _, reason := range structuralExclusions {
			addAuthorityIssue(report, "route_evidence_incomplete", item.ModelName, release.Ref, operationID, reason)
		}
		if planReady && len(operationIssues) == 0 {
			check.Ready = true
		} else if len(structuralExclusions) == 0 {
			addAuthorityIssue(report, authorityErrorCode(loadErr), item.ModelName, release.Ref, operationID, classifyAuthorityError(loadErr))
		}
		report.Checks = append(report.Checks, check)
	}
	report.ReleasesChecked++
	return nil
}

func authorityPlanReady(plan Plan, err error) (bool, []string) {
	structural := authorityStructuralExclusions(plan.Exclusions)
	if len(structural) != 0 {
		return false, structural
	}
	return err == nil || (errors.Is(err, ErrNoRoute) && authorityExclusionsAreOperational(plan.Exclusions)), nil
}

func authorityReleaseOperations(ctx context.Context, tx pgx.Tx, release authorityRelease) ([]string, error) {
	if release.CandidateVersion == legacyCandidateVersion {
		// A v1 release advertises legacy.any.v1, while the production Gateway
		// exposes both established project-default paid operations. Prove both
		// envelopes before a verify process is allowed to start; a successful
		// chat plan alone says nothing about embeddings adapter support.
		return legacyGatewayOperationSet(), nil
	}
	if release.CandidateVersion != canonicalCandidateVersion {
		return []string{string(modeloperation.IDChatCompletions)}, nil
	}
	rows, err := tx.Query(ctx, `
SELECT DISTINCT operation_id
  FROM runtime_route_revision_items
 WHERE release_id=$1
 ORDER BY operation_id`, release.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: read release operations: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	operations := make([]string, 0)
	for rows.Next() {
		var operation string
		if err := rows.Scan(&operation); err != nil {
			return nil, fmt.Errorf("%w: scan release operation: %v", ErrUnavailable, err)
		}
		operations = append(operations, operation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate release operations: %v", ErrUnavailable, err)
	}
	if len(operations) == 0 {
		// Force the ordinary manifest validator to report an empty/corrupt sealed
		// release instead of treating the release as having nothing to check.
		operations = append(operations, string(modeloperation.IDChatCompletions))
	}
	return operations, nil
}

func legacyGatewayOperationSet() []string {
	return []string{
		string(modeloperation.IDChatCompletions),
		string(modeloperation.IDEmbeddings),
	}
}

// authorityRouteOperationIssues proves that every currently selectable route
// can construct the exact public operation envelope without performing I/O.
// The ordinary Planner remains the sole selector and evidence validator; this
// check only closes the legacy.any.v1 ambiguity at the process-start boundary.
func authorityRouteOperationIssues(ctx context.Context, plan Plan) []string {
	if len(plan.Routes) == 0 {
		return nil
	}
	endpoint, body, supported := authorityOperationEnvelope(plan.ModelOperationID)
	if !supported {
		return []string{"operation_adapter_unsupported"}
	}
	registry := adapters.DefaultRegistry()
	for _, route := range plan.Routes {
		adapter, err := registry.Get(route.AdapterType)
		if err != nil {
			return []string{"operation_adapter_unsupported"}
		}
		request, err := adapter.BuildRequest(ctx, adapters.RequestInput{
			Endpoint:       endpoint,
			DownstreamBody: body,
			RequestedModel: plan.PublicModel,
			MappedModel:    route.UpstreamModel,
			Channel: models.Channel{
				Name:             route.ConnectionName,
				Provider:         route.Provider,
				BaseURL:          route.BaseURL,
				AdapterType:      route.AdapterType,
				AdapterConfig:    route.AdapterConfig,
				MaxContext:       route.MaxContext,
				DefaultMaxOutput: route.DefaultMaxOutput,
			},
			// This value is deliberately synthetic and process-local. BuildRequest
			// must not perform network I/O, and startup diagnostics never expose it.
			APIKey:    "authority-check-placeholder",
			RequestID: "authority-check",
		})
		if err != nil {
			return []string{"operation_adapter_unsupported"}
		}
		if request.Body != nil {
			_ = request.Body.Close()
		}
	}
	return nil
}

func authorityOperationEnvelope(operationID string) (adapters.Endpoint, []byte, bool) {
	switch modeloperation.ID(operationID) {
	case modeloperation.IDChatCompletions:
		return adapters.EndpointChatCompletions,
			[]byte(`{"model":"authority-check","messages":[{"role":"user","content":"ping"}],"max_tokens":1}`), true
	case modeloperation.IDEmbeddings:
		return adapters.EndpointEmbeddings,
			[]byte(`{"model":"authority-check","input":"ping","encoding_format":"float"}`), true
	default:
		return "", nil, false
	}
}

func appendUniqueAuthorityReasons(existing []string, additional ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additional))
	for _, reason := range existing {
		seen[reason] = struct{}{}
	}
	for _, reason := range additional {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, duplicate := seen[reason]; duplicate {
			continue
		}
		seen[reason] = struct{}{}
		existing = append(existing, reason)
	}
	return existing
}

func authorityExclusionsAreOperational(exclusions []Exclusion) bool {
	if len(exclusions) == 0 {
		return false
	}
	for _, exclusion := range exclusions {
		switch exclusion.Reason {
		case "administratively_disabled", "credential_unavailable", "channel_inactive",
			"circuit_recovery_pending", "probe_sample_not_selected", "circuit_unavailable",
			"no_active_key", "key_cooldown", "key_model_not_allowed", "no_eligible_key":
		default:
			return false
		}
	}
	return true
}

func authorityStructuralExclusions(exclusions []Exclusion) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, exclusion := range exclusions {
		if authorityExclusionsAreOperational([]Exclusion{exclusion}) {
			continue
		}
		reason := strings.TrimSpace(exclusion.Reason)
		if reason == "" {
			reason = "unknown_route_exclusion"
		}
		if _, exists := seen[reason]; exists {
			continue
		}
		seen[reason] = struct{}{}
		result = append(result, reason)
	}
	return result
}

func authorityErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrManifestUnsealed):
		return "manifest_unsealed"
	case errors.Is(err, ErrManifestStale):
		return "manifest_stale"
	case errors.Is(err, ErrParity):
		return "projection_parity_mismatch"
	case errors.Is(err, ErrNoRoute):
		return "route_authority_incomplete"
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, ErrInvalidMode):
		return "authority_request_invalid"
	default:
		return "authority_check_failed"
	}
}

func classifyAuthorityError(err error) string {
	switch {
	case errors.Is(err, ErrManifestUnsealed), errors.Is(err, ErrManifestStale):
		return "runtime_manifest"
	case errors.Is(err, ErrParity):
		return "projection_parity"
	case errors.Is(err, ErrNoRoute):
		return "route_evidence"
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, ErrInvalidMode):
		return "request"
	case errors.Is(err, ErrUnavailable):
		return "database"
	default:
		return "unknown"
	}
}

func addAuthorityIssue(report *AuthorityCheckReport, code, model, releaseRef, operationID, classification string) {
	report.BlockingIssues = append(report.BlockingIssues, AuthorityIssue{
		Code: strings.TrimSpace(code), Model: strings.TrimSpace(model),
		ReleaseRef: strings.TrimSpace(releaseRef), OperationID: strings.TrimSpace(operationID),
		Classification: strings.TrimSpace(classification),
	})
}
