package contentcapture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
)

var ErrStoreWriterClosed = errors.New("content writer is closed")

// ReservationWriter accepts bounded chunks under an existing durable reservation.
// It has one owner and must not be used concurrently. Its FileStore must remain
// open until Close. The capture owner bounds live writers and performs all I/O
// outside database transactions; this primitive grants no database authority.
// Failure and Close retain the exact temporary/final paths for reconciliation.
type ReservationWriter struct {
	store                              *FileStore
	file                               *os.File
	hash                               hash.Hash
	limits                             StoreLimits
	tempName, objectName, reservedName string
	written                            int64
	closed, fileClosed                 bool
}

func (s *FileStore) BeginReservation(ctx context.Context, reservation Reservation, limits StoreLimits) (*ReservationWriter, error) {
	if err := validateStoreReservation(ctx, s, reservation, limits); err != nil {
		return nil, err
	}
	return s.beginWriter(ctx, limits, reservation.ObjectName)
}

func validateStoreReservation(ctx context.Context, s *FileStore, reservation Reservation, limits StoreLimits) error {
	if err := validateStoreInputs(ctx, limits); err != nil {
		return err
	}
	if reservation.Binding != sBinding(s) {
		return ErrStoreBinding
	}
	if !validStoreName(reservation.ObjectName) {
		return ErrStorePath
	}
	if reservation.ReservedBytes <= 0 || limits.MaxSavedBytes > reservation.ReservedBytes {
		return ErrStoreLimit
	}
	return nil
}

func (s *FileStore) beginWriter(ctx context.Context, limits StoreLimits, reservedName string) (*ReservationWriter, error) {
	if err := validateStoreInputs(ctx, limits); err != nil {
		return nil, err
	}
	if err := s.verifyBinding(); err != nil {
		return nil, err
	}
	nonce := reservedName
	if nonce == "" {
		var err error
		nonce, err = randomStoreName()
		if err != nil {
			return nil, err
		}
	} else if _, err := s.root.Lstat(reservedName); err == nil {
		return nil, &StoreError{Stage: "existing_object", ObjectName: reservedName, Err: os.ErrExist}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	temp := "." + nonce + ".tmp"
	f, err := s.root.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, &StoreError{Stage: "create_temp", TempName: temp, ObjectName: reservedName, Err: err}
	}
	return &ReservationWriter{store: s, file: f, hash: sha256.New(), limits: limits, tempName: temp, reservedName: reservedName}, nil
}

// WriteChunk performs one bounded file write without retaining the input slice.
// An oversized chunk is rejected in full. All failures irreversibly close the
// writer; a later context cannot resume it or turn the failed capture into ready.
func (w *ReservationWriter) WriteChunk(ctx context.Context, chunk []byte) error {
	if err := w.check(ctx, "write"); err != nil {
		return err
	}
	if len(chunk) > storeBufferSize(w.limits) || int64(len(chunk)) > w.limits.MaxSavedBytes-w.written {
		return w.fail("size", ErrStoreLimit)
	}
	n, err := w.file.Write(chunk)
	w.written += int64(n)
	_, _ = w.hash.Write(chunk[:n])
	if err != nil {
		return w.fail("write", err)
	}
	if n != len(chunk) {
		return w.fail("write", io.ErrShortWrite)
	}
	return nil
}

// Commit publishes the complete inode without replacing an existing object,
// then persists its directory entry. A failure after Link can leave a final
// object but never returns a ready receipt. Commit is not retryable.
func (w *ReservationWriter) Commit(ctx context.Context) (StoredReceipt, error) {
	if err := w.check(ctx, "sync"); err != nil {
		return StoredReceipt{}, err
	}
	fail := func(stage string, err error) (StoredReceipt, error) { return StoredReceipt{}, w.fail(stage, err) }
	if err := w.file.Sync(); err != nil {
		return fail("sync", err)
	}
	info, err := w.file.Stat()
	if err != nil {
		return fail("stat", err)
	}
	if !info.Mode().IsRegular() || info.Size() != w.written {
		return fail("stat", ErrStoreIntegrity)
	}
	if err := w.store.verifyBinding(); err != nil {
		return fail("binding", err)
	}
	if err := verifyStoreEntry(w.store.root, w.tempName, info); err != nil {
		return fail("temp_identity", err)
	}
	name := w.reservedName
	if name == "" {
		name, err = randomStoreName()
		if err != nil {
			return fail("name", err)
		}
	}
	if err := w.store.root.Link(w.tempName, name); err != nil {
		return fail("publish", err)
	}
	w.objectName = name
	if err := verifyStoreEntry(w.store.root, w.objectName, info); err != nil {
		return fail("object_identity", err)
	}
	if err := w.closeFile(); err != nil {
		return fail("close", err)
	}
	if err := w.store.root.Remove(w.tempName); err != nil {
		return fail("unlink_temp", err)
	}
	w.tempName = ""
	if err := syncStoreDirectory(w.store.root); err != nil {
		return fail("sync_directory", err)
	}
	if err := ctx.Err(); err != nil {
		return fail("complete", err)
	}
	if err := w.store.verifyBinding(); err != nil {
		return fail("binding", err)
	}
	w.closed = true
	return StoredReceipt{Binding: w.store.binding, ObjectName: w.objectName, SavedBytes: w.written, SHA256: "sha256:" + hex.EncodeToString(w.hash.Sum(nil))}, nil
}

func (w *ReservationWriter) check(ctx context.Context, stage string) error {
	if w == nil || w.closed {
		return ErrStoreWriterClosed
	}
	if ctx == nil {
		return w.fail(stage, errors.New("content writer requires context"))
	}
	if err := ctx.Err(); err != nil {
		return w.fail(stage, err)
	}
	return nil
}

func (w *ReservationWriter) closeFile() error {
	if w.fileClosed {
		return nil
	}
	w.fileClosed = true
	return w.file.Close()
}

func (w *ReservationWriter) fail(stage string, cause error) error {
	w.closed = true
	return &StoreError{Stage: stage, TempName: w.tempName, ObjectName: w.objectName, WrittenBytes: w.written, Err: errors.Join(cause, w.closeFile())}
}

// Close aborts an unfinished writer. It is idempotent, never publishes, and
// leaves files and database quota untouched for the persistent capture owner.
func (w *ReservationWriter) Close() error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true
	if err := w.closeFile(); err != nil {
		return &StoreError{Stage: "close", TempName: w.tempName, ObjectName: w.objectName, WrittenBytes: w.written, Err: err}
	}
	return nil
}
