//go:build linux

package syncer

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func nativeRenameNoReplace(from *os.File, source string, to *os.File, destination string) error {
	err := unix.Renameat2(int(from.Fd()), source, int(to.Fd()), destination, unix.RENAME_NOREPLACE)
	// Older filesystems may reject this flag with EINVAL.
	if err == unix.EINVAL {
		return errors.ErrUnsupported
	}
	return err
}
