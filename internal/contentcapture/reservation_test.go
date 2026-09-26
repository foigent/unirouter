package contentcapture

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func reservationTestRequest() ReservationRequest {
	return ReservationRequest{OrgID: 1, ProjectID: 2, ContentRef: "uc1_" + strings.Repeat("a", 64), SourceFactID: "uf1_" + strings.Repeat("b", 64), Revision: 1, ExpectedPolicyVersion: "2", ExpectedRecoveryEpoch: 1,
		Protocol: "openai.chat", ObservationPoint: "client_request", MaxSavedBytes: 64, LeaseOwner: "synthetic-reservation-owner", LeaseTTL: 10 * time.Second, CreateStream: true}
}

func TestReservationRejectsInvalidInputBeforeDatabase(t *testing.T) {
	for name, mutate := range map[string]func(*ReservationRequest){
		"org": func(r *ReservationRequest) { r.OrgID = 0 }, "project": func(r *ReservationRequest) { r.ProjectID = -1 },
		"ref": func(r *ReservationRequest) { r.ContentRef = "../untrusted" }, "fact": func(r *ReservationRequest) { r.SourceFactID = "uc1_" + strings.Repeat("b", 64) },
		"revision": func(r *ReservationRequest) { r.Revision = 0 }, "epoch": func(r *ReservationRequest) { r.ExpectedRecoveryEpoch = 0 },
		"version": func(r *ReservationRequest) { r.ExpectedPolicyVersion = "02" }, "version overflow": func(r *ReservationRequest) { r.ExpectedPolicyVersion = "9223372036854775808" },
		"zero maximum": func(r *ReservationRequest) { r.MaxSavedBytes = 0 }, "negative maximum": func(r *ReservationRequest) { r.MaxSavedBytes = -1 }, "maximum overflow": func(r *ReservationRequest) { r.MaxSavedBytes = math.MaxInt64 },
		"owner": func(r *ReservationRequest) { r.LeaseOwner = "" }, "lease": func(r *ReservationRequest) { r.LeaseTTL = 0 }, "unrepresentable lease": func(r *ReservationRequest) { r.LeaseTTL = time.Nanosecond },
		"protocol": func(r *ReservationRequest) { r.Protocol = "" }, "point": func(r *ReservationRequest) { r.ObservationPoint = "" },
	} {
		t.Run(name, func(t *testing.T) {
			r := reservationTestRequest()
			mutate(&r)
			var s *PolicyStore
			_, err := s.Reserve(t.Context(), nil, r)
			require.ErrorIs(t, err, ErrReservationInvalid)
		})
	}
	var s *PolicyStore
	_, err := s.Reserve(nil, nil, reservationTestRequest())
	require.ErrorIs(t, err, ErrReservationInvalid)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.Reserve(ctx, nil, reservationTestRequest())
	require.ErrorIs(t, err, context.Canceled)
}

func TestReservationPreservesDeploymentStopsBeforeStoreAndDatabase(t *testing.T) {
	for _, change := range []func(*PolicyStoreOptions){func(o *PolicyStoreOptions) { o.DefaultMode = ModeDisabled }, func(o *PolicyStoreOptions) { o.FullContentEnabled = false }} {
		database := &policyReadDB{}
		options := policyStoreTestOptions()
		change(&options)
		s, err := NewPolicyStore(database, options)
		require.NoError(t, err)
		_, err = s.Reserve(t.Context(), nil, reservationTestRequest())
		require.ErrorIs(t, err, ErrPolicyInactive)
		require.Zero(t, database.calls)
	}
}

func TestReservationRequiresActualMarkerAndIndependentPool(t *testing.T) {
	files, root, binding := testFileStore(t)
	database := &policyReadDB{}
	s, err := NewPolicyStore(database, policyStoreTestOptions())
	require.NoError(t, err)
	_, err = s.Reserve(t.Context(), files, reservationTestRequest())
	require.ErrorIs(t, err, ErrReservationTransaction)
	// A transaction passed through DBQuerier must not be used as a savepoint.
	s.db = &policyWriteTx{}
	_, err = s.Reserve(t.Context(), files, reservationTestRequest())
	require.ErrorIs(t, err, ErrReservationTransaction)
	require.Zero(t, database.calls)
	// Alter only this task-owned marker: an identity string is insufficient.
	require.NoError(t, os.WriteFile(filepath.Join(root, storeMarkerName), []byte(`{"identity":"`+binding.Identity+`","token":"`+strings.Repeat("0", 64)+`"}`), 0600))
	_, err = s.Reserve(t.Context(), files, reservationTestRequest())
	require.ErrorIs(t, err, ErrStoreBinding)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1, "Reserve must never create body files")
}

func TestReservationQuotaArithmeticDoesNotOverflow(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		reserved, stored, amount, quota int64
		want                            bool
	}{
		{"exact", 40, 24, 64, 128, true}, {"one too many", 40, 25, 64, 128, false},
		{"all held", 128, 0, 1, 128, false}, {"all stored", 0, 128, 1, 128, false},
		{"addition overflow", math.MaxInt64, math.MaxInt64, 1, math.MaxInt64, false},
		{"near max", math.MaxInt64 - 2, 1, 1, math.MaxInt64, true},
		{"negative counter", -1, 0, 1, 2, false}, {"zero amount", 0, 0, 0, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, reservationFitsQuota(tc.reserved, tc.stored, tc.amount, tc.quota))
		})
	}
}

func TestReservationTimesUseServerClockAndFiniteRetention(t *testing.T) {
	now := time.Date(2026, 9, 15, 1, 2, 3, 123456789, time.UTC)
	lease, expires, err := reservationTimes(now, 10*time.Second+time.Nanosecond, 60)
	require.NoError(t, err)
	require.Equal(t, now.Truncate(time.Microsecond).Add(10*time.Second), lease)
	require.Equal(t, now.Truncate(time.Microsecond).Add(time.Minute), expires)
	for _, tc := range []struct {
		ttl       time.Duration
		retention int64
	}{{0, 60}, {61 * time.Second, 60}, {time.Second, 0}, {time.Second, math.MaxInt64}} {
		_, _, err := reservationTimes(now, tc.ttl, tc.retention)
		require.True(t, errors.Is(err, ErrReservationInvalid))
	}
}
