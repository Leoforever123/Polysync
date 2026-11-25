package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"polysync/internal/model"
	"polysync/internal/rootidentity"
	"polysync/internal/store"
	"polysync/internal/syncer"
)

// Model an upgrade from a version that never wrote a folder marker or rootId.
func reopenWithoutRootID(t *testing.T, n *groupTestNode, id string) {
	t.Helper()
	n.s.Stop()
	cfg := n.s.store.Config()
	for i := range cfg.Shares {
		if cfg.Shares[i].ID == id {
			cfg.Shares[i].RootID = ""
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(n.s.store.Dir(), "config.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(n.s.store.Dir(), "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	n.restart(t, st)
}

func TestExternalVolumeUpgradeUsesPriorContents(t *testing.T) {
	for _, role := range []string{"", model.RoleCollaborator} {
		t.Run(map[string]string{"": "pair", model.RoleCollaborator: "group"}[role], func(t *testing.T) {
			n := groupNodes(t, 2)
			disk, peer := n[0], n[1]
			slot := newVolumeSlot(t)
			disk.dir = slot.path
			writeTestFile(t, disk.dir, "a", "last synced A")
			writeTestFile(t, disk.dir, "b", "last synced B")
			id := groupLink(t, "", disk, peer, role, false)
			entries, err := os.ReadDir(disk.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if rootidentity.Reserved(e.Name()) {
					if err := os.Remove(filepath.Join(disk.dir, e.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
			slot.detach(t)
			reopenWithoutRootID(t, disk, id)
			pairTestNodes(t, n)
			if role == "" {
				if err := peer.s.store.SetPeerAddress(id, disk.id, "127.0.0.1:"+intText(disk.s.ListenPort())); err != nil {
					t.Fatal(err)
				}
			}
			checkPaused := func() {
				t.Helper()
				if err := volumeRound(t, id, peer, disk); !errors.Is(err, errRemoteRootPaused) {
					t.Fatalf("peer: %v", err)
				}
				if got := peer.s.Statuses()[id]; got.State != "waiting" || !strings.Contains(got.Message, "对端") {
					t.Fatalf("peer status: %+v", got)
				}
				if got := disk.s.Statuses()[id]; got.State != "paused" || got.LastSync.IsZero() || !strings.Contains(got.Message, "原盘") {
					t.Fatalf("local status: %+v", got)
				}
				if current, _ := disk.s.store.Share(id); current.RootID != "" {
					t.Fatal("enrolled an unverified directory")
				}
				files, err := os.ReadDir(slot.path)
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range files {
					if rootidentity.Reserved(file.Name()) {
						t.Fatal("marked an unverified directory")
					}
				}
				assertTestFile(t, peer.dir, "a", "last synced A")
				assertTestFile(t, peer.dir, "b", "last synced B")
			}
			checkPaused()
			slot.attach(t, slot.other)
			writeTestFile(t, disk.dir, "a", "last synced A")
			writeTestFile(t, disk.dir, "b", "different B")
			checkPaused()
			slot.detach(t)
			slot.attach(t, slot.original)
			writeTestFile(t, disk.dir, "new", "added since last sync")
			if err := volumeRound(t, id, disk, peer); err != nil {
				t.Fatal(err)
			}
			assertTestFile(t, peer.dir, "new", "added since last sync")
			sh, _ := disk.s.store.Share(id)
			if err := rootidentity.Check(sh.Path, sh.RootID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExternalVolumeUpgradeNeedsLivingEvidence(t *testing.T) {
	n := groupNodes(t, 2)
	disk, peer := n[0], n[1]
	writeTestFile(t, disk.dir, "gone", "old")
	id := groupLink(t, "", disk, peer, model.RoleCollaborator, false)
	stale, err := syncer.Scan(t.Context(), disk.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(disk.dir, "gone")); err != nil {
		t.Fatal(err)
	}
	if err := volumeRound(t, id, disk, peer); err != nil {
		t.Fatal(err)
	}
	if err := disk.s.store.SaveBaseline(id, peer.id, stale); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, disk.dir, "gone", "old")
	reopenWithoutRootID(t, disk, id)
	sh, _ := disk.s.store.Share(id)
	if err := disk.s.checkLocalRoot(t.Context(), sh, true); !errors.Is(err, errUnverifiedRoot) {
		t.Fatalf("adopted obsolete baseline: %v", err)
	}
}

func TestExternalVolumeSettingsPreserveBinding(t *testing.T) {
	n := groupNodes(t, 2)
	slot := newVolumeSlot(t)
	disk := n[0]
	disk.dir = slot.path
	id := groupLink(t, "", disk, n[1], "", false)
	before, _ := disk.s.store.Share(id)
	slot.detach(t)
	if err := os.Remove(slot.path); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.s.preparePath(slot.path, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(slot.path); !os.IsNotExist(err) {
		t.Fatalf("settings recreated unplugged path: %v", err)
	}
	if _, err := disk.s.preparePath(slot.other, id); err == nil {
		t.Fatal("settings accepted another folder")
	}
	edited := before
	edited.RootID = "other"
	edited.AutoSync = !before.AutoSync
	if err := disk.s.store.UpdateShare(edited); err != nil {
		t.Fatal(err)
	}
	after, _ := disk.s.store.Share(id)
	if before.RootID == "" || before.RootID != after.RootID || before.AutoSync == after.AutoSync {
		t.Fatalf("failed to preserve binding while editing settings")
	}
}
