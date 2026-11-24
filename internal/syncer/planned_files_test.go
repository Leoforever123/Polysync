package syncer

import (
	"crypto/sha256"
	"fmt"
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
			if err := archiveExisting(t.Context(), target, "notes", t.TempDir(), original); err != nil {
				t.Fatal(err)
			}
			var staged string
			if operation == "replace" {
				staged = filepath.Join(root, ".polysync-part-test")
				if err := os.WriteFile(staged, []byte("incoming"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			saved := false
			ctx := claimReadContext{Context: t.Context(), beforeRead: func() {
				if !saved {
					saved = true
					if err := os.WriteFile(target, []byte("just saved"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}}
			if err := applyArchivedFile(ctx, root, target, "notes", original, staged, t.TempDir()); !saved || err == nil {
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
			if err := archiveExisting(t.Context(), target, "notes", t.TempDir(), scanned); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			var staged string
			if operation == "replace" {
				staged = filepath.Join(root, ".polysync-part-test")
				if err := os.WriteFile(staged, []byte("peer"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := applyArchivedFile(t.Context(), root, target, "notes", scanned, staged, t.TempDir()); err == nil {
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
	if err := restoreClaim(claimed, target, changedTarget("notes")); err == nil || !strings.Contains(err.Error(), claimed) {
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
