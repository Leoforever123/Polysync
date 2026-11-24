//go:build !linux && !darwin && !windows

package syncer

import "errors"

func nativeRenameNoReplace(source, destination string) error {
	return errors.ErrUnsupported
}
