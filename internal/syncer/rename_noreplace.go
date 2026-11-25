package syncer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Each operation addresses an already-open parent. A changed mount at the
// configured pathname cannot redirect publication, fallback, or rollback.
func renameNoReplace(source *os.Root, oldName string, destination *os.Root, newName string) error {
	return renameOrLink(source, oldName, destination, newName, nativeRenameNoReplace)
}

func renameOrLink(source *os.Root, oldName string, destination *os.Root, newName string, rename func(*os.File, string, *os.File, string) error) error {
	parent := source
	if dir := filepath.Dir(oldName); dir != "." {
		var err error
		parent, err = source.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer parent.Close()
	}
	from, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := destination.Open(filepath.Dir(newName))
	if err != nil {
		return err
	}
	defer to.Close()
	oldLeaf, newLeaf := filepath.Base(oldName), filepath.Base(newName)
	err = rename(from, oldLeaf, to, newLeaf)
	if !errors.Is(err, errors.ErrUnsupported) {
		if err != nil {
			return &os.LinkError{Op: "rename", Old: oldName, New: newName, Err: err}
		}
		return nil
	}

	info, err := parent.Lstat(oldLeaf)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot publish non-regular file %q: %w", oldName, errors.ErrUnsupported)
	}
	// The filesystem decides whether the parents share a volume. A separate
	// os.Root for history frequently points to the same physical filesystem.
	err = nativeLinkAt(from, oldLeaf, to, newLeaf)
	if errors.Is(err, errors.ErrUnsupported) && source == destination {
		// Platforms without a two-directory link API still support in-root
		// links through the standard library.
		err = source.Link(oldName, newName)
	}
	if err != nil {
		return &os.LinkError{Op: "link", Old: oldName, New: newName, Err: err}
	}
	// The destination may already have been edited. Preserve it even if
	// removing the private source name fails.
	if err := parent.Remove(oldLeaf); err != nil {
		return fmt.Errorf("published %q but could not remove staged file: %w", newName, err)
	}
	return nil
}
