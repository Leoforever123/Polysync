package syncer

import (
	"bytes"
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

	"polysync/internal/model"
	"polysync/internal/protocol"
)

func SendFile(ctx context.Context, framer *protocol.Framer, root string, transfer Transfer) error {
	dir, done, err := workingRoot(ctx, root)
	if err != nil {
		return err
	}
	defer done()
	if err := ValidateRelativePath(transfer.Source); err != nil {
		return err
	}
	path := filepath.FromSlash(transfer.Source)
	if err := rejectSymlinkPath(dir, path, false); err != nil {
		return err
	}
	info, err := dir.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != transfer.Size {
		return fmt.Errorf("source changed during sync: %s", transfer.Source)
	}
	file, err := dir.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := checkRoot(ctx); err != nil {
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

func ReceiveFile(ctx context.Context, framer *protocol.Framer, root, historyRoot string, expected Transfer, scannedHash string) error {
	var header struct {
		protocol.FileHeader
		Message string `json:"message"`
	}
	if err := framer.ReadJSON(&header); err != nil {
		return err
	}
	if header.Type == "result" && header.Message != "" {
		return errors.New(header.Message)
	}
	if header.Type != "file" || header.Source != expected.Source || header.Dest != expected.Dest || header.Size != expected.Size || header.Hash != expected.Hash {
		return errors.New("received file header does not match sync plan")
	}
	return InstallPlannedContent(ctx, root, historyRoot, model.Entry{Path: header.Dest, Hash: header.Hash, Mode: header.Mode, ModTime: header.ModTime, Size: header.Size}, scannedHash, framer.CopyTo)
}

// InstallPlannedContent verifies both the incoming bytes and the scanned
// destination. Folder groups and legacy connections use the same commit path.
func InstallPlannedContent(ctx context.Context, root, historyRoot string, entry model.Entry, scannedHash string, copyTo func(io.Writer, int64) error) error {
	if err := checkRoot(ctx); err != nil {
		return err
	}
	dir, done, err := workingRoot(ctx, root)
	if err != nil {
		return err
	}
	defer done()
	if err := ValidateRelativePath(entry.Path); err != nil {
		return err
	}
	destination := filepath.FromSlash(entry.Path)
	if err := dir.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir, destination, true); err != nil {
		return err
	}
	tmpPath, tmp, err := createTemp(dir, filepath.Dir(destination), ".polysync-part-")
	if err != nil {
		return err
	}
	defer dir.Remove(tmpPath)

	hash := sha256.New()
	limited := io.MultiWriter(tmp, hash)
	if err := checkRoot(ctx); err != nil {
		tmp.Close()
		return err
	}
	if err := copyTo(limited, entry.Size); err != nil {
		tmp.Close()
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != entry.Hash {
		tmp.Close()
		return fmt.Errorf("hash mismatch for %s", entry.Path)
	}
	if err := checkRoot(ctx); err != nil {
		tmp.Close()
		return err
	}
	if err := finishStagedFile(tmp, os.FileMode(entry.Mode)); err != nil {
		return err
	}
	modTime := time.Unix(0, entry.ModTime)
	if err := dir.Chtimes(tmpPath, modTime, modTime); err != nil {
		return err
	}
	return applyPlannedFile(ctx, dir, entry.Path, historyRoot, scannedHash, tmpPath)
}

func DeleteFile(root, relative, historyRoot string) error {
	return DeleteFileContext(context.Background(), root, relative, historyRoot)
}

func DeleteFileContext(ctx context.Context, root, relative, historyRoot string) error {
	if err := checkRoot(ctx); err != nil {
		return err
	}
	dir, done, err := workingRoot(ctx, root)
	if err != nil {
		return err
	}
	defer done()
	if err := ValidateRelativePath(relative); err != nil {
		return err
	}
	path := filepath.FromSlash(relative)
	if err := rejectSymlinkPath(dir, path, true); err != nil {
		return err
	}
	if err := archiveExisting(ctx, dir, relative, historyRoot, ""); err != nil {
		return err
	}
	if err := checkRoot(ctx); err != nil {
		return err
	}
	err = dir.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	removeEmptyDirectories(dir, ".", filepath.Dir(path))
	return nil
}

func WriteResolvedFile(root, relative, historyRoot string, data []byte, mode os.FileMode) error {
	return WriteResolvedStream(context.Background(), root, relative, historyRoot, bytes.NewReader(data), int64(len(data)), "", mode)
}

// WriteResolvedStream stages and verifies content before touching the destination
// or its history. Memory usage is bounded independently of the object size.
func WriteResolvedStream(ctx context.Context, root, relative, historyRoot string, input io.Reader, size int64, expectedHash string, mode os.FileMode) error {
	if size < 0 || size > 16<<30 {
		return errors.New("invalid resolved file size")
	}
	if err := checkRoot(ctx); err != nil {
		return err
	}

	dir, done, err := workingRoot(ctx, root)
	if err != nil {
		return err
	}
	defer done()
	if err := ValidateRelativePath(relative); err != nil {
		return err
	}
	destination := filepath.FromSlash(relative)
	if err := dir.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir, destination, true); err != nil {
		return err
	}
	tmpPath, tmp, err := createTemp(dir, filepath.Dir(destination), ".polysync-part-")
	if err != nil {
		return err
	}
	defer dir.Remove(tmpPath)
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(tmp, hash), &contextReader{ctx: ctx, reader: io.LimitReader(input, size+1)}, make([]byte, 64<<10))
	if err != nil {
		tmp.Close()
		return err
	}
	if n != size || (expectedHash != "" && hex.EncodeToString(hash.Sum(nil)) != expectedHash) {
		tmp.Close()
		return errors.New("conflict object size or hash mismatch")
	}
	if err := checkRoot(ctx); err != nil {
		tmp.Close()
		return err
	}
	if err := finishStagedFile(tmp, mode); err != nil {
		return err
	}
	if err := archiveExisting(ctx, dir, relative, historyRoot, ""); err != nil {
		return err
	}
	if err := checkRoot(ctx); err != nil {
		return err
	}
	return replaceFile(dir, tmpPath, destination)
}

// Set permissions through the open file. On Android, changing a path's mode
// through os.Root would invoke fchmodat2, which is blocked by app seccomp.
func finishStagedFile(file *os.File, mode os.FileMode) error {
	err := file.Chmod(mode.Perm())
	if err == nil || errors.Is(err, os.ErrPermission) {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

func replaceFile(dir *os.Root, source, destination string) error {
	if err := dir.Rename(source, destination); err == nil {
		return nil
	}
	backup, backupFile, createErr := createTemp(dir, filepath.Dir(destination), ".polysync-backup-")
	if createErr != nil {
		return createErr
	}
	_ = backupFile.Close()
	_ = dir.Remove(backup)
	if err := dir.Rename(destination, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := dir.Rename(source, destination); err != nil {
		_ = dir.Rename(backup, destination)
		return err
	}
	_ = dir.Remove(backup)
	return nil
}

func archiveExisting(ctx context.Context, root *os.Root, relative, historyRoot, scannedHash string) error {
	if historyRoot == "" {
		return nil
	}
	source := filepath.FromSlash(relative)
	info, err := root.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		if scannedHash != "" {
			return changedTarget(relative)
		}
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
	// Discard empty timestamp directories when a concurrent edit aborts the copy.
	defer removeEmptyParents(historyRoot, filepath.Dir(destination))
	input, err := root.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	var writer io.Writer = output
	hash := sha256.New()
	if scannedHash != "" {
		writer = io.MultiWriter(output, hash)
	}
	if _, err := io.Copy(writer, &contextReader{ctx: ctx, reader: input}); err != nil {
		output.Close()
		_ = os.Remove(destination)
		return err
	}
	if scannedHash != "" && hex.EncodeToString(hash.Sum(nil)) != scannedHash {
		output.Close()
		_ = os.Remove(destination)
		return changedTarget(relative)
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

func rejectSymlinkPath(root *os.Root, rel string, allowMissingLeaf bool) error {
	current := "."
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
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

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
