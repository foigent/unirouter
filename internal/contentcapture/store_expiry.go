package contentcapture

import (
	"context"
	"errors"
	"os"
)

// RemoveExpired removes only an exact published object's regular final file.
// Its caller must verify the original persisted expiry and identity first.
// No scan, temporary file, directory or marker is removed. Missing files are
// idempotent only after the binding and directory durability are checked again.
func (s *FileStore) RemoveExpired(ctx context.Context, receipt StoredReceipt) error {
	if ctx == nil || receipt.Binding != sBinding(s) {
		return ErrStoreBinding
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validStoreName(receipt.ObjectName) {
		return ErrStorePath
	}
	if receipt.SavedBytes < 0 || !validStoreDigest(receipt.SHA256) {
		return ErrStoreIntegrity
	}
	if err := s.verifyBinding(); err != nil {
		return err
	}
	f, err := openStoreRegular(s.root, receipt.ObjectName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() != receipt.SavedBytes {
			return ErrStoreIntegrity
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.verifyBinding(); err != nil {
			return err
		}
		if err = verifyStoreEntry(s.root, receipt.ObjectName, info); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			if err = unlinkStoreFile(s.root, receipt.ObjectName); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	if err := s.verifyBinding(); err != nil {
		return err
	}
	if err := syncStoreDirectory(s.root); err != nil {
		return err
	}
	return ctx.Err()
}
