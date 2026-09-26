//go:build unix

package contentcapture

import (
	"golang.org/x/sys/unix"
	"os"
)

func unlinkStoreFile(root *os.Root, name string) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	// No AT_REMOVEDIR: a directory substituted at this name cannot be removed.
	return unix.Unlinkat(int(directory.Fd()), name, 0)
}
