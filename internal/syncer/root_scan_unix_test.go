//go:build linux || darwin

package syncer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
