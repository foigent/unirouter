// Package principallock owns the transaction-scoped serialization lane for
// user status, organization role, project membership, and sponsor authority.
package principallock

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
)

const userMutationLockBase int64 = 4120000000000000000

const workloadMutationLockBase int64 = 4130000000000000000

var ErrInvalidUserID = errors.New("invalid user mutation lock id")

// LockUser serializes every supported mutation that can change a user's live
// authority. Callers that lock multiple users must acquire them in ascending
// user-id order before calling this helper again for an already locked user.
func LockUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	if userID <= 0 || userID > (1<<63-1)-userMutationLockBase {
		return ErrInvalidUserID
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userMutationLockBase+userID)
	return err
}

// LockUsers acquires the shared principal lanes in ascending order. Repeated
// and zero IDs are ignored so callers can pass human/workload subject and
// sponsor projections without branching.
func LockUsers(ctx context.Context, tx pgx.Tx, userIDs ...int64) error {
	ids := append([]int64(nil), userIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	last := int64(0)
	for _, userID := range ids {
		if userID <= 0 || userID == last {
			continue
		}
		if err := LockUser(ctx, tx, userID); err != nil {
			return err
		}
		last = userID
	}
	return nil
}

// LockWorkloads serializes supported mutations of v2 workload identity state.
// It is acquired after user lanes and before Grant/request/application rows.
func LockWorkloads(ctx context.Context, tx pgx.Tx, workloadIDs ...int64) error {
	ids := append([]int64(nil), workloadIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	last := int64(0)
	for _, workloadID := range ids {
		if workloadID <= 0 || workloadID == last || workloadID > (1<<63-1)-workloadMutationLockBase {
			continue
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, workloadMutationLockBase+workloadID); err != nil {
			return err
		}
		last = workloadID
	}
	return nil
}
