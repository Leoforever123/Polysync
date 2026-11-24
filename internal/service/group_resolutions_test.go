package service

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
)

func folderConflict(t *testing.T, n *groupTestNode, id, status string) model.Conflict {
	t.Helper()
	all, err := n.s.store.Conflicts()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.ShareID == id && c.Status == status {
			return c
		}
	}
	t.Fatalf("no %s conflict in %s: %#v", status, id, all)
	return model.Conflict{}
}

func verifyGroupApplied(t *testing.T, n *groupTestNode, id string) {
	t.Helper()
	lock := n.s.lockFor(id)
	lock.Lock()
	lock.Unlock()
}

func TestGroupModifyDeleteDecisionsRelayAcrossCollaborators(t *testing.T) {
	for _, keep := range []bool{true, false} {
		name := "delete"
		if keep {
			name = "keep"
		}
		t.Run(name, func(t *testing.T) {
			ns := groupNodes(t, 3)
			a, b, c := ns[0], ns[1], ns[2]
			writeTestFile(t, a.dir, "note", "common base")
			id := groupLink(t, "", a, b, model.RoleCollaborator, false)
			groupLink(t, id, a, c, model.RoleCollaborator, false)
			groupLink(t, id, b, c, model.RoleCollaborator, false)
			writeTestFile(t, a.dir, "note", "new content")
			if err := os.Remove(filepath.Join(b.dir, "note")); err != nil {
				t.Fatal(err)
			}
			groupSync(t, id, a, b)
			before := folderConflict(t, b, id, "pending")
			if before.Kind != "modify-delete" || before.ConflictCopyPath != "" {
				t.Fatalf("wrong conflict: %#v", before)
			}
			ca := folderConflict(t, a, id, "pending")
			if ca.ID != before.ID {
				t.Fatal("sides disagree on the conflict identity")
			}
			content, err := a.s.ConflictContent(ca.ID)
			if err != nil || content.Base != "common base" {
				t.Fatalf("verified common ancestor missing: %v %q", err, content.Base)
			}
			memberRequest(t, a, "DELETE", "/api/shares/"+id+"/members/"+b.id, nil, 2)
			choice := "delete"
			if keep {
				choice = "local"
			}
			if err := a.s.ResolveConflict(t.Context(), ca.ID, choice, ""); err != nil {
				t.Fatal(err)
			}
			if keep {
				groupSync(t, id, c, a)
			} else {
				groupSync(t, id, a, c)
			}
			verifyGroupApplied(t, c, id)
			// C never detected the fork. Its persisted ledger is the only way
			// for B to learn that A already settled a modify/delete conflict.
			sh, _ := c.s.store.Share(id)
			ledger, err := c.s.store.GroupState(sh.ReplicaID)
			if err != nil || !slices.Contains(ledger.Resolved, ca.ID) {
				t.Fatalf("relay lost decision: %#v %v", ledger.Resolved, err)
			}
			if keep {
				groupSync(t, id, c, b)
			} else {
				groupSync(t, id, b, c)
			}
			verifyGroupApplied(t, b, id)
			cb := folderConflict(t, b, id, "resolved")
			if cb.ID != ca.ID {
				t.Fatal("closed wrong event")
			}
			if keep {
				assertTestFile(t, b.dir, "note", "new content")
			} else if _, err := os.Stat(filepath.Join(b.dir, "note")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("deletion did not arrive: %v", err)
			}
			writeTestFile(t, b.dir, "note", "later edit")
			if err := b.s.ResolveConflict(t.Context(), cb.ID, "delete", ""); err == nil {
				t.Fatal("stale resolution applied")
			}
			assertTestFile(t, b.dir, "note", "later edit")
		})
	}
}

type interruptGroupFrame struct {
	net.Conn
	stage string
}

func (c interruptGroupFrame) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"type":"`+c.stage+`"`)) {
		c.Close()
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(p)
}

func TestGroupResolutionRequiresCompletionAndIsFolderScoped(t *testing.T) {
	ns := groupNodes(t, 2)
	a, b := ns[0], ns[1]
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	as, _ := a.s.store.Share(id)
	bs, _ := b.s.store.Share(id)
	am, _ := shareMember(as, b.id)
	bm, _ := shareMember(bs, a.id)
	ap, _ := a.s.store.PairedDevice(b.id)
	bp, _ := b.s.store.PairedDevice(a.id)
	event := strings.Repeat("a", 32)
	other := strings.Repeat("b", 32)
	for _, n := range ns {
		status := "pending"
		if n == a {
			status = "resolved"
		}
		for _, c := range []model.Conflict{{ID: event, ShareID: id, Path: "note", Kind: "modify-delete", Status: status}, {ID: other, ShareID: "another-folder", Path: "note", Kind: "modify-delete", Status: status}} {
			if err := n.s.store.SaveConflict(c); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, stage := range []string{"group_applied", "group_done"} {
		client, server := net.Pipe()
		client.SetDeadline(time.Now().Add(4 * time.Second))
		server.SetDeadline(time.Now().Add(4 * time.Second))
		end := make(chan error, 1)
		go func() {
			defer server.Close()
			end <- b.s.groupSession(t.Context(), protocol.NewFramer(interruptGroupFrame{server, stage}), bs, bm, bp, false)
		}()
		err := a.s.groupSession(t.Context(), protocol.NewFramer(interruptGroupFrame{client, stage}), as, am, ap, true)
		client.Close()
		if err == nil || <-end == nil {
			t.Fatal("interrupted commit counted as complete")
		}
		c, _, err := b.s.store.Conflict(event)
		if err != nil || c.Status != "pending" {
			t.Fatalf("premature closure: %s %v", c.Status, err)
		}
	}
	groupSync(t, id, b, a)
	verifyGroupApplied(t, b, id)
	c, _, _ := b.s.store.Conflict(event)
	foreign, _, _ := b.s.store.Conflict(other)
	if c.Status != "resolved" || foreign.Status != "pending" {
		t.Fatalf("wrong folder scope: %s %s", c.Status, foreign.Status)
	}
	if err := validateGroupDecisions([]string{event}, true); err == nil {
		t.Fatal("read-only peer can publish decisions")
	}
	if err := b.s.markConflictsResolved(id, []string{other}); err != nil {
		t.Fatal(err)
	}
	foreign, _, _ = b.s.store.Conflict(other)
	if foreign.Status != "pending" {
		t.Fatal("legacy endpoint acknowledged a foreign folder")
	}
}

func TestGroupRepeatConflictGetsNewEventAndConflictCopy(t *testing.T) {
	ns := groupNodes(t, 2)
	a, b := ns[0], ns[1]
	writeTestFile(t, a.dir, "notes.txt", "base")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	var prior model.Conflict
	for range 2 {
		writeTestFile(t, a.dir, "notes.txt", "A version")
		writeTestFile(t, b.dir, "notes.txt", "B version")
		groupSync(t, id, b, a)
		verifyGroupApplied(t, a, id)
		current := folderConflict(t, a, id, "pending")
		base := "base"
		if prior.ID != "" {
			base = "new common state"
		}
		content, err := a.s.ConflictContent(current.ID)
		if err != nil || content.Base != base {
			t.Fatalf("ordinary edit conflict lost verified common ancestor: %v %q", err, content.Base)
		}

		if current.ID == prior.ID || current.ConflictCopyPath == "" {
			t.Fatal("old receipt suppressed a fresh fork")
		}
		if current.ConflictCopyPath == prior.ConflictCopyPath {
			t.Fatal("old tombstone suppressed a fresh conflict copy")
		}
		if _, err := os.Stat(filepath.Join(a.dir, current.ConflictCopyPath)); err != nil {
			t.Fatal(err)
		}
		if prior.ID != "" {
			return
		}
		prior = current
		if err := a.s.ResolveConflict(t.Context(), current.ID, "merged", "new common state"); err != nil {
			t.Fatal(err)
		}
		groupSync(t, id, b, a)
		assertTestFile(t, b.dir, "notes.txt", "new common state")
		if _, err := os.Stat(filepath.Join(b.dir, current.ConflictCopyPath)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("previous copy still present: %v", err)
		}
	}
}

func TestGroupObserveRepairsSizeWithoutInventingChangeOrAncestor(t *testing.T) {
	hash := strings.Repeat("c", 64)
	clock := map[string]uint64{"origin": 1}
	for _, writable := range []bool{true, false} {
		m := model.GroupState{Records: model.Revisions{"note": {Entry: model.Entry{Path: "note", Hash: hash, Size: 2}, Clock: maps.Clone(clock)}}}
		err := observeGroup(&m, map[string]model.Entry{"note": {Path: "note", Hash: hash, Size: 12}}, "other", writable)
		if err != nil || m.Records["note"].Size != 12 || !maps.Equal(m.Records["note"].Clock, clock) {
			t.Fatalf("bad metadata repair: %#v %v", m, err)
		}
	}
	parent := strings.Repeat("d", 64)
	a := model.GroupRevision{Entry: model.Entry{Path: "n", Hash: hash}, Clock: map[string]uint64{"a": 2}, Parent: &model.GroupAncestor{Hash: parent, Clock: map[string]uint64{"a": 1}}}
	b := model.GroupRevision{Entry: model.Entry{Path: "n", Hash: strings.Repeat("e", 64)}, Clock: map[string]uint64{"b": 2}, Parent: &model.GroupAncestor{Hash: parent, Clock: map[string]uint64{"b": 1}}}
	_, conflicts, err := mergeGroup("group", model.Revisions{"n": a}, model.Revisions{"n": b})
	if err != nil || len(conflicts) != 1 || conflicts[0].BaseHash != "" {
		t.Fatalf("equal bytes were mistaken for a common ancestor: %v %v", conflicts, err)
	}
}

func TestGroupReadOnlyFollowerCannotPublishResolvedConflict(t *testing.T) {
	ns := groupNodes(t, 2)
	a, b := ns[0], ns[1]
	id := groupLink(t, "", a, b, model.RoleFollower, false)
	e := model.Conflict{ID: strings.Repeat("e", 32), ShareID: id, Path: "note", Kind: "modify-delete", Status: "pending"}
	for _, n := range ns {
		if err := n.s.store.SaveConflict(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.s.ResolveConflict(t.Context(), e.ID, "delete", ""); err == nil {
		t.Fatal("follower could resolve an upstream conflict")
	}
	e.Status = "resolved"
	if err := b.s.store.SaveConflict(e); err != nil {
		t.Fatal(err)
	}
	for _, nodes := range [][]*groupTestNode{{a, b}, {b, a}} {
		groupSync(t, id, nodes[0], nodes[1])
		verifyGroupApplied(t, a, id)
		up, _, err := a.s.store.Conflict(e.ID)
		if err != nil || up.Status != "pending" {
			t.Fatalf("upstream accepted read-only resolution: %v %v", up, err)
		}
	}
}

func TestGroupStaleDialogPreservesNewerWorkingFile(t *testing.T) {
	ns := groupNodes(t, 2)
	a, b := ns[0], ns[1]
	writeTestFile(t, a.dir, "note", "base")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	writeTestFile(t, a.dir, "note", "branch")
	if err := os.Remove(filepath.Join(b.dir, "note")); err != nil {
		t.Fatal(err)
	}
	groupSync(t, id, a, b)
	verifyGroupApplied(t, b, id)
	event := folderConflict(t, b, id, "pending")
	writeTestFile(t, b.dir, "note", "edited while dialog was open")
	for _, choice := range []string{"delete", "remote", "merged"} {
		if err := b.s.ResolveConflict(t.Context(), event.ID, choice, "old merged text"); err == nil {
			t.Fatalf("stale dialog applied %s", choice)
		}
		assertTestFile(t, b.dir, "note", "edited while dialog was open")
	}
}

func TestGroupStaleSizeOnBothEndsRecoversThroughNormalSync(t *testing.T) {
	ns := groupNodes(t, 3)
	a, b := ns[0], ns[1]
	writeTestFile(t, a.dir, "note", "twelve bytes")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	var clock map[string]uint64
	for _, n := range ns[:2] {
		sh, _ := n.s.store.Share(id)
		state, err := n.s.store.GroupState(sh.ReplicaID)
		if err != nil {
			t.Fatal(err)
		}
		rev := state.Records["note"]
		clock = maps.Clone(rev.Clock)
		rev.Size = 1
		state.Records["note"] = rev
		if err = n.s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
			t.Fatal(err)
		}
	}
	groupSync(t, id, b, a)
	verifyGroupApplied(t, a, id)
	for _, n := range ns[:2] {
		sh, _ := n.s.store.Share(id)
		state, err := n.s.store.GroupState(sh.ReplicaID)
		if err != nil {
			t.Fatal(err)
		}
		if r := state.Records["note"]; r.Size != int64(len("twelve bytes")) || !maps.Equal(r.Clock, clock) {
			t.Fatalf("stale size was not repaired: %#v", r)
		}
		assertTestFile(t, n.dir, "note", "twelve bytes")
	}
	// A new replica must download using the repaired byte count without any
	// cached object for this content.
	groupLink(t, id, a, ns[2], model.RoleFollower, false)
	assertTestFile(t, ns[2].dir, "note", "twelve bytes")

}
