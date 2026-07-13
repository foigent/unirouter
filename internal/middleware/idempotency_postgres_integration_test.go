package middleware

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F17PostgresIdempotencyScopeReplayMismatchAndConcurrency(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	store := NewPostgresIdempotencyStore(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	keyHash := hashIdempotencyKey("shared-external-value", idempotencyTestSecret)
	fingerprint := strings.Repeat("a", 64)

	first := IdempotencyClaimInput{
		OrgID:              101,
		APIKeyID:           1001,
		KeyHash:            keyHash,
		RequestFingerprint: fingerprint,
		OperationID:        "op_00000000000000000000000000000001",
		ClientRequestID:    "client-correlation-1",
		CreatedAt:          now,
		ExpiresAt:          now.Add(DefaultIdempotencyTTL),
	}
	claim, err := store.Claim(ctx, first)
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, first.OperationID, claim.Record.OperationID)
	require.NoError(t, store.Complete(ctx, first.OrgID, first.APIKeyID, first.KeyHash, first.OperationID, IdempotencyStateCompleted, 200))
	_, err = pool.Exec(ctx, `
		UPDATE request_idempotency
		SET usage_authority='provider_reported', usage_purpose='customer_charge',
		    usage_finality='final', usage_contract_version='unirouter.usage.v1',
		    delivery_mode='nonstream', delivery_outcome='completed',
		    delivered_bytes=512, delivered_chunks=1, delivery_updated_at=NOW()
		WHERE org_id=$1 AND api_key_id=$2 AND key_hash=$3`, first.OrgID, first.APIKeyID, first.KeyHash)
	require.NoError(t, err)

	replay := first
	replay.OperationID = "op_00000000000000000000000000000002"
	replay.CreatedAt = now.Add(time.Second)
	replay.ExpiresAt = replay.CreatedAt.Add(DefaultIdempotencyTTL)
	claim, err = store.Claim(ctx, replay)
	require.NoError(t, err)
	require.False(t, claim.Claimed)
	require.Equal(t, first.OperationID, claim.Record.OperationID)
	require.Equal(t, IdempotencyStateCompleted, claim.Record.State)
	require.NotNil(t, claim.Record.OriginalHTTPStatus)
	require.Equal(t, 200, *claim.Record.OriginalHTTPStatus)
	require.Equal(t, "provider_reported", claim.Record.UsageAuthority)
	require.Equal(t, "customer_charge", claim.Record.UsagePurpose)
	require.Equal(t, "final", claim.Record.UsageFinality)
	require.Equal(t, "nonstream", claim.Record.DeliveryMode)
	require.Equal(t, "completed", claim.Record.DeliveryOutcome)
	require.EqualValues(t, 512, claim.Record.DeliveredBytes)

	mismatch := replay
	mismatch.OperationID = "op_00000000000000000000000000000003"
	mismatch.RequestFingerprint = strings.Repeat("b", 64)
	claim, err = store.Claim(ctx, mismatch)
	require.NoError(t, err)
	require.False(t, claim.Claimed)
	require.NotEqual(t, mismatch.RequestFingerprint, claim.Record.RequestFingerprint)

	// The same external key and fingerprint belong to independent tenant/key
	// namespaces; neither namespace can discover or block the other.
	crossOrg := replay
	crossOrg.OrgID = 202
	crossOrg.APIKeyID = 2002
	crossOrg.OperationID = "op_00000000000000000000000000000004"
	claim, err = store.Claim(ctx, crossOrg)
	require.NoError(t, err)
	require.True(t, claim.Claimed)

	// An expired metadata-only claim can be safely replaced without retaining or
	// replaying any model response.
	expiredKey := hashIdempotencyKey("expired-key", idempotencyTestSecret)
	expired := first
	expired.KeyHash = expiredKey
	expired.OperationID = "op_00000000000000000000000000000005"
	expired.CreatedAt = now.Add(-48 * time.Hour)
	expired.ExpiresAt = now.Add(-24 * time.Hour)
	claim, err = store.Claim(ctx, expired)
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	replacement := expired
	replacement.OperationID = "op_00000000000000000000000000000006"
	replacement.CreatedAt = now
	replacement.ExpiresAt = now.Add(DefaultIdempotencyTTL)
	claim, err = store.Claim(ctx, replacement)
	require.NoError(t, err)
	require.True(t, claim.Claimed)
	require.Equal(t, replacement.OperationID, claim.Record.OperationID)

	concurrentKey := hashIdempotencyKey("100-way-claim", idempotencyTestSecret)
	var won atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			operationID := fmt.Sprintf("op_%032x", 1000+index)
			result, claimErr := store.Claim(ctx, IdempotencyClaimInput{
				OrgID:              303,
				APIKeyID:           3003,
				KeyHash:            concurrentKey,
				RequestFingerprint: fingerprint,
				OperationID:        operationID,
				CreatedAt:          now.Add(2 * time.Second),
				ExpiresAt:          now.Add(2*time.Second + DefaultIdempotencyTTL),
			})
			if claimErr != nil {
				errs <- claimErr
				return
			}
			if result.Claimed {
				won.Add(1)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for claimErr := range errs {
		require.NoError(t, claimErr)
	}
	require.EqualValues(t, 1, won.Load())
	var concurrentRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM request_idempotency WHERE org_id = 303 AND api_key_id = 3003 AND key_hash = $1`,
		concurrentKey,
	).Scan(&concurrentRows))
	require.Equal(t, 1, concurrentRows)
}
