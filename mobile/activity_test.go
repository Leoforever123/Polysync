package mobile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActivityBridgeDoesNotDependOnFullStatus(t *testing.T) {
	e := testEngine(t)
	if e.HasActiveWork() {
		t.Fatal("idle engine active")
	}
	path := filepath.Join(e.svc.Store().Dir(), "conflicts")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "bad.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Request("GET", "/api/status", ""); err == nil {
		t.Fatal("expected corrupt status error")
	}
	if e.HasActiveWork() {
		t.Fatal("activity check depended on corrupt history")
	}
	e.Stop()
	if e.HasActiveWork() {
		t.Fatal("stopped engine active")
	}
}
