package contentcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testFileStore(t *testing.T) (*FileStore, string, StoreBinding) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binding, err := InitializeFileStore(root, "test-only-storage")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenFileStore(root, binding)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root, binding
}
func testStoreLimits() StoreLimits {
	return StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 7}
}

func TestFileStoreReservationExactNameAndNoOverwrite(t *testing.T) {
	s, root, binding := testFileStore(t)
	r := Reservation{Binding: binding, ObjectName: strings.Repeat("a", 64), ReservedBytes: 1024}
	for _, body := range []string{"", "non-json\x00bytes"} {
		r.ObjectName, _ = randomStoreName()
		receipt, err := s.SaveReservation(t.Context(), r, strings.NewReader(body), testStoreLimits())
		if err != nil || receipt.ObjectName != r.ObjectName || receipt.Binding != binding || receipt.SavedBytes != int64(len(body)) {
			t.Fatalf("reservation receipt: %#v %v", receipt, err)
		}
		if _, err = os.Stat(filepath.Join(root, "."+r.ObjectName+".tmp")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("success kept temporary file: %v", err)
		}
		if _, err = s.SaveReservation(t.Context(), r, strings.NewReader("replacement"), testStoreLimits()); !errors.Is(err, os.ErrExist) {
			t.Fatalf("overwrote reserved final: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(root, r.ObjectName))
		if err != nil || string(got) != body {
			t.Fatalf("existing file changed: %q %v", got, err)
		}
	}
	r.ObjectName, _ = randomStoreName()
	_, err := s.SaveReservation(t.Context(), r, strings.NewReader(strings.Repeat("x", 1025)), testStoreLimits())
	var savedErr *StoreError
	if !errors.As(err, &savedErr) || !errors.Is(err, ErrStoreLimit) || savedErr.TempName != "."+r.ObjectName+".tmp" {
		t.Fatalf("unrecoverable failure: %#v %v", savedErr, err)
	}
	before, err := os.ReadFile(filepath.Join(root, savedErr.TempName))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveReservation(t.Context(), r, strings.NewReader("retry"), testStoreLimits())
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("reused failed temp: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, savedErr.TempName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed temp was changed")
	}
}

func TestFileStoreReservationRejectsBindingNameAndBudget(t *testing.T) {
	s, _, binding := testFileStore(t)
	base := Reservation{Binding: binding, ObjectName: strings.Repeat("a", 64), ReservedBytes: 1024}
	for name, mutate := range map[string]func(*Reservation){
		"token":    func(r *Reservation) { r.Binding.Token = strings.Repeat("b", 64) },
		"identity": func(r *Reservation) { r.Binding.Identity = "another-store" },
		"path":     func(r *Reservation) { r.ObjectName = "../escape" },
		"ref":      func(r *Reservation) { r.ObjectName = "uc1_" + r.ObjectName },
		"budget":   func(r *Reservation) { r.ReservedBytes = 1023 },
		"zero":     func(r *Reservation) { r.ReservedBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			mutate(&r)
			if _, err := s.SaveReservation(t.Context(), r, strings.NewReader("data"), testStoreLimits()); err == nil {
				t.Fatal("accepted invalid reservation")
			}
		})
	}
}

func TestFileStoreZeroAndBinaryRoundTrip(t *testing.T) {
	s, root, _ := testFileStore(t)
	for _, body := range [][]byte{{}, {0, 255, 1, 128, '\n', '{', '"'}, []byte("a short body")} {
		r, err := s.Save(context.Background(), bytes.NewReader(body), testStoreLimits())
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(body)
		if !validStoreName(r.ObjectName) || r.SavedBytes != int64(len(body)) || r.SHA256 != "sha256:"+hex.EncodeToString(h[:]) {
			t.Fatalf("invalid receipt: %#v", r)
		}
		f, err := s.OpenVerified(context.Background(), r, testStoreLimits())
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("round trip %x: %v", got, err)
		}
		info, err := os.Stat(filepath.Join(root, r.ObjectName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("object permissions: %v", info.Mode())
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("unexpected temp files retained on success: %v", entries)
	}
}

func TestFileStoreBindingAndPaths(t *testing.T) {
	s, root, binding := testFileStore(t)
	limits := testStoreLimits()
	_, otherRoot, otherBinding := testFileStore(t)
	if binding.Identity != otherBinding.Identity || binding.Token == otherBinding.Token {
		t.Fatal("independent roots did not receive independent binding evidence")
	}
	if _, err := OpenFileStore(otherRoot, binding); !errors.Is(err, ErrStoreBinding) {
		t.Fatalf("accepted same identity on another directory: %v", err)
	}
	if _, err := InitializeFileStore(root, binding.Identity); err == nil {
		t.Fatal("replaced existing binding")
	}
	wrong := binding
	wrong.Identity = "different"
	if _, err := OpenFileStore(root, wrong); !errors.Is(err, ErrStoreBinding) {
		t.Fatal(err)
	}
	receipt, err := s.Save(context.Background(), strings.NewReader("secret"), limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", "/etc/passwd", "uc1_" + receipt.ObjectName, "nested/" + receipt.ObjectName, strings.ToUpper(receipt.ObjectName), "."} {
		altered := receipt
		altered.ObjectName = name
		if _, err := s.OpenVerified(context.Background(), altered, limits); !errors.Is(err, ErrStorePath) {
			t.Fatalf("accepted path %q: %v", name, err)
		}
	}
	receipt.Binding = otherBinding
	if _, err := s.OpenVerified(context.Background(), receipt, limits); !errors.Is(err, ErrStoreBinding) {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked-root")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileStore(link, binding); !errors.Is(err, ErrStorePath) {
		t.Fatalf("accepted symlink root: %v", err)
	}
	if _, err := OpenFileStore(filepath.Join(link, "."), binding); err == nil {
		t.Fatal("accepted symlink path component")
	}
	if err := os.WriteFile(filepath.Join(root, storeMarkerName), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(context.Background(), strings.NewReader("x"), limits); !errors.Is(err, ErrStoreBinding) {
		t.Fatal("continued after binding changed")
	}
}

func TestFileStoreRejectsCorruptionAndSymlinks(t *testing.T) {
	s, root, _ := testFileStore(t)
	limits := testStoreLimits()
	for _, kind := range []string{"same_length", "truncated", "extended", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			r, err := s.Save(context.Background(), strings.NewReader("original"), limits)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, r.ObjectName)
			switch kind {
			case "same_length":
				err = os.WriteFile(path, []byte("modified"), 0600)
			case "truncated":
				err = os.Truncate(path, 1)
			case "extended":
				err = os.WriteFile(path, []byte("original-extra"), 0600)
			case "symlink":
				// Only task-created files are moved/removed in these tests.
				err = os.Rename(path, path+"-held")
				if err == nil {
					err = os.Symlink(path+"-held", path)
				}
			case "directory":
				err = os.Rename(path, path+"-held")
				if err == nil {
					err = os.Mkdir(path, 0700)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if f, err := s.OpenVerified(context.Background(), r, limits); err == nil {
				_ = f.Close()
				t.Fatal("accepted corrupted representation")
			}
		})
	}
	r, err := s.Save(context.Background(), strings.NewReader("a"), limits)
	if err != nil {
		t.Fatal(err)
	}
	r.SHA256 = "sha256:" + strings.Repeat("0", 64)
	if _, err := s.OpenVerified(context.Background(), r, limits); !errors.Is(err, ErrStoreIntegrity) {
		t.Fatal(err)
	}
}

func TestFileStoreReturnsSameVerifiedDescriptor(t *testing.T) {
	s, root, _ := testFileStore(t)
	limits := testStoreLimits()
	r, err := s.Save(context.Background(), strings.NewReader("original"), limits)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.OpenVerified(context.Background(), r, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	path := filepath.Join(root, r.ObjectName)
	if err := os.Rename(path, path+"-held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("attacker"), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	if err != nil || string(body) != "original" {
		t.Fatalf("reopened replaced path: %q %v", body, err)
	}
}

type countingContentReader struct {
	remaining int64
	maxRead   int
	calls     int
}

func (r *countingContentReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	r.calls++
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := int64(0); i < n; i++ {
		p[i] = 0xa5
	}
	r.remaining -= n
	return int(n), nil
}

func TestFileStoreLargeBoundedRepresentation(t *testing.T) {
	s, _, _ := testFileStore(t)
	size := int64(17*1024*1024 + 1)
	limits := StoreLimits{MaxSavedBytes: size, MaxReadBytes: size, MaxBufferBytes: 4096}
	reader := &countingContentReader{remaining: size}
	r, err := s.Save(context.Background(), reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	if r.SavedBytes != size || reader.maxRead > 4096 || reader.calls < int(size/4096) {
		t.Fatalf("unbounded copy: %#v %#v", r, reader)
	}
	f, err := s.OpenVerified(context.Background(), r, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n, err := io.CopyBuffer(io.Discard, f, make([]byte, 4096))
	if err != nil || n != size {
		t.Fatalf("large read: %d %v", n, err)
	}
	if storeBufferSize(StoreLimits{MaxSavedBytes: size, MaxBufferBytes: size}) > int(storeBufferCeiling) {
		t.Fatal("large configured buffer caused unbounded allocation")
	}
}

func TestFileStoreLimitFailureLeavesPreciseOwnedTemp(t *testing.T) {
	s, root, _ := testFileStore(t)
	limits := StoreLimits{MaxSavedBytes: 10, MaxReadBytes: 10, MaxBufferBytes: 3}
	r, err := s.Save(context.Background(), strings.NewReader("12345678901"), limits)
	var failure *StoreError
	if !errors.As(err, &failure) || !errors.Is(err, ErrStoreLimit) || r != (StoredReceipt{}) {
		t.Fatalf("unexpected size result: %#v %v", r, err)
	}
	if failure.TempName == "" || failure.ObjectName != "" || failure.WrittenBytes > 10 {
		t.Fatalf("lost orphan location: %#v", failure)
	}
	info, err := os.Stat(filepath.Join(root, failure.TempName))
	if err != nil || info.Size() != failure.WrittenBytes {
		t.Fatalf("temp accounting: %v %v", info, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected files: %v", entries)
	}
	ok, err := s.Save(context.Background(), strings.NewReader("1234567890"), limits)
	if err != nil {
		t.Fatal(err)
	}
	lower := limits
	lower.MaxSavedBytes = 9
	if _, err := s.OpenVerified(context.Background(), ok, lower); !errors.Is(err, ErrStoreLimit) {
		t.Fatal("read ignored max_saved limit")
	}
	lower = limits
	lower.MaxReadBytes = 9
	if _, err := s.OpenVerified(context.Background(), ok, lower); !errors.Is(err, ErrStoreLimit) {
		t.Fatal("read ignored max_read limit")
	}
}

type failingContentReader struct{}

func (failingContentReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic input failure")
}

type stalledContentReader struct{}

func (stalledContentReader) Read([]byte) (int, error) { return 0, nil }

type cancelContentReader struct{ cancel context.CancelFunc }

func (r cancelContentReader) Read(p []byte) (int, error) { p[0] = 1; r.cancel(); return 1, nil }

func TestFileStoreReaderFailuresAndCancellation(t *testing.T) {
	s, root, _ := testFileStore(t)
	for _, reader := range []io.Reader{failingContentReader{}, stalledContentReader{}} {
		r, err := s.Save(context.Background(), reader, testStoreLimits())
		var failure *StoreError
		if !errors.As(err, &failure) || r != (StoredReceipt{}) || failure.TempName == "" {
			t.Fatalf("failed without recovery path: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, failure.TempName)); err != nil {
			t.Fatal("primitive deleted failure evidence")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := s.Save(ctx, cancelContentReader{cancel}, testStoreLimits())
	var failure *StoreError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || failure.WrittenBytes != 0 {
		t.Fatalf("canceled save: %v", err)
	}
	if _, err := s.Save(ctx, strings.NewReader("x"), testStoreLimits()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r, err := s.Save(context.Background(), strings.NewReader("x"), testStoreLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenVerified(ctx, r, testStoreLimits()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
