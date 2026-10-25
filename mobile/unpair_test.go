package mobile

import (
	"os"
	"path/filepath"
	"testing"

	"polysync/internal/model"
)

func TestMobileUnpairPreservesWorkspaceExport(t *testing.T) {
	e := testEngine(t)
	st := e.svc.Store()
	if err := st.SavePairedDevice(model.PairedDevice{ID: "peer"}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(e.dir, "workspaces", "local")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := st.AddShare(model.Share{ID: "local", Path: root, PeerDeviceID: "peer", State: "active", AutoSync: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Request("DELETE", "/api/paired-devices/peer", ""); err != nil {
		t.Fatal(err)
	}
	path, err := e.WorkspaceExportPath("local", "keep.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep" {
		t.Fatalf("lost workspace export: %q %v", data, err)
	}
	if _, err := e.Request("POST", "/api/shares/local/sync", ""); err == nil {
		t.Fatal("disabled workspace synchronized")
	}
	for _, p := range []string{"/api/paired-devices/peer/extra", "/api/paired-devices/.."} {
		if allowed("DELETE", p) {
			t.Fatalf("bridge accepted %s", p)
		}
	}
	if allowed("POST", "/api/paired-devices/peer") {
		t.Fatal("bridge accepted unsupported method")
	}
}
