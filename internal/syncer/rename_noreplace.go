package syncer

import (
	"errors"
	"fmt"
	"os"
)

// renameNoReplace publishes a private staged path only if destination is absent.
func renameNoReplace(source, destination string) error {
	err := nativeRenameNoReplace(source, destination)
	if errors.Is(err, errors.ErrUnsupported) {
		return linkNoReplace(source, destination)
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: destination, Err: err}
	}
	return nil
}

func linkNoReplace(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot publish non-regular file %q: %w", source, errors.ErrUnsupported)
	}
	if err := os.Link(source, destination); err != nil {
		return err
	}
	// The destination is now visible and could already have been edited.
	// Preserve it even if removing the private source name fails.
	if err := os.Remove(source); err != nil {
		return fmt.Errorf("published %q but could not remove staged file: %w", destination, err)
	}
	return nil
}
