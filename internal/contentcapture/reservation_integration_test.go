//go:build integration

package contentcapture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// These tests use the existing isolated, migrated M119 fixture. Compile-only
// validation does not establish any PostgreSQL behavior or real authorization.
func reservationPGInput(org, project int64, label string) ReservationRequest {
	r := reservationTestRequest()
	r.OrgID = org
	r.ProjectID = project
	sum := sha256.Sum256([]byte("synthetic-reservation-ref:" + label))
	r.ContentRef = "uc1_" + hex.EncodeToString(sum[:])
	sum = sha256.Sum256([]byte("synthetic-reservation-fact:" + label))
	r.SourceFactID = "uf1_" + hex.EncodeToString(sum[:])
	return r
}

func reservationPGPolicy(t *testing.T, pool *pgxpool.Pool, s *PolicyStore, org, project int64, config PolicyConfig, expected string, apply bool) PolicyView {
	t.Helper()
	tx := policyStorePGTx(t, pool, org, project)
	view, err := s.PutTx(t.Context(), tx, org, project, PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: expected, Desired: config}, PolicyApplication{StorageStatus: "ready", ApplyNow: apply, Now: time.Now()})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return view
}

func reservationPGCounts(t *testing.T, pool *pgxpool.Pool, org, project int64) (reserved, stored, objects, streams int64) {
	t.Helper()
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COALESCE((SELECT reserved_bytes FROM execution_content_policies WHERE org_id=$1 AND project_id=$2),0),COALESCE((SELECT stored_bytes FROM execution_content_policies WHERE org_id=$1 AND project_id=$2),0),(SELECT count(*) FROM execution_content_objects WHERE org_id=$1 AND project_id=$2),(SELECT count(*) FROM execution_content_streams WHERE org_id=$1 AND project_id=$2)`, org, project).Scan(&reserved, &stored, &objects, &streams))
	return
}

func TestReservationPGRequiresExplicitEffectiveFullPolicy(t *testing.T) {
	for _, mode := range []string{"absent", ModeMetadataOnly, ModeDisabled, "pending_full"} {
		t.Run(mode, func(t *testing.T) {
			pool, s, org, project := setupPolicyStorePG(t)
			files, root, _ := testFileStore(t)
			switch mode {
			case ModeMetadataOnly:
				reservationPGPolicy(t, pool, s, org, project, testMetadataPolicy(), "1", true)
			case ModeDisabled:
				c := testMetadataPolicy()
				c.Mode = ModeDisabled
				reservationPGPolicy(t, pool, s, org, project, c, "1", true)
			case "pending_full":
				reservationPGPolicy(t, pool, s, org, project, testFullPolicy(), "1", false)
			}
			_, err := s.Reserve(t.Context(), files, reservationPGInput(org, project, mode))
			require.ErrorIs(t, err, ErrPolicyInactive)
			reserved, stored, objects, streams := reservationPGCounts(t, pool, org, project)
			require.Zero(t, reserved)
			require.Zero(t, stored)
			require.Zero(t, objects)
			require.Zero(t, streams, "failed explicit initialization must roll back")
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestReservationPGPersistsExactBindingAndNoReadAuthority(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	files, root, binding := testFileStore(t)
	policy := reservationPGPolicy(t, pool, s, org, project, testFullPolicy(), "1", true)
	input := reservationPGInput(org, project, "binding")
	reservation, err := s.Reserve(t.Context(), files, input)
	require.NoError(t, err)
	require.Equal(t, policy.Effective, reservation.Effective)
	require.Equal(t, policy.EffectiveAt, reservation.EffectiveAt)
	require.Equal(t, binding, reservation.Binding)
	require.Equal(t, int64(1), reservation.RecoveryEpoch)
	require.True(t, validStoreName(reservation.ObjectName))
	var ref, fact, identity, token, path, state, owner string
	var revision, version, epoch, bytes, saved int64
	var expires, lease time.Time
	var digest *string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT content_ref,source_fact_id,revision,policy_version,recovery_epoch,storage_identity,storage_binding_token,relative_path,state,reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at FROM execution_content_objects WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&ref, &fact, &revision, &version, &epoch, &identity, &token, &path, &state, &bytes, &saved, &digest, &expires, &owner, &lease))
	require.Equal(t, input.ContentRef, ref)
	require.Equal(t, input.SourceFactID, fact)
	require.Equal(t, input.Revision, revision)
	require.Equal(t, int64(2), version)
	require.Equal(t, int64(1), epoch)
	require.Equal(t, binding.Identity, identity)
	require.Equal(t, binding.Token, token)
	require.Equal(t, reservation.ObjectName, path)
	require.Equal(t, "reserved", state)
	require.Equal(t, input.MaxSavedBytes, bytes)
	require.Zero(t, saved)
	require.Nil(t, digest)
	require.Equal(t, input.LeaseOwner, owner)
	require.True(t, expires.Equal(reservation.ExpiresAt))
	require.True(t, lease.Equal(reservation.LeaseExpiresAt))
	require.Equal(t, 50*time.Second, expires.Sub(lease))
	var scopes, keys int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_content_scopes WHERE org_id=$1 AND project_id=$2),(SELECT count(*) FROM api_keys WHERE org_id=$1 AND project_id=$2)`, org, project).Scan(&scopes, &keys))
	require.Zero(t, scopes)
	require.Zero(t, keys)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "reservation creates no object or temporary file")
	_, err = s.Reserve(t.Context(), files, input)
	require.ErrorIs(t, err, ErrReservationConflict)
	input.ContentRef = reservationPGInput(org, project, "different-ref").ContentRef
	_, err = s.Reserve(t.Context(), files, input)
	require.ErrorIs(t, err, ErrReservationConflict, "same fact/revision cannot reserve twice")
	reserved, stored, objects, streams := reservationPGCounts(t, pool, org, project)
	require.Equal(t, int64(64), reserved)
	require.Zero(t, stored)
	require.Equal(t, int64(1), objects)
	require.Equal(t, int64(1), streams)
	// Later policy changes do not rewrite the original reservation version.
	reservationPGPolicy(t, pool, s, org, project, testMetadataPolicy(), "2", true)
	var originalConfig []byte
	var originalEffectiveAt time.Time
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT policy_version,policy_config,policy_effective_at FROM execution_content_objects WHERE content_ref=$1`, reservation.ContentRef).Scan(&version, &originalConfig, &originalEffectiveAt))
	require.Equal(t, int64(2), version)
	decoded, err := DecodePolicyConfig(originalConfig)
	require.NoError(t, err)
	require.Equal(t, policy.Effective.Config, decoded)
	require.Equal(t, policy.EffectiveAt, originalEffectiveAt.UTC().Format("2006-01-02T15:04:05.000000Z"))
	require.Equal(t, ModeFullContent, reservation.Effective.Config.Mode)
}

func TestReservationPGConcurrentQuotaCountsReservedAndStored(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	files, _, _ := testFileStore(t)
	config := testFullPolicy()
	config.QuotaBytes = policyNumber(100)
	reservationPGPolicy(t, pool, s, org, project, config, "1", true)
	// Synthetic existing stored usage leaves exactly one 64-byte reservation.
	_, err := pool.Exec(t.Context(), `UPDATE execution_content_policies SET stored_bytes=36 WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, label := range []string{"quota-one", "quota-two"} {
		input := reservationPGInput(org, project, label)
		go func() { <-start; _, err := s.Reserve(ctx, files, input); results <- err }()
	}
	close(start)
	var successes, rejections int
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err == nil {
				successes++
			} else if errors.Is(err, ErrReservationQuota) {
				rejections++
			} else {
				t.Fatalf("unexpected reservation error: %v", err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, rejections)
	reserved, stored, objects, _ := reservationPGCounts(t, pool, org, project)
	require.Equal(t, int64(64), reserved)
	require.Equal(t, int64(36), stored)
	require.Equal(t, int64(1), objects)
	// Expiry is not an implicit refund; this allocation remains charged until
	// the explicitly separate reconciliation owner handles it.
	_, err = pool.Exec(t.Context(), `UPDATE execution_content_objects SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, err)
	_, err = s.Reserve(t.Context(), files, reservationPGInput(org, project, "expired-lease-still-counts"))
	require.ErrorIs(t, err, ErrReservationQuota)
}

func TestReservationPGRereadsPolicyAfterCompetingCAS(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	files, _, _ := testFileStore(t)
	reservationPGPolicy(t, pool, s, org, project, testFullPolicy(), "1", true)
	tx := policyStorePGTx(t, pool, org, project)
	config := testFullPolicy()
	config.QuotaBytes = policyNumber(512)
	_, err := s.PutTx(t.Context(), tx, org, project, PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "2", Desired: config}, PolicyApplication{StorageStatus: "ready", ApplyNow: true, Now: time.Now()})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := s.Reserve(ctx, files, reservationPGInput(org, project, "policy-race"))
		result <- err
	}()
	// Observe the actual row-lock wait before committing the competing policy.
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%application_reason,reserved_bytes,stored_bytes%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(t.Context()))
	select {
	case err := <-result:
		require.ErrorIs(t, err, ErrPolicyVersionConflict)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	reserved, stored, objects, streams := reservationPGCounts(t, pool, org, project)
	require.Zero(t, reserved)
	require.Zero(t, stored)
	require.Zero(t, objects)
	require.Zero(t, streams)
}

func TestReservationPGChecksSourceEpochAndPolicyLimits(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	files, _, _ := testFileStore(t)
	reservationPGPolicy(t, pool, s, org, project, testFullPolicy(), "1", true)
	input := reservationPGInput(org, project, "scope")
	input.CreateStream = false
	_, err := s.Reserve(t.Context(), files, input)
	require.ErrorIs(t, err, ErrReservationUnconfigured)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance,recovery_epoch) VALUES($1,$2,'wrong-source',4)`, org, project)
	require.NoError(t, err)
	_, err = s.Reserve(t.Context(), files, input)
	require.ErrorIs(t, err, ErrReservationScope)
	_, err = pool.Exec(t.Context(), `UPDATE execution_content_streams SET source_instance=$3 WHERE org_id=$1 AND project_id=$2`, org, project, s.options.SourceInstance)
	require.NoError(t, err)
	_, err = s.Reserve(t.Context(), files, input)
	require.ErrorIs(t, err, ErrReservationScope)
	input.ExpectedRecoveryEpoch = 4
	for _, mutate := range []func(*ReservationRequest){func(r *ReservationRequest) { r.MaxSavedBytes = 65 }, func(r *ReservationRequest) { r.LeaseTTL = 61 * time.Second }} {
		bad := input
		mutate(&bad)
		_, err = s.Reserve(t.Context(), files, bad)
		require.ErrorIs(t, err, ErrReservationInvalid)
	}
	bad := input
	bad.ObservationPoint = "provider_request"
	_, err = s.Reserve(t.Context(), files, bad)
	require.ErrorIs(t, err, ErrPolicyInactive)
	result, err := s.Reserve(t.Context(), files, input)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.RecoveryEpoch)
}

func TestReservationPGObjectFailureRollsBackQuotaAndNewStream(t *testing.T) {
	pool, s, org, project := setupPolicyStorePG(t)
	files, _, _ := testFileStore(t)
	reservationPGPolicy(t, pool, s, org, project, testFullPolicy(), "1", true)
	var before, after string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_jsonb(p)::text FROM execution_content_policies p WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&before))
	// Exact, test-owned objects in this fixture's isolated database only.
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION synthetic_reservation_insert_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic reservation insert failure'; END $$`)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS synthetic_reservation_insert_failure ON execution_content_objects`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DROP FUNCTION IF EXISTS synthetic_reservation_insert_failure()`)
		require.NoError(t, err)
	})
	_, err = pool.Exec(t.Context(), `CREATE TRIGGER synthetic_reservation_insert_failure BEFORE INSERT ON execution_content_objects FOR EACH ROW EXECUTE FUNCTION synthetic_reservation_insert_failure()`)
	require.NoError(t, err)
	input := reservationPGInput(org, project, "rollback")
	_, err = s.Reserve(t.Context(), files, input)
	require.ErrorContains(t, err, "synthetic reservation insert failure")
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_jsonb(p)::text FROM execution_content_policies p WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&after))
	require.Equal(t, before, after)
	reserved, stored, objects, streams := reservationPGCounts(t, pool, org, project)
	require.Zero(t, reserved)
	require.Zero(t, stored)
	require.Zero(t, objects)
	require.Zero(t, streams)
	_, err = pool.Exec(t.Context(), `DROP TRIGGER synthetic_reservation_insert_failure ON execution_content_objects`)
	require.NoError(t, err)
	_, err = s.Reserve(t.Context(), files, input)
	require.NoError(t, err)
}
