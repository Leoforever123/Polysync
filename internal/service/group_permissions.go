package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"polysync/internal/model"
	"polysync/internal/protocol"
)

type groupPolicy struct {
	Type  string `json:"type"`
	Role  string `json:"role"`
	Allow bool   `json:"allow"`
	Grant string `json:"grant"`
}

func grantID(m model.ShareMember) string {
	if m.GrantID != "" {
		return m.GrantID
	}
	return m.InvitationID
}

// Each side computes the effective grant from its own peer record and the
// authenticated peer's consent. A follower can never promote its own grant.
func (s *Service) groupPolicy(f *protocol.Framer, sh model.Share, m model.ShareMember, client bool) (localRO, peerRO bool, err error) {
	own := groupPolicy{Type: "group_policy", Role: sh.LocalRole, Allow: m.AllowWriteBack, Grant: grantID(m)}
	if client {
		if err = f.WriteJSON(own); err != nil {
			return
		}
	}
	var peer groupPolicy
	if err = f.ReadJSONLimit(&peer, 64<<10); err != nil {
		return
	}
	if peer.Type != "group_policy" || peer.Role != m.Role || len(peer.Grant) > 128 {
		err = errors.New("成员角色与文件夹授权不一致")
		return
	}
	if !client {
		if err = f.WriteJSON(own); err != nil {
			return
		}
	}
	effective := m.AllowWriteBack && peer.Allow && own.Grant == peer.Grant
	localRO = sh.LocalRole == model.RoleFollower && !effective
	peerRO = m.Role == model.RoleFollower && !effective
	if sh.LocalRole == model.RoleFollower && (own.Grant != peer.Grant || (!peer.Allow && (m.AllowWriteBack || m.WriteBackPending))) {
		_, err = s.editGroup(sh.ID, func(current *model.Share, exists bool) error {
			if !exists || len(current.Members) != 1 || current.Members[0].DeviceID != m.DeviceID {
				return errors.New("上游授权已撤销")
			}
			current.Members[0].GrantID = peer.Grant
			current.Members[0].AllowWriteBack = false
			current.Members[0].WriteBackPending = peer.Allow
			return nil
		})
	}
	return
}

func (s *Service) handleGroupMemberChange(w http.ResponseWriter, r *http.Request) {
	s.peerControlMu.RLock()
	defer s.peerControlMu.RUnlock()
	id, peer := r.PathValue("id"), r.PathValue("device")
	lock := s.lockFor(id)
	if !lock.TryLock() {
		writeAPIError(w, 409, errors.New("正在同步，完成后可以更改成员"))
		return
	}
	defer lock.Unlock()
	var in struct {
		Allow *bool `json:"allowWriteBack"`
	}
	if r.Method == http.MethodPut {
		if err := readJSON(r, &in); err != nil {
			writeAPIError(w, 400, err)
			return
		}
		if in.Allow == nil {
			writeAPIError(w, 400, errors.New("请选择是否允许回传"))
			return
		}
	}
	_, err := s.editGroup(id, func(sh *model.Share, exists bool) error {
		if !exists || sh.LocalRole == "" {
			return errors.New("多设备文件夹不存在")
		}
		for i, m := range sh.Members {
			if m.DeviceID != peer {
				continue
			}
			if r.Method == http.MethodDelete {
				sh.Members = slices.Delete(sh.Members, i, i+1)
				sh.RefreshMembers()
				return nil
			}
			if sh.LocalRole != model.RoleCollaborator || m.Role != model.RoleFollower || m.State != "active" {
				return errors.New("只有上游可以更改已连接跟随设备的回传权限")
			}
			if m.AllowWriteBack != *in.Allow {
				sh.Members[i].GrantID = randomToken(16)
				sh.Members[i].AllowWriteBack = *in.Allow
			}
			return nil
		}
		return errors.New("该成员已断开")
	})
	if err != nil {
		writeAPIError(w, 409, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

type writeBackPath struct {
	Path   string `json:"path"`
	Change string `json:"change"`
}
type writeBackPreview struct {
	Token    string          `json:"token"`
	Added    int             `json:"added"`
	Modified int             `json:"modified"`
	Deleted  int             `json:"deleted"`
	Paths    []writeBackPath `json:"paths"`
}

func (s *Service) previewWriteBack(ctx context.Context, sh model.Share) (writeBackPreview, error) {
	var out writeBackPreview
	if sh.LocalRole != model.RoleFollower || len(sh.Members) != 1 || !sh.Members[0].WriteBackPending {
		return out, errors.New("没有待确认的回传授权")
	}
	state, err := s.checkedGroupState(sh)
	if err != nil {
		return out, err
	}
	ctx, volume, err := s.openVolume(ctx, sh)
	if err != nil {
		return out, err
	}
	defer volume.dir.Close()
	scan, err := volume.scan(ctx)
	if err != nil {
		return out, err
	}
	files := revisionManifest(scan)
	names := make([]string, 0, len(files)+len(state.Records))
	for name := range state.Records {
		names = append(names, name)
	}
	for name := range files {
		if _, ok := state.Records[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	if err := encoder.Encode([]string{sh.ID, grantID(sh.Members[0])}); err != nil {
		return out, err
	}
	out.Paths = []writeBackPath{}
	for _, name := range names {
		old, next := state.Records[name].Hash, files[name].Hash
		if old == next {
			continue
		}
		change := "modified"
		switch {
		case old == "":
			out.Added++
			change = "added"
		case next == "":
			out.Deleted++
			change = "deleted"
		default:
			out.Modified++
		}
		if len(out.Paths) < 50 {
			out.Paths = append(out.Paths, writeBackPath{name, change})
		}
		if err := encoder.Encode([]string{name, old, next}); err != nil {
			return out, err
		}
	}
	out.Token = hex.EncodeToString(hash.Sum(nil))
	return out, nil
}

func (s *Service) handleWriteBack(w http.ResponseWriter, r *http.Request) {
	s.peerControlMu.RLock()
	defer s.peerControlMu.RUnlock()
	id := r.PathValue("id")
	lock := s.lockFor(id)
	if !lock.TryLock() {
		writeAPIError(w, 409, ErrBusy)
		return
	}
	defer lock.Unlock()
	sh, ok := s.store.Share(id)
	if !ok {
		writeAPIError(w, 404, errors.New("文件夹不存在"))
		return
	}
	out, err := s.previewWriteBack(r.Context(), sh)
	if err != nil {
		writeAPIError(w, 409, err)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, out)
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := readJSON(r, &in); err != nil {
		writeAPIError(w, 400, err)
		return
	}
	if out.Token != in.Token {
		writeAPIError(w, 409, errors.New("文件或授权已变化，请重新查看待回传内容"))
		return
	}
	_, err = s.editGroup(id, func(current *model.Share, exists bool) error {
		if !exists || len(current.Members) != 1 || !current.Members[0].WriteBackPending || grantID(current.Members[0]) != grantID(sh.Members[0]) {
			return errors.New("回传授权已变化")
		}
		current.Members[0].AllowWriteBack = true
		current.Members[0].WriteBackPending = false
		return nil
	})
	if err != nil {
		writeAPIError(w, 409, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
