//go:build integration

package limitpolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F07PostgresRedisMultiInstanceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var projectID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Limit Project','limit-project','active') RETURNING id`, orgID).Scan(&projectID))
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (org_id,project_id,name,key_hash,key_prefix,status,rpm_limit,qps_limit,policy_labels)
		VALUES ($1,$2,'limit-key','unused','ur-limit-it','active',1000,1000,'production') RETURNING id`, orgID, projectID).Scan(&keyID))
	_, err := pool.Exec(ctx, `
		INSERT INTO distributed_limit_policies (
			org_id,project_id,policy_label,name,model,mode,failure_mode,
			rps_limit,rpm_limit,tpm_limit,concurrency_limit
		) VALUES ($1,$2,'production','integration-production','company-chat','enforce','fail_closed',1000,1000,1000,3)`, orgID, projectID)
	require.NoError(t, err)

	redisAddress := os.Getenv("REDIS_ADDR")
	if redisAddress == "" {
		redisAddress = "localhost:6379"
	}
	firstRedis := redis.NewClient(&redis.Options{Addr: redisAddress, Password: os.Getenv("REDIS_PASSWORD")})
	secondRedis := redis.NewClient(&redis.Options{Addr: redisAddress, Password: os.Getenv("REDIS_PASSWORD")})
	t.Cleanup(func() { _ = firstRedis.Close(); _ = secondRedis.Close() })
	require.NoError(t, firstRedis.Ping(ctx).Err())
	require.NoError(t, firstRedis.FlushDB(ctx).Err())
	options := DefaultOptions()
	options.GlobalConcurrency = 1_000
	options.OrgConcurrency = 1_000
	first, err := NewService(pool, firstRedis, options)
	require.NoError(t, err)
	second, err := NewService(pool, secondRedis, options)
	require.NoError(t, err)
	require.NoError(t, first.Refresh(ctx))
	require.NoError(t, second.Refresh(ctx))

	request := func(operation string, input, output int64) AdmissionRequest {
		return AdmissionRequest{
			OperationID: operation,
			Subject: Subject{
				OrgID: orgID, ProjectID: projectID, APIKeyID: keyID, Model: "company-chat",
				BaseRPSLimit: 1_000, BaseRPMLimit: 1_000,
				// The database snapshot is authoritative; caller-supplied labels
				// cannot remove the server-owned production selector.
				PolicyLabels: []string{"staging"},
			},
			EstimatedInput: input, ReservedOutput: output, LeaseTTL: time.Minute,
		}
	}

	var admitted atomic.Int64
	var denied atomic.Int64
	reservations := make(chan *Reservation, 20)
	var wg sync.WaitGroup
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			service := first
			if index%2 == 1 {
				service = second
			}
			reservation, reserveErr := service.Reserve(ctx, request(fmt.Sprintf("multi-%02d", index), 1, 1))
			if reserveErr == nil {
				admitted.Add(1)
				reservations <- reservation
			} else if errors.Is(reserveErr, ErrConcurrencyExceeded) {
				denied.Add(1)
			} else {
				t.Errorf("unexpected reserve error: %v", reserveErr)
			}
		}(index)
	}
	wg.Wait()
	close(reservations)
	require.Equal(t, int64(3), admitted.Load())
	require.Equal(t, int64(17), denied.Load())
	for reservation := range reservations {
		require.NoError(t, first.CancelBeforeDispatch(ctx, reservation))
	}

	large, err := first.Reserve(ctx, request("tpm-large", 20, 60))
	require.NoError(t, err)
	_, err = second.Reserve(ctx, request("tpm-too-large", 500, 500))
	require.ErrorIs(t, err, ErrTPMExceeded)
	require.NoError(t, first.Finalize(ctx, large, 30))
	remaining, err := second.Reserve(ctx, request("tpm-after-settle", 100, 100))
	require.NoError(t, err)
	require.NoError(t, second.CancelBeforeDispatch(ctx, remaining))

	keys := operationRuntimeKeys("policy:1", "integration-cluster-slot")
	for _, key := range keys[1:] {
		require.Equal(t, redisClusterTag(keys[0]), redisClusterTag(key))
	}
}

func redisClusterTag(key string) string {
	start := -1
	for index, r := range key {
		if r == '{' {
			start = index + 1
		}
		if r == '}' && start > 0 {
			return key[start:index]
		}
	}
	return ""
}
