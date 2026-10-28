package upstreamrelease

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type Reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func Read(ctx context.Context, q Reader, orgID, deploymentID int64, lock bool) (State, Scope, error) {
	query := `SELECT d.customer_org_id,d.id,d.connection_id,c.provider,c.base_url,d.upstream_model,
 to_jsonb(d)->'upstream_release_observation',COALESCE((to_jsonb(d)->>'upstream_observation_revision')::bigint,0)
 FROM model_deployments d JOIN provider_connections c ON c.id=d.connection_id
 WHERE d.customer_org_id=$1 AND d.id=$2`
	if lock {
		query += ` FOR UPDATE OF d,c`
	}
	var state State
	var scope Scope
	var raw json.RawMessage
	err := q.QueryRow(ctx, query, orgID, deploymentID).Scan(&scope.CustomerOrgID, &scope.DeploymentID, &scope.ConnectionID,
		&scope.Provider, &scope.BaseURL, &scope.APIModel, &raw, &state.Revision)
	if err != nil {
		return state, scope, err
	}
	state.Observation, err = Decode(raw)
	if err != nil {
		return state, scope, err
	}
	// An origin change may make the old recorded scope stale; it is shown as such,
	// never silently rebound to the new endpoint. A new PUT creates a fresh epoch.
	if (state.Observation == nil) != (state.Revision == 0) {
		return state, scope, ErrInvalid
	}
	return state, scope, nil
}

func Probe(ctx context.Context, q Reader, capability bool, orgID, deploymentID, probeID int64) (*Observation, error) {
	query := `SELECT p.request_summary->'upstream_release_observation' FROM model_deployment_probes p
 JOIN model_deployments d ON d.id=p.deployment_id
 WHERE d.customer_org_id=$1 AND p.deployment_id=$2 AND p.probe_id=$3`
	if capability {
		query = `SELECT request_summary->'upstream_release_observation' FROM model_capability_probes
 WHERE customer_org_id=$1 AND deployment_id=$2 AND capability_probe_id=$3`
	}
	var raw json.RawMessage
	if err := q.QueryRow(ctx, query, orgID, deploymentID, probeID).Scan(&raw); err != nil {
		return nil, err
	}
	return Decode(raw)
}
