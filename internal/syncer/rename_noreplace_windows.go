//go:build windows

package syncer

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// FileRenameInformation accepts a destination directory handle and one leaf
// name. ReplaceIfExists is zero, including on filesystems without POSIX flags.
func nativeRenameNoReplace(from *os.File, source string, to *os.File, destination string) error {
	name, err := windows.NewNTUnicodeString(source)
	if err != nil {
		return err
	}
	attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(from.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var status windows.IO_STATUS_BLOCK
	var file windows.Handle
	err = windows.NtCreateFile(&file, windows.DELETE|windows.SYNCHRONIZE, &attrs, &status, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return ntRenameError(err)
	}
	defer windows.CloseHandle(file)
	leaf, err := windows.UTF16FromString(destination)
	if err != nil {
		return err
	}
	// A leaf name is at most 255 UTF-16 units on supported Windows filesystems.
	var rename struct {
		Replace   byte
		Directory windows.Handle
		Length    uint32
		Name      [256]uint16
	}
	if len(leaf) > len(rename.Name) {
		return windows.ERROR_FILENAME_EXCED_RANGE
	}
	rename.Directory = windows.Handle(to.Fd())
	rename.Length = uint32(2 * (len(leaf) - 1))
	copy(rename.Name[:], leaf)
	err = windows.NtSetInformationFile(file, &status, (*byte)(unsafe.Pointer(&rename)), uint32(unsafe.Sizeof(rename)), windows.FileRenameInformation)
	return ntRenameError(err)
}

func ntRenameError(err error) error {
	var status windows.NTStatus
	if errors.As(err, &status) {
		err = status.Errno()
	}
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return errors.ErrUnsupported
	}
	return err
}
