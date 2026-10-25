package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"polysync/internal/service"
	"polysync/internal/store"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine()
	if err := e.Start(t.TempDir(), "Android test", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Stop)
	return e
}

func TestRepeatedStartStopPreservesIdentityAndReleasesPort(t *testing.T) {
	e := NewEngine()
	dir := t.TempDir()
	id := ""
	for i := 0; i < 20; i++ {
		if err := e.Start(dir, "Phone", "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		if id == "" {
			id = e.svc.Store().Config().DeviceID
		} else if id != e.svc.Store().Config().DeviceID {
			t.Fatal("identity changed")
		}
		addr := fmt.Sprintf("127.0.0.1:%d", e.svc.ListenPort())
		e.Stop()
		e.Stop()
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		listener.Close()
		if _, err = e.Request("GET", "/api/status", ""); err == nil {
			t.Fatal("stopped engine handled request")
		}
	}
}

func TestBridgeRestrictsDesktopOperationsAndPaths(t *testing.T) {
	e := testEngine(t)
	for _, p := range []string{"/api/pick-folder", "/api/update/install", "/api/shares", "/api/transfer-settings", "/api/transfers/id/files/0/open"} {
		if _, err := e.Request("POST", p, "{}"); err == nil {
			t.Fatalf("allowed %s", p)
		}
	}
	if _, err := e.SendImport("peer", "../state/config.json", "leak"); err == nil {
		t.Fatal("path traversal")
	}
	if _, err := e.CreateWorkspace("../bad", "peer", ""); err == nil {
		t.Fatal("invalid workspace")
	}
}

func TestMobilePairsTransfersAndSyncsWithDesktop(t *testing.T) {
	e := testEngine(t)
	st, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	desktop := service.New(st)
	if err = desktop.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer desktop.Stop()
	address := fmt.Sprintf("127.0.0.1:%d", desktop.ListenPort())
	body, _ := json.Marshal(map[string]string{"address": address})
	result, err := e.Request("POST", "/api/pair/start", string(body))
	if err != nil {
		t.Fatal(err)
	}
	var pairing struct {
		SessionID string `json:"sessionId"`
	}
	if err = json.Unmarshal([]byte(result), &pairing); err != nil {
		t.Fatal(err)
	}
	requests := desktop.PairingRequests()
	if len(requests) != 1 {
		t.Fatal("missing request")
	}
	if err = desktop.ApprovePairingRequest(requests[0].ID); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]string{"sessionId": pairing.SessionID, "code": desktop.PairingRequests()[0].Code})
	if _, err = e.Request("POST", "/api/pair/confirm", string(body)); err != nil {
		t.Fatal(err)
	}
	peer := st.Config().DeviceID
	data := []byte("mobile to desktop through the real TLS protocol")
	if err = os.WriteFile(filepath.Join(e.dir, "imports", "input"), data, 0600); err != nil {
		t.Fatal(err)
	}
	id, err := e.SendImport(peer, "input", "手机.txt")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		result, er := e.Request("GET", "/api/transfers", "")
		if er != nil {
			return false
		}
		var r struct {
			Tasks []service.TransferTask `json:"tasks"`
		}
		_ = json.Unmarshal([]byte(result), &r)
		for _, task := range r.Tasks {
			if task.ID == id {
				return task.State == "complete"
			}
		}
		return false
	})
	// Desktop-side receipt is persisted by the existing receiver, not a mock.
	records, err := os.ReadDir(filepath.Join(st.Dir(), "transfers", "records"))
	if err != nil || len(records) != 1 {
		t.Fatalf("receipt: %v %v", records, err)
	}
	var receipt service.TransferTask
	b, err := os.ReadFile(filepath.Join(st.Dir(), "transfers", "records", records[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &receipt); err != nil {
		t.Fatal(err)
	}
	received, err := os.ReadFile(filepath.Join(receipt.StorageDir, receipt.Files[0].Blob))
	if err != nil || string(received) != string(data) {
		t.Fatalf("wrong receipt: %v", err)
	}
	root := t.TempDir()
	if err = os.WriteFile(filepath.Join(root, "电脑.txt"), []byte("desktop workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	share, err := desktop.InviteShare(context.Background(), e.svc.Store().Config().DeviceID, "Documents", root, false, 30)
	if err != nil {
		t.Fatal(err)
	}
	invites := e.svc.ShareInvitations()
	if len(invites) != 1 {
		t.Fatal("missing invite")
	}
	if _, err = e.CreateWorkspace("Documents", "", invites[0].ID); err != nil {
		t.Fatal(err)
	}
	var importedPath string
	waitFor(t, func() bool { importedPath, err = e.WorkspaceExportPath(share.ID, "电脑.txt"); return err == nil })
	if b, err = os.ReadFile(importedPath); err != nil || string(b) != "desktop workspace" {
		t.Fatal("workspace content differs")
	}
	if err = e.ImportToWorkspace(share.ID, "input", "phone-copy.txt"); err != nil {
		t.Fatal(err)
	}
	if err = e.ImportToWorkspace(share.ID, "input", "phone-copy.txt"); err == nil {
		t.Fatal("import overwrote existing file")
	}
	if _, err = e.WorkspaceExportPath(share.ID, "../state/config.json"); err == nil {
		t.Fatal("workspace traversal")
	}
	// First automatic invite sync must finish before requesting another session.
	waitFor(t, func() bool { return e.svc.Statuses()[share.ID].State != "syncing" })
	if err = e.svc.SyncShare(context.Background(), share.ID, true); err != nil {
		t.Fatal(err)
	}
	if b, err = os.ReadFile(filepath.Join(root, "phone-copy.txt")); err != nil || string(b) != string(data) {
		t.Fatal("workspace failed to sync back")
	}
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}
