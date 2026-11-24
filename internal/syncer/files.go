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
	destination, err := SecureJoin(root, entry.Path)
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
	if err := copyTo(limited, entry.Size); err != nil {
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
	if got := hex.EncodeToString(hash.Sum(nil)); got != entry.Hash {
		return fmt.Errorf("hash mismatch for %s", entry.Path)
	}
	if err := os.Chmod(tmpPath, os.FileMode(entry.Mode)&0o777); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	modTime := time.Unix(0, entry.ModTime)
	if err := os.Chtimes(tmpPath, modTime, modTime); err != nil {
		return err
	}
	return applyPlannedFile(ctx, root, entry.Path, historyRoot, scannedHash, tmpPath)
}

func DeleteFile(root, relative, historyRoot string) error {
	path, err := SecureJoin(root, relative)
	if err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, path, true); err != nil {
		return err
	}
	if err := archiveExisting(context.Background(), path, relative, historyRoot, ""); err != nil {
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
	return WriteResolvedStream(context.Background(), root, relative, historyRoot, bytes.NewReader(data), int64(len(data)), "", mode)
}

// WriteResolvedStream stages and verifies content before touching the destination
// or its history. Memory usage is bounded independently of the object size.
func WriteResolvedStream(ctx context.Context, root, relative, historyRoot string, input io.Reader, size int64, expectedHash string, mode os.FileMode) error {
	if size < 0 || size > 16<<30 {
		return errors.New("invalid resolved file size")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

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
	if err := ctx.Err(); err != nil {
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
	if err := archiveExisting(ctx, destination, relative, historyRoot, ""); err != nil {
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

func archiveExisting(ctx context.Context, source, relative, historyRoot, scannedHash string) error {
	if historyRoot == "" {
		return nil
	}
	info, err := os.Lstat(source)
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
	input, err := os.Open(source)
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
