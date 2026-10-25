//go:build !windows

package mobile

import (
	"os"
	"path/filepath"
	"polysync/internal/model"
	"testing"
)

func TestWorkspaceExportThroughApplicationDirectoryAlias(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	e := NewEngine()
	if err := e.Start(alias, "Phone", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	path := filepath.Join(alias, "workspaces", "documents")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "document.txt"), []byte("phone document"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store().AddShare(model.Share{ID: "test", Path: path}); err != nil {
		t.Fatal(err)
	}
	exported, err := e.WorkspaceExportPath("test", "document.txt")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(exported)
	if err != nil || string(content) != "phone document" {
		t.Fatalf("wrong exported content: %v", err)
	}
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.WorkspaceExportPath("test", "escape.txt"); err == nil {
		t.Fatal("export followed an escaping symlink")
	}
}
