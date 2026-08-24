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
)

const maxFileSize = int64(16 << 30)

func Scan(ctx context.Context, root string) ([]model.Entry, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("open sync folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sync path is not a directory: %s", root)
	}

	entries := make([]model.Entry, 0)
	err = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root || item.IsDir() {
			return nil
		}
		if isInternalTemporary(item.Name()) {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxFileSize {
			return fmt.Errorf("file exceeds 16 GiB limit: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := ValidateRelativePath(rel); err != nil {
			return err
		}
		hash, err := hashFile(ctx, path)
		if err != nil {
			return err
		}
		entries = append(entries, model.Entry{
			Path: rel, Hash: hash, Size: info.Size(), ModTime: info.ModTime().UnixNano(), Mode: uint32(info.Mode().Perm()),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan sync folder: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func isInternalTemporary(name string) bool {
	return strings.HasPrefix(name, ".polysync-part-") || strings.HasPrefix(name, ".polysync-backup-")
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			if _, err := hash.Write(buffer[:n]); err != nil {
				return "", err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ValidateRelativePath(path string) error {
	if path == "" || strings.ContainsRune(path, '\x00') || strings.Contains(path, "\\") {
		return errors.New("invalid relative path")
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
