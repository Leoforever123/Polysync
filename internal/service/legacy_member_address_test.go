package service

import (
	"testing"

	"polysync/internal/model"
	"polysync/internal/protocol"
	"polysync/internal/store"
)

func TestLegacyMemberKeepsAuthenticatedAddress(t *testing.T) {
	nodes := groupNodes(t, 3)
	host, legacy, other := nodes[0], nodes[1], nodes[2]
	id, invitation := store.RandomID(), randomToken(16)
	const stale = "127.0.0.1:0"
	address := "127.0.0.1:" + intText(legacy.s.ListenPort())
	hostAddress := "127.0.0.1:" + intText(host.s.ListenPort())
	sh := model.Share{ID: id, ReplicaID: store.RandomID(), Name: "shared", LocalRole: model.RoleCollaborator, Path: host.dir,
		Members: []model.ShareMember{{DeviceID: legacy.id, InvitationID: invitation, Legacy: true, Role: model.RoleCollaborator, State: "pending", Address: stale}}}
	sh.RefreshMembers()
	if err := host.s.store.AddShare(sh); err != nil {
		t.Fatal(err)
	}
	if err := legacy.s.store.AddShare(model.Share{ID: id, Path: legacy.dir, Name: "shared", PeerDeviceID: host.id, PeerAddress: hostAddress, State: "active"}); err != nil {
		t.Fatal(err)
	}

	accept := func(caller *groupTestNode) error {
		t.Helper()
		peer, _ := caller.s.store.PairedDevice(host.id)
		return caller.s.groupControl(t.Context(), peer, hostAddress, "share_accept",
			protocol.ShareAccept{Type: "share_accept", ID: invitation, ShareID: id})
	}
	check := func(wantState, wantAddress string) {
		t.Helper()
		saved, ok := host.s.store.Share(id)
		member, exists := shareMember(saved, legacy.id)
		if !ok || !exists || member.State != wantState || member.Address != wantAddress || saved.PeerAddress != wantAddress {
			t.Fatalf("expected member %s at %s, got %+v, display address %q", wantState, wantAddress, member, saved.PeerAddress)
		}
	}
	if err := accept(other); err == nil {
		t.Fatal("uninvited device changed legacy membership")
	}
	check("pending", stale)
	if err := accept(legacy); err != nil {
		t.Fatal(err)
	}
	check("active", address)
	writeTestFile(t, host.dir, "first.txt", "after acceptance")
	if err := host.s.SyncShare(t.Context(), id, true); err != nil {
		t.Fatal(err)
	}
	assertTestFile(t, legacy.dir, "first.txt", "after acceptance")
	lock := legacy.s.lockFor(id)
	lock.Lock()
	lock.Unlock()

	// A later inbound legacy sync also reports its current listening address.
	sh, _ = host.s.store.Share(id)
	sh.Members[0].Address = stale
	sh.RefreshMembers()
	if err := host.s.store.UpdateShare(sh); err != nil {
		t.Fatal(err)
	}
	if err := legacy.s.SyncShare(t.Context(), id, true); err != nil {
		t.Fatal(err)
	}
	check("active", address)
	lock = host.s.lockFor(id)
	lock.Lock()
	lock.Unlock()
	writeTestFile(t, host.dir, "later.txt", "after reconnect")
	if err := host.s.SyncShare(t.Context(), id, true); err != nil {
		t.Fatal(err)
	}
	assertTestFile(t, legacy.dir, "later.txt", "after reconnect")
}
