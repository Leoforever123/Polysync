package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type claimReadContext struct {
	context.Context
	beforeRead func()
}

func (c claimReadContext) Err() error {
	c.beforeRead()
	return c.Context.Err()
}

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
	var editor *os.File
	t.Cleanup(func() {
		if editor != nil {
			_ = editor.Close()
		}
	})
	checks := 0
	ctx := claimReadContext{Context: t.Context(), beforeRead: func() {
		// hashFile polls the context immediately before each read. Hold a
		// writer on the original inode across the read of the first block.
		checks++
		switch checks {
		case 1:
			dirs, err := os.ReadDir(root)
			if err != nil || len(dirs) != 1 || !dirs[0].IsDir() {
				t.Fatalf("expected one recovery directory: %v, %v", dirs, err)
			}
			claimed := filepath.Join(root, dirs[0].Name(), originalDirName, name)
			editor, err = os.OpenFile(claimed, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
		case 2:
			if _, err := editor.WriteAt([]byte(edit), 0); err != nil {
				t.Fatal(err)
			}
			claimed := editor.Name()
			if err := editor.Close(); err != nil {
				t.Fatal(err)
			}
			editor = nil
			// Make the modification visible even on coarse-timestamp volumes.
			changedTime := originalTime.Add(24 * time.Hour)
			if err := os.Chtimes(claimed, changedTime, changedTime); err != nil {
				t.Fatal(err)
			}
		}
	}}
	scannedHash := fmt.Sprintf("%x", sha256.Sum256(original))
	err := applyArchivedFile(ctx, target, name, scannedHash, "", true)
	if checks < 2 || err == nil {
		t.Fatalf("expected the deletion to stop after the edit, checks=%d, err=%v", checks, err)
	}
	copy(original, edit)
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("the working file must contain the saved edit after rollback: %v", err)
	}
}
