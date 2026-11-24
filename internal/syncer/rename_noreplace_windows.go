//go:build windows

package syncer

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func nativeRenameNoReplace(source, destination string) error {
	from, err := movePath(source)
	if err != nil {
		return err
	}
	to, err := movePath(destination)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(from, to, 0)
	if err == windows.ERROR_INVALID_FUNCTION {
		return errors.ErrUnsupported
	}
	return err
}

// MoveFileExW needs extended-length names on systems with MAX_PATH enabled.
func movePath(name string) (*uint16, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	if len(absolute) >= 248 && !strings.HasPrefix(absolute, `\\?\`) && !strings.HasPrefix(absolute, `\\.\`) {
		if strings.HasPrefix(absolute, `\\`) {
			absolute = `\\?\UNC\` + absolute[2:]
		} else {
			absolute = `\\?\` + absolute
		}
	}
	return windows.UTF16PtrFromString(absolute)
}
