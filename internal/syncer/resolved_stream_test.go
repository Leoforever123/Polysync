package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type generatedContent struct {
	remaining int64
	maxRead   int
	cancel    context.CancelFunc
}

func (r *generatedContent) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.remaining -= n
	if r.cancel != nil {
		r.cancel()
	}
	return int(n), nil
}
func TestResolvedStreamLargeFileBoundedReadsAndHistory(t *testing.T) {
	root, history := t.TempDir(), t.TempDir()
	destination := filepath.Join(root, "file.bin")
	if err := os.WriteFile(destination, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	const size = 32 << 20
	digest := sha256.New()
	if _, err := io.Copy(digest, &generatedContent{remaining: size}); err != nil {
		t.Fatal(err)
	}
	input := &generatedContent{remaining: size}
	want := hex.EncodeToString(digest.Sum(nil))
	if err := WriteResolvedStream(context.Background(), root, "file.bin", history, input, size, want, 0644); err != nil {
		t.Fatal(err)
	}
	if input.maxRead > 64<<10 {
		t.Fatalf("unbounded reads: %d", input.maxRead)
	}
	output, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	digest.Reset()
	n, err := io.Copy(digest, output)
	if err != nil || n != size || hex.EncodeToString(digest.Sum(nil)) != want {
		t.Fatal("wrong restored content", n, err)
	}
	matches, err := filepath.Glob(filepath.Join(history, "*", "file.bin"))
	if err != nil || len(matches) != 1 {
		t.Fatal(matches, err)
	}
	old, err := os.ReadFile(matches[0])
	if err != nil || string(old) != "original" {
		t.Fatal("history lost", err)
	}
}
func TestResolvedStreamFailurePreservesDestinationAndHistory(t *testing.T) {
	for _, kind := range []string{"hash", "short", "long", "cancel", "cancel-during-read", "path"} {
		t.Run(kind, func(t *testing.T) {
			root, history := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var reader io.Reader = bytes.NewBufferString("new")
			size := int64(3)
			hash := ""
			path := "file"
			switch kind {
			case "hash":
				hash = hex.EncodeToString(make([]byte, 32))
			case "short":
				size = 4
			case "long":
				size = 2
			case "cancel":
				cancel()
			case "cancel-during-read":
				reader = &generatedContent{remaining: 128 << 10, cancel: cancel}
				size = 128 << 10
			case "path":
				path = "../escape"
			}
			err := WriteResolvedStream(ctx, root, path, history, reader, size, hash, 0644)
			if err == nil {
				t.Fatal("expected failure")
			}
			if kind == "cancel-during-read" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			original, err := os.ReadFile(filepath.Join(root, "file"))
			if err != nil || string(original) != "original" {
				t.Fatal("destination changed", err)
			}
			entries, err := os.ReadDir(history)
			if err != nil || len(entries) != 0 {
				t.Fatal("history changed before verification", err)
			}
			entries, err = os.ReadDir(root)
			if err != nil || len(entries) != 1 {
				t.Fatal("temporary file leaked", err)
			}
		})
	}
}
