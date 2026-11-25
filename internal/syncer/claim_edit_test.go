package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimRestoresEditToAlreadyHashedBytes(t *testing.T) {
	root := t.TempDir()
	const name = "working.txt"
	target := filepath.Join(root, name)
	original := bytes.Repeat([]byte("a"), 1<<20)
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	originalTime := time.Unix(1600000000, 0)
	if err := os.Chtimes(target, originalTime, originalTime); err != nil {
		t.Fatal(err)
	}

	const edit = "user saved this"
	var saved bool
	hash := func(ctx context.Context, input io.Reader) (string, int64, error) {
		// Hold a writer while the actual claimed inode is hashed, then edit
		// bytes already read just before verifyClaim rechecks its metadata.
		claimed := input.(*os.File).Name()
		editor, err := os.OpenFile(claimed, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		digest, size, err := hashReader(ctx, input)
		if err != nil {
			editor.Close()
			t.Fatal(err)
		}
		if _, err := editor.WriteAt([]byte(edit), 0); err != nil {
			editor.Close()
			t.Fatal(err)
		}
		if err := editor.Close(); err != nil {
			t.Fatal(err)
		}
		// Make the edit visible even on coarse-timestamp volumes.
		changedTime := originalTime.Add(24 * time.Hour)
		if err := os.Chtimes(claimed, changedTime, changedTime); err != nil {
			t.Fatal(err)
		}
		saved = true
		return digest, size, nil
	}
	scannedHash := fmt.Sprintf("%x", sha256.Sum256(original))
	err := applyArchivedFile(t.Context(), openTestRoot(t, root), name, scannedHash, "", t.TempDir(), hash)
	if !saved || err == nil {
		t.Fatalf("expected deletion to stop after the edit, saved=%t err=%v", saved, err)
	}
	copy(original, edit)
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("the working file must contain the saved edit after rollback: %v", err)
	}
}

func TestCanceledPublishWithoutVolumeGuardKeepsStagedFile(t *testing.T) {
	root := openTestRoot(t, t.TempDir())
	const staged = ".polysync-part-test"
	if err := root.WriteFile(staged, []byte("staged"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := applyPlannedFile(ctx, root, "published", "", "", staged); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publish: %v", err)
	}
	if _, err := root.Lstat("published"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled publish created a destination: %v", err)
	}
	if data, err := root.ReadFile(staged); err != nil || string(data) != "staged" {
		t.Fatalf("canceled publish lost staged content: %q, %v", data, err)
	}
}
