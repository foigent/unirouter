package modelreadiness

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modelgov"
)

const MaxCredentialCandidates = 100

type ProjectSnapshot struct {
	OrgID            int64
	Status           string
	BudgetConfigured bool
	BudgetCents      int64
	BudgetPeriod     string
}

func ReadProject(ctx context.Context, q db.DBQuerier, projectID int64) (ProjectSnapshot, error) {
	var snapshot ProjectSnapshot
	var budget sql.NullInt64
	err := q.QueryRow(ctx, `
		SELECT org_id,status,budget_cents,COALESCE(budget_period,'')
		FROM projects WHERE id=$1`, projectID).
		Scan(&snapshot.OrgID, &snapshot.Status, &budget, &snapshot.BudgetPeriod)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.BudgetConfigured = budget.Valid
	if budget.Valid {
		snapshot.BudgetCents = budget.Int64
	}
	return snapshot, nil
}

// GovernanceSnapshot combines the exact modelgov access decision with the
// explanatory alias and model-set facts that do not participate in any other
// runtime mutation.
type GovernanceSnapshot struct {
	Access                modelgov.AccessResult
	AccessError           error
	AliasConfigured       bool
	AliasStatus           string
	AliasEvidenceError    error
	AssignedModelSetCount int
	ModelSetEvidenceError error
}

func InspectGovernance(ctx context.Context, q db.DBQuerier, orgID, projectID int64, requestedModel string) GovernanceSnapshot {
	snapshot := GovernanceSnapshot{}
	snapshot.Access, snapshot.AccessError = modelgov.CheckAccess(ctx, q, orgID, projectID, requestedModel)
	if snapshot.Access.Alias == "" {
		err := q.QueryRow(ctx, `SELECT status FROM model_aliases WHERE org_id=$1 AND alias=$2`, orgID, requestedModel).
			Scan(&snapshot.AliasStatus)
		switch {
		case err == nil:
			snapshot.AliasConfigured = true
		case errors.Is(err, pgx.ErrNoRows):
		default:
			snapshot.AliasEvidenceError = err
		}
	}
	snapshot.ModelSetEvidenceError = q.QueryRow(ctx, `
		SELECT COUNT(*) FROM project_model_sets pms
		JOIN projects p ON p.id=pms.project_id
		JOIN model_sets ms ON ms.id=pms.model_set_id AND ms.org_id=p.org_id
		WHERE pms.project_id=$1 AND p.org_id=$2`, projectID, orgID).
		Scan(&snapshot.AssignedModelSetCount)
	return snapshot
}

type RegistrySnapshot struct {
	Exists          bool
	Lifecycle       string
	ApprovedCatalog bool
}

func ReadRegistry(ctx context.Context, q db.DBQuerier, orgID int64, model string) (RegistrySnapshot, error) {
	var snapshot RegistrySnapshot
	err := q.QueryRow(ctx, `
		SELECT lifecycle,approved_catalog FROM model_registry
		WHERE org_id=$1 AND model_name=$2`, orgID, model).
		Scan(&snapshot.Lifecycle, &snapshot.ApprovedCatalog)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrySnapshot{}, nil
	}
	if err != nil {
		return RegistrySnapshot{}, err
	}
	snapshot.Exists = true
	return snapshot, nil
}

type WorkloadSnapshot struct {
	Exists                  bool
	ID                      int64
	Status                  string
	IdentityContractVersion string
	Environment             string
	ProductionUse           bool
	EvidenceError           error
}

type GrantSnapshot struct {
	ID               int64
	Status           string
	ScopeMode        string
	AllowedModels    []string
	AllowedEndpoints []string
	ValidFrom        time.Time
	ValidUntil       *time.Time
}

func (g GrantSnapshot) AllowsExecution(requested, resolved, endpoint string) bool {
	if g.ScopeMode == accessscope.ModeProjectDefault {
		return accessscope.IsProjectDefaultEndpoint(endpoint)
	}
	if g.ScopeMode != accessscope.ModeExplicit || !slices.Contains(g.AllowedEndpoints, endpoint) {
		return false
	}
	return slices.Contains(g.AllowedModels, requested) || slices.Contains(g.AllowedModels, resolved)
}

type CallerInput struct {
	OrgID          int64
	ProjectID      int64
	Caller         string
	ActorUserID    int64
	ApplicationRef string
	RequestedModel string
	ResolvedModel  string
	Endpoint       string
}

type CallerSnapshot struct {
	Workload             WorkloadSnapshot
	Grant                GrantSnapshot
	GrantEvidenceError   error
	CredentialCandidates bool
	LiveBoundary         bool
	Credentials          []accessgrantboundary.Snapshot
	CredentialError      error
}

// InspectCaller resolves the same live accessgrantboundary snapshots used by
// Gateway authentication. It selects an existing subject and active credential
// family but never issues, rotates, revokes, or authenticates a secret.
func InspectCaller(ctx context.Context, q db.DBQuerier, input CallerInput) CallerSnapshot {
	result := CallerSnapshot{Credentials: []accessgrantboundary.Snapshot{}}
	workloadID := int64(0)
	if input.Caller == "workload_application" {
		result.Workload.EvidenceError = q.QueryRow(ctx, `
			SELECT id,status,identity_contract_version,environment,production_use
			FROM workload_applications
			WHERE application_ref=$1 AND org_id=$2 AND project_id=$3`,
			input.ApplicationRef, input.OrgID, input.ProjectID).
			Scan(&result.Workload.ID, &result.Workload.Status, &result.Workload.IdentityContractVersion,
				&result.Workload.Environment, &result.Workload.ProductionUse)
		switch {
		case result.Workload.EvidenceError == nil:
			result.Workload.Exists = true
			workloadID = result.Workload.ID
		case errors.Is(result.Workload.EvidenceError, pgx.ErrNoRows):
			result.Workload.EvidenceError = nil
		}
	}

	var modelsJSON, endpointsJSON []byte
	var validUntil sql.NullTime
	result.GrantEvidenceError = q.QueryRow(ctx, `
		SELECT ag.id,ag.status,ag.scope_mode,ag.allowed_models,ag.allowed_endpoints,ag.valid_from,ag.valid_until
		FROM access_grants ag
		WHERE ag.org_id=$1 AND ag.project_id=$2
		  AND (($3='self' AND ag.subject_type='user' AND ag.subject_user_id=$4)
		    OR ($3='workload_application' AND ag.subject_type='workload_application' AND ag.subject_workload_application_id=$5))
		ORDER BY (ag.status<>'revoked') DESC,ag.id DESC LIMIT 1`,
		input.OrgID, input.ProjectID, input.Caller, input.ActorUserID, workloadID).
		Scan(&result.Grant.ID, &result.Grant.Status, &result.Grant.ScopeMode, &modelsJSON, &endpointsJSON,
			&result.Grant.ValidFrom, &validUntil)
	if errors.Is(result.GrantEvidenceError, pgx.ErrNoRows) {
		result.GrantEvidenceError = nil
		return result
	}
	if result.GrantEvidenceError != nil {
		return result
	}
	if validUntil.Valid {
		value := validUntil.Time
		result.Grant.ValidUntil = &value
	}
	if json.Unmarshal(modelsJSON, &result.Grant.AllowedModels) != nil ||
		json.Unmarshal(endpointsJSON, &result.Grant.AllowedEndpoints) != nil {
		result.GrantEvidenceError = errors.New("invalid access grant scope")
		return result
	}

	rows, err := q.Query(ctx, `
		SELECT k.id
		FROM access_credentials ac
		JOIN api_keys k
		  ON k.access_credential_id=ac.id AND k.access_grant_id=ac.access_grant_id
		 AND k.org_id=ac.org_id AND k.project_id=ac.project_id AND k.credential_ref=ac.credential_ref
		WHERE ac.access_grant_id=$1 AND ac.org_id=$2 AND ac.project_id=$3
		  AND ac.status='active' AND k.status='active' AND COALESCE(k.purpose,'')=''
		ORDER BY k.id DESC
		LIMIT $4`, result.Grant.ID, input.OrgID, input.ProjectID, MaxCredentialCandidates+1)
	if err != nil {
		result.CredentialError = err
		return result
	}
	defer rows.Close()
	keyIDs := make([]int64, 0, MaxCredentialCandidates)
	for rows.Next() {
		var keyID int64
		if err := rows.Scan(&keyID); err != nil {
			result.CredentialError = err
			return result
		}
		keyIDs = append(keyIDs, keyID)
		if len(keyIDs) > MaxCredentialCandidates {
			result.CredentialError = errors.New("access credential candidate limit exceeded")
			return result
		}
	}
	if err := rows.Err(); err != nil {
		result.CredentialError = err
		return result
	}
	rows.Close()
	result.CredentialCandidates = len(keyIDs) > 0
	for _, keyID := range keyIDs {
		snapshot, err := accessgrantboundary.Resolve(ctx, q, keyID)
		if errors.Is(err, accessgrantboundary.ErrInactive) {
			continue
		}
		if err != nil {
			result.CredentialError = err
			return result
		}
		result.LiveBoundary = true
		if snapshot.AllowsExecution(input.RequestedModel, input.ResolvedModel, input.Endpoint) {
			result.Credentials = append(result.Credentials, snapshot)
		}
	}
	return result
}
