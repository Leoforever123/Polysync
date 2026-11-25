package syncer

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriterHeldAcrossCommitCanStillSave(t *testing.T) {
	for _, operation := range []string{"overwrite", "delete"} {
		for _, local := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local-history=%t", operation, local), func(t *testing.T) {
				root, history := t.TempDir(), t.TempDir()
				search := filepath.Join(history, "open-files")
				if local {
					history, search = "", root
				}
				const name = "nested/notes.txt"
				target := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				editor, err := os.OpenFile(target, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer editor.Close()
				original, err := editor.Stat()
				if err != nil {
					t.Fatal(err)
				}
				var staged string
				if operation == "overwrite" {
					staged = ".polysync-part-test"
					if err := os.WriteFile(filepath.Join(root, staged), []byte("peer"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte("old")))
				err = applyPlannedFile(t.Context(), openTestRoot(t, root), name, history, hash, staged)
				if err != nil {
					// Windows editors may deny deletion sharing. In that case the
					// old plan must stop and the open file must still own the name.
					if runtime.GOOS != "windows" {
						t.Fatal(err)
					}
					live, statErr := os.Stat(target)
					if statErr != nil || !os.SameFile(live, original) {
						t.Fatalf("failed commit lost original: %v / %v", err, statErr)
					}
					search = root
				}
				if _, err := editor.WriteAt([]byte("late save"), 0); err != nil {
					t.Fatal(err)
				}
				if err := editor.Sync(); err != nil {
					t.Fatal(err)
				}
				var found bool
				err = filepath.WalkDir(search, func(path string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					info, err := d.Info()
					if err != nil || !os.SameFile(info, original) {
						return err
					}
					data, err := os.ReadFile(path)
					found = string(data) == "late save"
					return err
				})
				if err != nil || !found {
					t.Fatalf("late write inaccessible: %v", err)
				}
				entries, err := Scan(t.Context(), root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.Path != name {
						t.Fatalf("private retained file would be transferred: %s", entry.Path)
					}
				}
			})
		}
	}
}
