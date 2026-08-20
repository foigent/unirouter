// Package executionprojection materializes the compact, query-oriented view of
// an immutable provider-attempt route receipt. It never joins mutable display
// names and it never selects an arbitrary attempt for a request.
package executionprojection

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

type Source string

const (
	SourceProviderAttempt            Source = "provider_attempt"
	SourcePrecontractProviderAttempt Source = "precontract_provider_attempt"
	SourceResponseCache              Source = "response_cache"
	SourceNoProviderAttempt          Source = "no_provider_attempt"
)

var sourceHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var ErrExpectedAttemptMissing = errors.New("executionprojection: expected provider attempt is missing")

type Projection struct {
	Source          Source                               `json:"source"`
	Compact         *executionevidence.CompactProjection `json:"compact,omitempty"`
	LegacyChannelID int64                                `json:"legacy_channel_id,omitempty"`
}

type ResolveInput struct {
	OrgID           int64
	RequestID       string
	AttemptID       string
	NoAttemptSource Source
	AttemptRequired bool
	// ExpectedLegacyChannelID is an optional consistency assertion, never a
	// lookup key or fallback identity. Zero means the caller has no legacy
	// channel assertion (for example a route-independent trace event).
	ExpectedLegacyChannelID int64
}

type Resolver interface {
	ResolveExecutionProjection(context.Context, ResolveInput) (Projection, error)
}

type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Resolve reads one exact provider attempt. A request-level latest/first lookup
// would make failover attribution nondeterministic and is intentionally absent.
func Resolve(ctx context.Context, db Querier, input ResolveInput) (Projection, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.AttemptID = strings.TrimSpace(input.AttemptID)
	if input.OrgID <= 0 || input.RequestID == "" || len(input.RequestID) > 64 || len(input.AttemptID) > 64 {
		return Projection{}, fmt.Errorf("executionprojection: invalid attempt lookup identity")
	}
	fallback := Projection{Source: input.NoAttemptSource}
	if _, err := Normalize(fallback); err != nil {
		return Projection{}, err
	}
	if input.AttemptID == "" {
		if input.AttemptRequired {
			return Projection{}, ErrExpectedAttemptMissing
		}
		return fallback, nil
	}
	if db == nil {
		return Projection{}, fmt.Errorf("executionprojection: evidence database is unavailable")
	}
	rows, err := db.Query(ctx, `
SELECT route_evidence_version,route_authority_mode,route_evidence_hash,
       route_public_model_id,deployment_id,route_connection_id,
       route_credential_binding_id,route_model_operation_id,
       route_runtime_release_id,route_snapshot_hash,channel_id,
       route_public_execution_binding_id,route_delivery_contract_version,
       route_charge_contract_version,route_idempotency_contract_version
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, input.OrgID, input.RequestID, input.AttemptID)
	if err != nil {
		return Projection{}, fmt.Errorf("executionprojection: read provider attempt: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Projection{}, fmt.Errorf("executionprojection: read provider attempt: %w", err)
		}
		if input.AttemptRequired {
			return Projection{}, ErrExpectedAttemptMissing
		}
		return fallback, nil
	}
	var version, mode, evidenceHash, operationID, snapshotHash sql.NullString
	var deliveryVersion, chargeVersion, idempotencyVersion sql.NullString
	var publicModelID, deploymentID, connectionID, bindingID, releaseID, channelID sql.NullInt64
	var publicExecutionBindingID sql.NullInt64
	if err := rows.Scan(
		&version, &mode, &evidenceHash,
		&publicModelID, &deploymentID, &connectionID,
		&bindingID, &operationID, &releaseID, &snapshotHash, &channelID,
		&publicExecutionBindingID, &deliveryVersion, &chargeVersion, &idempotencyVersion,
	); err != nil {
		return Projection{}, fmt.Errorf("executionprojection: scan provider attempt: %w", err)
	}
	if rows.Next() {
		return Projection{}, fmt.Errorf("executionprojection: duplicate exact provider attempt")
	}
	if err := rows.Err(); err != nil {
		return Projection{}, fmt.Errorf("executionprojection: read provider attempt: %w", err)
	}
	actualLegacyChannelID := nullableInt64(channelID)
	if input.ExpectedLegacyChannelID > 0 && input.ExpectedLegacyChannelID != actualLegacyChannelID {
		return Projection{}, fmt.Errorf("executionprojection: exact provider attempt route mismatch")
	}
	if !version.Valid {
		return Normalize(Projection{
			Source: SourcePrecontractProviderAttempt, LegacyChannelID: actualLegacyChannelID,
		})
	}
	compact := executionevidence.CompactProjection{
		ContractVersion:            version.String,
		AuthorityMode:              executionevidence.AuthorityMode(mode.String),
		EvidenceHash:               evidenceHash.String,
		PublicModelID:              nullableInt64(publicModelID),
		DeploymentID:               nullableInt64(deploymentID),
		ConnectionID:               nullableInt64(connectionID),
		CredentialBindingID:        nullableInt64(bindingID),
		ModelOperationID:           operationID.String,
		RuntimeReleaseID:           nullableInt64(releaseID),
		SnapshotHash:               snapshotHash.String,
		LegacyChannelID:            actualLegacyChannelID,
		PublicExecutionBindingID:   nullableInt64(publicExecutionBindingID),
		DeliveryContractVersion:    deliveryVersion.String,
		ChargeContractVersion:      chargeVersion.String,
		IdempotencyContractVersion: idempotencyVersion.String,
	}
	return Normalize(Projection{Source: SourceProviderAttempt, Compact: &compact})
}

func Normalize(value Projection) (Projection, error) {
	switch value.Source {
	case SourceProviderAttempt:
		if value.Compact == nil || value.LegacyChannelID != 0 {
			return Projection{}, fmt.Errorf("executionprojection: provider attempt requires compact evidence")
		}
		normalized, err := executionevidence.NormalizeCompact(*value.Compact)
		if err != nil {
			return Projection{}, fmt.Errorf("executionprojection: compact evidence: %w", err)
		}
		value.Compact = &normalized
	case SourcePrecontractProviderAttempt:
		if value.Compact != nil || value.LegacyChannelID <= 0 {
			return Projection{}, fmt.Errorf("executionprojection: invalid pre-contract attempt projection")
		}
	case SourceResponseCache, SourceNoProviderAttempt:
		if value.Compact != nil || value.LegacyChannelID != 0 {
			return Projection{}, fmt.Errorf("executionprojection: non-attempt source cannot carry route evidence")
		}
	default:
		return Projection{}, fmt.Errorf("executionprojection: invalid source %q", value.Source)
	}
	return value, nil
}

// BindSourceEventHash makes the compact receipt part of the existing event
// digest. It is deliberately one-way: the full route receipt stays in the
// immutable provider-attempt ledger.
func BindSourceEventHash(baseHash string, value Projection) (string, error) {
	baseHash = strings.TrimSpace(baseHash)
	if !sourceHashPattern.MatchString(baseHash) {
		return "", fmt.Errorf("executionprojection: invalid base source-event hash")
	}
	normalized, err := Normalize(value)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(struct {
		Contract   string     `json:"contract"`
		BaseHash   string     `json:"base_hash"`
		Projection Projection `json:"projection"`
	}{Contract: "unirouter.execution-projection.v1", BaseHash: baseHash, Projection: normalized})
	if err != nil {
		return "", fmt.Errorf("executionprojection: encode source binding: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func nullableInt64(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}
