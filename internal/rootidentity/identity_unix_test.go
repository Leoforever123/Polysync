//go:build linux || darwin

package rootidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBindRootDoesNotEnrollDirectoryReplacingOpenHandle(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "live")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := os.Rename(path, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := BindRoot(dir); err == nil {
		t.Fatal("enrolled a changed pathname")
	}
	files, err := os.ReadDir(path)
	if err != nil || len(files) != 0 {
		t.Fatalf("modified replacement: %v, %v", files, err)
	}
}
