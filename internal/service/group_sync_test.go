package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"polysync/internal/model"
	"polysync/internal/store"
)

type groupTestNode struct {
	s       *Service
	dir, id string
}

func (n *groupTestNode) restart(t *testing.T, st *store.Store) {
	t.Helper()
	n.s = NewWithOptions(st, Options{DisableAutoSync: true, DisableDiscovery: true})
	if err := n.s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func groupNodes(t *testing.T, n int) []*groupTestNode {
	t.Helper()
	nodes := make([]*groupTestNode, n)
	for i := range nodes {
		st, err := store.Open(t.TempDir(), "127.0.0.1:0", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		if err = st.SetDeviceName(string(rune('A' + i))); err != nil {
			t.Fatal(err)
		}
		node := &groupTestNode{dir: t.TempDir(), id: st.Config().DeviceID}
		nodes[i] = node
		node.restart(t, st)
		t.Cleanup(func() { node.s.Stop() })
	}
	pairTestNodes(t, nodes)
	return nodes
}

func pairTestNodes(t *testing.T, nodes []*groupTestNode) {
	t.Helper()
	for _, local := range nodes {
		for _, remote := range nodes {
			if local == remote {
				continue
			}
			cfg := remote.s.store.Config()
			err := local.s.store.SavePairedDevice(model.PairedDevice{ID: remote.id, Name: cfg.DeviceName, PublicKey: cfg.IdentityPublicKey, Addresses: []string{"127.0.0.1:" + intText(remote.s.ListenPort())}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func groupJSON(t *testing.T, n *groupTestNode, endpoint string, input map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", endpoint, bytes.NewReader(body))
	w := httptest.NewRecorder()
	n.s.Handler().ServeHTTP(w, r)
	if w.Code/100 != 2 {
		t.Fatalf("%s returned %d: %s", endpoint, w.Code, w.Body.String())
	}
	var result map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func groupLink(t *testing.T, group string, from, to *groupTestNode, role string, write bool) string {
	t.Helper()
	params := map[string]any{"deviceId": to.id, "role": role, "allowWriteBack": write}
	if group == "" {
		params["path"] = from.dir
		params["name"] = "Projects"
		res := groupJSON(t, from, "/api/shares/invite", params)
		group = res["id"].(string)
	} else {
		groupJSON(t, from, "/api/shares/"+group+"/members", params)
	}
	for _, inv := range to.s.ShareInvitations() {
		if inv.ShareID != group || inv.DeviceID != from.id {
			continue
		}
		params = map[string]any{"path": to.dir}
		if _, ok := to.s.store.Share(group); ok {
			params = map[string]any{}
		}
		groupJSON(t, to, "/api/share-invitations/"+inv.ID+"/accept", params)
		deadline := time.Now().Add(5 * time.Second)
		for {
			if st := to.s.Statuses()[group]; st.State == "synced" || st.State == "error" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("initial group sync did not finish")
			}
			time.Sleep(5 * time.Millisecond)
		}
		return group
	}
	t.Fatal("invitation not delivered")
	return ""
}

func groupRound(t *testing.T, id string, from, to *groupTestNode) error {
	t.Helper()
	sh, _ := from.s.store.Share(id)
	m, _ := shareMember(sh, to.id)
	p, _ := from.s.store.PairedDevice(to.id)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		lock := from.s.lockFor(id)
		lock.Lock()
		err := from.s.dialGroup(ctx, sh, m, p)
		lock.Unlock()
		if err == nil || !strings.Contains(err.Error(), ErrBusy.Error()) || ctx.Err() != nil {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}
func groupSync(t *testing.T, id string, from, to *groupTestNode) {
	t.Helper()
	if err := groupRound(t, id, from, to); err != nil {
		t.Fatalf("sync %s -> %s: %v", from.s.store.Config().DeviceName, to.s.store.Config().DeviceName, err)
	}
}

func TestGroupMeshSyncsOfflineAndMergesConcurrentEdits(t *testing.T) {
	nodes := groupNodes(t, 3)
	a, b, c := nodes[0], nodes[1], nodes[2]
	writeTestFile(t, a.dir, "obsolete.txt", "initial")
	writeTestFile(t, a.dir, "notes.txt", "base")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	groupLink(t, id, a, c, model.RoleCollaborator, false)
	groupLink(t, id, b, c, model.RoleCollaborator, false)
	groupSync(t, id, a, b)
	groupSync(t, id, b, c)
	st := a.s.store
	a.s.Stop()
	if err := os.Remove(filepath.Join(b.dir, "obsolete.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, b.dir, "notes.txt", "B edition")
	writeTestFile(t, c.dir, "notes.txt", "C edition")
	groupSync(t, id, b, c)
	if _, err := os.Stat(filepath.Join(c.dir, "obsolete.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("C did not apply deletion: %v", err)
	}
	a.restart(t, st)
	pairTestNodes(t, nodes)
	groupSync(t, id, c, a)
	groupSync(t, id, a, b)
	groupSync(t, id, b, c)
	for _, n := range nodes {
		if _, err := os.Stat(filepath.Join(n.dir, "obsolete.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("old offline file reappeared: %v", err)
		}
		matches, err := filepath.Glob(filepath.Join(n.dir, "notes.polysync-conflict-*.txt"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("conflict copies: %v %v", matches, err)
		}
		main, err := os.ReadFile(filepath.Join(n.dir, "notes.txt"))
		if err != nil {
			t.Fatal(err)
		}
		alt, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		if !((string(main) == "B edition" && string(alt) == "C edition") || (string(main) == "C edition" && string(alt) == "B edition")) {
			t.Fatalf("lost concurrent content: %s and %s", main, alt)
		}
	}
}

func TestGroupFollowerKeepsLocalChangesAndCanOnlyUseAuthorizedUpstream(t *testing.T) {
	nodes := groupNodes(t, 4)
	a, b, c, d := nodes[0], nodes[1], nodes[2], nodes[3]
	writeTestFile(t, a.dir, "notes.txt", "upstream")
	writeTestFile(t, a.dir, "kept.txt", "upstream")
	id := groupLink(t, "", a, b, model.RoleCollaborator, false)
	groupLink(t, id, a, c, model.RoleFollower, false)
	groupLink(t, id, a, d, model.RoleFollower, true)
	groupSync(t, id, a, c)
	groupSync(t, id, d, a)
	writeTestFile(t, c.dir, "notes.txt", "private edit")
	writeTestFile(t, c.dir, "extra.txt", "private file")
	if err := os.Remove(filepath.Join(c.dir, "kept.txt")); err != nil {
		t.Fatal(err)
	}
	groupSync(t, id, c, a)
	groupSync(t, id, a, c)
	assertTestFile(t, a.dir, "notes.txt", "upstream")
	assertTestFile(t, c.dir, "notes.txt", "private edit")
	assertTestFile(t, a.dir, "kept.txt", "upstream")
	if _, err := os.Stat(filepath.Join(a.dir, "extra.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("read-only follower uploaded a private file")
	}
	if _, err := os.Stat(filepath.Join(c.dir, "kept.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("unchanged upstream overrode follower's local deletion")
	}
	writeTestFile(t, d.dir, "notes.txt", "accepted writeback")
	groupSync(t, id, d, a)
	groupSync(t, id, a, b)
	groupSync(t, id, c, a)
	assertTestFile(t, b.dir, "notes.txt", "accepted writeback")
	assertTestFile(t, c.dir, "notes.txt", "accepted writeback")
	assertTestFile(t, c.dir, "extra.txt", "private file")
	history, err := filepath.Glob(filepath.Join(c.s.store.Dir(), "history", id, "*", "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	preserved := false
	for _, p := range history {
		v, e := os.ReadFile(p)
		if e == nil && string(v) == "private edit" {
			preserved = true
		}
	}
	if !preserved {
		t.Fatal("upstream update discarded the follower's local revision")
	}
	if err := groupRound(t, id, b, c); err == nil {
		t.Fatal("paired device without an approved group edge synced with follower")
	}
	if _, err := c.s.inviteGroup(t.Context(), model.Share{ID: id}, memberInput{DeviceID: b.id, Role: model.RoleCollaborator}, false); err == nil {
		t.Fatal("follower enrolled another member")
	}
	if _, err := b.s.inviteGroup(t.Context(), model.Share{ID: id}, memberInput{DeviceID: c.id, Role: model.RoleFollower}, false); err == nil {
		t.Fatal("follower accepted a second upstream")
	}
	if err := a.s.UnpairDevice(c.id); err != nil {
		t.Fatal(err)
	}
	groupSync(t, id, d, a)
	if err := groupRound(t, id, c, a); err == nil {
		t.Fatal("unpaired follower retained access")
	}
}

func TestGroupReadOnlyGrantEnforcedByUpstream(t *testing.T) {
	nodes := groupNodes(t, 2)
	a, b := nodes[0], nodes[1]
	id := groupLink(t, "", a, b, model.RoleFollower, false)
	// A local preference cannot turn the upstream's receive-only grant into
	// write permission, even if a peer's local configuration was restored wrong.
	share, _ := b.s.store.Share(id)
	share.Members[0].AllowWriteBack = true
	if err := b.s.store.UpdateShare(share); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, b.dir, "not-approved", "private")
	if err := groupRound(t, id, b, a); err == nil {
		t.Fatal("upstream accepted ungranted writeback")
	}
	if _, err := os.Stat(filepath.Join(a.dir, "not-approved")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("unapproved file appeared upstream")
	}
}
