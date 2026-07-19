//go:build linux

package snapshotpublish

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxQualifiedBackendUsesOneTerminalNoReplaceLink(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "snapshot.json")
	stageValue, err := Prepare(destination)
	if err != nil {
		t.Fatalf("required Linux backend must execute, not skip: %v", err)
	}
	stage, ok := stageValue.(*linuxStage)
	if !ok || stage.Backend() != LinuxExt4Backend {
		t.Fatalf("stage=%T backend=%q", stageValue, stageValue.Backend())
	}
	t.Logf("qualified publication backend: %s", stage.Backend())

	realLink := stage.linkNoReplace
	linkCalls := 0
	stage.linkNoReplace = func(oldDirFD int, oldPath string, newDirFD int, newPath string, flags int) error {
		linkCalls++
		return realLink(oldDirFD, oldPath, newDirFD, newPath, flags)
	}
	if _, err := stage.Write([]byte("complete-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := stage.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatal(err)
	}
	if linkCalls != 1 {
		t.Fatalf("link calls=%d", linkCalls)
	}
	if err := stage.Commit(); !errors.Is(err, ErrStageState) || linkCalls != 1 {
		t.Fatalf("second commit err=%v link calls=%d", err, linkCalls)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(destination)
	if err != nil || string(payload) != "complete-bytes" {
		t.Fatalf("published bytes=%q err=%v", payload, err)
	}
}

func TestLinuxQualifiedBackendDocumentedLinkFailurePublishesNothingAndNeverRetries(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "snapshot.json")
	stageValue, err := Prepare(destination)
	if err != nil {
		t.Fatalf("required Linux backend must execute, not skip: %v", err)
	}
	stage := stageValue.(*linuxStage)
	linkCalls := 0
	stage.linkNoReplace = func(int, string, int, string, int) error {
		linkCalls++
		return unix.EIO
	}
	if err := stage.Commit(); !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("commit error=%v", err)
	}
	if err := stage.Commit(); !errors.Is(err, ErrStageState) {
		t.Fatalf("second commit error=%v", err)
	}
	if linkCalls != 1 {
		t.Fatalf("link calls=%d", linkCalls)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("failed commit created destination: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("parent entries=%v err=%v", entries, err)
	}
}

func TestLinuxFilesystemQualificationRejectsRemoteAndUnprovenBackends(t *testing.T) {
	if !qualifiedFilesystemIdentity(unix.EXT4_SUPER_MAGIC, "ext4") {
		t.Fatal("ext4 must be the sole initially qualified backend")
	}
	for _, identity := range []struct {
		magic int64
		name  string
	}{
		{unix.EXT4_SUPER_MAGIC, "ext2"},
		{unix.EXT4_SUPER_MAGIC, "ext3"},
		{unix.NFS_SUPER_MAGIC, "nfs"},
		{unix.OVERLAYFS_SUPER_MAGIC, "overlay"},
		{unix.TMPFS_MAGIC, "tmpfs"},
		{unix.XFS_SUPER_MAGIC, "xfs"},
		{unix.BTRFS_SUPER_MAGIC, "btrfs"},
		{0, "ext4"},
	} {
		if qualifiedFilesystemIdentity(identity.magic, identity.name) {
			t.Fatalf("unproven filesystem accepted: magic=%#x type=%s", identity.magic, identity.name)
		}
	}
}

func TestLinuxMountInfoParserBindsMountIDToExactFilesystemType(t *testing.T) {
	const mountInfo = `36 25 8:1 / / rw,relatime shared:1 - ext4 /dev/root rw
37 25 0:5 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
`
	filesystemType, err := mountFilesystemType(strings.NewReader(mountInfo), 36)
	if err != nil || filesystemType != "ext4" {
		t.Fatalf("filesystem type=%q err=%v", filesystemType, err)
	}
	for _, tc := range []struct {
		name, input string
		mountID     uint64
	}{
		{name: "missing", input: mountInfo, mountID: 99},
		{name: "malformed", input: "36 malformed", mountID: 36},
		{name: "duplicate", input: mountInfo + "36 25 8:1 / /other rw - ext4 /dev/root rw\n", mountID: 36},
	} {
		if value, err := mountFilesystemType(strings.NewReader(tc.input), tc.mountID); err == nil || value != "" {
			t.Fatalf("%s value=%q err=%v", tc.name, value, err)
		}
	}
}

func TestLinuxPublisherSourceHasNoNamedCreateRenameOrCleanupMutation(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	path := filepath.Join(filepath.Dir(currentFile), "publisher_linux.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"Creat": true, "Rename": true, "Renameat": true, "Renameat2": true,
		"Unlink": true, "Unlinkat": true, "Remove": true, "RemoveAll": true,
		"OpenFile": true, "WriteFile": true, "Create": true, "CreateTemp": true,
		"TempFile": true, "MkdirTemp": true, "Truncate": true,
		"Link": true, "Symlink": true, "Symlinkat": true,
		"Mknod": true, "Mknodat": true, "Mkdir": true, "Mkdirat": true, "MkdirAll": true,
		"Chmod": true, "Chown": true, "Lchown": true,
	}
	linkatReferences := 0
	linkCommitCalls := 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && (identifier.Name == "O_CREAT" || identifier.Name == "O_TRUNC") {
			t.Errorf("forbidden named-create/truncate flag anywhere in publisher: %s", identifier.Name)
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && forbidden[selector.Sel.Name] {
			t.Errorf("forbidden directory mutation call: %s", selector.Sel.Name)
		}
		if ok && selector.Sel.Name == "linkNoReplace" {
			linkCommitCalls++
		}
		if ok && (selector.Sel.Name == "Open" || selector.Sel.Name == "Openat") {
			ast.Inspect(call, func(child ast.Node) bool {
				identifier, ok := child.(*ast.Ident)
				if ok && identifier.Name == "basename" {
					t.Error("final basename passed to open operation")
				}
				selector, ok := child.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "basename" {
					t.Error("final basename passed to open operation")
				}
				return true
			})
		}
		return true
	})
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Linkat" {
			linkatReferences++
		}
		return true
	})
	if linkatReferences != 1 || linkCommitCalls != 1 {
		t.Fatalf("linkat references=%d final commit calls=%d want=1/1", linkatReferences, linkCommitCalls)
	}
}
