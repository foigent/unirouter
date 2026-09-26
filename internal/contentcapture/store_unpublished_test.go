package contentcapture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnpublishedFilesOfflineExactPair(t *testing.T) {
	for _, kind := range []string{"absent", "temp", "final", "same_inode", "different_inode", "symlink_final", "symlink_temp", "directory_final", "directory_temp", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			store, root, binding := testFileStore(t)
			name := strings.Repeat("a", 64)
			temp := "." + name + ".tmp"
			finalPath, tempPath := filepath.Join(root, name), filepath.Join(root, temp)
			side := filepath.Join(root, "keep")
			require.NoError(t, os.WriteFile(side, []byte("keep"), 0600))
			marker, err := os.ReadFile(filepath.Join(root, storeMarkerName))
			require.NoError(t, err)
			write := func(p string) { require.NoError(t, os.WriteFile(p, []byte("body"), 0600)) }
			switch kind {
			case "temp":
				write(tempPath)
			case "final":
				write(finalPath)
			case "same_inode":
				write(tempPath)
				require.NoError(t, os.Link(tempPath, finalPath))
			case "different_inode":
				write(finalPath)
				write(tempPath)
			case "symlink_final":
				require.NoError(t, os.Symlink("keep", finalPath))
			case "symlink_temp":
				write(finalPath)
				require.NoError(t, os.Symlink("keep", tempPath))
			case "directory_final":
				require.NoError(t, os.Mkdir(finalPath, 0700))
			case "directory_temp":
				write(finalPath)
				require.NoError(t, os.Mkdir(tempPath, 0700))
			case "oversize":
				require.NoError(t, os.WriteFile(finalPath, []byte("oversized"), 0600))
			}
			names := []string{name, temp, "keep", storeMarkerName}
			before := map[string]os.FileInfo{}
			for _, n := range names {
				info, e := os.Lstat(filepath.Join(root, n))
				if e == nil {
					before[n] = info
				} else {
					require.ErrorIs(t, e, os.ErrNotExist)
				}
			}
			state, checkErr := store.InspectUnpublished(t.Context(), binding, name, 4)
			for n, info := range before {
				now, e := os.Lstat(filepath.Join(root, n))
				require.NoError(t, e)
				require.True(t, os.SameFile(info, now))
				require.Equal(t, info.ModTime(), now.ModTime())
				require.Equal(t, info.Size(), now.Size())
			}
			safe := kind == "absent" || kind == "temp" || kind == "final" || kind == "same_inode"
			err = store.RemoveUnpublished(t.Context(), binding, name, 4)
			if safe {
				require.NoError(t, checkErr)
				require.NoError(t, err)
				require.Equal(t, kind == "final" || kind == "same_inode", state.FinalPresent)
				require.NoFileExists(t, finalPath)
				require.NoFileExists(t, tempPath)
				require.NoError(t, store.RemoveUnpublished(t.Context(), binding, name, 4))
			} else {
				require.Error(t, checkErr)
				require.Error(t, err)
				for n, info := range before {
					now, e := os.Lstat(filepath.Join(root, n))
					require.NoError(t, e)
					require.True(t, os.SameFile(info, now))
				}
			}
			after, e := os.ReadFile(filepath.Join(root, storeMarkerName))
			require.NoError(t, e)
			require.Equal(t, marker, after)
			raw, e := os.ReadFile(side)
			require.NoError(t, e)
			require.Equal(t, "keep", string(raw))
		})
	}
}
func TestUnpublishedFilesBindingCancellationAndDurabilityRetry(t *testing.T) {
	store, root, binding := testFileStore(t)
	name := strings.Repeat("b", 64)
	require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("body"), 0600))
	bad := binding
	bad.Token = strings.Repeat("f", 64)
	require.ErrorIs(t, store.RemoveUnpublished(t.Context(), bad, name, 4), ErrStoreBinding)
	require.ErrorIs(t, store.RemoveUnpublished(t.Context(), binding, "../"+name, 4), ErrStorePath)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, store.RemoveUnpublished(ctx, binding, name, 4), context.Canceled)
	require.FileExists(t, filepath.Join(root, name))
	injected := errors.New("synthetic directory sync failure")
	calls := 0
	err := store.removeUnpublished(t.Context(), binding, name, 4, unlinkStoreFile, func(*os.Root) error { calls++; return injected })
	require.ErrorIs(t, err, injected)
	require.Equal(t, 1, calls)
	require.NoFileExists(t, filepath.Join(root, name))
	require.NoError(t, store.RemoveUnpublished(t.Context(), binding, name, 4), "missing path must still complete real directory sync")
}
func TestUnpublishedFilesReplacementAndPartialUnlinkRetry(t *testing.T) {
	for _, kind := range []string{"unlink_failure", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			store, root, binding := testFileStore(t)
			name := strings.Repeat("c", 64)
			temp := "." + name + ".tmp"
			require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("body"), 0600))
			require.NoError(t, os.Link(filepath.Join(root, name), filepath.Join(root, temp)))
			injected := errors.New("synthetic unlink failure")
			calls := 0
			err := store.removeUnpublished(t.Context(), binding, name, 4, func(r *os.Root, n string) error {
				calls++
				if calls == 2 {
					return injected
				}
				e := unlinkStoreFile(r, n)
				if kind == "replacement" {
					require.NoError(t, os.Remove(filepath.Join(root, temp)))
					require.NoError(t, os.WriteFile(filepath.Join(root, temp), []byte("new!"), 0600))
				}
				return e
			}, syncStoreDirectory)
			require.Error(t, err)
			require.NoFileExists(t, filepath.Join(root, name))
			require.FileExists(t, filepath.Join(root, temp))
			if kind == "unlink_failure" {
				require.ErrorIs(t, err, injected)
				require.NoError(t, store.RemoveUnpublished(t.Context(), binding, name, 4))
			} else {
				require.ErrorIs(t, err, ErrStorePath)
				require.Equal(t, 1, calls)
			}
		})
	}
}
