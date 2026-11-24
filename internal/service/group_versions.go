package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"polysync/internal/model"
	"polysync/internal/syncer"
)

func revisionManifest(entries []model.Entry) map[string]model.Entry {
	files := make(map[string]model.Entry, len(entries))
	for _, e := range entries {
		files[e.Path] = e
	}
	return files
}

func observeGroup(state *model.GroupState, files map[string]model.Entry, device string, writable bool) error {
	// Pending contains the intended state before a previous interrupted commit.
	// Only absorb versions whose content actually reached the working tree.
	for name, rev := range state.Pending {
		if files[name].Hash == rev.Hash {
			state.Records[name] = rev
		}
	}
	state.Pending = nil
	for n, e := range files {
		if old, ok := state.Records[n]; ok && e.Hash == old.Hash && e.Size != old.Size {
			old.Size = e.Size
			state.Records[n] = old
		}
	}
	if !writable {
		return nil
	}
	update := func(name string, e model.Entry) error {
		old := state.Records[name]
		if e.Hash == old.Hash {
			return nil
		}
		clock := maps.Clone(old.Clock)
		if clock == nil {
			clock = make(map[string]uint64)
		}
		if clock[device] == ^uint64(0) {
			return errors.New("文件版本计数已耗尽")
		}
		clock[device]++
		e.Path = name
		next := model.GroupRevision{Entry: e, Clock: clock, ConflictOrigin: old.ConflictOrigin}
		if old.Hash != "" && len(old.Clock) > 0 {
			next.Parent = &model.GroupAncestor{Hash: old.Hash, Clock: old.Clock}
		}
		state.Records[name] = next
		return nil
	}
	for name, e := range files {
		if err := update(name, e); err != nil {
			return err
		}
	}
	for name, rev := range state.Records {
		if _, ok := files[name]; !ok && rev.Hash != "" {
			if err := update(name, model.Entry{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func dominates(a, b map[string]uint64) bool {
	for id, n := range b {
		if a[id] < n {
			return false
		}
	}
	return true
}

func validateGroupOffer(records model.Revisions, readOnly bool) error {
	if readOnly && len(records) > 0 {
		return errors.New("此跟随设备仅有接收权限")
	}
	for name, rev := range records {
		if err := syncer.ValidateRelativePath(name); err != nil {
			return err
		}
		if (rev.ConflictOrigin != "" && len(rev.ConflictOrigin) != sha256.Size*2) || name != rev.Path || rev.Size < 0 || rev.Size > 16<<30 || len(rev.Clock) == 0 || len(rev.Clock) > 1024 {
			return errors.New("无效的文件版本记录")
		}
		if !validClock(rev.Clock) {
			return errors.New("无效的设备版本")
		}
		if p := rev.Parent; p != nil {
			if !validClock(p.Clock) || !validHex(p.Hash, sha256.Size) || !dominates(rev.Clock, p.Clock) || dominates(p.Clock, rev.Clock) {
				return errors.New("无效的共同版本记录")
			}
		}
		if rev.Hash != "" && !validHex(rev.Hash, sha256.Size) {
			return errors.New("无效的内容摘要")
		}
	}
	return nil
}

func validHex(hash string, size int) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == size && hash == strings.ToLower(hash)
}

func validClock(clock map[string]uint64) bool {
	if len(clock) == 0 || len(clock) > 1024 {
		return false
	}
	for id, n := range clock {
		if id == "" || len(id) > 128 || n == 0 {
			return false
		}
	}
	return true
}

// The same logical fork has the same identity whichever neighbor saw it first.
// Individual branches matter: a later recurrence can have identical file bytes.
func groupFork(name string, a, b model.GroupRevision) (string, error) {
	if a.Hash > b.Hash {
		a, b = b, a
	}
	h := sha256.New()
	if err := json.NewEncoder(h).Encode([]any{name, a.Hash, a.Clock, b.Hash, b.Clock}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func joinClock(a, b map[string]uint64) map[string]uint64 {
	c := maps.Clone(a)
	for id, n := range b {
		c[id] = max(c[id], n)
	}
	return c
}

func groupConflictName(name, hash string) string {
	ext := path.Ext(name)
	return strings.TrimSuffix(name, ext) + ".polysync-conflict-" + hash[:16] + ext
}

// A path is a causal register. Independent values are joined deterministically,
// preserving the losing content under the same name on every member.
func mergeGroup(local, remote model.Revisions) (model.Revisions, []model.PlanConflict, error) {
	target := maps.Clone(local)
	if target == nil {
		target = make(model.Revisions)
	}
	var conflicts []model.PlanConflict
	extra := make(model.Revisions)
	for name, b := range remote {
		a, ok := local[name]
		if !ok {
			target[name] = b
			continue
		}
		ab, ba := dominates(a.Clock, b.Clock), dominates(b.Clock, a.Clock)
		switch {
		case ab && ba && a.Hash != b.Hash:
			return nil, nil, fmt.Errorf("同一文件版本的内容不一致：%s", name)
		case ab:
			continue
		case ba:
			target[name] = b
			continue
		}
		winner, other := a, b
		if b.Hash > a.Hash {
			winner, other = b, a
		}
		winner.Clock = joinClock(a.Clock, b.Clock)
		target[name] = winner
		if a.Hash == b.Hash {
			continue
		}
		event, err := groupFork(name, a, b)
		if err != nil {
			return nil, nil, err
		}
		detail := model.PlanConflict{GroupEvent: event, Path: name, ServerHash: a.Hash, ClientHash: b.Hash, ServerExists: a.Hash != "", ClientExists: b.Hash != "", Kind: "modify-delete"}
		if a.Parent != nil && b.Parent != nil && a.Parent.Hash == b.Parent.Hash && maps.Equal(a.Parent.Clock, b.Parent.Clock) {
			detail.BaseHash = a.Parent.Hash
		}
		if other.Hash != "" {
			detail.Kind = "modify-modify"
			detail.ConflictCopyPath = groupConflictName(name, other.Hash)
			other.ConflictOrigin = event
			extra[name] = other
		}
		conflicts = append(conflicts, detail)
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	for i, conflict := range conflicts {
		rev, ok := extra[conflict.Path]
		if !ok {
			continue
		}
		first := conflict.ConflictCopyPath
		ext := path.Ext(first)
		for attempt := 1; ; attempt++ {
			name := first
			if attempt > 1 {
				name = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(first, ext), attempt, ext)
			}
			existing, occupied := target[name]
			if occupied && existing.ConflictOrigin != rev.ConflictOrigin {
				continue
			}
			conflicts[i].ConflictCopyPath = name
			if !occupied {
				rev.Path = name
				target[name] = rev
			}
			break
		}
	}
	return target, conflicts, nil
}

func revisionPaths(records model.Revisions) []string { return slices.Sorted(maps.Keys(records)) }

func (s *Service) saveGroupConflicts(share model.Share, conflicts []model.PlanConflict, self, peer model.PairedDevice) error {
	for _, c := range conflicts {
		h := sha256.Sum256([]byte(share.ID + "\x00" + c.GroupEvent))
		id := hex.EncodeToString(h[:16])
		if _, ok, err := s.store.Conflict(id); err != nil {
			return err
		} else if ok {
			continue
		}
		localHash, remoteHash := c.ServerHash, c.ClientHash
		base := c.BaseHash
		if base != "" {
			if _, err := os.Stat(s.store.ObjectPath(base)); errors.Is(err, os.ErrNotExist) {
				base = ""
			} else if err != nil {
				return err
			}
		}
		record := model.Conflict{ID: id, ShareID: share.ID, Path: c.Path, GroupEvent: c.GroupEvent, Kind: c.Kind, BaseHash: base, LocalHash: localHash, RemoteHash: remoteHash, LocalExists: localHash != "", RemoteExists: remoteHash != "", LocalDevice: self.Name, RemoteDevice: peer.Name, ConflictCopyPath: c.ConflictCopyPath, Status: "pending", CreatedAt: time.Now()}
		if err := s.store.SaveConflict(record); err != nil {
			return err
		}
	}
	return nil
}
