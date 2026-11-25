//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd && !windows

package syncer

import (
	"errors"
	"os"
)

func nativeLinkAt(from *os.File, source string, to *os.File, destination string) error {
	return errors.ErrUnsupported
}
