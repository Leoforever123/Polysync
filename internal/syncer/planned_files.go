package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const commitDirPrefix = ".polysync-commit-"
const originalDirName = ".polysync-original-v1"

func changedTarget(relative string) error {
	return fmt.Errorf("同步期间本机文件发生变化，请重新同步：%s", relative)
}

func DeletePlannedFile(ctx context.Context, root, relative, historyRoot, scannedHash string) error {
	dir, done, err := workingRoot(ctx, root)
	if err != nil {
		return err
	}
	defer done()
	err = applyPlannedFile(ctx, dir, relative, historyRoot, scannedHash, "")
	if err == nil {
		removeEmptyDirectories(dir, ".", filepath.Dir(filepath.FromSlash(relative)))
	}
	return err
}

// scannedHash describes the destination on this device at initial scan time.
// An empty hash means the name must still be free, including for empty files.
func applyPlannedFile(ctx context.Context, root *os.Root, relative, historyRoot, scannedHash, staged string) error {
	if err := ValidateRelativePath(relative); err != nil {
		return err
	}
	target := filepath.FromSlash(relative)
	if err := rejectSymlinkPath(root, target, true); err != nil {
		return err
	}
	if err := checkRoot(ctx); err != nil {
		return err
	}
	if scannedHash == "" {
		if staged == "" {
			return changedTarget(relative) // A planned deletion requires a scanned file.
		}
		return publishPlannedFile(root, staged, target, relative)
	}

	// Keep the working file in place during the potentially slow history copy.
	if err := archiveExisting(ctx, root, relative, historyRoot, scannedHash); err != nil {
		return err
	}
	return applyArchivedFile(ctx, root, relative, scannedHash, staged, historyRoot)
}

func applyArchivedFile(ctx context.Context, root *os.Root, relative, scannedHash, staged, historyRoot string) error {
	target := filepath.FromSlash(relative)
	if err := checkRoot(ctx); err != nil {
		return err
	}
	dir, err := mkdirTemp(root, filepath.Dir(target), commitDirPrefix)
	if err != nil {
		return err
	}
	defer root.Remove(dir) // Only removes an empty directory. An interrupted claim is recoverable.
	originalDir := filepath.Join(dir, originalDirName)
	if err := root.Mkdir(originalDir, 0o700); err != nil {
		return err
	}
	defer root.Remove(originalDir)
	claimed := filepath.Join(originalDir, filepath.Base(target))
	if err := checkRoot(ctx); err != nil {
		return err
	}
	if err := root.Rename(target, claimed); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return changedTarget(relative)
		}
		return err
	}

	// Verify the file we actually moved. A save can replace the name at any
	// point during archiving, even with identical size and modification time.
	info, err := verifyClaim(ctx, root, claimed, relative, scannedHash)
	if err != nil {
		return restoreClaim(root, claimed, target, err)
	}
	if err := checkRoot(ctx); err != nil {
		return restoreClaim(root, claimed, target, err)
	}
	if staged != "" {
		err = renameNoReplace(root, staged, root, target)
	} else {
		// The verified file is already detached. Never unlink this name again.
		_, err = root.Lstat(target)
		if err == nil {
			err = os.ErrExist
		} else if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if errors.Is(err, os.ErrExist) && historyRoot != "" {
		// Another save owns the live name. The verified old revision is already
		// in history; let the next scan reconcile the newly saved version.
		return errors.Join(changedTarget(relative), retainClaim(root, claimed, relative, historyRoot, info))
	}
	if err != nil {
		return restoreClaim(root, claimed, target, err)
	}
	return retainClaim(root, claimed, relative, historyRoot, info)
}

func verifyClaim(ctx context.Context, root *os.Root, claimed, relative, scannedHash string) (os.FileInfo, error) {
	info, err := root.Lstat(claimed)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, changedTarget(relative)
	}
	file, err := root.Open(claimed)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	digest, _, err := hashReader(ctx, file)
	if err != nil {
		return nil, err
	}
	if digest != scannedHash {
		return nil, changedTarget(relative)
	}
	if err := checkClaimInfo(root, claimed, info, relative); err != nil {
		return nil, err
	}
	return info, ctx.Err()
}

func checkClaimInfo(root *os.Root, claimed string, before os.FileInfo, displayPath string) error {
	after, err := root.Lstat(claimed)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return changedTarget(displayPath)
	}
	return nil
}

// Keep the original inode reachable. An editor may flush an already-open handle
// even after we verified, replaced and archived the old contents.
func retainClaim(root *os.Root, claimed, relative, historyRoot string, info os.FileInfo) error {
	if err := checkClaimInfo(root, claimed, info, claimed); err != nil {
		return err // An edit is already observable. Leave the interrupted claim for recovery.
	}
	if historyRoot != "" {
		// History is outside the shared folder and may be on another device.
		history := filepath.Join(historyRoot, "open-files")
		if err := os.MkdirAll(history, 0700); err == nil {
			if outside, err := os.OpenRoot(history); err == nil {
				defer outside.Close()
				if err := moveRetainedFile(root, claimed, outside, ".", relative, false); err == nil {
					return nil
				}
			}
		}
	}
	// A rename cannot move an inode to another volume. Prefer the top of
	// the sync root, then the top of a nested mount containing this file.
	dir := "."
	parts := strings.Split(relative, "/")
	for _, part := range parts {
		if err := moveRetainedFile(root, claimed, root, dir, relative, true); err == nil {
			return nil
		}
		dir = filepath.Join(dir, part)
	}
	return fmt.Errorf("请保留并检查原文件：%s", filepath.Join(root.Name(), claimed))
}

const localHistoryPrefix = ".polysync-local-history-"
const retainedFileName = ".polysync-backup-open-file"

func moveRetainedFile(source *os.Root, claimed string, root *os.Root, parent, relative string, local bool) error {
	if err := root.MkdirAll(parent, 0700); err != nil {
		return err
	}
	prefix := time.Now().UTC().Format("20060102T150405Z") + "-"
	if local {
		prefix = localHistoryPrefix + prefix
	}
	dir, err := mkdirTemp(root, parent, prefix)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, filepath.FromSlash(relative))
	if local {
		// Older clients also exclude this filename from their manifests.
		dest = filepath.Join(dest, retainedFileName)
	}
	if err = root.MkdirAll(filepath.Dir(dest), 0700); err == nil {
		err = renameNoReplace(source, claimed, root, dest)
	}
	if err != nil {
		// Only remove empty private directories. Never delete a published inode.
		removeEmptyDirectories(root, parent, filepath.Dir(dest))
	}
	return err
}

func publishPlannedFile(root *os.Root, staged, target, relative string) error {
	err := renameNoReplace(root, staged, root, target)
	if errors.Is(err, os.ErrExist) {
		return changedTarget(relative)
	}
	return err
}

func restoreClaim(root *os.Root, claimed, target string, cause error) error {
	// A second save while restoring owns the live name. Preserve both files.
	if err := renameNoReplace(root, claimed, root, target); err != nil {
		return fmt.Errorf("%w；恢复文件保存在 %s：%v", cause, filepath.Join(root.Name(), claimed), err)
	}
	return cause
}

// All callers pass clean relative paths descending from parent.
func removeEmptyDirectories(root *os.Root, parent, dir string) {
	for dir != parent && dir != "." {
		if err := root.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
