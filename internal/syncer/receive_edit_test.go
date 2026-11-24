package syncer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"polysync/internal/protocol"
)

type payloadReaderFunc func([]byte) (int, error)

func (read payloadReaderFunc) Read(p []byte) (int, error) { return read(p) }

func TestReceiveProtectsEditDuringPayload(t *testing.T) {
	root, history := t.TempDir(), t.TempDir()
	const name = "notes.txt"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("before transfer"), 0o644); err != nil {
		t.Fatal(err)
	}
	scannedHash, err := hashFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	const first, last = "first chunk", "last chunk"
	size := int64(len(first) + len(last))
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(first+last)))
	transfer := Transfer{Source: name, Dest: name, Size: size, Hash: digest, Mode: 0o644}
	var header bytes.Buffer
	err = protocol.NewFramer(&header).WriteJSON(protocol.FileHeader{
		Type: "file", Source: name, Dest: name, Size: size, Hash: digest, Mode: transfer.Mode,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Delay the local save until the receiver asks for the final payload chunk.
	const edit = "saved while file was in flight"
	tail := strings.NewReader(last)
	saved := false
	finalChunk := payloadReaderFunc(func(p []byte) (int, error) {
		if !saved {
			if err := os.WriteFile(path, []byte(edit), 0o644); err != nil {
				return 0, err
			}
			saved = true
		}
		return tail.Read(p)
	})
	input := io.MultiReader(&header, strings.NewReader(first), finalChunk)
	stream := struct {
		io.Reader
		io.Writer
	}{input, io.Discard}

	err = ReceiveFile(t.Context(), protocol.NewFramer(stream), root, history, transfer, scannedHash)
	if !saved || err == nil {
		t.Fatalf("expected rejection after mid-transfer save, saved=%t, err=%v", saved, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != edit {
		t.Fatalf("lost mid-transfer save: %q, %v", data, err)
	}
}
