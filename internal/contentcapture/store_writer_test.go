package contentcapture

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testReservationWriter(t *testing.T, s *FileStore, binding StoreBinding, limits StoreLimits) *ReservationWriter {
	t.Helper()
	name, err := randomStoreName()
	require.NoError(t, err)
	w, err := s.BeginReservation(t.Context(), Reservation{Binding: binding, ObjectName: name, ReservedBytes: limits.MaxSavedBytes}, limits)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	return w
}

func TestReservationWritersCompleteIndependently(t *testing.T) {
	s, root, binding := testFileStore(t)
	limits := testStoreLimits()
	a := testReservationWriter(t, s, binding, limits)
	b := testReservationWriter(t, s, binding, limits)
	require.NoError(t, a.WriteChunk(t.Context(), []byte("first")))
	require.NoError(t, b.WriteChunk(t.Context(), []byte{0, 255, 7}))
	bReceipt, err := b.Commit(t.Context())
	require.NoError(t, err, "B must finish while A has no EOF/Commit")
	_, err = os.Stat(filepath.Join(root, a.reservedName))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, a.WriteChunk(t.Context(), []byte("second")))
	aReceipt, err := a.Commit(t.Context())
	require.NoError(t, err)
	for _, test := range []struct {
		receipt StoredReceipt
		body    []byte
	}{{aReceipt, []byte("firstsecond")}, {bReceipt, []byte{0, 255, 7}}} {
		f, err := s.OpenVerified(t.Context(), test.receipt, limits)
		require.NoError(t, err)
		got, err := io.ReadAll(f)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		require.Equal(t, test.body, got)
	}
	zero := testReservationWriter(t, s, binding, limits)
	receipt, err := zero.Commit(t.Context())
	require.NoError(t, err)
	require.Zero(t, receipt.SavedBytes)
	f, err := s.OpenVerified(t.Context(), receipt, limits)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	for _, w := range []*ReservationWriter{a, b, zero} {
		require.ErrorIs(t, w.WriteChunk(t.Context(), nil), ErrStoreWriterClosed)
		_, err := w.Commit(t.Context())
		require.ErrorIs(t, err, ErrStoreWriterClosed)
		require.NoError(t, w.Close())
		_, err = w.file.Stat()
		require.ErrorIs(t, err, os.ErrClosed)
	}
}

func TestReservationWriterFailuresAreTerminal(t *testing.T) {
	for _, kind := range []string{"chunk_limit", "total_limit", "cancel", "nil_context", "abort", "marker", "temp_identity", "final_conflict"} {
		t.Run(kind, func(t *testing.T) {
			s, root, binding := testFileStore(t)
			limits := StoreLimits{MaxSavedBytes: 10, MaxReadBytes: 10, MaxBufferBytes: 7}
			if kind == "chunk_limit" {
				limits.MaxSavedBytes, limits.MaxReadBytes = 1024, 1024
			}
			w := testReservationWriter(t, s, binding, limits)
			require.NoError(t, w.WriteChunk(t.Context(), []byte("1234567")))
			temp := w.tempName
			var err error
			switch kind {
			case "chunk_limit":
				err = w.WriteChunk(t.Context(), []byte("12345678"))
				require.ErrorIs(t, err, ErrStoreLimit)
			case "total_limit":
				err = w.WriteChunk(t.Context(), []byte("8901"))
				require.ErrorIs(t, err, ErrStoreLimit)
			case "cancel":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				err = w.WriteChunk(ctx, []byte("8"))
				require.ErrorIs(t, err, context.Canceled)
			case "nil_context":
				err = w.WriteChunk(nil, []byte("8"))
			case "abort":
				require.NoError(t, w.Close())
			case "marker":
				require.NoError(t, os.WriteFile(filepath.Join(root, storeMarkerName), []byte("changed"), 0600))
				_, err = w.Commit(t.Context())
				require.ErrorIs(t, err, ErrStoreBinding)
			case "temp_identity":
				require.NoError(t, os.Rename(filepath.Join(root, temp), filepath.Join(root, temp+".original")))
				require.NoError(t, os.WriteFile(filepath.Join(root, temp), []byte("foreign"), 0600))
				_, err = w.Commit(t.Context())
				require.ErrorIs(t, err, ErrStorePath)
			case "final_conflict":
				require.NoError(t, os.WriteFile(filepath.Join(root, w.reservedName), []byte("original"), 0600))
				_, err = w.Commit(t.Context())
				require.ErrorIs(t, err, os.ErrExist)
				got, readErr := os.ReadFile(filepath.Join(root, w.reservedName))
				require.NoError(t, readErr)
				require.Equal(t, "original", string(got))
			}
			if kind != "abort" {
				var failure *StoreError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, temp, failure.TempName)
				require.EqualValues(t, 7, failure.WrittenBytes)
			}
			_, err = w.file.Stat()
			require.ErrorIs(t, err, os.ErrClosed)
			require.ErrorIs(t, w.WriteChunk(context.Background(), nil), ErrStoreWriterClosed)
			receipt, err := w.Commit(context.Background())
			require.ErrorIs(t, err, ErrStoreWriterClosed)
			require.Empty(t, receipt)
			require.NoError(t, w.Close())
		})
	}
}

type cancelAtSecondCheck struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelAtSecondCheck) Err() error {
	c.checks++
	if c.checks == 2 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestReservationWriterPostLinkFailureRetainsObjectWithoutReceipt(t *testing.T) {
	s, root, binding := testFileStore(t)
	w := testReservationWriter(t, s, binding, testStoreLimits())
	require.NoError(t, w.WriteChunk(t.Context(), []byte("bytes")))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	receipt, err := w.Commit(&cancelAtSecondCheck{Context: ctx, cancel: cancel})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, receipt)
	var failure *StoreError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "complete", failure.Stage)
	require.Equal(t, w.reservedName, failure.ObjectName)
	require.Empty(t, failure.TempName)
	require.EqualValues(t, 5, failure.WrittenBytes)
	got, err := os.ReadFile(filepath.Join(root, failure.ObjectName))
	require.NoError(t, err)
	require.Equal(t, "bytes", string(got))
	_, err = w.file.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = w.Commit(t.Context())
	require.ErrorIs(t, err, ErrStoreWriterClosed)
}

func TestReservationWriterSameNameCannotOpenTwoWriters(t *testing.T) {
	s, _, binding := testFileStore(t)
	r := Reservation{Binding: binding, ObjectName: strings.Repeat("c", 64), ReservedBytes: 1024}
	w, err := s.BeginReservation(t.Context(), r, testStoreLimits())
	require.NoError(t, err)
	defer w.Close()
	_, err = s.BeginReservation(t.Context(), r, testStoreLimits())
	require.ErrorIs(t, err, os.ErrExist)
	_, err = w.Commit(t.Context())
	require.NoError(t, err)
	_, err = s.BeginReservation(t.Context(), r, testStoreLimits())
	require.ErrorIs(t, err, os.ErrExist)
}

type storeReaderFunc func([]byte) (int, error)

func (f storeReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestFileStoreReaderTerminalSemantics(t *testing.T) {
	for _, kind := range []string{"bytes_eof", "bytes_error", "zero", "negative", "excess_count", "cancel_after_read"} {
		t.Run(kind, func(t *testing.T) {
			s, root, _ := testFileStore(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			readErr := errors.New("synthetic read failure")
			reader := storeReaderFunc(func(p []byte) (int, error) {
				copy(p, "abc")
				switch kind {
				case "bytes_eof":
					return 3, io.EOF
				case "bytes_error":
					return 3, readErr
				case "zero":
					return 0, nil
				case "negative":
					return -1, nil
				case "excess_count":
					return len(p) + 1, nil
				default:
					cancel()
					return 3, io.EOF
				}
			})
			receipt, err := s.Save(ctx, reader, testStoreLimits())
			if kind == "bytes_eof" {
				require.NoError(t, err)
				require.EqualValues(t, 3, receipt.SavedBytes)
				return
			}
			require.Empty(t, receipt)
			var failure *StoreError
			require.ErrorAs(t, err, &failure)
			want := int64(0)
			if kind == "bytes_error" {
				want = 3
				require.ErrorIs(t, err, readErr)
			}
			require.Equal(t, want, failure.WrittenBytes)
			require.Empty(t, failure.ObjectName)
			info, statErr := os.Stat(filepath.Join(root, failure.TempName))
			require.NoError(t, statErr)
			require.Equal(t, want, info.Size())
		})
	}
}
