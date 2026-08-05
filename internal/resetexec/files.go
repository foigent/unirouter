package resetexec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
)

var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func VerifyFileSHA256(path, expected string, maxBytes int64) ([]byte, error) {
	if path == "" || !sha256Pattern.MatchString(expected) {
		return nil, fmt.Errorf("verified artifact path and sha256 digest are required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect verified artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("verified artifact must be a regular file without group or other permissions")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open verified artifact: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened verified artifact: %w", err)
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("verified artifact changed while opening or is not private")
	}
	hash := sha256.New()
	var payload []byte
	if maxBytes > 0 {
		limited := io.LimitReader(file, maxBytes+1)
		payload, err = io.ReadAll(limited)
		if err != nil {
			return nil, fmt.Errorf("read verified artifact: %w", err)
		}
		if int64(len(payload)) > maxBytes {
			return nil, fmt.Errorf("verified artifact exceeds %d bytes", maxBytes)
		}
		if _, err := hash.Write(payload); err != nil {
			return nil, fmt.Errorf("hash verified artifact: %w", err)
		}
	} else {
		if _, err := io.Copy(hash, file); err != nil {
			return nil, fmt.Errorf("hash verified artifact: %w", err)
		}
	}
	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return nil, fmt.Errorf("verified artifact sha256 mismatch")
	}
	return payload, nil
}
