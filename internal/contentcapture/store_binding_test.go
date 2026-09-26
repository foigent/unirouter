package contentcapture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func bindingTestPaths(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	require.NoError(t, os.Chmod(parent, 0700))
	return root, filepath.Join(parent, "binding")
}

func TestContentStoreBindingInitReadOnlyAndNoReplacement(t *testing.T) {
	root, token := bindingTestPaths(t)
	require.NoError(t, InitializeBoundFileStore(t.Context(), root, "test-binding", token))
	markerPath := filepath.Join(root, storeMarkerName)
	marker, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	secret, err := os.ReadFile(token)
	require.NoError(t, err)
	before, err := os.Stat(markerPath)
	require.NoError(t, err)
	tokenBefore, err := os.Stat(token)
	require.NoError(t, err)
	for range 2 {
		store, err := OpenBoundFileStore(root, "test-binding", token)
		require.NoError(t, err)
		require.NoError(t, store.CheckReady(t.Context()))
		require.NoError(t, store.Close())
	}
	for _, operation := range []func(context.Context, string, string, string) error{InitializeBoundFileStore, ExportFileStoreToken} {
		require.Error(t, operation(t.Context(), root, "test-binding", token))
	}
	_, err = OpenBoundFileStore(root, "different", token)
	require.ErrorIs(t, err, ErrStoreBinding)
	after, err := os.Stat(markerPath)
	require.NoError(t, err)
	tokenAfter, err := os.Stat(token)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	require.True(t, before.ModTime().Equal(after.ModTime()))
	require.True(t, os.SameFile(tokenBefore, tokenAfter))
	require.True(t, tokenBefore.ModTime().Equal(tokenAfter.ModTime()))
	require.Equal(t, os.FileMode(0600), tokenAfter.Mode().Perm())
	actual, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	require.True(t, string(marker) == string(actual), "marker changed")
	actual, err = os.ReadFile(token)
	require.NoError(t, err)
	require.True(t, string(secret) == string(actual), "token changed")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	entries, err = os.ReadDir(filepath.Dir(token))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestContentStoreBindingExplicitExportRetainsPartialFile(t *testing.T) {
	root, token := bindingTestPaths(t)
	binding, err := InitializeFileStore(root, "test-export")
	require.NoError(t, err)
	marker, err := os.ReadFile(filepath.Join(root, storeMarkerName))
	require.NoError(t, err)
	// Exact partial state after a failed token write, not a simulated fsync claim.
	require.NoError(t, os.WriteFile(token, []byte("short"), 0600))
	require.Error(t, ExportFileStoreToken(t.Context(), root, "test-export", token))
	partial, err := os.ReadFile(token)
	require.NoError(t, err)
	require.Equal(t, "short", string(partial))
	fresh := token + "-new"
	require.NoError(t, ExportFileStoreToken(t.Context(), root, "test-export", fresh))
	got, err := ReadStoreBindingToken(fresh)
	require.NoError(t, err)
	require.True(t, got == binding.Token, "export must preserve the original token")
	store, err := OpenBoundFileStore(root, "test-export", fresh)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	after, err := os.ReadFile(filepath.Join(root, storeMarkerName))
	require.NoError(t, err)
	require.True(t, string(marker) == string(after), "export changed the marker")
	require.Error(t, ExportFileStoreToken(t.Context(), root, "wrong", token+"-wrong"))
	require.NoFileExists(t, token+"-wrong")
	require.NoError(t, os.WriteFile(filepath.Join(root, storeMarkerName), append(marker, '\n'), 0600))
	require.Error(t, ExportFileStoreToken(t.Context(), root, "test-export", token+"-noncanonical"))
	require.NoFileExists(t, token+"-noncanonical")
}

func TestContentStoreBindingPreflightDoesNotCreate(t *testing.T) {
	for _, kind := range []string{"nonempty", "wide_root", "wide_parent", "token_exists", "root_link", "parent_link", "token_link", "token_directory", "traversal", "marker_target", "identity_space", "identity_bytes", "identity_control", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			root, token := bindingTestPaths(t)
			originalRoot := root
			identity := "test-negative"
			ctx := t.Context()
			switch kind {
			case "nonempty":
				require.NoError(t, os.WriteFile(filepath.Join(root, "keep"), []byte("kept"), 0600))
			case "wide_root":
				require.NoError(t, os.Chmod(root, 0750))
			case "wide_parent":
				require.NoError(t, os.Chmod(filepath.Dir(token), 0750))
			case "token_exists":
				require.NoError(t, os.WriteFile(token, []byte("kept"), 0600))
			case "root_link":
				link := filepath.Join(filepath.Dir(token), "root-link")
				require.NoError(t, os.Symlink(root, link))
				root = link
			case "parent_link":
				link := filepath.Join(root, "parent-link")
				require.NoError(t, os.Symlink(filepath.Dir(token), link))
				token = filepath.Join(link, "binding")
			case "token_link":
				require.NoError(t, os.Symlink(filepath.Join(root, "kept"), token))
			case "token_directory":
				require.NoError(t, os.Mkdir(token, 0700))
			case "traversal":
				token = filepath.Dir(token) + "/../binding"
			case "marker_target":
				token = filepath.Join(root, storeMarkerName)
			case "identity_space":
				identity = " spaced"
			case "identity_bytes":
				identity = strings.Repeat("字", 43)
			case "identity_control":
				identity = "bad\x00identity"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			require.Error(t, InitializeBoundFileStore(ctx, root, identity, token))
			require.NoFileExists(t, filepath.Join(originalRoot, storeMarkerName))
		})
	}
	root, token := bindingTestPaths(t)
	identity := strings.Repeat("a", 128)
	require.NoError(t, InitializeBoundFileStore(t.Context(), root, identity, token))
	store, err := OpenBoundFileStore(root, identity, token)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

func TestContentStoreBindingTokenReaderRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"permissions", "oversize", "directory", "symlink", "parent_symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, token := bindingTestPaths(t)
			secret := strings.Repeat("a", 64)
			require.NoError(t, os.WriteFile(token, []byte(secret), 0600))
			switch kind {
			case "permissions":
				require.NoError(t, os.Chmod(token, 0640))
			case "oversize":
				require.NoError(t, os.WriteFile(token, []byte(strings.Repeat(secret, 3)), 0600))
			case "directory":
				token = root
			case "symlink":
				link := filepath.Join(root, "link")
				require.NoError(t, os.Symlink(token, link))
				token = link
			case "parent_symlink":
				link := filepath.Join(root, "link")
				require.NoError(t, os.Symlink(filepath.Dir(token), link))
				token = filepath.Join(link, "binding")
			}
			got, err := ReadStoreBindingToken(token)
			require.Error(t, err)
			require.Empty(t, got)
			require.NotContains(t, err.Error(), secret)
		})
	}
}
