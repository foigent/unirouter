package snapshotpublish

import (
	"errors"
	"io/fs"
)

var (
	ErrUnsupported        = errors.New("strict snapshot publication is unsupported")
	ErrInvalidDestination = errors.New("strict snapshot destination is invalid")
	ErrInvalidParent      = errors.New("strict snapshot parent is invalid")
	ErrInvalidStage       = errors.New("strict snapshot anonymous stage is invalid")
	ErrStageState         = errors.New("strict snapshot anonymous stage state is invalid")
	ErrCommitFailed       = errors.New("strict snapshot atomic publication failed")
	ErrTargetExists       = fs.ErrExist
)

const LinuxExt4Backend = "linux/ext4/o_tmpfile/procfd-linkat"

// Identity is the descriptor-derived identity used to prove that publication
// keeps operating on the same anonymous inode.
type Identity struct {
	Device    uint64
	Inode     uint64
	UID       uint32
	Mode      uint32
	Nlink     uint64
	Size      int64
	Regular   bool
	Directory bool
}

// Stage exposes only anonymous-inode I/O and the single atomic link commit.
// Implementations must never create a named temporary file, rename, or unlink
// a destination entry.
type Stage interface {
	Backend() string
	Write([]byte) (int, error)
	ReadAt([]byte, int64) (int, error)
	Sync() error
	Stat() (Identity, error)
	Commit() error
	Close() error
}

// Prepare performs the complete platform/filesystem capability preflight and
// retains the anonymous inode for the generation attempt.
func Prepare(path string) (Stage, error) {
	return prepare(path)
}
