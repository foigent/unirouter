package contentcapture

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreExpiryExactRegularAndDurableAbsence(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := InitializeFileStore(root, "expiry")
	require.NoError(t, err)
	store, err := OpenFileStore(root, binding)
	require.NoError(t, err)
	defer store.Close()
	receipt, err := store.Save(t.Context(), bytes.NewBufferString("body"), StoreLimits{16, 16, 4})
	require.NoError(t, err)
	retained := receipt.ObjectName + ".tmp"
	require.NoError(t, os.WriteFile(filepath.Join(root, retained), []byte("retain"), 0600))
	bad := receipt
	bad.Binding.Token = strings.Repeat("f", 64)
	require.ErrorIs(t, store.RemoveExpired(t.Context(), bad), ErrStoreBinding)
	bad = receipt
	bad.ObjectName = "../" + receipt.ObjectName
	require.ErrorIs(t, store.RemoveExpired(t.Context(), bad), ErrStorePath)
	bad = receipt
	bad.SavedBytes++
	require.ErrorIs(t, store.RemoveExpired(t.Context(), bad), ErrStoreIntegrity)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, store.RemoveExpired(ctx, receipt), context.Canceled)
	require.FileExists(t, filepath.Join(root, receipt.ObjectName))
	require.NoError(t, store.RemoveExpired(t.Context(), receipt))
	require.NoFileExists(t, filepath.Join(root, receipt.ObjectName))
	require.NoError(t, store.RemoveExpired(t.Context(), receipt))
	require.FileExists(t, filepath.Join(root, retained))
	require.FileExists(t, filepath.Join(root, storeMarkerName))
	// Even an already absent file cannot free quota under a changed marker.
	marker := filepath.Join(root, storeMarkerName)
	raw, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(marker, []byte("changed"), 0600))
	require.ErrorIs(t, store.RemoveExpired(t.Context(), receipt), ErrStoreBinding)
	require.NoError(t, os.WriteFile(marker, raw, 0600))
	require.NoError(t, os.Mkdir(filepath.Join(root, receipt.ObjectName), 0700))
	require.ErrorIs(t, store.RemoveExpired(t.Context(), receipt), ErrStorePath)
	require.Error(t, unlinkStoreFile(store.root, receipt.ObjectName), "file-only unlink must not remove directories")
	require.DirExists(t, filepath.Join(root, receipt.ObjectName))
	require.NoError(t, os.Remove(filepath.Join(root, receipt.ObjectName)))
	require.NoError(t, os.Symlink(retained, filepath.Join(root, receipt.ObjectName)))
	require.ErrorIs(t, store.RemoveExpired(t.Context(), receipt), ErrStorePath)
	target, err := os.Readlink(filepath.Join(root, receipt.ObjectName))
	require.NoError(t, err)
	require.Equal(t, retained, target)
}
