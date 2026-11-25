package rootidentity

import (
	"encoding/hex"
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

func nativeIdentity(dir *os.File) (identity, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &stat); err != nil {
		return identity{}, err
	}
	fs := unix.ByteSliceToString(stat.Fstypename[:])
	id := identity{Kind: "darwin-" + fs}
	if fs == "apfs" || fs == "hfs" {
		var st unix.Stat_t
		if err := unix.Fstat(int(dir.Fd()), &st); err != nil {
			return identity{}, err
		}
		id.Directory = strconv.FormatUint(st.Ino, 16)
	}
	// Volume attributes are read at the volume root. Match f_fsid only
	// within this lookup; the kernel can assign a different ID on remount.
	mount, err := os.Open(unix.ByteSliceToString(stat.Mntonname[:]))
	if err != nil {
		return identity{}, err
	}
	defer mount.Close()
	var mounted unix.Statfs_t
	if err := unix.Fstatfs(int(mount.Fd()), &mounted); err != nil {
		return identity{}, err
	}
	if mounted.Fsid != stat.Fsid {
		return identity{}, ErrDifferentRoot
	}
	attr := unix.Attrlist{Bitmapcount: 5, Volattr: unix.ATTR_VOL_INFO | unix.ATTR_VOL_UUID}
	var value struct {
		Length uint32
		UUID   [16]byte
	}
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, mount.Fd(), uintptr(unsafe.Pointer(&attr)), uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value), 0, 0)
	if errno != 0 {
		if errno == unix.ENOTSUP || errno == unix.EINVAL || errno == unix.ENOSYS {
			return id, nil
		}
		return identity{}, errno
	}
	if value.Length == uint32(unsafe.Sizeof(value)) && value.UUID != [16]byte{} {
		id.Volume = hex.EncodeToString(value.UUID[:])
	}
	return id, nil
}
