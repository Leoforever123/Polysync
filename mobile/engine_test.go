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

	"polysync/internal/model"
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

func TestWorkspaceRoleAndMemberBridge(t *testing.T) {
	e := testEngine(t)
	for _, tc := range []struct {
		role       string
		write      bool
		invitation string
	}{
		{"administrator", false, ""}, {"collaborator", true, ""}, {"follower", false, "absent"},
	} {
		if _, err := e.CreateWorkspaceWithRole("Documents", "paired", tc.invitation, tc.role, tc.write); err == nil {
			t.Errorf("unexpected success for %+v", tc)
		}
	}
	entries, err := os.ReadDir(filepath.Join(e.dir, "workspaces"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected private directories: %v, %v", entries, err)
	}
	for _, route := range []struct{ method, path string }{
		{"PUT", "/api/shares/abc/members/device"}, {"DELETE", "/api/shares/abc/members/device"}, {"GET", "/api/shares/abc/writeback-preview"}, {"POST", "/api/shares/abc/writeback-confirm"},
	} {
		if !allowed(route.method, route.path) {
			t.Errorf("missing route %+v", route)
		}
	}
	if !allowed("POST", "/api/shares/abc/members") {
		t.Fatal("missing member invitation route")
	}
	for _, path := range []string{"/api/shares/../members", "/api/shares/x//members", "/api/shares/x/members?path=/tmp"} {
		if allowed("POST", path) {
			t.Errorf("unexpected member route %q", path)
		}
	}
	if allowed("GET", "/api/shares/abc/members") {
		t.Fatal("unexpected method")
	}
}

func TestAdditionalInvitationReusesMobileWorkspace(t *testing.T) {
	a, b, c := testEngine(t), testEngine(t), testEngine(t)
	engines := []*Engine{a, b, c}
	id := func(e *Engine) string { return e.svc.Store().Config().DeviceID }
	for _, local := range engines {
		for _, peer := range engines {
			if local == peer {
				continue
			}
			cfg := peer.svc.Store().Config()
			if err := local.svc.Store().SavePairedDevice(model.PairedDevice{ID: cfg.DeviceID, Name: cfg.DeviceName,
				PublicKey: cfg.IdentityPublicKey, Addresses: []string{fmt.Sprintf("127.0.0.1:%d", peer.svc.ListenPort())}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	response, err := a.CreateWorkspaceWithRole("文档", id(b), "", "collaborator", false)
	if err != nil {
		t.Fatal(err)
	}
	var group model.Share
	if err := json.Unmarshal([]byte(response), &group); err != nil {
		t.Fatal(err)
	}
	accept := func(node *Engine) {
		t.Helper()
		invites := node.svc.ShareInvitations()
		if len(invites) != 1 {
			t.Fatalf("expected one invitation: %+v", invites)
		}
		if _, err := node.CreateWorkspace("文档", "", invites[0].ID); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return !node.HasActiveWork() })
	}
	add := func(from, to *Engine) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"deviceId": id(to), "role": "collaborator"})
		if _, err := from.Request("POST", "/api/shares/"+group.ID+"/members", string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	accept(b)
	root, err := b.workspace(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	add(a, c)
	accept(c)
	add(c, b)
	accept(b)
	entries, err := os.ReadDir(filepath.Join(b.dir, "workspaces"))
	stored, exists := b.svc.Store().Share(group.ID)
	if err != nil || len(entries) != 1 || !exists || stored.Path != root || len(stored.Members) != 2 {
		t.Fatalf("mobile roots or memberships changed: %v / %+v / %v", entries, stored, err)
	}
}
