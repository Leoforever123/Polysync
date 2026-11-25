package mobile

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"polysync/internal/model"
	"polysync/internal/syncer"
)

func TestWorkspaceShowsPublishedFilesDuringTransfer(t *testing.T) {
	e := testEngine(t)
	const id = "workspace"
	path := filepath.Join(e.dir, "workspaces", id)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Store().AddShare(model.Share{ID: id, Path: path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "imports", "source"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	var visible []string
	for _, prefix := range []string{".polysync-part-", ".polysync-backup-"} {
		dir := prefix + "project"
		if err := os.Mkdir(filepath.Join(path, dir), 0700); err != nil {
			t.Fatal(err)
		}
		name := dir + "/notes"
		if err := os.WriteFile(filepath.Join(path, filepath.FromSlash(name)), []byte("ordinary"), 0600); err != nil {
			t.Fatal(err)
		}
		visible = append(visible, name)
		if _, err := e.WorkspaceExportPath(id, name); err != nil {
			t.Fatalf("export from ordinary directory: %v", err)
		}
		if err := e.ImportToWorkspace(id, "source", prefix+"new"); err == nil {
			t.Fatalf("imported reserved name %q", prefix+"new")
		}
	}
	const backup = ".polysync-backup-project/.polysync-backup-rollback"
	if err := os.WriteFile(filepath.Join(path, filepath.FromSlash(backup)), []byte("old version"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.WorkspaceExportPath(id, backup); err == nil {
		t.Fatal("exported private backup")
	}
	checkNames := func(want []string) {
		t.Helper()
		list, err := e.WorkspaceFiles(id)
		if err != nil {
			t.Fatal(err)
		}
		var files []struct{ Name string }
		if err := json.Unmarshal([]byte(list), &files); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range files {
			names = append(names, f.Name)
		}
		slices.Sort(want)
		if !slices.Equal(want, names) {
			t.Fatalf("workspace shows %v, want %v", names, want)
		}
	}
	const data = "partial content"
	entry := model.Entry{Path: "received", Size: int64(len(data)), Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(data))), Mode: 0600}
	err := syncer.InstallPlannedContent(t.Context(), path, "", entry, "", func(w io.Writer, _ int64) error {
		if _, err := io.WriteString(w, data[:3]); err != nil {
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		var actual string
		for _, f := range entries {
			if !f.IsDir() && strings.HasPrefix(f.Name(), ".polysync-part-") {
				actual = f.Name()
			}
		}
		if actual == "" {
			t.Fatal("no active staging file")
		}
		checkNames(visible)
		if _, err := e.WorkspaceExportPath(id, actual); err == nil {
			t.Fatal("exported incomplete incoming transfer")
		}
		_, err = io.WriteString(w, data[3:])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	checkNames(append(visible, entry.Path))
}
