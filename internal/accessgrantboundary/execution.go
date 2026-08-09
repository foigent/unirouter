package accessgrantboundary

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/principallock"
)

// ExecutionBinding is immutable admission evidence for a durable async job.
// Grant-bound execution follows the logical Credential family so replacing a
// secret version does not strand accepted work. Legacy jobs remain bound to
// the physical API-key version.
type ExecutionBinding struct {
	OrgID              int64
	ProjectID          int64
	AdmissionAPIKeyID  int64
	AccessGrantID      int64
	AccessCredentialID int64
	CredentialRef      string
	RequestedModel     string
	RoutingModel       string
	Endpoint           string
}

func ResolveExecution(ctx context.Context, q db.DBQuerier, binding ExecutionBinding) (Snapshot, error) {
	if q == nil || binding.OrgID <= 0 || binding.ProjectID <= 0 || binding.AdmissionAPIKeyID <= 0 ||
		!validMediaDispatchEndpoint(binding.Endpoint) || strings.TrimSpace(binding.RequestedModel) == "" {
		return Snapshot{}, ErrInactive
	}
	bound := binding.AccessGrantID > 0 || binding.AccessCredentialID > 0 || strings.TrimSpace(binding.CredentialRef) != ""
	keyID := binding.AdmissionAPIKeyID
	if bound {
		if binding.AccessGrantID <= 0 || binding.AccessCredentialID <= 0 || strings.TrimSpace(binding.CredentialRef) == "" {
			return Snapshot{}, ErrInactive
		}
		if err := q.QueryRow(ctx, `
			SELECT id
			  FROM api_keys
			 WHERE org_id=$1 AND project_id=$2 AND access_grant_id=$3
			   AND access_credential_id=$4 AND credential_ref=$5
			   AND status='active' AND COALESCE(purpose,'')=''
			 ORDER BY id
			 LIMIT 1`,
			binding.OrgID, binding.ProjectID, binding.AccessGrantID,
			binding.AccessCredentialID, binding.CredentialRef,
		).Scan(&keyID); err != nil {
			return Snapshot{}, executionResolveError(err)
		}
	}
	snapshot, err := Resolve(ctx, q, keyID)
	if err != nil {
		return Snapshot{}, executionResolveError(err)
	}
	if snapshot.APIKey.OrgID != binding.OrgID || snapshot.APIKey.ProjectID == nil ||
		*snapshot.APIKey.ProjectID != binding.ProjectID || snapshot.APIKey.Purpose != "" {
		return Snapshot{}, ErrInactive
	}
	if bound {
		if !snapshot.APIKey.GrantBound || snapshot.APIKey.AccessGrantID == nil || snapshot.APIKey.AccessCredentialID == nil ||
			*snapshot.APIKey.AccessGrantID != binding.AccessGrantID ||
			*snapshot.APIKey.AccessCredentialID != binding.AccessCredentialID ||
			snapshot.APIKey.CredentialRef != binding.CredentialRef {
			return Snapshot{}, ErrInactive
		}
	} else if snapshot.APIKey.GrantBound || snapshot.APIKey.ID != binding.AdmissionAPIKeyID {
		return Snapshot{}, ErrInactive
	}
	if !snapshot.AllowsExecution(binding.RequestedModel, binding.RoutingModel, "/v1/media/jobs") {
		return Snapshot{}, ErrInactive
	}
	return snapshot, nil
}

// AllowsExecution applies the exact live model and endpoint intersection that
// Gateway and durable media dispatch use after Resolve has materialized the
// current Grant into the physical key snapshot. It is pure and is therefore
// also safe for read-only readiness projection.
func (s Snapshot) AllowsExecution(requestedModel, resolvedModel, endpoint string) bool {
	endpoint = accessscope.CanonicalExecutionEndpoint(strings.TrimSpace(endpoint))
	if !accessscope.IsSupportedEndpoint(endpoint) || s.APIKey.EndpointDenied ||
		!commaScopeAllows(s.APIKey.AllowedEndpoints, endpoint) || s.APIKey.ModelScopeDenied {
		return false
	}
	return commaScopeAllows(s.APIKey.AllowedModels, requestedModel) ||
		commaScopeAllows(s.APIKey.AllowedModels, resolvedModel)
}

func validMediaDispatchEndpoint(endpoint string) bool {
	endpoint = strings.TrimSpace(endpoint)
	return endpoint == "/v1/media/jobs" && accessscope.CanonicalExecutionEndpoint(endpoint) == endpoint
}

// ResolveExecutionForCommit repeats the family-aware read after acquiring the
// same principal and scope rows used by credential/Grant containment. The
// caller keeps tx open through the durable dispatch-state write.
func ResolveExecutionForCommit(ctx context.Context, tx pgx.Tx, binding ExecutionBinding) (Snapshot, error) {
	if tx == nil {
		return Snapshot{}, ErrInactive
	}
	initial, err := ResolveExecution(ctx, tx, binding)
	if err != nil {
		return Snapshot{}, err
	}
	ownerID := pointerID(initial.APIKey.OwnerUserID)
	workloadID := pointerID(initial.APIKey.WorkloadApplicationID)
	if err := principallock.LockUsers(ctx, tx, ownerID, initial.GrantSubjectUserID, initial.GrantSponsorUserID); err != nil {
		return Snapshot{}, err
	}
	if err := principallock.LockWorkloads(ctx, tx, workloadID, initial.GrantSubjectWorkloadID); err != nil {
		return Snapshot{}, err
	}
	if err := lockExecutionRows(ctx, tx, initial, binding); err != nil {
		return Snapshot{}, err
	}
	return ResolveExecution(ctx, tx, binding)
}

func lockExecutionRows(ctx context.Context, tx pgx.Tx, snapshot Snapshot, binding ExecutionBinding) error {
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM orgs WHERE id=$1 FOR SHARE`, binding.OrgID).Scan(&locked); err != nil {
		return executionResolveError(err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1 AND org_id=$2 FOR SHARE`, binding.ProjectID, binding.OrgID).Scan(&locked); err != nil {
		return executionResolveError(err)
	}
	if snapshot.APIKey.WorkloadApplicationID != nil {
		if err := tx.QueryRow(ctx, `SELECT id FROM workload_applications WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`,
			*snapshot.APIKey.WorkloadApplicationID, binding.OrgID, binding.ProjectID).Scan(&locked); err != nil {
			return executionResolveError(err)
		}
	}
	if binding.AccessGrantID > 0 {
		if err := tx.QueryRow(ctx, `SELECT id FROM access_grants WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`,
			binding.AccessGrantID, binding.OrgID, binding.ProjectID).Scan(&locked); err != nil {
			return executionResolveError(err)
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM access_credentials WHERE id=$1 AND credential_ref=$2 AND access_grant_id=$3 AND org_id=$4 AND project_id=$5 FOR SHARE`,
			binding.AccessCredentialID, binding.CredentialRef, binding.AccessGrantID, binding.OrgID, binding.ProjectID).Scan(&locked); err != nil {
			return executionResolveError(err)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM api_keys WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`,
		snapshot.APIKey.ID, binding.OrgID, binding.ProjectID).Scan(&locked); err != nil {
		return executionResolveError(err)
	}
	return nil
}

func commaScopeAllows(raw, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	values := strings.Split(raw, ",")
	if len(values) == 1 && strings.TrimSpace(values[0]) == "" {
		return true
	}
	for _, candidate := range values {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == value {
			return true
		}
	}
	return false
}

func pointerID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func executionResolveError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrInactive) {
		return ErrInactive
	}
	return err
}
