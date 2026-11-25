//go:build linux || darwin

package syncer

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"polysync/internal/model"
)

func TestStagedTransfersPreserveModeAndTimestamp(t *testing.T) {
	path := t.TempDir()
	const contents = "echo ready"
	const mode os.FileMode = 0750
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))
	when := time.Unix(1711111111, 0)
	entry := model.Entry{Path: "received", Size: int64(len(contents)), Mode: uint32(mode), Hash: digest, ModTime: when.UnixNano()}
	err := InstallPlannedContent(t.Context(), path, "", entry, "", func(w io.Writer, _ int64) error {
		_, err := io.WriteString(w, contents)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteResolvedStream(t.Context(), path, "resolved", "", strings.NewReader(contents), entry.Size, digest, mode); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{entry.Path, "resolved"} {
		info, err := os.Stat(filepath.Join(path, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %v, want %v", name, info.Mode().Perm(), mode)
		}
		if name == entry.Path && !info.ModTime().Equal(when) {
			t.Fatalf("modtime = %v, want %v", info.ModTime(), when)
		}
	}
}
