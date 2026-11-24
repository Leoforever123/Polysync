package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
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
		state.Records[name] = model.GroupRevision{Entry: e, Clock: clock}
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

func validateGroupRecords(records model.Revisions) error {
	for name, rev := range records {
		if err := syncer.ValidateRelativePath(name); err != nil {
			return err
		}
		if name != rev.Path || rev.Size < 0 || rev.Size > 16<<30 || len(rev.Clock) == 0 || len(rev.Clock) > 1024 {
			return errors.New("无效的文件版本记录")
		}
		for id, n := range rev.Clock {
			if id == "" || len(id) > 128 || n == 0 {
				return errors.New("无效的设备版本")
			}
		}
		if rev.Hash != "" {
			decoded, err := hex.DecodeString(rev.Hash)
			if err != nil || len(decoded) != sha256.Size || rev.Hash != strings.ToLower(rev.Hash) {
				return errors.New("无效的内容摘要")
			}
		}
	}
	return nil
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
		detail := model.PlanConflict{Path: name, ServerHash: a.Hash, ClientHash: b.Hash, ServerExists: a.Hash != "", ClientExists: b.Hash != "", Kind: "modify-delete"}
		if other.Hash != "" {
			detail.Kind = "modify-modify"
			detail.ConflictCopyPath = groupConflictName(name, other.Hash)
			other.Path = detail.ConflictCopyPath
			extra[other.Path] = other
		}
		conflicts = append(conflicts, detail)
	}
	for name, rev := range extra {
		if existing, ok := target[name]; ok {
			if existing.Hash == rev.Hash || dominates(existing.Clock, rev.Clock) {
				continue
			}
			return nil, nil, fmt.Errorf("冲突副本名称已被使用：%s", name)
		}
		target[name] = rev
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	return target, conflicts, nil
}

func revisionPaths(records model.Revisions) []string { return slices.Sorted(maps.Keys(records)) }

// To keep conflict identifiers identical in both directions, the physical
// pair's original device ID determines which side is passed as "server".
func (s *Service) saveGroupConflicts(share model.Share, conflicts []model.PlanConflict, self, peer model.PairedDevice) error {
	for _, c := range conflicts {
		if self.ID > peer.ID {
			c.ServerExists, c.ClientExists = c.ClientExists, c.ServerExists
			c.ServerHash, c.ClientHash = c.ClientHash, c.ServerHash
		}
		h := sha256.Sum256([]byte(share.ID + "\x00" + c.Path + "\x00" + c.ServerHash + "\x00" + c.ClientHash))
		id := hex.EncodeToString(h[:16])
		if _, ok, err := s.store.Conflict(id); err != nil {
			return err
		} else if ok {
			continue
		}
		localHash, remoteHash := c.ServerHash, c.ClientHash
		if self.ID > peer.ID {
			localHash, remoteHash = remoteHash, localHash
		}
		record := model.Conflict{ID: id, ShareID: share.ID, Path: c.Path, Kind: c.Kind, LocalHash: localHash, RemoteHash: remoteHash, LocalExists: localHash != "", RemoteExists: remoteHash != "", LocalDevice: self.Name, RemoteDevice: peer.Name, ConflictCopyPath: c.ConflictCopyPath, Status: "pending", CreatedAt: time.Now()}
		if err := s.store.SaveConflict(record); err != nil {
			return err
		}
	}
	return nil
}
