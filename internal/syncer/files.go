package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"polysync/internal/protocol"
)

func SendFile(ctx context.Context, framer *protocol.Framer, root string, transfer Transfer) error {
	path, err := SecureJoin(root, transfer.Source)
	if err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, path, false); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != transfer.Size {
		return fmt.Errorf("source changed during sync: %s", transfer.Source)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	header := protocol.FileHeader{
		Type: "file", Source: transfer.Source, Dest: transfer.Dest, Size: transfer.Size,
		Hash: transfer.Hash, Mode: transfer.Mode, ModTime: transfer.ModTime,
	}
	if err := framer.WriteJSON(header); err != nil {
		return err
	}
	return framer.CopyFrom(file, transfer.Size)
}

func ReceiveFile(ctx context.Context, framer *protocol.Framer, root, historyRoot string, expected Transfer) error {
	var header protocol.FileHeader
	if err := framer.ReadJSON(&header); err != nil {
		return err
	}
	if header.Type != "file" || header.Source != expected.Source || header.Dest != expected.Dest || header.Size != expected.Size || header.Hash != expected.Hash {
		return errors.New("received file header does not match sync plan")
	}
	destination, err := SecureJoin(root, header.Dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, destination, true); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".polysync-part-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hash := sha256.New()
	limited := io.MultiWriter(tmp, hash)
	if err := ctx.Err(); err != nil {
		tmp.Close()
		return err
	}
	if err := framer.CopyTo(limited, header.Size); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != header.Hash {
		return fmt.Errorf("hash mismatch for %s", header.Dest)
	}
	if err := os.Chmod(tmpPath, os.FileMode(header.Mode)&0o777); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	modTime := time.Unix(0, header.ModTime)
	if err := os.Chtimes(tmpPath, modTime, modTime); err != nil {
		return err
	}
	if err := archiveExisting(destination, header.Dest, historyRoot); err != nil {
		return err
	}
	return replaceFile(tmpPath, destination)
}

func DeleteFile(root, relative, historyRoot string) error {
	path, err := SecureJoin(root, relative)
	if err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, path, true); err != nil {
		return err
	}
	if err := archiveExisting(path, relative, historyRoot); err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	removeEmptyParents(root, filepath.Dir(path))
	return nil
}

func WriteResolvedFile(root, relative, historyRoot string, data []byte, mode os.FileMode) error {
	destination, err := SecureJoin(root, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, destination, true); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".polysync-part-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode&0o777); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	if err := archiveExisting(destination, relative, historyRoot); err != nil {
		return err
	}
	return replaceFile(tmpPath, destination)
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	backupFile, createErr := os.CreateTemp(filepath.Dir(destination), ".polysync-backup-*")
	if createErr != nil {
		return createErr
	}
	backup := backupFile.Name()
	_ = backupFile.Close()
	_ = os.Remove(backup)
	if err := os.Rename(destination, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		_ = os.Rename(backup, destination)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func archiveExisting(source, relative, historyRoot string) error {
	if historyRoot == "" {
		return nil
	}
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot archive non-regular file: %s", relative)
	}
	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	destination := filepath.Join(historyRoot, timestamp, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}

func rejectSymlinkPath(root, target string, allowMissingLeaf bool) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return err
	}
	current := rootAbs
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && allowMissingLeaf {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported in sync paths: %s", current)
		}
	}
	return nil
}

func removeEmptyParents(root, current string) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != rootAbs && strings.HasPrefix(current, rootAbs+string(filepath.Separator)) {
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}
