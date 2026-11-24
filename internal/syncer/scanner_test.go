package syncer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestScanAndSecureJoin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "nested/hello.txt" || entries[0].Size != 5 {
		t.Fatalf("unexpected manifest: %#v", entries)
	}
	if _, err := SecureJoin(root, "../escape.txt"); err == nil {
		t.Fatal("parent traversal must be rejected")
	}
}

func TestScanRejectsFileReplacedAfterDirectoryWalk(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "notes")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the previous inode alive so its identity cannot be recycled.
	if err := os.Rename(target, filepath.Join(t.TempDir(), "previous")); err != nil {
		t.Fatal(err)
	}
	content := []byte("the new, longer revision")
	if err := os.WriteFile(target, content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanRegularFile(t.Context(), target, "notes", observed); err == nil {
		t.Fatal("accepted a replaced file with stale directory-entry metadata")
	}
	entries, err := Scan(t.Context(), root)
	if err != nil || len(entries) != 1 || entries[0].Size != int64(len(content)) || entries[0].Hash != fmt.Sprintf("%x", sha256.Sum256(content)) {
		t.Fatalf("fresh scan did not recover: %+v, %v", entries, err)
	}
}

func TestScanRejectsGrowthWhileHashing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "notes")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	// The scanner has already opened and measured the file when hashing
	// checks for cancellation before its first read.
	change := sync.OnceFunc(func() {
		if err := os.WriteFile(target, []byte("saved while being hashed"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	ctx := claimReadContext{Context: t.Context(), beforeRead: change}
	if _, err := scanRegularFile(ctx, target, "notes", before); err == nil {
		t.Fatal("accepted a digest for bytes of a different size")
	}
}
