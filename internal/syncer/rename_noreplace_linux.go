//go:build linux

package syncer

import (
	"errors"

	"golang.org/x/sys/unix"
)

func nativeRenameNoReplace(source, destination string) error {
	err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
	// Older filesystems may reject this flag with EINVAL.
	if err == unix.EINVAL {
		return errors.ErrUnsupported
	}
	return err
}
