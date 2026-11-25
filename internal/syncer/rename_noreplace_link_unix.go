//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package syncer

import (
	"os"

	"golang.org/x/sys/unix"
)

func nativeLinkAt(from *os.File, source string, to *os.File, destination string) error {
	return unix.Linkat(int(from.Fd()), source, int(to.Fd()), destination, 0)
}
