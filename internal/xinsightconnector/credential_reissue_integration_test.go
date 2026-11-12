//go:build integration

package xinsightconnector

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type undeliveredRecoveryFixture struct {
	journal       *PostgresPhaseJournal
	clock         *time.Time
	oldID         string
	oldTicket     string
	newTicket     string
	newResolution CredentialRecoveryResolution
	currentExpiry time.Time
}

func setupUndeliveredRecovery(t *testing.T) undeliveredRecoveryFixture {
	t.Helper()
	journal, clock := setupCredentialLifecycle(t)
	ctx := t.Context()
	*clock = clock.Add(6 * time.Hour)

	tx, err := journal.db.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	installation, err := readInstallationByIdentity(ctx, tx, cp2aInstallationID, false)
	require.NoError(t, err)
	binding, err := readControlBinding(ctx, tx, cp2aInstallationID, false)
	require.NoError(t, err)

	oldTicket := "xirt_" + strings.Repeat("A", 43)
	oldResolution := CredentialRecoveryResolution{
		SchemaVersion: CredentialRecoverySchema, InstallationID: installation.InstallationID,
		DeploymentID: installation.XDeploymentID, ProjectID: binding.controlProjectID,
		FinalEventScope: []string{installation.EventScope}, SourceBindingSHA256: installation.SourceBindingSHA256,
		XInsightOrigin: "https://control.example.test", UniRouterOrigin: "https://ur.example.test",
		RecoveryID: "xrr_" + strings.Repeat("A", 22), PreviousTokenSHA256: binding.controlTokenSHA256,
		ExpiresAt: clock.Add(15 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
	journal.controlBearer = func() (string, error) { return "xib_B" + cp2aControlToken[5:], nil }
	oldID, err := journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, oldTicket, oldResolution)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	// The old ticket expires without an outbound attempt. The same peer
	// issues a different ticket against the same expired current credential.
	*clock = clock.Add(16 * time.Minute)
	newResolution := oldResolution
	newResolution.RecoveryID = "xrr_" + strings.Repeat("B", 22)
	newResolution.ExpiresAt = clock.Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)
	journal.controlBearer = func() (string, error) { return "xib_C" + cp2aControlToken[5:], nil }
	return undeliveredRecoveryFixture{
		journal: journal, clock: clock, oldID: oldID, oldTicket: oldTicket,
		newTicket: "xirt_" + strings.Repeat("B", 43), newResolution: newResolution, currentExpiry: binding.controlTokenExpiresAt.Time,
	}
}

func TestControlCredentialRecoveryReissueOfUndeliveredOperationPostgresIntegration(t *testing.T) {
	f := setupUndeliveredRecovery(t)
	ctx := t.Context()
	journal := f.journal

	for _, tc := range []struct {
		name string
		edit func(*CredentialRecoveryResolution, *string)
	}{
		{"same_ticket_different_id", func(_ *CredentialRecoveryResolution, ticket *string) { *ticket = f.oldTicket }},
		{"same_id_different_ticket", func(r *CredentialRecoveryResolution, _ *string) {
			r.RecoveryID = "xrr_" + strings.Repeat("A", 22)
		}},
		{"wrong_peer_predecessor", func(r *CredentialRecoveryResolution, _ *string) {
			r.PreviousTokenSHA256 = hashBytes([]byte("different predecessor"))
		}},
		{"wrong_scope", func(r *CredentialRecoveryResolution, _ *string) { r.ProjectID = "other.control.project" }},
		{"unrelated_peer_prepared", func(r *CredentialRecoveryResolution, _ *string) {
			r.SupersededOperationID = "xcr_" + strings.Repeat("Z", 22)
			r.SupersededReplacementSHA256 = hashBytes([]byte("different replacement"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolution, ticket := f.newResolution, f.newTicket
			tc.edit(&resolution, &ticket)
			tx, err := journal.db.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(ctx)
			_, err = journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, ticket, resolution)
			require.ErrorIs(t, err, ErrConflict)
		})
	}
	t.Run("fence_drift", func(t *testing.T) {
		tx, err := journal.db.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, "UPDATE xinsight_connector_installations SET control_fence=control_fence+1 WHERE installation_id=$1", cp2aInstallationID)
		require.NoError(t, err)
		_, err = journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, f.newTicket, f.newResolution)
		require.ErrorIs(t, err, ErrConflict)
	})

	type result struct {
		id  string
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			tx, err := journal.db.Begin(ctx)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer tx.Rollback(ctx)
			id, err := journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, f.newTicket, f.newResolution)
			if err == nil {
				err = tx.Commit(ctx)
			}
			results <- result{id, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.NotEqual(t, f.oldID, first.id)
	require.Equal(t, first.id, second.id, "concurrent confirmations of the same ticket reuse one operation")

	var oldState, localPredecessor string
	require.NoError(t, journal.db.QueryRow(ctx, "SELECT state FROM xinsight_connector_credential_operations WHERE operation_id=$1", f.oldID).Scan(&oldState))
	require.Equal(t, "superseded", oldState)
	require.NoError(t, journal.db.QueryRow(ctx, "SELECT superseded_operation_id FROM xinsight_connector_credential_operations WHERE operation_id=$1", first.id).Scan(&localPredecessor))
	require.Equal(t, f.oldID, localPredecessor)

	pending, err := journal.PendingCredential(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, first.id, pending.request.OperationID)
	require.Equal(t, "pending", pending.state)
	require.NotNil(t, pending.request.SupersededOperationID)
	require.Empty(t, *pending.request.SupersededOperationID, "the local predecessor was never accepted by the peer")
	status, err := journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.True(t, f.currentExpiry.Equal(status.CurrentExpiresAt), "confirmation does not promote the replacement")

	confirmedExpiry := f.clock.Add(24 * time.Hour)
	require.NoError(t, journal.RecordCredentialReceipt(ctx, pending, credentialLifecycleReceipt(pending, confirmedExpiry, "committed")))
	require.NoError(t, journal.PromoteCredential(ctx, cp2aInstallationID, first.id))
	status, err = journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "healthy", status.State)
	require.True(t, confirmedExpiry.Equal(status.CurrentExpiresAt))
}

func TestControlCredentialRecoveryReissueRejectsKnownPeerReceiptPostgresIntegration(t *testing.T) {
	for _, peerState := range []string{"prepared", "committed"} {
		t.Run(peerState, func(t *testing.T) {
			f := setupUndeliveredRecovery(t)
			ctx := t.Context()
			old, err := f.journal.PendingCredential(ctx, cp2aInstallationID)
			require.NoError(t, err)
			require.NoError(t, f.journal.RecordCredentialReceipt(ctx, old, credentialLifecycleReceipt(old, f.clock.Add(time.Hour), peerState)))

			tx, err := f.journal.db.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(ctx)
			_, err = f.journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, f.newTicket, f.newResolution)
			require.ErrorIs(t, err, ErrConflict)
			require.NoError(t, tx.Rollback(ctx))
			retained, err := f.journal.PendingCredential(ctx, cp2aInstallationID)
			require.NoError(t, err)
			require.Equal(t, f.oldID, retained.request.OperationID)
			require.NotNil(t, retained.receipt)
		})
	}
}

func TestControlCredentialRecoveryReissueChecksExpiryAfterInstallationLockPostgresIntegration(t *testing.T) {
	f := setupUndeliveredRecovery(t)
	ctx := t.Context()
	blocker, err := f.journal.db.Begin(ctx)
	require.NoError(t, err)
	defer blocker.Rollback(ctx)
	_, err = readInstallationByIdentity(ctx, blocker, cp2aInstallationID, true)
	require.NoError(t, err)

	var expired atomic.Bool
	before := *f.clock
	f.journal.now = func() time.Time {
		if expired.Load() {
			return before.Add(time.Hour)
		}
		return before
	}
	pid := make(chan int32, 1)
	done := make(chan error, 1)
	go func() {
		tx, err := f.journal.db.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer tx.Rollback(ctx)
		var backendPID int32
		if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
			done <- err
			return
		}
		pid <- backendPID
		_, err = f.journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, f.newTicket, f.newResolution)
		done <- err
	}()
	var backendPID int32
	select {
	case backendPID = <-pid:
	case err := <-done:
		t.Fatalf("confirmation did not reach the installation lock: %v", err)
	}
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.journal.db.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1))>0", backendPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	expired.Store(true)
	require.NoError(t, blocker.Rollback(ctx))
	require.ErrorIs(t, <-done, ErrConflict)
	pending, err := f.journal.PendingCredential(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, f.oldID, pending.request.OperationID)
}
