//go:build linux

package snapshotpublish

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	strictParentMode = uint32(0o700)
	strictFileMode   = uint32(0o600)
)

type linuxStage struct {
	parentFD       int
	stageFD        int
	basename       string
	procPath       string
	parentIdentity Identity
	stageIdentity  Identity
	commitAttempt  bool
	committed      bool
	closed         bool
	linkNoReplace  func(int, string, int, string, int) error
	backend        string
}

func prepare(path string) (Stage, error) {
	if path == "" || path == "-" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidDestination
	}
	basename := filepath.Base(path)
	if basename == "" || basename == "." || basename == ".." || strings.ContainsRune(basename, filepath.Separator) {
		return nil, ErrInvalidDestination
	}

	parentFD, err := openVerifiedParent(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	closeParent := true
	defer func() {
		if closeParent {
			_ = unix.Close(parentFD)
		}
	}()

	parentIdentity, err := identityForFD(parentFD)
	if err != nil || !validParentIdentity(parentIdentity) {
		return nil, ErrInvalidParent
	}
	var target unix.Stat_t
	err = unix.Fstatat(parentFD, basename, &target, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case err == nil:
		return nil, ErrTargetExists
	case errors.Is(err, unix.ENOENT):
		// The final atomic link remains the authoritative race resolution.
	default:
		return nil, ErrInvalidDestination
	}

	backend, err := qualifiedFilesystemBackend(parentFD)
	if err != nil {
		return nil, ErrUnsupported
	}

	stageFD, err := unix.Openat(parentFD, ".", unix.O_TMPFILE|unix.O_RDWR|unix.O_CLOEXEC, strictFileMode)
	if err != nil {
		return nil, ErrUnsupported
	}
	closeStage := true
	defer func() {
		if closeStage {
			_ = unix.Close(stageFD)
		}
	}()

	if err := unix.Fchmod(stageFD, strictFileMode); err != nil {
		return nil, ErrInvalidStage
	}
	stageIdentity, err := identityForFD(stageFD)
	if err != nil || !validAnonymousIdentity(stageIdentity) {
		return nil, ErrInvalidStage
	}

	procPath := "/proc/self/fd/" + strconv.Itoa(stageFD)
	var procStat unix.Stat_t
	if err := unix.Stat(procPath, &procStat); err != nil {
		return nil, ErrUnsupported
	}
	procIdentity := identityFromStat(procStat)
	if !sameInode(stageIdentity, procIdentity) || !validAnonymousIdentity(procIdentity) {
		return nil, ErrUnsupported
	}

	closeParent = false
	closeStage = false
	return &linuxStage{
		parentFD:       parentFD,
		stageFD:        stageFD,
		basename:       basename,
		procPath:       procPath,
		parentIdentity: parentIdentity,
		stageIdentity:  stageIdentity,
		linkNoReplace:  unix.Linkat,
		backend:        backend,
	}, nil
}

func (s *linuxStage) Backend() string { return s.backend }

func qualifiedFilesystemBackend(parentFD int) (string, error) {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(parentFD, &filesystem); err != nil || filesystem.Type != unix.EXT4_SUPER_MAGIC {
		return "", ErrUnsupported
	}
	var statx unix.Statx_t
	if err := unix.Statx(parentFD, "", unix.AT_EMPTY_PATH|unix.AT_STATX_SYNC_AS_STAT, unix.STATX_MNT_ID, &statx); err != nil || statx.Mask&unix.STATX_MNT_ID == 0 || statx.Mnt_id == 0 {
		return "", ErrUnsupported
	}
	mountInfoFD, err := unix.Open("/proc/self/mountinfo", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", ErrUnsupported
	}
	mountInfo := os.NewFile(uintptr(mountInfoFD), "/proc/self/mountinfo")
	if mountInfo == nil {
		_ = unix.Close(mountInfoFD)
		return "", ErrUnsupported
	}
	defer func() { _ = mountInfo.Close() }()
	filesystemType, err := mountFilesystemType(mountInfo, statx.Mnt_id)
	if err != nil || !qualifiedFilesystemIdentity(filesystem.Type, filesystemType) {
		return "", ErrUnsupported
	}
	return LinuxExt4Backend, nil
}

func qualifiedFilesystemIdentity(filesystemMagic int64, filesystemType string) bool {
	return filesystemMagic == unix.EXT4_SUPER_MAGIC && filesystemType == "ext4"
}

func mountFilesystemType(reader io.Reader, mountID uint64) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	matchedType := ""
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 7 {
			return "", ErrUnsupported
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return "", ErrUnsupported
		}
		if id != mountID {
			continue
		}
		separator := -1
		for idx := 6; idx < len(fields); idx++ {
			if fields[idx] == "-" {
				separator = idx
				break
			}
		}
		if separator < 0 || separator+2 >= len(fields) || matchedType != "" {
			return "", ErrUnsupported
		}
		matchedType = fields[separator+1]
	}
	if scanner.Err() != nil || matchedType == "" {
		return "", ErrUnsupported
	}
	return matchedType, nil
}

func openVerifiedParent(path string) (int, error) {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, ErrInvalidParent
	}
	currentFD := rootFD
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, component := range components {
		if component == "" {
			continue
		}
		if component == "." || component == ".." {
			_ = unix.Close(currentFD)
			return -1, ErrInvalidParent
		}
		nextFD, err := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			_ = unix.Close(currentFD)
			return -1, ErrInvalidParent
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return currentFD, nil
}

func (s *linuxStage) Write(payload []byte) (int, error) {
	if s.closed || s.commitAttempt {
		return 0, ErrStageState
	}
	return unix.Write(s.stageFD, payload)
}

func (s *linuxStage) ReadAt(payload []byte, offset int64) (int, error) {
	if s.closed || s.commitAttempt {
		return 0, ErrStageState
	}
	total := 0
	for total < len(payload) {
		n, err := unix.Pread(s.stageFD, payload[total:], offset+int64(total))
		if errors.Is(err, unix.EINTR) {
			if n > 0 {
				if n > len(payload)-total {
					return total, ErrInvalidStage
				}
				total += n
			}
			continue
		}
		if n < 0 || n > len(payload)-total {
			return total, ErrInvalidStage
		}
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.EOF
		}
	}
	return total, nil
}

func (s *linuxStage) Sync() error {
	if s.closed || s.commitAttempt {
		return ErrStageState
	}
	return unix.Fsync(s.stageFD)
}

func (s *linuxStage) Stat() (Identity, error) {
	if s.closed || s.commitAttempt {
		return Identity{}, ErrStageState
	}
	identity, err := identityForFD(s.stageFD)
	if err != nil {
		return Identity{}, ErrInvalidStage
	}
	return identity, nil
}

func (s *linuxStage) Commit() error {
	if s.closed || s.commitAttempt {
		return ErrStageState
	}
	s.commitAttempt = true

	parentIdentity, err := identityForFD(s.parentFD)
	if err != nil || !sameInode(parentIdentity, s.parentIdentity) || !validParentIdentity(parentIdentity) {
		return ErrCommitFailed
	}
	stageIdentity, err := identityForFD(s.stageFD)
	if err != nil || !sameInode(stageIdentity, s.stageIdentity) || !validAnonymousIdentity(stageIdentity) {
		return ErrCommitFailed
	}
	var procStat unix.Stat_t
	if err := unix.Stat(s.procPath, &procStat); err != nil {
		return ErrCommitFailed
	}
	if procIdentity := identityFromStat(procStat); !sameInode(procIdentity, stageIdentity) || !validAnonymousIdentity(procIdentity) {
		return ErrCommitFailed
	}

	// This is the sole final-name mutation. Linux link(2) documents definitive
	// failures for the accepted local ext4 backend; NFS and every unqualified
	// filesystem are rejected during Prepare because this state machine has no
	// post-link outcome-resolution step.
	if err := s.linkNoReplace(unix.AT_FDCWD, s.procPath, s.parentFD, s.basename, unix.AT_SYMLINK_FOLLOW); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fs.ErrExist
		}
		return ErrCommitFailed
	}
	s.committed = true
	return nil
}

func (s *linuxStage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	stageErr := unix.Close(s.stageFD)
	parentErr := unix.Close(s.parentFD)
	if stageErr != nil {
		return stageErr
	}
	return parentErr
}

func identityForFD(fd int) (Identity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return Identity{}, err
	}
	return identityFromStat(stat), nil
}

func identityFromStat(stat unix.Stat_t) Identity {
	return Identity{
		Device:    uint64(stat.Dev),
		Inode:     stat.Ino,
		UID:       stat.Uid,
		Mode:      uint32(stat.Mode) & 0o7777,
		Nlink:     uint64(stat.Nlink),
		Size:      stat.Size,
		Regular:   uint32(stat.Mode)&unix.S_IFMT == unix.S_IFREG,
		Directory: uint32(stat.Mode)&unix.S_IFMT == unix.S_IFDIR,
	}
}

func sameInode(left, right Identity) bool {
	return left.Device == right.Device && left.Inode == right.Inode
}

func validParentIdentity(identity Identity) bool {
	return identity.Directory && identity.UID == uint32(os.Geteuid()) && identity.Mode == strictParentMode
}

func validAnonymousIdentity(identity Identity) bool {
	return identity.Regular && identity.UID == uint32(os.Geteuid()) && identity.Mode == strictFileMode && identity.Nlink == 0
}
