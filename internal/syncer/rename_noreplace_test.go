package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNoReplace(t *testing.T) {
	for _, method := range []struct {
		name string
		move func(string, string) error
	}{
		{"native or fallback", renameNoReplace},
		{"link fallback", linkNoReplace},
	} {
		t.Run(method.name, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "published")
			// Two complete files race for one destination. The winner must be
			// published whole and the losing source must remain available.
			type result struct {
				name string
				err  error
			}
			start := make(chan struct{})
			results := make(chan result, 2)
			names := []string{"first", "second"}
			for _, name := range names {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range names {
				go func() {
					<-start
					results <- result{name, method.move(filepath.Join(dir, name), destination)}
				}()
			}
			close(start)
			first, second := <-results, <-results
			winner, loser := first, second
			if first.err != nil {
				winner, loser = second, first
			}
			if winner.err != nil || !errors.Is(loser.err, os.ErrExist) {
				t.Fatalf("expected one success and one existing destination, got %v and %v", first.err, second.err)
			}
			if _, err := os.Lstat(filepath.Join(dir, winner.name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("winning source after publication: %v", err)
			}
			published, err := os.ReadFile(destination)
			if err != nil || string(published) != winner.name {
				t.Fatalf("destination = %q, %v; want %q", published, err, winner.name)
			}
			preserved, err := os.ReadFile(filepath.Join(dir, loser.name))
			if err != nil || string(preserved) != loser.name {
				t.Fatalf("losing source = %q, %v; want %q", preserved, err, loser.name)
			}
		})
	}
}

func TestLinkNoReplaceRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "published")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := linkNoReplace(source, destination); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("publishing directory: %v", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination after failed publication: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source after failed publication: %v", err)
	}
}
