package service

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/store"
)

// volumeSlot always exposes the same pathname. By default directory renames
// preserve the original root for a portable detach/reattach simulation.
// Under an isolated Linux mount namespace the same scenarios switch two
// independent tmpfs volumes, including a replacement with a copied marker.
type volumeSlot struct {
	path, original, other, mounted string
	realMounts                     bool
}

func newVolumeSlot(t *testing.T) *volumeSlot {
	t.Helper()
	base := t.TempDir()
	slot := &volumeSlot{
		path: filepath.Join(base, "visible", "mount"), original: filepath.Join(base, "original"),
		other: filepath.Join(base, "other"), realMounts: os.Getenv("POLYSYNC_TEST_REAL_MOUNTS") == "1",
	}
	for _, dir := range []string{slot.path, slot.original, slot.other} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if slot.realMounts {
		if runtime.GOOS != "linux" {
			t.Fatal("real mount regression requires a private Linux mount namespace")
		}
		for _, dir := range []string{slot.original, slot.other} {
			runVolumeCommand(t, "mount", "-t", "tmpfs", "-o", "size=4m", "polysync-test", dir)
			t.Cleanup(func() { runVolumeCommand(t, "umount", dir) })
		}
		t.Cleanup(func() {
			if slot.mounted != "" {
				runVolumeCommand(t, "umount", slot.path)
			}
		})
	}
	slot.attach(t, slot.original)
	return slot
}

func runVolumeCommand(t *testing.T, cmd string, args ...string) {
	t.Helper()
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", cmd, args, err, out)
	}
}

func (v *volumeSlot) attach(t *testing.T, volume string) {
	t.Helper()
	if v.realMounts {
		runVolumeCommand(t, "mount", "--bind", volume, v.path)
	} else {
		if err := os.Remove(v.path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(volume, v.path); err != nil {
			t.Fatal(err)
		}
	}
	v.mounted = volume
}

func (v *volumeSlot) detach(t *testing.T) {
	t.Helper()
	if v.realMounts {
		// Surprise disconnection also removes mounts that have open handles.
		runVolumeCommand(t, "umount", "--lazy", v.path)
	} else {
		if err := os.Rename(v.path, v.mounted); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(v.path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	v.mounted = ""
}

func volumeRound(t *testing.T, id string, from, to *groupTestNode) error {
	t.Helper()
	err := from.s.SyncShare(t.Context(), id, true)
	verifyGroupApplied(t, to, id)
	return err
}

func volumeJournal(t *testing.T, id string, n *groupTestNode) any {
	t.Helper()
	sh, ok := n.s.store.Share(id)
	if !ok {
		t.Fatal("missing share")
	}
	if sh.LocalRole != "" {
		state, err := n.s.store.GroupState(sh.ReplicaID)
		if err != nil || len(state.Records) == 0 {
			t.Fatalf("missing group journal for %s: %v", id, err)
		}
		return state
	}
	baseline, err := n.s.store.LoadBaseline(id, sh.PeerDeviceID)
	if err != nil || len(baseline) == 0 {
		t.Fatalf("missing pair baseline for %s: %v", id, err)
	}
	return baseline
}

func TestExternalVolumeTwoPeerLifecycle(t *testing.T) {
	for _, protocol := range []string{"pair", "group"} {
		for _, initiator := range []string{"volume", "peer"} {
			t.Run(protocol+"/"+initiator, func(t *testing.T) {
				ns := groupNodes(t, 2)
				disk, peer := ns[0], ns[1]
				slot := newVolumeSlot(t)
				disk.dir = slot.path
				for _, name := range []string{"keep", "edit", "delete"} {
					writeTestFile(t, disk.dir, name, "original "+name)
				}
				role := ""
				if protocol == "group" {
					role = model.RoleCollaborator
				}
				id := groupLink(t, "", disk, peer, role, false)
				assertTestFile(t, peer.dir, "keep", "original keep")
				from, to := disk, peer
				if initiator == "peer" {
					from, to = peer, disk
				}
				before := volumeJournal(t, id, disk)
				peerBefore := volumeJournal(t, id, peer)
				checkPaused := func() {
					t.Helper()
					if err := volumeRound(t, id, from, to); err == nil {
						t.Fatal("offline or replacement disk was accepted")
					}
					if st := disk.s.Statuses()[id]; st.State != "paused" {
						t.Fatalf("disk status while unavailable: %+v", st)
					}
					assertTestFile(t, peer.dir, "keep", "original keep")
					if !reflect.DeepEqual(before, volumeJournal(t, id, disk)) {
						t.Fatal("offline folder journal advanced")
					}
				}

				slot.detach(t)
				// A detached volume can expose an empty mountpoint or no path.
				checkPaused()
				entries, err := os.ReadDir(slot.path)
				if err != nil || len(entries) != 0 {
					t.Fatalf("wrote to empty mountpoint: %v, %v", entries, err)
				}
				if err := os.Remove(slot.path); err != nil {
					t.Fatal(err)
				}
				checkPaused()
				if _, err := os.Stat(slot.path); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("missing mountpoint recreated: %v", err)
				}
				if !reflect.DeepEqual(peerBefore, volumeJournal(t, id, peer)) {
					t.Fatal("unchanged peer journal advanced while disk absent")
				}
				if err := os.Mkdir(slot.path, 0700); err != nil {
					t.Fatal(err)
				}
				// Real replacement media can even carry a backup of the marker.
				if slot.realMounts {
					if err := os.CopyFS(slot.other, os.DirFS(slot.original)); err != nil {
						t.Fatal(err)
					}
				}
				slot.attach(t, slot.other)
				writeTestFile(t, disk.dir, "keep", "other disk")
				writeTestFile(t, disk.dir, "private", "never export")
				checkPaused()
				writeTestFile(t, peer.dir, "edit", "changed while disconnected")
				writeTestFile(t, peer.dir, "new", "created while disconnected")
				if err := os.Remove(filepath.Join(peer.dir, "delete")); err != nil {
					t.Fatal(err)
				}
				checkPaused()
				assertTestFile(t, disk.dir, "keep", "other disk")
				assertTestFile(t, disk.dir, "private", "never export")
				for _, missing := range []string{filepath.Join(peer.dir, "private"), filepath.Join(disk.dir, "new")} {
					if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("unexpected transfer at %s: %v", missing, err)
					}
				}
				slot.detach(t)
				slot.attach(t, slot.original)
				assertTestFile(t, disk.dir, "edit", "original edit")
				assertTestFile(t, disk.dir, "delete", "original delete")
				if err := volumeRound(t, id, from, to); err != nil {
					t.Fatalf("original disk did not resume: %v", err)
				}
				for _, n := range ns {
					assertTestFile(t, n.dir, "keep", "original keep")
					assertTestFile(t, n.dir, "edit", "changed while disconnected")
					assertTestFile(t, n.dir, "new", "created while disconnected")
					if _, err := os.Stat(filepath.Join(n.dir, "delete")); !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("legitimate peer-side deletion lost: %v", err)
					}
				}
				// Emptying an attached folder is still an intentional deletion.
				for _, name := range []string{"keep", "edit", "new"} {
					if err := os.Remove(filepath.Join(disk.dir, name)); err != nil {
						t.Fatal(err)
					}
				}
				if err := volumeRound(t, id, from, to); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"keep", "edit", "new"} {
					if _, err := os.Stat(filepath.Join(peer.dir, name)); !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("clearing the original folder did not propagate %s: %v", name, err)
					}
				}
			})
		}
	}
}

// A second disk can contain exactly the bytes a stale plan expects to delete.
// Ordinary content revalidation alone would accept this operation.
func TestExternalVolumeChangesAfterPlanning(t *testing.T) {
	for _, initiator := range []string{"volume", "peer"} {
		t.Run(initiator, func(t *testing.T) {
			ns := groupNodes(t, 2)
			disk, peer := ns[0], ns[1]
			slot := newVolumeSlot(t)
			disk.dir = slot.path
			writeTestFile(t, disk.dir, "note", "same bytes")
			id := groupLink(t, "", disk, peer, "", false)
			assertTestFile(t, peer.dir, "note", "same bytes")
			if err := os.Remove(filepath.Join(peer.dir, "note")); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, slot.other, "note", "same bytes")
			from, to := disk, peer
			if initiator == "peer" {
				from, to = peer, disk
			}
			swap, original := preparePlanVolumeSwap(t, slot)
			before := volumeJournal(t, id, disk)
			planned, proceed := make(chan struct{}), make(chan struct{})
			serverLock := to.s.lockFor(id)
			serverLock.Lock()
			makePlan := to.s.planSync
			to.s.planSync = func(a, b, base []model.Entry, name string, now time.Time) model.Plan {
				plan := makePlan(a, b, base, name, now)
				close(planned)
				<-proceed
				return plan
			}
			serverLock.Unlock()
			done := make(chan error, 1)
			go func() { done <- from.s.SyncShare(t.Context(), id, true) }()
			func() {
				defer close(proceed)
				select {
				case <-planned:
				case err := <-done:
					t.Fatalf("failed before planning: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for plan")
				}
				swap()
			}()
			err := <-done
			serverLock.Lock()
			to.s.planSync = makePlan
			serverLock.Unlock()
			if err == nil {
				t.Error("accepted a plan after the disk changed")
			}
			assertTestFile(t, slot.path, "note", "same bytes")
			assertTestFile(t, original, "note", "same bytes")
			if !reflect.DeepEqual(before, volumeJournal(t, id, disk)) {
				t.Fatal("advanced baseline after interrupted operation")
			}
		})
	}
}

// Windows cannot rename an open directory. An intermediate junction switches
// the mount path without renaming the original directory or its pinned handle.
func preparePlanVolumeSwap(t *testing.T, slot *volumeSlot) (func(), string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return func() {
			slot.detach(t)
			slot.attach(t, slot.other)
		}, slot.original
	}

	view := filepath.Dir(slot.path)
	original := view + "-original"
	other := view + "-other"
	if err := os.Rename(view, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(slot.other, filepath.Join(other, filepath.Base(slot.path))); err != nil {
		t.Fatal(err)
	}
	runVolumeCommand(t, "cmd.exe", "/d", "/c", "mklink", "/j", view, original)
	return func() {
		if err := os.Remove(view); err != nil {
			t.Fatal(err)
		}
		runVolumeCommand(t, "cmd.exe", "/d", "/c", "mklink", "/j", view, other)
	}, filepath.Join(original, filepath.Base(slot.path))
}

func TestExternalVolumeSchedulerResumesOnlyTheOriginalFolder(t *testing.T) {
	for _, protocol := range []string{"pair", "group"} {
		t.Run(protocol, func(t *testing.T) {
			ns := groupNodes(t, 2)
			disk, peer := ns[0], ns[1]
			slot := newVolumeSlot(t)
			disk.dir = slot.path
			role := ""
			if protocol == "group" {
				role = model.RoleCollaborator
			}
			writeTestFile(t, disk.dir, "stay", "on disk")
			id := groupLink(t, "", disk, peer, role, false)
			// A long normal interval ensures restoration triggers the next
			// round promptly instead of merely waiting for the interval.
			share, _ := disk.s.store.Share(id)
			share.AutoSync, share.IntervalSeconds = true, 86400
			if err := disk.s.store.UpdateShare(share); err != nil {
				t.Fatal(err)
			}
			healthy := &groupTestNode{s: disk.s, id: disk.id, dir: t.TempDir()}
			healthyPeer := &groupTestNode{s: peer.s, id: peer.id, dir: t.TempDir()}
			writeTestFile(t, healthy.dir, "independent", "before")
			otherID := groupLink(t, "", healthy, healthyPeer, role, false)
			slot.detach(t)
			disk.s.checkSyncFolders(t.Context())
			waitForVolumeState(t, disk, id, "paused")
			// The same scheduler tick independently probes the healthy share.
			// Wait for that probe to release its lock before a manual request.
			verifyGroupApplied(t, healthy, otherID)
			assertTestFile(t, peer.dir, "stay", "on disk")
			writeTestFile(t, healthy.dir, "independent", "after")
			if err := volumeRound(t, otherID, healthy, healthyPeer); err != nil {
				t.Fatal(err)
			}
			assertTestFile(t, healthyPeer.dir, "independent", "after")

			slot.attach(t, slot.other)
			writeTestFile(t, slot.path, "different", "private disk")
			// Reopening the persisted configuration while a different disk is
			// present must never adopt that disk as the original.
			dir := disk.s.store.Dir()
			disk.s.Stop()
			st, err := store.Open(dir, "127.0.0.1:0", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			disk.s = NewWithOptions(st, Options{DisableDiscovery: true})
			if err := disk.s.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			disk.s.checkSyncFolders(t.Context())
			waitForVolumeState(t, disk, id, "paused")
			if _, err := os.Stat(filepath.Join(peer.dir, "different")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("replacement was adopted after restart: %v", err)
			}
			writeTestFile(t, peer.dir, "new", "from peer while disk offline")
			slot.detach(t)
			slot.attach(t, slot.original)
			// Only the normal timer is running after reconnection.
			waitForVolumeState(t, disk, id, "synced")
			assertTestFile(t, disk.dir, "new", "from peer while disk offline")
			assertTestFile(t, disk.dir, "stay", "on disk")
			assertTestFile(t, slot.other, "different", "private disk")
		})
	}
}

func TestExternalVolumeManualFolderRetriesOnlyAnInterruptedRequest(t *testing.T) {
	nodes := groupNodes(t, 2)
	disk, peer := nodes[0], nodes[1]
	slot := newVolumeSlot(t)
	disk.dir = slot.path
	writeTestFile(t, disk.dir, "note", "before")
	id := groupLink(t, "", disk, peer, model.RoleCollaborator, false)
	writeTestFile(t, peer.dir, "note", "saved on peer")
	// A manual folder with no pending work simply becomes available again.
	slot.detach(t)
	disk.s.checkSyncFolders(t.Context())
	waitForVolumeState(t, disk, id, "paused")
	slot.attach(t, slot.original)
	disk.s.checkSyncFolders(t.Context())
	waitForVolumeState(t, disk, id, "idle")
	assertTestFile(t, disk.dir, "note", "before")

	// A user who already requested a sync need not press the button again.
	slot.detach(t)
	response := memberRequest(t, disk, "POST", "/api/shares/"+id+"/sync", nil, 4)
	if response["error"] == nil {
		t.Fatal("missing pause reason for interrupted manual sync")
	}
	waitForVolumeState(t, disk, id, "paused")
	slot.attach(t, slot.original)
	disk.s.checkSyncFolders(t.Context())
	waitForVolumeState(t, disk, id, "synced")
	assertTestFile(t, disk.dir, "note", "saved on peer")
}

func waitForVolumeState(t *testing.T, n *groupTestNode, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status := n.s.Statuses()[id]
		if status.State == want {
			verifyGroupApplied(t, n, id)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %s for %s, got %+v", want, id, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
