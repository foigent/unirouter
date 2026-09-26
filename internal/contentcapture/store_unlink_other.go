//go:build !unix

package contentcapture

import "os"

func unlinkStoreFile(_ *os.Root, _ string) error {
	// Fail closed until this platform has a file-only anchored unlink primitive.
	return ErrStorePath
}
