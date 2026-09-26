package contentcapture

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const storeMarkerName = ".unirouter-content-store"
const storeBufferCeiling = int64(32 * 1024)

var (
	ErrStoreBinding   = errors.New("content store binding mismatch")
	ErrStoreIntegrity = errors.New("content object integrity check failed")
	ErrStoreLimit     = errors.New("content object exceeds configured limit")
	ErrStorePath      = errors.New("invalid content store path")
)

// StoreBinding is backed by a durable random marker in the actual directory.
// Identity alone never establishes a shared store. The owner persists Token
// with its deployment/DB binding. This local check does not establish cross-node
// visibility, nor replace recovery-epoch validation after a restore.
type StoreBinding struct {
	Identity string `json:"identity"`
	Token    string `json:"token"`
}

type StoreLimits struct {
	MaxSavedBytes  int64
	MaxReadBytes   int64
	MaxBufferBytes int64
}

// StoredReceipt is internal metadata, not a client-selectable path or read
// grant. Read authorization and DB reservation/manifest publication are owned
// by the caller. SavedBytes may be zero.
type StoredReceipt struct {
	Binding    StoreBinding
	ObjectName string
	SavedBytes int64
	SHA256     string
}

// StoreError preserves exact task-created recovery locations. Neither a temp
// file nor ObjectName on an error is a ready receipt. An owner must reconcile
// these with its durable reservation; this primitive never scans or retries.
type StoreError struct {
	Stage        string
	TempName     string
	ObjectName   string
	WrittenBytes int64
	Err          error
}

func (e *StoreError) Error() string {
	return fmt.Sprintf("content store %s failed: %v", e.Stage, e.Err)
}
func (e *StoreError) Unwrap() error { return e.Err }

type FileStore struct {
	root    *os.Root
	binding StoreBinding
}

// InitializeFileStore exclusively creates a marker in an existing controlled
// directory. It never replaces an existing marker or initializes implicitly on
// open. On failure an explicitly reported marker may remain for reconciliation.
func InitializeFileStore(rootPath, identity string) (StoreBinding, error) {
	if !policyText(identity) {
		return StoreBinding{}, ErrStoreBinding
	}
	root, err := openStoreRoot(rootPath)
	if err != nil {
		return StoreBinding{}, err
	}
	defer root.Close()
	return initializeFileStoreRoot(root, identity)
}

func initializeFileStoreRoot(root *os.Root, identity string) (StoreBinding, error) {
	token, err := randomStoreName()
	if err != nil {
		return StoreBinding{}, err
	}
	binding := StoreBinding{Identity: identity, Token: token}
	data, _ := json.Marshal(binding)
	f, err := root.OpenFile(storeMarkerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return StoreBinding{}, &StoreError{Stage: "create_binding", Err: err}
	}
	fail := func(stage string, cause error) (StoreBinding, error) {
		_ = f.Close()
		return StoreBinding{}, &StoreError{Stage: stage, TempName: storeMarkerName, Err: cause}
	}
	if _, err = f.Write(data); err != nil {
		return fail("write_binding", err)
	}
	if err = f.Sync(); err != nil {
		return fail("sync_binding", err)
	}
	if err = f.Close(); err != nil {
		return fail("close_binding", err)
	}
	if err = syncStoreDirectory(root); err != nil {
		return fail("sync_binding_directory", err)
	}
	return binding, nil
}

func OpenFileStore(rootPath string, expected StoreBinding) (*FileStore, error) {
	if !policyText(expected.Identity) || !validStoreName(expected.Token) {
		return nil, ErrStoreBinding
	}
	root, err := openStoreRoot(rootPath)
	if err != nil {
		return nil, err
	}
	s := &FileStore{root: root, binding: expected}
	if err = s.verifyBinding(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return s, nil
}

func (s *FileStore) Close() error {
	if s == nil || s.root == nil {
		return nil
	}
	return s.root.Close()
}

// Save incrementally writes/hash-checks a bounded representation. Context is
// checked between bounded reads/writes; a reader with blocking I/O must itself
// support cancellation. The caller must hold its persistent quota reservation.
func (s *FileStore) Save(ctx context.Context, reader io.Reader, limits StoreLimits) (StoredReceipt, error) {
	return s.saveNamed(ctx, reader, limits, "")
}

// SaveReservation uses the exact name and marker already committed by Reserve.
// Its temporary name is also derivable from that reservation after a crash.
// Existing final or temporary files are never replaced, reused, or removed.
// This file primitive does not verify DB ownership; PublishReservedContent must
// independently validate the persistent reservation before publishing a manifest.
func (s *FileStore) SaveReservation(ctx context.Context, reservation Reservation, reader io.Reader, limits StoreLimits) (StoredReceipt, error) {
	if err := validateStoreReservation(ctx, s, reservation, limits); err != nil {
		return StoredReceipt{}, err
	}
	return s.saveNamed(ctx, reader, limits, reservation.ObjectName)
}

func (s *FileStore) saveNamed(ctx context.Context, reader io.Reader, limits StoreLimits, reservedName string) (StoredReceipt, error) {
	if err := validateStoreInputs(ctx, limits); err != nil {
		return StoredReceipt{}, err
	}
	if reader == nil {
		return StoredReceipt{}, errors.New("content reader is required")
	}
	writer, err := s.beginWriter(ctx, limits, reservedName)
	if err != nil {
		return StoredReceipt{}, err
	}
	defer writer.Close()
	fail := func(stage string, cause error) (StoredReceipt, error) {
		return StoredReceipt{}, writer.fail(stage, cause)
	}
	buffer := make([]byte, storeBufferSize(limits))
	for {
		if err := ctx.Err(); err != nil {
			return fail("read", err)
		}
		readSize := int64(len(buffer))
		if remaining := limits.MaxSavedBytes - writer.written; readSize > remaining {
			readSize = remaining + 1
		}
		n, readErr := reader.Read(buffer[:readSize])
		if n < 0 || n > int(readSize) {
			return fail("read", errors.New("invalid reader count"))
		}
		if int64(n) > limits.MaxSavedBytes-writer.written {
			return fail("size", ErrStoreLimit)
		}
		if n > 0 {
			if err := writer.WriteChunk(ctx, buffer[:n]); err != nil {
				return StoredReceipt{}, err
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return fail("read", readErr)
			}
			break
		}
		if n == 0 {
			return fail("read", io.ErrNoProgress)
		}
	}
	return writer.Commit(ctx)
}

// OpenVerified validates the entire representation using bounded memory and
// returns the SAME opened file rewound to zero. Replacing its pathname after
// this call cannot redirect that descriptor. A controlled root must prevent
// other writers modifying this immutable inode in place during delivery.
func (s *FileStore) OpenVerified(ctx context.Context, receipt StoredReceipt, limits StoreLimits) (*os.File, error) {
	if err := validateStoreInputs(ctx, limits); err != nil {
		return nil, err
	}
	if receipt.Binding != sBinding(s) {
		return nil, ErrStoreBinding
	}
	if !validStoreName(receipt.ObjectName) {
		return nil, ErrStorePath
	}
	if receipt.SavedBytes < 0 || receipt.SavedBytes > limits.MaxSavedBytes || receipt.SavedBytes > limits.MaxReadBytes {
		return nil, ErrStoreLimit
	}
	if !validStoreDigest(receipt.SHA256) {
		return nil, ErrStoreIntegrity
	}
	if err := s.verifyBinding(); err != nil {
		return nil, err
	}
	f, err := openStoreRegular(s.root, receipt.ObjectName)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*os.File, error) { _ = f.Close(); return nil, cause }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if info.Size() != receipt.SavedBytes {
		return fail(ErrStoreIntegrity)
	}
	hash := sha256.New()
	buffer := make([]byte, storeBufferSize(limits))
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		readSize := int64(len(buffer))
		if remaining := receipt.SavedBytes - total; readSize > remaining {
			readSize = remaining + 1
		}
		n, readErr := f.Read(buffer[:readSize])
		total += int64(n)
		if total > receipt.SavedBytes {
			return fail(ErrStoreIntegrity)
		}
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return fail(readErr)
			}
			break
		}
		if n == 0 {
			return fail(io.ErrNoProgress)
		}
	}
	if total != receipt.SavedBytes || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != receipt.SHA256 {
		return fail(ErrStoreIntegrity)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := s.verifyBinding(); err != nil {
		return fail(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return f, nil
}

// CheckReady verifies the existing marker without initializing storage or
// granting capture authority. It performs bounded file I/O outside DB locks.
func (s *FileStore) CheckReady(ctx context.Context) error {
	if ctx == nil {
		return ErrStoreBinding
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.verifyBinding(); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *FileStore) verifyBinding() error {
	if s == nil || s.root == nil {
		return ErrStoreBinding
	}
	f, err := openStoreRegular(s.root, storeMarkerName)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreBinding, err)
	}
	defer f.Close()
	// The marker is a bounded canonical record, not a body representation.
	data, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreBinding, err)
	}
	expected, _ := json.Marshal(s.binding)
	if len(data) > 1024 || !bytes.Equal(data, expected) {
		return ErrStoreBinding
	}
	return nil
}

func openStoreRoot(rootPath string) (*os.Root, error) {
	if !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath || rootPath == string(filepath.Separator) {
		return nil, ErrStorePath
	}
	// Walk from an anchored filesystem root. Lstat/SameFile checks reject
	// symlinks in every component; os.Root keeps traversal beneath each handle.
	volume := filepath.VolumeName(rootPath)
	root, err := os.OpenRoot(volume + string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	components := strings.Split(strings.TrimPrefix(rootPath, volume+string(filepath.Separator)), string(filepath.Separator))
	for _, component := range components {
		before, err := root.Lstat(component)
		if err != nil {
			_ = root.Close()
			return nil, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			_ = root.Close()
			return nil, ErrStorePath
		}
		next, err := root.OpenRoot(component)
		if err != nil {
			_ = root.Close()
			return nil, err
		}
		opened, statErr := next.Stat(".")
		after, afterErr := root.Lstat(component)
		_ = root.Close()
		if statErr != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
			_ = next.Close()
			return nil, ErrStorePath
		}
		root = next
	}
	return root, nil
}

func openStoreRegular(root *os.Root, name string) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, ErrStorePath
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = f.Close()
		return nil, ErrStorePath
	}
	if err := verifyStoreEntry(root, name, opened); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
func verifyStoreEntry(root *os.Root, name string, opened os.FileInfo) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, opened) {
		return ErrStorePath
	}
	return nil
}
func syncStoreDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func randomStoreName() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}
func validStoreName(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validStoreDigest(s string) bool {
	return strings.HasPrefix(s, "sha256:") && validStoreName(strings.TrimPrefix(s, "sha256:"))
}
func sBinding(s *FileStore) StoreBinding {
	if s == nil {
		return StoreBinding{}
	}
	return s.binding
}
func validateStoreInputs(ctx context.Context, l StoreLimits) error {
	if ctx == nil {
		return errors.New("content store context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !positivePolicyInteger(l.MaxSavedBytes) || !positivePolicyInteger(l.MaxReadBytes) || !positivePolicyInteger(l.MaxBufferBytes) || l.MaxReadBytes < l.MaxSavedBytes {
		return ErrStoreLimit
	}
	return nil
}
func storeBufferSize(l StoreLimits) int {
	n := min(l.MaxBufferBytes, storeBufferCeiling, l.MaxSavedBytes)
	return int(n)
}
