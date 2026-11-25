//go:build !linux && !darwin && !windows

package syncer

import (
	"errors"
	"os"
)

func nativeRenameNoReplace(from *os.File, source string, to *os.File, destination string) error {
	return errors.ErrUnsupported
}
