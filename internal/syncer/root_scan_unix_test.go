//go:build linux || darwin

package syncer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"polysync/internal/model"
)

type rootSwapContext struct {
	context.Context
	check func()
}

func (c rootSwapContext) Err() error { c.check(); return c.Context.Err() }

func TestScanRootUsesOriginalDirectoryDuringPathSwap(t *testing.T) {
	base := t.TempDir()
	live, original, replacement := filepath.Join(base, "live"), filepath.Join(base, "old"), filepath.Join(base, "replacement")
	for p, text := range map[string]string{live: "original contents", replacement: "replacement contents"} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "document"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.OpenRoot(live)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	rename := func(a, b string) {
		t.Helper()
		if err := os.Rename(a, b); err != nil {
			t.Fatal(err)
		}
	}
	steps := 0
	ctx := rootSwapContext{t.Context(), func() {
		steps++
		switch steps {
		case 2: // Name enumerated; simulate the mount changing before opening the file.
			rename(live, original)
			rename(replacement, live)
		case 3: // Restore the original root while the opened file is hashed.
			rename(live, replacement)
			rename(original, live)
		}
	}}
	entries, err := ScanRoot(ctx, dir)
	if err != nil || steps < 3 || len(entries) != 1 || entries[0].Hash != fmt.Sprintf("%x", sha256.Sum256([]byte("original contents"))) {
		t.Fatalf("scan mixed two roots during pathname reuse: %v, %v (steps %d)", entries, err, steps)
	}
}

func TestTransferKeepsOriginalHandleThroughPublishRestoreAndCleanup(t *testing.T) {
	for _, phase := range []string{"before claim", "before rollback", "before publish", "aborted transfer cleanup"} {
		t.Run(phase, func(t *testing.T) {
			base := t.TempDir()
			live, old, other := filepath.Join(base, "live"), filepath.Join(base, "old"), filepath.Join(base, "other")
			for _, path := range []string{live, other} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			root := openTestRoot(t, live)
			const name = "work.txt"
			const original = "the original"
			const replacement = "only on the second disk"
			if err := os.WriteFile(filepath.Join(other, name), []byte(replacement), 0600); err != nil {
				t.Fatal(err)
			}
			swap := func() {
				t.Helper()
				if err := os.Rename(live, old); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(other, live); err != nil {
					t.Fatal(err)
				}
			}
			var check func() error
			swapped := false
			offline := errors.New("detached")
			switch phase {
			case "before claim", "before rollback":
				if err := root.WriteFile(name, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				check = func() error {
					if swapped {
						return offline
					}
					matches, err := filepath.Glob(filepath.Join(live, commitDirPrefix+"*", originalDirName))
					if err != nil {
						t.Fatal(err)
					}
					if len(matches) == 0 {
						return nil
					}
					_, err = root.Lstat(name)
					if err != nil && !errors.Is(err, os.ErrNotExist) {
						t.Fatal(err)
					}
					if (phase == "before claim" && err == nil) || (phase == "before rollback" && errors.Is(err, os.ErrNotExist)) {
						swap()
						swapped = true
						if phase == "before rollback" {
							return offline
						}
						// Simulate loss after the guard has already returned success.
					}
					return nil
				}
				ctx := WithRootCheck(t.Context(), root, check)
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(original)))
				if err := DeletePlannedFile(ctx, live, name, t.TempDir(), digest); !errors.Is(err, offline) {
					t.Fatalf("delete = %v", err)
				}
				data, err := root.ReadFile(name)
				if err != nil || string(data) != original {
					t.Fatalf("original after rollback = %q, %v", data, err)
				}
			case "before publish":
				if err := root.WriteFile(".polysync-part-staged", []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				ctx := WithRootCheck(t.Context(), root, func() error {
					swap()
					swapped = true
					return nil
				})
				if err := applyPlannedFile(ctx, root, name, "", "", ".polysync-part-staged"); err != nil {
					t.Fatal(err)
				}
				data, err := root.ReadFile(name)
				if err != nil || string(data) != original {
					t.Fatalf("published to wrong folder: %q, %v", data, err)
				}
			case "aborted transfer cleanup":
				ctx := WithRootCheck(t.Context(), root, func() error { return nil })
				var staged string
				err := InstallPlannedContent(ctx, live, "", model.Entry{Path: "new"}, "", func(io.Writer, int64) error {
					files, err := os.ReadDir(live)
					if err != nil || len(files) != 1 {
						t.Fatalf("staged file = %v, %v", files, err)
					}
					staged = files[0].Name()
					if err := os.WriteFile(filepath.Join(other, staged), []byte(replacement), 0600); err != nil {
						t.Fatal(err)
					}
					swap()
					swapped = true
					return offline
				})
				if !errors.Is(err, offline) {
					t.Fatalf("copy = %v", err)
				}
				if _, err := root.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("temp not cleaned in original: %v", err)
				}
				got, err := os.ReadFile(filepath.Join(live, staged))
				if err != nil || string(got) != replacement {
					t.Fatalf("cleanup touched replacement: %q, %v", got, err)
				}
			}
			got, err := os.ReadFile(filepath.Join(live, name))
			if !swapped || err != nil || string(got) != replacement {
				t.Fatalf("replacement changed: %q, %v (swapped %v)", got, err, swapped)
			}
		})
	}
}
