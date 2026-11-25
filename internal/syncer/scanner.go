package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"polysync/internal/model"
	"polysync/internal/rootidentity"
)

const maxFileSize = int64(16 << 30)

func Scan(ctx context.Context, root string) ([]model.Entry, error) {
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open sync folder: %w", err)
	}
	defer handle.Close()
	return ScanRoot(ctx, handle)
}

// ScanRoot reads names, metadata and bytes relative to one open directory. A
// replacement subsequently mounted at the same pathname cannot supply entries.
func ScanRoot(ctx context.Context, root *os.Root) ([]model.Entry, error) {
	opened, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	checkPath := func() error {
		current, err := os.Lstat(root.Name())
		if err != nil {
			return err
		}
		if !current.IsDir() || !os.SameFile(opened, current) {
			return errors.New("同步文件夹已离线或路径已改变")
		}
		return nil
	}
	if err := checkPath(); err != nil {
		return nil, err
	}
	entries := make([]model.Entry, 0)
	tree := root.FS()
	err = fs.WalkDir(tree, ".", func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if item.IsDir() && strings.HasPrefix(item.Name(), commitDirPrefix) {
			originalDir := path + "/" + originalDirName
			info, err := root.Lstat(originalDir)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil && info.IsDir() {
				files, err := fs.ReadDir(tree, originalDir)
				if err != nil {
					return err
				}
				if len(files) > 0 {
					return fmt.Errorf("同步提交中断，请检查并恢复此目录中的原文件后再同步：%s", filepath.Join(root.Name(), originalDir))
				}
				return filepath.SkipDir
			}
		}
		if rootidentity.Reserved(item.Name()) {
			if item.IsDir() {
				return fmt.Errorf("目录名称已由同步标记保留：%s", path)
			}
			return nil
		}
		if item.IsDir() {
			return nil
		}
		if isInternalTemporary(item.Name()) {
			return nil
		}
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := ValidateRelativePath(path); err != nil {
			return err
		}
		entry, err := scanRegularFile(ctx, root, path, info)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan sync folder: %w", err)
	}
	if err := checkPath(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func isInternalTemporary(name string) bool {
	return strings.HasPrefix(name, ".polysync-part-") || strings.HasPrefix(name, ".polysync-backup-")
}

// Read metadata and contents from the same opened file. Re-check the live
// name before advertising the result: an editor can replace it during a scan.
func scanRegularFile(ctx context.Context, root *os.Root, relative string, observed fs.FileInfo) (model.Entry, error) {
	file, err := root.Open(relative)
	if err != nil {
		return model.Entry{}, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return model.Entry{}, err
	}
	if !before.Mode().IsRegular() || observed.Size() != before.Size() || !os.SameFile(observed, before) {
		return model.Entry{}, changedTarget(relative)
	}
	if before.Size() > maxFileSize {
		return model.Entry{}, fmt.Errorf("file exceeds 16 GiB limit: %s", relative)
	}
	digest, size, err := hashReader(ctx, io.LimitReader(file, before.Size()+1))
	if err != nil {
		return model.Entry{}, err
	}
	after, err := root.Lstat(relative)
	if err != nil {
		return model.Entry{}, err
	}
	if size != before.Size() || !os.SameFile(before, after) || after.Size() != size ||
		!after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		return model.Entry{}, changedTarget(relative)
	}
	return model.Entry{Path: relative, Hash: digest, Size: size, ModTime: before.ModTime().UnixNano(), Mode: uint32(before.Mode().Perm())}, nil
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest, _, err := hashReader(ctx, file)
	return digest, err
}

func hashReader(ctx context.Context, input io.Reader) (string, int64, error) {
	hash := sha256.New()
	size, err := io.CopyBuffer(hash, &contextReader{ctx: ctx, reader: input}, make([]byte, 256*1024))
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func ValidateRelativePath(path string) error {
	if path == "" || strings.ContainsRune(path, '\x00') || strings.Contains(path, "\\") {
		return errors.New("invalid relative path")
	}
	for part := range strings.SplitSeq(path, "/") {
		if rootidentity.Reserved(part) {
			return fmt.Errorf("reserved sync marker: %q", path)
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || path == "." || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "../") || path == ".." {
		return fmt.Errorf("unsafe relative path: %q", path)
	}
	return nil
}

func SecureJoin(root, relative string) (string, error) {
	if err := ValidateRelativePath(relative); err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootAbs, filepath.FromSlash(relative))
	rel, err := filepath.Rel(rootAbs, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes sync folder: %q", relative)
	}
	return joined, nil
}
