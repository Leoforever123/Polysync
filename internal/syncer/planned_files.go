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
	return applyArchivedFile(ctx, root, target, relative, scannedHash, staged, historyRoot)
}

func applyArchivedFile(ctx context.Context, root, target, relative, scannedHash, staged, historyRoot string) error {
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
	if errors.Is(err, os.ErrExist) && historyRoot != "" {
		// Another save owns the live name. The verified old revision is already
		// in history; let the next scan reconcile the newly saved version.
		return errors.Join(changedTarget(relative), retainClaim(claimed, root, relative, historyRoot, info))
	}
	if err != nil {
		return restoreClaim(claimed, target, err)
	}
	return retainClaim(claimed, root, relative, historyRoot, info)
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

// Keep the original inode reachable. An editor may flush an already-open handle
// even after we verified, replaced and archived the old contents.
func retainClaim(claimed, root, relative, historyRoot string, info os.FileInfo) error {
	if err := checkClaimInfo(claimed, info, claimed); err != nil {
		return err // An edit is already observable. Leave the interrupted claim for recovery.
	}
	if historyRoot != "" {
		if err := moveRetainedFile(claimed, filepath.Join(historyRoot, "open-files"), relative, false); err == nil {
			return nil
		}
	}
	// A rename cannot move an inode to another volume. Prefer the top of
	// the sync root, then the top of a nested mount containing this file.
	dir := root
	parts := strings.Split(relative, "/")
	for _, part := range parts {
		if err := moveRetainedFile(claimed, dir, relative, true); err == nil {
			return nil
		}
		dir = filepath.Join(dir, part)
	}
	return fmt.Errorf("请保留并检查原文件：%s", claimed)
}

const localHistoryPrefix = ".polysync-local-history-"
const retainedFileName = ".polysync-backup-open-file"

func moveRetainedFile(claimed, parent, relative string, local bool) error {
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	prefix := time.Now().UTC().Format("20060102T150405Z") + "-"
	if local {
		prefix = localHistoryPrefix + prefix
	}
	dir, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, filepath.FromSlash(relative))
	if local {
		// Older clients also exclude this filename from their manifests.
		dest = filepath.Join(dest, retainedFileName)
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err == nil {
		err = renameNoReplace(claimed, dest)
	}
	if err != nil {
		// Only remove empty private directories. Never delete a published inode.
		removeEmptyParents(parent, filepath.Dir(dest))
	}
	return err
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
