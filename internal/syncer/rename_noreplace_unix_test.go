//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Both parent paths change after the move has opened them and before it
// discovers that only the hard-link fallback is available.
func TestLinkFallbackPinsBothParentDirectories(t *testing.T) {
	source, history := openTestRoot(t, t.TempDir()), openTestRoot(t, t.TempDir())
	for _, root := range []*os.Root{source, history} {
		if err := root.Mkdir("current", 0700); err != nil {
			t.Fatal(err)
		}
	}
	name := filepath.Join("current", "note")
	if err := source.WriteFile(name, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	swap := func(*os.File, string, *os.File, string) error {
		for _, root := range []*os.Root{source, history} {
			if err := root.Rename("current", "detached"); err != nil {
				t.Fatal(err)
			}
			if err := root.Mkdir("current", 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := source.WriteFile(name, []byte("replacement"), 0600); err != nil {
			t.Fatal(err)
		}
		return errors.ErrUnsupported
	}
	if err := renameOrLink(source, name, history, name, swap); err != nil {
		t.Fatal(err)
	}
	if data, err := source.ReadFile(name); err != nil || string(data) != "replacement" {
		t.Fatalf("replacement source was touched: %q, %v", data, err)
	}
	if _, err := history.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrote to replacement history: %v", err)
	}
	detached := filepath.Join("detached", "note")
	if _, err := source.Stat(detached); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old name still exists in original source: %v", err)
	}
	if data, err := history.ReadFile(detached); err != nil || string(data) != "original" {
		t.Fatalf("history on original filesystem: %q, %v", data, err)
	}
}
