package service

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"polysync/internal/model"
)

func TestGroupOnlyPersistsReceiptsForWitnessedConflicts(t *testing.T) {
	ns := groupNodes(t, 2)
	a, b := ns[0], ns[1]
	writeTestFile(t, a.dir, "note", "real content")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	foreign := strings.Repeat("a", 32)
	unknown := strings.Repeat("b", 32)
	for _, n := range ns {
		sh, _ := n.s.store.Share(id)
		state, err := n.s.store.GroupState(sh.ReplicaID)
		if err != nil {
			t.Fatal(err)
		}
		state.Resolved = []string{foreign, unknown}
		if err = n.s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
			t.Fatal(err)
		}
	}
	// A v0.5.0-style sender knows only the ID. B has a record with that
	// ID in another folder, and no file fork to substantiate it here.
	if err := a.s.store.SaveConflict(model.Conflict{ID: foreign, ShareID: id, Status: "resolved", Path: "note"}); err != nil {
		t.Fatal(err)
	}
	if err := b.s.store.SaveConflict(model.Conflict{ID: foreign, ShareID: "other-group", Status: "pending", Path: "note"}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][]*groupTestNode{{a, b}, {b, a}, {a, b}} {
		groupSync(t, id, pair[0], pair[1])
	}
	sh, _ := b.s.store.Share(id)
	state, err := b.s.checkedGroupState(sh)
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.s.groupResolvedIDs(sh, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Resolved) != 0 || len(out) != 0 {
		t.Fatalf("unwitnessed receipts persisted or forwarded: %v %v", state.Resolved, out)
	}
	c, _, err := b.s.store.Conflict(foreign)
	if err != nil || c.Status != "pending" {
		t.Fatalf("unrelated conflict changed: %#v %v", c, err)
	}
}

func TestGroupConfirmedGenerationCannotAcquireInventedForks(t *testing.T) {
	id := "group-1"
	name := "notes"
	a := model.GroupRevision{Entry: model.Entry{Path: name, Hash: strings.Repeat("f", 64)}, Clock: map[string]uint64{"a": 4}}
	b := model.GroupRevision{Entry: model.Entry{Path: name}, Clock: map[string]uint64{"b": 3}}
	local, conflicts, err := mergeGroup(id, model.Revisions{name: a}, model.Revisions{name: b})
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("setup fork: %v %#v", err, conflicts)
	}
	real := conflictID(id, conflicts[0].GroupEvent)
	for _, n := range []uint64{2, 3} {
		for _, advance := range []bool{false, true} {
			remote := local[name]
			remote.Clock = maps.Clone(remote.Clock)
			remote.Forks = make(map[string]model.GroupFork)
			if advance {
				remote.Clock["a"]++
			}
			f := model.GroupFork{{Hash: strings.Repeat("1", 64), Clock: map[string]uint64{"b": n}}, {Hash: a.Hash, Clock: a.Clock}}
			event, err := forkID(name, f)
			if err != nil {
				t.Fatal(err)
			}
			fake := conflictID(id, event)
			remote.Forks[fake] = f
			if err := validateGroupOffer(id, model.Revisions{name: remote}, false); err != nil {
				t.Fatalf("test requires a syntactically valid alternative: %v", err)
			}
			result, _, err := mergeGroup(id, local, model.Revisions{name: remote})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range []map[string]model.GroupFork{result[name].Forks, followerForks(local[name], remote)} {
				if _, ok := w[fake]; ok {
					t.Fatal("a sealed history accepted a different branch or ancestor")
				}
				if _, ok := w[real]; !ok {
					t.Fatal("lost the verified event")
				}
			}
			if err := validateGroupOffer("another-group", model.Revisions{name: remote}, false); err == nil {
				t.Fatal("fork evidence replayed in a different group")
			}
		}
	}
}

func TestGroupCorruptLocalRevisionFailsWithoutTouchingFiles(t *testing.T) {
	ns := groupNodes(t, 2)
	a, b := ns[0], ns[1]
	writeTestFile(t, a.dir, "note", "base")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	sh, _ := a.s.store.Share(id)
	prior, err := a.s.checkedGroupState(sh)
	if err != nil {
		t.Fatal(err)
	}
	bad := prior.Records["note"]
	bad.Hash = "f"
	for _, pending := range []bool{false, true} {
		state := prior
		state.Records = maps.Clone(prior.Records)
		if pending {
			state.Pending = model.Revisions{"note": bad}
		} else {
			state.Records["note"] = bad
		}
		if err := a.s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][]*groupTestNode{{a, b}, {b, a}} {
			err := groupRound(t, id, pair[0], pair[1])
			if err == nil || !strings.Contains(err.Error(), "本机文件版本记录") {
				t.Fatalf("corrupt journal accepted or wrong error: %v", err)
			}
		}
		for _, n := range ns {
			assertTestFile(t, n.dir, "note", "base")
		}
	}
	if _, err := groupConflictName("note", "f"); err == nil {
		t.Fatal("short conflict digest accepted")
	}
	if err := a.s.store.SaveGroupState(sh.ReplicaID, prior); err != nil {
		t.Fatal(err)
	}
	groupSync(t, id, b, a)
}

func TestGroupFollowerCanSwitchToUpstreamWithEarlierHistory(t *testing.T) {
	ns := groupNodes(t, 4)
	a, b, c, d := ns[0], ns[1], ns[2], ns[3]
	writeTestFile(t, a.dir, "note", "base")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	groupLink(t, id, a, c, model.RoleFollower, false)
	groupLink(t, id, a, d, model.RoleCollaborator, false)
	writeTestFile(t, a.dir, "note", "edited on A")
	if err := os.Remove(filepath.Join(b.dir, "note")); err != nil {
		t.Fatal(err)
	}
	groupSync(t, id, a, b)
	groupSync(t, id, a, c)
	cs, _ := c.s.store.Share(id)
	old, err := c.s.checkedGroupState(cs)
	if err != nil || len(old.Records["note"].Forks) != 1 {
		t.Fatalf("missing prior witness: %v %#v", err, old)
	}
	memberRequest(t, c, "DELETE", "/api/shares/"+id+"/members/"+a.id, nil, 2)
	groupLink(t, id, d, c, model.RoleFollower, false)
	groupSync(t, id, c, d)
	groupSync(t, id, d, c)
	state, err := c.s.checkedGroupState(cs)
	if err != nil || len(state.Records["note"].Forks) != 0 {
		t.Fatalf("new upstream inherited unsupported witness: %v %#v", err, state)
	}
	if !slices.Equal(state.Resolved, []string{}) {
		t.Fatalf("unsubstantiated resolutions survived: %v", state.Resolved)
	}
	assertTestFile(t, c.dir, "note", "base")
}
