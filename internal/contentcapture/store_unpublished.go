package contentcapture

import (
	"context"
	"errors"
	"os"
)

// UnpublishedFileState contains no body bytes, binding token or read authority.
// The caller must prove the exact reservation is unpublished and all shared
// writers/readers/maintenance have stopped and cannot restart during recovery.
type UnpublishedFileState struct {
	FinalPresent bool  `json:"final_present"`
	TempPresent  bool  `json:"temp_present"`
	FinalBytes   int64 `json:"final_bytes"`
	TempBytes    int64 `json:"temp_bytes"`
}

type unpublishedFilePair struct {
	state UnpublishedFileState
	names [2]string
	files [2]*os.File
	infos [2]os.FileInfo
}

func (p *unpublishedFilePair) close() {
	for _, f := range p.files {
		if f != nil {
			_ = f.Close()
		}
	}
}

// InspectUnpublished inspects only the exact final and .<final>.tmp names. It
// neither scans nor syncs/writes anything. Both names must refer to one inode
// when both exist. Missing names are valid interrupted-write states.
func (s *FileStore) InspectUnpublished(ctx context.Context, binding StoreBinding, name string, reserved int64) (UnpublishedFileState, error) {
	p, err := s.openUnpublished(ctx, binding, name, reserved)
	if p != nil {
		defer p.close()
		return p.state, err
	}
	return UnpublishedFileState{}, err
}
func (s *FileStore) openUnpublished(ctx context.Context, binding StoreBinding, name string, reserved int64) (*unpublishedFilePair, error) {
	if ctx == nil || binding != sBinding(s) {
		return nil, ErrStoreBinding
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validStoreName(name) {
		return nil, ErrStorePath
	}
	if reserved <= 0 || reserved > maxPolicyInteger {
		return nil, ErrStoreLimit
	}
	if err := s.verifyBinding(); err != nil {
		return nil, err
	}
	p := &unpublishedFilePair{names: [2]string{name, "." + name + ".tmp"}}
	fail := func(err error) (*unpublishedFilePair, error) { p.close(); return nil, err }
	for i, n := range p.names {
		f, err := openStoreRegular(s.root, n)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fail(err)
		}
		p.files[i] = f
		info, err := f.Stat()
		if err != nil {
			return fail(err)
		}
		if info.Size() < 0 || info.Size() > reserved {
			return fail(ErrStoreIntegrity)
		}
		p.infos[i] = info
	}
	if p.infos[0] != nil && p.infos[1] != nil && !os.SameFile(p.infos[0], p.infos[1]) {
		return fail(ErrStorePath)
	}
	if p.infos[0] != nil {
		p.state.FinalPresent = true
		p.state.FinalBytes = p.infos[0].Size()
	}
	if p.infos[1] != nil {
		p.state.TempPresent = true
		p.state.TempBytes = p.infos[1].Size()
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return p, nil
}

// RemoveUnpublished is an offline file primitive, never an online GC. Its
// caller owns the two short database phases and durable once-only quota marker.
// Failure can leave either/both exact names absent; the reservation stays charged
// until a later call proves both absences durable and commits the DB marker.
func (s *FileStore) RemoveUnpublished(ctx context.Context, binding StoreBinding, name string, reserved int64) error {
	return s.removeUnpublished(ctx, binding, name, reserved, unlinkStoreFile, syncStoreDirectory)
}
func (s *FileStore) removeUnpublished(ctx context.Context, binding StoreBinding, name string, reserved int64, unlink func(*os.Root, string) error, syncDir func(*os.Root) error) error {
	p, err := s.openUnpublished(ctx, binding, name, reserved)
	if err != nil {
		return err
	}
	defer p.close()
	// All names were checked before the first unlink; a bad temp never permits
	// deleting a good final. Open descriptors bind the initial inode identities.
	for i, n := range p.names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.verifyBinding(); err != nil {
			return err
		}
		if p.infos[i] == nil {
			if _, err = s.root.Lstat(n); !errors.Is(err, os.ErrNotExist) {
				return ErrStorePath
			}
			continue
		}
		err = verifyStoreEntry(s.root, n, p.infos[i])
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err = unlink(s.root, n); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err = s.verifyBinding(); err != nil {
		return err
	}
	if err = syncDir(s.root); err != nil {
		return err
	}
	for _, n := range p.names {
		if _, err = s.root.Lstat(n); !errors.Is(err, os.ErrNotExist) {
			return ErrStorePath
		}
	}
	return ctx.Err()
}
