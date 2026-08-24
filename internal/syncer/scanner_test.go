package syncer

import (
	"context"
	"os"
	"path/filepath"
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
