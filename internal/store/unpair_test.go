package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"polysync/internal/model"
)

func TestUnpairIsAtomicAndLeavesOtherPeersUntouched(t *testing.T) {
	s, err := Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"remove", "keep"} {
		if err := s.SavePairedDevice(model.PairedDevice{ID: id}); err != nil {
			t.Fatal(err)
		}
		if err := s.AddShare(model.Share{ID: id, PeerDeviceID: id, State: "active", AutoSync: true}); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveShareInvitation(model.ShareInvitation{ID: id, DeviceID: id}); err != nil {
			t.Fatal(err)
		}
	}
	before := s.Config()
	// A directory in place of the staging file deterministically fails persistence.
	if err := os.Mkdir(s.configPath+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePairedDevice("remove"); err == nil {
		t.Fatal("expected persistence error")
	}
	if !reflect.DeepEqual(before, s.Config()) {
		t.Fatal("failed write changed live configuration")
	}
	if err := os.Remove(s.configPath + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePairedDevice("remove"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.PairedDevice("keep"); !ok {
		t.Fatal("unrelated trust removed")
	}
	kept, _ := s.Share("keep")
	if kept.State != "active" || !kept.AutoSync {
		t.Fatal("unrelated sync disabled")
	}
	if got := s.Config().ShareInvitations; len(got) != 1 || got[0].ID != "keep" {
		t.Fatalf("invitations: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "config.json")); err != nil {
		t.Fatal(err)
	}
}
