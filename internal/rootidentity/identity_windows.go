package rootidentity

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func nativeIdentity(dir *os.File) (identity, error) {
	var info windows.ByHandleFileInformation
	handle := windows.Handle(dir.Fd())
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return identity{}, err
	}
	id := identity{Kind: "windows", Volume: fmt.Sprintf("%08x", info.VolumeSerialNumber)}
	// NTFS has persistent file IDs. FAT directory IDs can change across
	// remounts; the directory token provides the persistent part there.
	var fs [64]uint16
	if err := windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err == nil &&
		strings.EqualFold(windows.UTF16ToString(fs[:]), "NTFS") {
		id.Directory = strconv.FormatUint(uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow), 16)
	}
	return id, nil
}

func hideMarker(marker *os.File) error {
	handle := windows.Handle(marker.Fd())
	var current windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &current); err != nil {
		return err
	}
	// FILE_BASIC_INFO. Zero timestamps leave the existing file times intact.
	var basic struct {
		Times      [4]int64
		Attributes uint32
		Padding    uint32
	}
	basic.Attributes = (current.FileAttributes &^ windows.FILE_ATTRIBUTE_NORMAL) | windows.FILE_ATTRIBUTE_HIDDEN
	err := windows.SetFileInformationByHandle(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic)))
	// Some network filesystems provide no Windows attribute support.
	if err == windows.ERROR_NOT_SUPPORTED || err == windows.ERROR_INVALID_FUNCTION {
		return nil
	}
	return err
}
