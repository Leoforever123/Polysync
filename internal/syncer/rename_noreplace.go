package syncer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Both parent directories are opened from their original root handles. A
// changed mount at the configured pathname cannot redirect publish or rollback.
func renameNoReplace(source *os.Root, oldName string, destination *os.Root, newName string) error {
	from, err := source.Open(filepath.Dir(oldName))
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := destination.Open(filepath.Dir(newName))
	if err != nil {
		return err
	}
	defer to.Close()
	err = nativeRenameNoReplace(from, filepath.Base(oldName), to, filepath.Base(newName))
	if errors.Is(err, errors.ErrUnsupported) && source == destination {
		return linkNoReplace(source, oldName, newName)
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldName, New: newName, Err: err}
	}
	return nil
}

func linkNoReplace(root *os.Root, source, destination string) error {
	info, err := root.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot publish non-regular file %q: %w", source, errors.ErrUnsupported)
	}
	if err := root.Link(source, destination); err != nil {
		return err
	}
	// The destination is now visible and could already have been edited.
	// Preserve it even if removing the private source name fails.
	if err := root.Remove(source); err != nil {
		return fmt.Errorf("published %q but could not remove staged file: %w", destination, err)
	}
	return nil
}
