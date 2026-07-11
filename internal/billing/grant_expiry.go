package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	internaldb "github.com/uniroute/unirouter/internal/db"
)

// GrantExpiryResult reports one bounded cleanup pass.
type GrantExpiryResult struct {
	Organizations int
	Grants        int
}

// ExpireGrants atomically removes each expired grant's remaining balance from
// the aggregate wallet and marks the grant terminal. Wallets are locked before
// grant rows, matching Freeze/Settle/Cancel and avoiding opposite-order
// deadlocks. A crash can no longer deduct the wallet in one transaction and
// leave the same grant active for a second deduction on the next cleanup pass.
func ExpireGrants(ctx context.Context, database internaldb.DBQuerier, maxOrganizations int) (GrantExpiryResult, error) {
	if maxOrganizations <= 0 {
		maxOrganizations = 1000
	}
	rows, err := database.Query(ctx,
		`SELECT DISTINCT org_id
		 FROM grants
		 WHERE status = 'active'
		   AND expires_at IS NOT NULL
		   AND expires_at < NOW()
		 ORDER BY org_id
		 LIMIT $1`, maxOrganizations)
	if err != nil {
		return GrantExpiryResult{}, fmt.Errorf("query organizations with expired grants: %w", err)
	}
	orgIDs := make([]int64, 0)
	for rows.Next() {
		var orgID int64
		if err := rows.Scan(&orgID); err != nil {
			rows.Close()
			return GrantExpiryResult{}, fmt.Errorf("scan organization with expired grants: %w", err)
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return GrantExpiryResult{}, fmt.Errorf("iterate organizations with expired grants: %w", err)
	}
	rows.Close()

	result := GrantExpiryResult{}
	for _, orgID := range orgIDs {
		expired, err := expireGrantsForOrganization(ctx, database, orgID)
		if err != nil {
			return result, err
		}
		if expired > 0 {
			result.Organizations++
			result.Grants += expired
		}
	}
	return result, nil
}

func expireGrantsForOrganization(ctx context.Context, database internaldb.DBQuerier, orgID int64) (int, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin grant expiry for org %d: %w", orgID, err)
	}
	defer tx.Rollback(ctx)

	var walletOrgID int64
	if err := tx.QueryRow(ctx,
		`SELECT org_id FROM wallets WHERE org_id = $1 FOR UPDATE`, orgID,
	).Scan(&walletOrgID); err != nil {
		if err == pgx.ErrNoRows {
			return 0, fmt.Errorf("expire grants for org %d: wallet not found", orgID)
		}
		return 0, fmt.Errorf("lock wallet for grant expiry: %w", err)
	}

	expired, _, err := expireGrantRowsInLockedWallet(ctx, tx, orgID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit grant expiry for org %d: %w", orgID, err)
	}
	return expired, nil
}

// expireGrantRowsInLockedWallet requires the caller to hold the wallet row
// lock. It is shared by scheduled cleanup and Freeze's lazy expiry check so
// credit cannot be spent in the interval between expires_at and the next job.
func expireGrantRowsInLockedWallet(ctx context.Context, tx pgx.Tx, orgID int64) (expiredCount, expiredBalance int, err error) {
	grantRows, err := tx.Query(ctx,
		`SELECT id, balance
		 FROM grants
		 WHERE org_id = $1
		   AND status = 'active'
		   AND expires_at IS NOT NULL
		   AND expires_at < NOW()
		 ORDER BY expires_at ASC, id ASC
		 FOR UPDATE`, orgID)
	if err != nil {
		return 0, 0, fmt.Errorf("lock expired grants for org %d: %w", orgID, err)
	}
	type expiredGrant struct {
		id      int64
		balance int
	}
	grants := make([]expiredGrant, 0)
	total := 0
	for grantRows.Next() {
		var grant expiredGrant
		if err := grantRows.Scan(&grant.id, &grant.balance); err != nil {
			grantRows.Close()
			return 0, 0, fmt.Errorf("scan expired grant for org %d: %w", orgID, err)
		}
		grants = append(grants, grant)
		total += grant.balance
	}
	if err := grantRows.Err(); err != nil {
		grantRows.Close()
		return 0, 0, fmt.Errorf("iterate expired grants for org %d: %w", orgID, err)
	}
	grantRows.Close()
	if len(grants) == 0 {
		return 0, 0, nil
	}

	for _, grant := range grants {
		if _, err := tx.Exec(ctx,
			`UPDATE grants
			 SET balance = 0, status = 'expired', updated_at = NOW()
			 WHERE id = $1 AND status = 'active'`, grant.id,
		); err != nil {
			return 0, 0, fmt.Errorf("mark grant %d expired: %w", grant.id, err)
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE wallets
		 SET granted_balance = GREATEST(granted_balance - $1, 0), updated_at = NOW()
		 WHERE org_id = $2`, total, orgID,
	); err != nil {
		return 0, 0, fmt.Errorf("deduct expired grant balance for org %d: %w", orgID, err)
	}
	return len(grants), total, nil
}
