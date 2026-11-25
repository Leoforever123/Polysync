package rootidentity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

func nativeIdentity(dir *os.File) (identity, error) {
	fd := int(dir.Fd())
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil {
		return identity{}, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return identity{}, err
	}
	id := identity{Kind: fmt.Sprintf("linux-%x", uint32(fs.Type))}
	switch uint32(fs.Type) {
	case unix.EXT4_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.TMPFS_MAGIC:
		// ext and btrfs derive f_fsid from the filesystem UUID (and btrfs
		// subvolume). tmpfs identifies the lifetime of the in-memory mount.
		id.Volume = fmt.Sprintf("%08x%08x", uint32(fs.Fsid.Val[0]), uint32(fs.Fsid.Val[1]))
		id.Directory = strconv.FormatUint(st.Ino, 16)
	default:
		// FAT, exFAT and XFS statfs may return a transient kernel device
		// number. Resolve it to the on-disk UUID maintained by udev instead.
		if unix.Major(uint64(st.Dev)) != 0 {
			volume, err := blockUUID(uint64(st.Dev))
			if err != nil {
				return identity{}, err
			}
			id.Volume = volume
		}
		if uint32(fs.Type) == unix.XFS_SUPER_MAGIC || uint32(fs.Type) == unix.F2FS_SUPER_MAGIC {
			id.Directory = strconv.FormatUint(st.Ino, 16)
		}
	}
	return id, nil
}

func blockUUID(dev uint64) (string, error) {
	const directory = "/dev/disk/by-uuid"
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		// Android and containers commonly do not expose udev. The local
		// marker still distinguishes independently mounted folders.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		var st unix.Stat_t
		if err := unix.Stat(filepath.Join(directory, entry.Name()), &st); err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			return "", err
		}
		if st.Mode&unix.S_IFMT == unix.S_IFBLK && uint64(st.Rdev) == dev {
			return entry.Name(), nil
		}
	}
	return "", nil
}
