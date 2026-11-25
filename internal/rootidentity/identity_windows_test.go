package rootidentity

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMarkerHiddenInExplorer(t *testing.T) {
	path := t.TempDir()
	if _, err := Bind(path); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(path)
	if err != nil || len(files) != 1 {
		t.Fatalf("marker list %v, %v", files, err)
	}
	name, err := windows.UTF16PtrFromString(filepath.Join(path, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
		t.Fatalf("attributes %#x, %v", attrs, err)
	}
}
