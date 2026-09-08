package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// Empty is the existing internal v2 command contract. Explicit unknown versions
// are rejected; the runtime supplies a known version for every new stream.
func normalizeStreamCounterVersion(version string) (string, error) {
	if version == "" {
		version = metering.GatewayCounterEstimateVersionV2
	}
	if _, err := metering.CounterSource(version); err != nil {
		return "", err
	}
	return version, nil
}

// The relation lock precedes the ledger read: an expand/down transaction cannot
// change the column or publish a new-version lease between SQL selection and use.
// Never cache this choice across transactions (a bridge can survive M115).
func streamCounterColumnTx(ctx context.Context, tx pgx.Tx, prefix string) (string, bool, error) {
	if _, err := tx.Exec(ctx, `LOCK TABLE stream_delivery_leases IN ROW EXCLUSIVE MODE`); err != nil {
		return "", false, err
	}
	var expanded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.StreamCounterExpandHead).Scan(&expanded); err != nil {
		return "", false, err
	}
	if expanded {
		return prefix + "counter_version", true, nil
	}
	return "'" + metering.GatewayCounterEstimateVersionV2 + "'", false, nil
}

// Final provider usage is independent of the estimate pin. Counter settlement,
// including already-settled replay, must use the lease's immutable implementation.
func requireSettlementCounterVersionTx(ctx context.Context, tx pgx.Tx, req SettleRequest) error {
	if req.UsageAuthority != metering.UsageAuthorityGatewayCounter {
		return nil
	}
	column, expanded, err := streamCounterColumnTx(ctx, tx, "")
	if err != nil {
		return fmt.Errorf("read counter schema: %w", err)
	}
	if !expanded && req.UsageEstimateVersion != metering.GatewayCounterEstimateVersionV2 {
		return ErrSettlementUsageConflict
	}
	var version string
	err = tx.QueryRow(ctx, `SELECT `+column+` FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2 FOR UPDATE`, req.OrgID, req.RequestID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		// Historical v2 callers predate persistent stream leases.
		if req.UsageEstimateVersion == metering.GatewayCounterEstimateVersionV2 {
			return nil
		}
		return ErrSettlementUsageConflict
	}
	if err != nil {
		return err
	}
	if req.StreamFence != nil {
		expected, err := normalizeStreamCounterVersion(req.StreamFence.CounterVersion)
		if err != nil || expected != version {
			return ErrSettlementUsageConflict
		}
	}
	if !metering.ValidCounterPair(req.UsageSource, version) || req.UsageEstimateVersion != version {
		return ErrSettlementUsageConflict
	}
	return nil
}
