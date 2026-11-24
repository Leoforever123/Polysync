package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const commitDirPrefix = ".polysync-commit-"
const originalDirName = ".polysync-original-v1"

func changedTarget(relative string) error {
	return fmt.Errorf("同步期间本机文件发生变化，请重新同步：%s", relative)
}

func DeletePlannedFile(ctx context.Context, root, relative, historyRoot, scannedHash string) error {
	err := applyPlannedFile(ctx, root, relative, historyRoot, scannedHash, "")
	if err == nil {
		removeEmptyParents(root, filepath.Join(root, filepath.Dir(filepath.FromSlash(relative))))
	}
	return err
}

// scannedHash describes the destination on this device at initial scan time.
// An empty hash means the name must still be free, including for empty files.
func applyPlannedFile(ctx context.Context, root, relative, historyRoot, scannedHash, staged string) error {
	target, err := SecureJoin(root, relative)
	if err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, target, true); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if scannedHash == "" {
		if staged == "" {
			return changedTarget(relative) // A planned deletion requires a scanned file.
		}
		return publishPlannedFile(staged, target, relative)
	}

	// Keep the working file in place during the potentially slow history copy.
	if err := archiveExisting(ctx, target, relative, historyRoot, scannedHash); err != nil {
		return err
	}
	return applyArchivedFile(ctx, target, relative, scannedHash, staged, historyRoot != "")
}

func applyArchivedFile(ctx context.Context, target, relative, scannedHash, staged string, archived bool) error {
	dir, err := os.MkdirTemp(filepath.Dir(target), commitDirPrefix+"*")
	if err != nil {
		return err
	}
	defer os.Remove(dir) // Only removes an empty directory. An interrupted claim is recoverable.
	originalDir := filepath.Join(dir, originalDirName)
	if err := os.Mkdir(originalDir, 0o700); err != nil {
		return err
	}
	defer os.Remove(originalDir)
	claimed := filepath.Join(originalDir, filepath.Base(target))
	if err := os.Rename(target, claimed); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return changedTarget(relative)
		}
		return err
	}

	// Verify the file we actually moved. A save can replace the name at any
	// point during archiving, even with identical size and modification time.
	info, err := verifyClaim(ctx, claimed, relative, scannedHash)
	if err != nil {
		return restoreClaim(claimed, target, err)
	}
	if staged != "" {
		err = renameNoReplace(staged, target)
	} else {
		// The verified file is already detached. Never unlink this name again.
		_, err = os.Lstat(target)
		if err == nil {
			err = os.ErrExist
		} else if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if errors.Is(err, os.ErrExist) && archived {
		// Another save owns the live name. The verified old revision is already
		// in history; let the next scan reconcile the newly saved version.
		return errors.Join(changedTarget(relative), discardVerifiedClaim(claimed, info))
	}
	if err != nil {
		return restoreClaim(claimed, target, err)
	}
	return discardVerifiedClaim(claimed, info)
}

func verifyClaim(ctx context.Context, claimed, relative, scannedHash string) (os.FileInfo, error) {
	info, err := os.Lstat(claimed)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, changedTarget(relative)
	}
	digest, err := hashFile(ctx, claimed)
	if err != nil {
		return nil, err
	}
	if digest != scannedHash {
		return nil, changedTarget(relative)
	}
	if err := checkClaimInfo(claimed, info, relative); err != nil {
		return nil, err
	}
	return info, ctx.Err()
}

func checkClaimInfo(claimed string, before os.FileInfo, displayPath string) error {
	after, err := os.Lstat(claimed)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return changedTarget(displayPath)
	}
	return nil
}

func discardVerifiedClaim(claimed string, info os.FileInfo) error {
	if err := checkClaimInfo(claimed, info, claimed); err != nil {
		return err // Keep late edits through an open handle in the recovery directory.
	}
	return os.Remove(claimed)
}

func publishPlannedFile(staged, target, relative string) error {
	err := renameNoReplace(staged, target)
	if errors.Is(err, os.ErrExist) {
		return changedTarget(relative)
	}
	return err
}

func restoreClaim(claimed, target string, cause error) error {
	// A second save while restoring owns the live name. Preserve both files.
	if err := renameNoReplace(claimed, target); err != nil {
		return fmt.Errorf("%w；恢复文件保存在 %s：%v", cause, claimed, err)
	}
	return cause
}
