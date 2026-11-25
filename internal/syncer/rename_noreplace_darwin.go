//go:build darwin

package syncer

import "os"
import "golang.org/x/sys/unix"

func nativeRenameNoReplace(from *os.File, source string, to *os.File, destination string) error {
	return unix.RenameatxNp(int(from.Fd()), source, int(to.Fd()), destination, unix.RENAME_EXCL)
}
