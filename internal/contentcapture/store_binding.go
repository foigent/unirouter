package contentcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func validateBindingPaths(root, identity, tokenFile string) error {
	if !policyText(identity) || identity != strings.TrimSpace(identity) || len(identity) > 128 ||
		root != strings.TrimSpace(root) || strings.ContainsAny(root, "\x00\r\n") || tokenFile != strings.TrimSpace(tokenFile) ||
		!filepath.IsAbs(tokenFile) || filepath.Clean(tokenFile) != tokenFile ||
		strings.ContainsAny(tokenFile, "\x00\r\n") || tokenFile == filepath.Join(root, storeMarkerName) {
		return ErrStoreBinding
	}
	return nil
}

func openPrivateStoreDirectory(path string) (*os.Root, error) {
	root, err := openStoreRoot(path)
	if err != nil {
		return nil, ErrStorePath
	}
	info, err := root.Stat(".")
	if err != nil || info.Mode().Perm()&0077 != 0 {
		_ = root.Close()
		return nil, errors.New("content storage directory must be private")
	}
	return root, nil
}

func openTokenParent(path string) (*os.Root, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path != strings.TrimSpace(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, "", ErrStorePath
	}
	parent, err := openPrivateStoreDirectory(filepath.Dir(path))
	return parent, filepath.Base(path), err
}

// ReadStoreBindingToken never prints contents or follows a symlink component.
func ReadStoreBindingToken(path string) (string, error) {
	parent, name, err := openTokenParent(path)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	f, err := openStoreRegular(parent, name)
	if err != nil {
		return "", errors.New("content binding file is unavailable or not regular")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Mode().Perm()&0077 != 0 || info.Size() > 128 {
		return "", errors.New("content binding file must be private and bounded")
	}
	wire, err := io.ReadAll(io.LimitReader(f, 129))
	if err != nil || len(wire) > 128 {
		return "", ErrStoreBinding
	}
	token := strings.TrimSpace(string(wire))
	if !validStoreName(token) {
		return "", ErrStoreBinding
	}
	return token, nil
}

// OpenBoundFileStore is shared by explicit CLI checks and ordinary startup.
// Only explicit init/export operations can create binding files.
func OpenBoundFileStore(rootPath, identity, tokenFile string) (*FileStore, error) {
	if err := validateBindingPaths(rootPath, identity, tokenFile); err != nil {
		return nil, err
	}
	token, err := ReadStoreBindingToken(tokenFile)
	if err != nil {
		return nil, err
	}
	root, err := openPrivateStoreDirectory(rootPath)
	if err != nil {
		return nil, err
	}
	s := &FileStore{root: root, binding: StoreBinding{Identity: identity, Token: token}}
	if err := s.verifyBinding(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return s, nil
}

func checkTokenAbsent(parent *os.Root, name string) error {
	_, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("content binding file already exists; select a new token-file path")
	}
	return errors.New("content binding file path is unavailable")
}

func writeStoreToken(ctx context.Context, parent *os.Root, name, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("content binding creation failed; existing files retained")
	}
	defer f.Close()
	// Failure may leave a short file. Explicit export to another NEW path is
	// the recovery path; never hide a retry, overwrite or removal here.
	data := []byte(token + "\n")
	n, err := f.Write(data)
	if err != nil || n != len(data) {
		return errors.New("content binding write incomplete; select a new export path")
	}
	if err := f.Sync(); err != nil {
		return errors.New("content binding sync unconfirmed; files retained")
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(data)) || info.Mode().Perm()&0077 != 0 || verifyStoreEntry(parent, name, info) != nil {
		return errors.New("content binding identity unconfirmed; files retained")
	}
	if err := f.Close(); err != nil {
		return errors.New("content binding close unconfirmed; files retained")
	}
	if err := syncStoreDirectory(parent); err != nil {
		return errors.New("content binding directory sync unconfirmed; files retained")
	}
	return ctx.Err()
}

// InitializeBoundFileStore initializes only an empty, private directory.
// Partial failure retains every exact created file for explicit recovery.
func InitializeBoundFileStore(ctx context.Context, rootPath, identity, tokenFile string) error {
	return createBoundStore(ctx, rootPath, identity, tokenFile, false)
}

// ExportFileStoreToken recovers a token from an existing canonical marker.
// It does not create or rotate a marker, and never replaces a token file.
func ExportFileStoreToken(ctx context.Context, rootPath, identity, tokenFile string) error {
	return createBoundStore(ctx, rootPath, identity, tokenFile, true)
}

func createBoundStore(ctx context.Context, rootPath, identity, tokenFile string, export bool) error {
	if ctx == nil {
		return errors.New("content storage operation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBindingPaths(rootPath, identity, tokenFile); err != nil {
		return err
	}
	root, err := openPrivateStoreDirectory(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	parent, name, err := openTokenParent(tokenFile)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := checkTokenAbsent(parent, name); err != nil {
		return err
	}
	var binding StoreBinding
	if export {
		marker, err := openStoreRegular(root, storeMarkerName)
		if err != nil {
			return ErrStoreBinding
		}
		wire, readErr := io.ReadAll(io.LimitReader(marker, 1025))
		info, statErr := marker.Stat()
		if readErr != nil || statErr != nil || len(wire) > 1024 || json.Unmarshal(wire, &binding) != nil || binding.Identity != identity || !validStoreName(binding.Token) {
			_ = marker.Close()
			return ErrStoreBinding
		}
		canonical, _ := json.Marshal(binding)
		if !bytes.Equal(wire, canonical) || verifyStoreEntry(root, storeMarkerName, info) != nil {
			_ = marker.Close()
			return ErrStoreBinding
		}
		// A previous init may have left complete readable bytes after fsync
		// failed. Explicit export must establish marker durability first.
		if err := marker.Sync(); err != nil {
			_ = marker.Close()
			return errors.New("content marker sync unconfirmed; token not created")
		}
		if err := verifyStoreEntry(root, storeMarkerName, info); err != nil {
			_ = marker.Close()
			return ErrStoreBinding
		}
		if err := marker.Close(); err != nil {
			return errors.New("content marker close unconfirmed; token not created")
		}
		if err := syncStoreDirectory(root); err != nil {
			return errors.New("content marker directory sync unconfirmed; token not created")
		}
	} else {
		dir, err := root.Open(".")
		if err != nil {
			return ErrStorePath
		}
		entries, readErr := dir.ReadDir(1)
		closeErr := dir.Close()
		if len(entries) != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil {
			return errors.New("content initialization requires an empty directory")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		binding, err = initializeFileStoreRoot(root, identity)
		if err != nil {
			return errors.New("content marker initialization unconfirmed; files retained")
		}
	}
	store := &FileStore{root: root, binding: binding}
	if err := store.verifyBinding(); err != nil {
		return ErrStoreBinding
	}
	if err := writeStoreToken(ctx, parent, name, binding.Token); err != nil {
		return err
	}
	return store.CheckReady(ctx)
}
