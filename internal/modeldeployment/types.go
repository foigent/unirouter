// Package modeldeployment owns the expand-phase canonical provider connection,
// model deployment and credential-binding reconciliation contract. It does not
// select routes or change the legacy gateway authority.
package modeldeployment

import (
	"errors"
	"time"
)

const (
	ReconcileContractVersion  = "unirouter.model-deployment-reconcile.v1"
	LegacyOperationID         = "legacy.any.v1"
	LegacyProbeProfileID      = "legacy.channel_model_probe"
	LegacyProbeProfileVersion = "v1"
)

var (
	ErrIncomplete         = errors.New("modeldeployment: canonical projection incomplete")
	ErrRetiredIdentity    = errors.New("modeldeployment: retired canonical identity cannot be reactivated")
	ErrCanonicalAuthority = errors.New("modeldeployment: current v2 runtime authority rejects legacy reconciliation")
)

type Issue struct {
	Severity     string `json:"severity"`
	Code         string `json:"code"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Message      string `json:"message"`
}

type Counts struct {
	LegacyConnections                 int `json:"legacy_connections"`
	CanonicalConnections              int `json:"canonical_connections"`
	LegacyConfiguredModels            int `json:"legacy_configured_models"`
	CanonicalDeployments              int `json:"canonical_deployments"`
	LegacyCredentials                 int `json:"legacy_credentials"`
	CanonicalCredentials              int `json:"canonical_credentials"`
	ExpectedCredentialDeploymentLinks int `json:"expected_credential_deployment_links"`
	CredentialDeploymentLinks         int `json:"credential_deployment_links"`
	CanonicalOperations               int `json:"canonical_operations"`
	LegacyProbes                      int `json:"legacy_probes"`
	CanonicalProbes                   int `json:"canonical_probes"`
	CurrentRouteOverrides             int `json:"current_route_overrides"`
	CurrentRouteItems                 int `json:"current_route_items"`
	HistoricalRouteItems              int `json:"historical_route_items"`
	CallLogsAttributed                int `json:"call_logs_attributed"`
	ProviderAttemptsAttributed        int `json:"provider_attempts_attributed"`
	ConnectionsRetired                int `json:"connections_retired"`
	DeploymentsRetired                int `json:"deployments_retired"`
	CredentialsRetired                int `json:"credentials_retired"`
}

type Report struct {
	ContractVersion string    `json:"contract_version"`
	CustomerOrgID   int64     `json:"customer_org_id"`
	GeneratedAt     time.Time `json:"generated_at"`
	Ready           bool      `json:"ready"`
	Counts          Counts    `json:"counts"`
	BlockingIssues  []Issue   `json:"blocking_issues"`
	Warnings        []Issue   `json:"warnings"`
	Limitations     []string  `json:"limitations"`
}
