package syncer

import (
	"os"
	"path/filepath"
	"polysync/internal/rootidentity"
	"strings"
	"testing"
)

func TestScanExcludesAndProtectsLocalRootMarker(t *testing.T) {
	path := t.TempDir()
	if _, err := rootidentity.Bind(path); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(path)
	if err != nil || len(files) != 1 {
		t.Fatalf("marker creation: %v, %v", files, err)
	}
	name := files[0].Name()
	for _, target := range []string{name, strings.ToUpper(name), name + ":stream", name + "/child"} {
		if _, err := SecureJoin(path, target); err == nil {
			t.Errorf("accepted %q", target)
		}
	}
	entries, err := Scan(t.Context(), path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("shared local marker: %v, %v", entries, err)
	}
	if err := os.Remove(filepath.Join(path, name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, name), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(t.Context(), path); err == nil {
		t.Fatal("silently skipped contents of a colliding user directory")
	}
}

func openTestRoot(t *testing.T, path string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}
