package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/store"
)

func TestUnpairAPIKeepsLocalWorkspaceAndRevokesTrust(t *testing.T) {
	st, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SavePairedDevice(model.PairedDevice{ID: "peer", Name: "Phone"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeTestFile(t, root, "keep.txt", "keep me")
	if err := st.AddShare(model.Share{ID: "workspace", Path: root, PeerDeviceID: "peer", State: "active", AutoSync: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveShareInvitation(model.ShareInvitation{ID: "invite", DeviceID: "peer"}); err != nil {
		t.Fatal(err)
	}
	s := New(st)
	t.Cleanup(s.Stop)
	s.pairingRequests["stale"] = pendingPair{Request: model.PairingRequest{DeviceID: "peer"}}
	s.outboundPairs["stale"] = outboundPair{DeviceID: "peer"}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("DELETE", "/api/paired-devices/peer", nil))
	if w.Code != 200 {
		t.Fatalf("unpair: HTTP %d: %s", w.Code, w.Body.String())
	}
	if _, ok := st.PairedDevice("peer"); ok {
		t.Fatal("trust retained")
	}
	share, ok := st.Share("workspace")
	if !ok || share.State != "unpaired" || share.AutoSync {
		t.Fatalf("workspace not safely disabled: %+v", share)
	}
	if len(s.ShareInvitations()) != 0 || len(st.Config().ShareInvitations) != 0 {
		t.Fatal("stale invitations retained")
	}
	if len(s.pairingRequests) != 0 || len(s.outboundPairs) != 0 {
		t.Fatal("stale pairing sessions retained")
	}
	assertTestFile(t, root, "keep.txt", "keep me")
	if _, err := s.CreateTransfer("peer", []TransferFile{{Name: "new.txt", Size: 0}}); err == nil {
		t.Fatal("transfer accepted after unpair")
	}
	reopened, err := store.Open(st.Dir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.PairedDevice("peer"); ok {
		t.Fatal("trust restored after restart")
	}
	if err := st.SavePairedDevice(model.PairedDevice{ID: "peer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncShare(context.Background(), "workspace", true); err == nil {
		t.Fatal("re-pairing silently resumed old sync")
	}
}

func TestUnpairRefusesBusyWorkAndCrossOriginRequests(t *testing.T) {
	s := basicService(t)
	if err := s.transfers.load(); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SavePairedDevice(model.PairedDevice{ID: "peer"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "http://localhost/api/paired-devices/peer", nil)
	r.Header.Set("Origin", "http://other.example")
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross origin: %d", w.Code)
	}
	if err := s.beginSyncWork(); err != nil {
		t.Fatal(err)
	}
	err := s.UnpairDevice("peer")
	s.endSyncWork()
	if !errors.Is(err, errUnpairBusy) {
		t.Fatalf("active sync: %v", err)
	}
	_, done, err := s.beginTransferWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = s.UnpairDevice("peer")
	done()
	if !errors.Is(err, errUnpairBusy) {
		t.Fatalf("active transfer: %v", err)
	}
	s.peerControlMu.RLock()
	err = s.UnpairDevice("peer")
	s.peerControlMu.RUnlock()
	if !errors.Is(err, errUnpairBusy) {
		t.Fatalf("active control operation: %v", err)
	}
	task, err := s.CreateTransfer("peer", []TransferFile{{Name: "waiting.txt", Size: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UnpairDevice("peer"); !errors.Is(err, errUnpairBusy) {
		t.Fatalf("queued transfer: %v", err)
	}
	if _, ok := s.store.PairedDevice("peer"); !ok {
		t.Fatal("busy request revoked trust")
	}
	if err := s.transfers.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnpairDevice("peer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.transfers.Task(task.ID); err != nil {
		t.Fatal("transfer history removed", err)
	}
	if err := s.UnpairDevice("missing"); err == nil {
		t.Fatal("unknown peer accepted")
	}
}

func TestUnpairedPeerCannotSendOrInviteAndCanPairAgain(t *testing.T) {
	a, b := transferPair(t)
	aID, bID := a.store.Config().DeviceID, b.store.Config().DeviceID
	if err := b.UnpairDevice(aID); err != nil {
		t.Fatal(err)
	}
	task := sendTestTransfer(t, a, b, []string{"must not arrive"})
	if task.State != "failed" || !strings.Contains(task.Message, "尚未配对") {
		t.Fatalf("revoked TLS peer was not rejected: %+v", task)
	}
	if len(b.transfers.List()) != 0 {
		t.Fatal("receiver accepted untrusted file metadata")
	}
	if _, err := a.InviteShare(context.Background(), bID, "blocked", t.TempDir(), false, 30); err == nil {
		t.Fatal("revoked peer invited a share")
	}
	// The outgoing worker finishes bookkeeping just after its terminal state.
	deadline := time.Now().Add(3 * time.Second)
	for a.HasActiveWork() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := a.UnpairDevice(bID); err != nil {
		t.Fatal(err)
	}
	session, err := a.StartPairing(context.Background(), "", "127.0.0.1:"+intText(b.ListenPort()))
	if err != nil {
		t.Fatal(err)
	}
	requests := b.PairingRequests()
	if len(requests) != 1 {
		t.Fatalf("new request missing: %v", requests)
	}
	if err := b.ApprovePairingRequest(requests[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfirmPairing(context.Background(), session, b.PairingRequests()[0].Code); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.store.PairedDevice(bID); !ok {
		t.Fatal("new explicit pairing missing")
	}
}
