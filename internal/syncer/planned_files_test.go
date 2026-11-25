package syncer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAtVacatedNameCanRescan(t *testing.T) {
	for _, operation := range []string{"delete", "replace"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "notes")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			original := fmt.Sprintf("%x", sha256.Sum256([]byte("old")))
			if err := archiveExisting(t.Context(), openTestRoot(t, root), "notes", t.TempDir(), original); err != nil {
				t.Fatal(err)
			}
			var staged string
			if operation == "replace" {
				staged = ".polysync-part-test"
				if err := os.WriteFile(filepath.Join(root, staged), []byte("incoming"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			saved := false
			hash := func(ctx context.Context, file io.Reader) (string, int64, error) {
				if err := os.WriteFile(target, []byte("just saved"), 0o600); err != nil {
					t.Fatal(err)
				}
				saved = true
				return hashReader(ctx, file)
			}
			if err := applyArchivedFile(t.Context(), openTestRoot(t, root), "notes", original, staged, t.TempDir(), hash); !saved || err == nil {
				t.Fatalf("expected the concurrent save to abort the plan: %v", err)
			}
			entries, err := Scan(t.Context(), root)
			want := fmt.Sprintf("%x", sha256.Sum256([]byte("just saved")))
			if err != nil || len(entries) != 1 || entries[0].Path != "notes" || entries[0].Hash != want {
				t.Fatalf("next scan did not see the saved file: %v, %v", entries, err)
			}
		})
	}
}

func TestFileSavedAfterArchiveIsRestored(t *testing.T) {
	for _, operation := range []string{"delete", "replace"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "notes")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			manifest, err := Scan(t.Context(), root)
			if err != nil || len(manifest) != 1 {
				t.Fatal(manifest, err)
			}
			scanned := manifest[0].Hash
			if err := archiveExisting(t.Context(), openTestRoot(t, root), "notes", t.TempDir(), scanned); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			var staged string
			if operation == "replace" {
				staged = ".polysync-part-test"
				if err := os.WriteFile(filepath.Join(root, staged), []byte("peer"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := applyArchivedFile(t.Context(), openTestRoot(t, root), "notes", scanned, staged, t.TempDir(), hashReader); err == nil {
				t.Fatal("committed a stale plan after archiving")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "new" {
				t.Fatalf("local save = %q, %v", data, err)
			}
			if _, err := Scan(t.Context(), root); err != nil {
				t.Fatalf("could not rescan after restoring the edit: %v", err)
			}
		})
	}
}

func TestInterruptedCommitPreservesBothLocalSavesAndStopsScan(t *testing.T) {
	root := t.TempDir()
	temp, err := os.MkdirTemp(root, commitDirPrefix+"*")
	if err != nil {
		t.Fatal(err)
	}
	holder := filepath.Join(temp, originalDirName)
	if err := os.Mkdir(holder, 0o700); err != nil {
		t.Fatal(err)
	}
	if entries, err := Scan(t.Context(), root); err != nil || len(entries) != 0 {
		t.Fatalf("empty staging directory: %v, %v", entries, err)
	}
	target, claimed := filepath.Join(root, "notes"), filepath.Join(holder, "notes")
	for path, data := range map[string]string{target: "second save", claimed: "first save"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := restoreClaim(openTestRoot(t, root), filepath.Join(filepath.Base(temp), originalDirName, "notes"), "notes", changedTarget("notes")); err == nil || !strings.Contains(err.Error(), claimed) {
		t.Fatalf("missing recovery location: %v", err)
	}
	for path, want := range map[string]string{target: "second save", claimed: "first save"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
	if _, err := Scan(t.Context(), root); err == nil || !strings.Contains(err.Error(), holder) {
		t.Fatalf("expected an interrupted commit with its recovery location: %v", err)
	}
}

func TestScanIncludesOrdinaryDirectoryWithCommitPrefix(t *testing.T) {
	root := t.TempDir()
	name := commitDirPrefix + "project"
	if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name, "notes"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := Scan(t.Context(), root)
	if err != nil || len(entries) != 1 || entries[0].Path != name+"/notes" {
		t.Fatalf("ordinary directory contents: %v, %v", entries, err)
	}
}
