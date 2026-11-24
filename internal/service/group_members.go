package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
	"polysync/internal/store"
)

var errGroupUnsupported = errors.New("对方的 PolySync 需要升级后才能加入多设备文件夹")

type memberInput struct {
	DeviceID       string `json:"deviceId"`
	Role           string `json:"role"`
	AllowWriteBack bool   `json:"allowWriteBack"`
}

type groupInvite struct {
	protocol.ShareInvite
	Role           string `json:"role"`
	AllowWriteBack bool   `json:"allowWriteBack"`
}

func validRole(role string, write bool) bool {
	return role == model.RoleFollower || (role == model.RoleCollaborator && !write)
}

func shareMember(share model.Share, id string) (model.ShareMember, bool) {
	for _, m := range share.Members {
		if m.DeviceID == id {
			return m, true
		}
	}
	return model.ShareMember{}, false
}

func (s *Service) groupControl(ctx context.Context, device model.PairedDevice, address, op string, request any) error {
	if address == "" {
		return errors.New("设备没有可用地址")
	}
	conn, err := s.dialTLS(ctx, address, &device)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	stopService := context.AfterFunc(s.ctx, func() { conn.Close() })
	defer stopService()
	cfg := s.store.Config()
	f := protocol.NewFramer(conn)
	if err := f.WriteJSON(protocol.Hello{Type: "hello", Operation: op, Protocol: model.ProtocolVersion, DeviceID: cfg.DeviceID, DeviceName: cfg.DeviceName, ListenPort: s.listenPort}); err != nil {
		return err
	}
	if err := f.WriteJSON(request); err != nil {
		return err
	}
	var result protocol.Result
	if err := f.ReadJSON(&result); err != nil {
		return err
	}
	if !result.OK {
		if result.Code == "operation" {
			return errGroupUnsupported
		}
		return errors.New(result.Message)
	}
	return nil
}

// editGroup serializes membership and root registration, including overlap
// validation. Never wait for the network while holding the configuration lock.
func (s *Service) editGroup(id string, change func(*model.Share, bool) error) (model.Share, error) {
	s.transferConfigMu.Lock()
	defer s.transferConfigMu.Unlock()
	share, exists := s.store.Share(id)
	if err := change(&share, exists); err != nil {
		return model.Share{}, err
	}
	if err := s.checkTransferOverlap(share.Path); err != nil {
		return model.Share{}, err
	}
	for _, other := range s.store.Config().Shares {
		if other.ID != id && pathsOverlap(share.Path, other.Path) {
			return model.Share{}, fmt.Errorf("这个目录已用于“%s”", other.Name)
		}
	}
	var err error
	if exists {
		err = s.store.UpdateShare(share)
	} else {
		err = s.store.AddShare(share)
	}
	if err == nil && !exists {
		s.RegisterShare(id)
	}
	return share, err
}

func promoteLegacy(share *model.Share) error {
	if share.LocalRole != "" {
		return nil
	}
	if share.PeerDeviceID == "" || share.State != "active" {
		return errors.New("请先完成原有设备的文件夹邀请")
	}
	share.LocalRole = model.RoleCollaborator
	share.Members = []model.ShareMember{{DeviceID: share.PeerDeviceID, Role: model.RoleCollaborator, State: "active", Address: share.PeerAddress, Legacy: true}}
	return nil
}

func (s *Service) inviteGroup(ctx context.Context, seed model.Share, member memberInput, create bool) (model.Share, error) {
	s.peerControlMu.RLock()
	defer s.peerControlMu.RUnlock()
	if !validRole(member.Role, member.AllowWriteBack) {
		return model.Share{}, errors.New("请选择有效的设备角色")
	}
	dev, ok := s.store.PairedDevice(member.DeviceID)
	if !ok {
		return model.Share{}, errors.New("请先配对这台设备")
	}
	address := s.bestDeviceAddress(dev)
	if create {
		seed.ID = store.RandomID()
	}
	id := randomToken(16)
	group, err := s.editGroup(seed.ID, func(sh *model.Share, exists bool) error {
		if create && !exists {
			*sh = seed
			sh.LocalRole = model.RoleCollaborator
			sh.PeerDeviceID = dev.ID
			sh.PeerAddress = address
			sh.State = "pending"
		} else if !exists {
			return errors.New("文件夹不存在")
		} else if err := promoteLegacy(sh); err != nil {
			return err
		}
		if sh.LocalRole != model.RoleCollaborator || sh.State == "unpaired" {
			return errors.New("请在协作设备上邀请成员")
		}
		if m, ok := shareMember(*sh, dev.ID); ok {
			if m.State == "pending" && m.Role == member.Role && m.AllowWriteBack == member.AllowWriteBack {
				id = m.InvitationID
				return nil
			}
			return errors.New("这台设备已经在文件夹中")
		}
		sh.Members = append(sh.Members, model.ShareMember{DeviceID: dev.ID, State: "pending", Role: member.Role, AllowWriteBack: member.AllowWriteBack, InvitationID: id, Address: address})
		return nil
	})
	if err != nil {
		return model.Share{}, err
	}
	req := groupInvite{ShareInvite: protocol.ShareInvite{Type: "group_invite", ID: id, ShareID: group.ID, Name: group.Name}, Role: member.Role, AllowWriteBack: member.AllowWriteBack}
	m, _ := shareMember(group, dev.ID)
	if !m.Legacy {
		err = s.groupControl(ctx, dev, address, "group_invite", req)
	}
	if (errors.Is(err, errGroupUnsupported) || m.Legacy) && member.Role == model.RoleCollaborator && group.State == "pending" && len(group.Members) == 1 {
		group, err = s.editGroup(group.ID, func(sh *model.Share, exists bool) error {
			if !exists || len(sh.Members) != 1 || sh.Members[0].InvitationID != id || sh.State != "pending" {
				return errors.New("文件夹成员已改变")
			}
			sh.Members[0].Legacy = true
			return nil
		})
		if err == nil {
			req.ShareInvite.Type = "share_invite"
			err = s.groupControl(ctx, dev, address, "share_invite", req.ShareInvite)
		}
	}
	if err != nil {
		return group, fmt.Errorf("邀请待发送，可以在文件夹中重试：%w", err)
	}
	return group, nil
}

func (s *Service) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	var in memberInput
	if err := readJSON(r, &in); err != nil {
		writeAPIError(w, 400, err)
		return
	}
	share, err := s.inviteGroup(r.Context(), model.Share{ID: r.PathValue("id")}, in, false)
	if err != nil {
		writeJSON(w, 409, map[string]any{"id": share.ID, "error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"id": share.ID})
}

func (s *Service) handleGroupInvite(conn *tls.Conn, f *protocol.Framer, hello protocol.Hello) error {
	dev, err := s.requirePaired(conn, hello.DeviceID)
	if err != nil {
		return err
	}
	var req groupInvite
	if err := f.ReadJSONLimit(&req, 64<<10); err != nil {
		return err
	}
	if req.Type != "group_invite" || !validRole(req.Role, req.AllowWriteBack) || req.ShareID == "" || len(req.ShareID) > 128 || strings.ContainsAny(req.ShareID, "/\\") || req.ShareID == "." || req.ShareID == ".." || req.ID == "" || len(req.ID) > 128 || strings.TrimSpace(req.Name) == "" {
		return errors.New("无效的文件夹邀请")
	}
	if sh, ok := s.store.Share(req.ShareID); ok {
		if m, ok := shareMember(sh, dev.ID); ok && m.InvitationID == req.ID && m.State == "active" {
			return f.WriteJSON(protocol.Result{Type: "result", OK: true})
		}
		if sh.LocalRole == model.RoleFollower || req.Role != model.RoleCollaborator || sh.State == "unpaired" {
			return errors.New("跟随设备只能连接一个上游")
		}
	}
	inv := model.ShareInvitation{ID: req.ID, ShareID: req.ShareID, Name: req.Name, DeviceID: dev.ID, DeviceName: dev.Name, Address: remoteAddress(conn.RemoteAddr(), hello.ListenPort), CreatedAt: time.Now(), Role: req.Role, AllowWriteBack: req.AllowWriteBack}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.shareInvitations[inv.ID]; ok && (prior.DeviceID != inv.DeviceID || prior.ShareID != inv.ShareID || prior.Role != inv.Role || prior.AllowWriteBack != inv.AllowWriteBack) {
		return errors.New("邀请与之前的授权不一致")
	}
	if err := s.store.SaveShareInvitation(inv); err != nil {
		return err
	}
	s.shareInvitations[inv.ID] = inv
	return f.WriteJSON(protocol.Result{Type: "result", OK: true})
}

func (s *Service) handleGroupAccept(conn *tls.Conn, f *protocol.Framer, hello protocol.Hello) error {
	dev, err := s.requirePaired(conn, hello.DeviceID)
	if err != nil {
		return err
	}
	var req protocol.ShareAccept
	if err := f.ReadJSONLimit(&req, 64<<10); err != nil {
		return err
	}
	if req.Type != "group_accept" || req.ID == "" {
		return errors.New("无效的邀请确认")
	}
	_, err = s.editGroup(req.ShareID, func(sh *model.Share, exists bool) error {
		if exists && sh.LocalRole == model.RoleCollaborator && sh.State != "unpaired" {
			for i, m := range sh.Members {
				if m.DeviceID == dev.ID && m.InvitationID == req.ID {
					sh.Members[i].State = "active"
					sh.Members[i].Address = remoteAddress(conn.RemoteAddr(), hello.ListenPort)
					sh.State = "active"
					return nil
				}
			}
		}
		return errors.New("没有为这台设备授予此文件夹权限")
	})
	if err != nil {
		return err
	}
	return f.WriteJSON(protocol.Result{Type: "result", OK: true})
}

func (s *Service) acceptGroup(ctx context.Context, inv model.ShareInvitation, dir string, auto bool, interval int) (model.Share, error) {
	device, ok := s.store.PairedDevice(inv.DeviceID)
	if !ok {
		return model.Share{}, errors.New("请先配对上游设备")
	}
	if existing, ok := s.store.Share(inv.ShareID); ok {
		dir = existing.Path
	}
	path, err := s.preparePath(dir, inv.ShareID)
	if err != nil {
		return model.Share{}, err
	}
	// The pending membership cannot send or receive files until confirmation.
	group, err := s.editGroup(inv.ShareID, func(sh *model.Share, exists bool) error {
		if !exists {
			*sh = model.Share{ID: inv.ShareID, Name: inv.Name, Path: path, AutoSync: auto, IntervalSeconds: interval, LocalRole: inv.Role, PeerDeviceID: device.ID, PeerAddress: inv.Address, State: "pending"}
		} else {
			if err := promoteLegacy(sh); err != nil {
				return err
			}
			if sh.State == "unpaired" || sh.LocalRole != inv.Role || filepath.Clean(sh.Path) != path {
				return errors.New("邀请与本机已有文件夹角色不一致")
			}
			if m, ok := shareMember(*sh, device.ID); ok {
				if m.InvitationID == inv.ID && m.AllowWriteBack == inv.AllowWriteBack {
					return nil
				}
				return errors.New("已存在来自这台设备的连接")
			}
			if sh.LocalRole == model.RoleFollower {
				return errors.New("跟随设备已有上游")
			}
		}
		sh.Members = append(sh.Members, model.ShareMember{DeviceID: device.ID, Role: model.RoleCollaborator, AllowWriteBack: inv.AllowWriteBack, Address: inv.Address, State: "pending", InvitationID: inv.ID})
		return nil
	})
	if err != nil {
		return model.Share{}, err
	}
	if err = s.groupControl(ctx, device, inv.Address, "group_accept", protocol.ShareAccept{Type: "group_accept", ID: inv.ID, ShareID: inv.ShareID}); err != nil {
		return group, err
	}
	group, err = s.editGroup(inv.ShareID, func(sh *model.Share, exists bool) error {
		if exists {
			for i, m := range sh.Members {
				if m.DeviceID == device.ID && m.InvitationID == inv.ID {
					sh.Members[i].State = "active"
					sh.State = "active"
					return nil
				}
			}
		}
		return errors.New("文件夹授权已撤销")
	})
	if err != nil {
		return group, err
	}
	s.mu.Lock()
	if err = s.store.RemoveShareInvitation(inv.ID); err == nil {
		delete(s.shareInvitations, inv.ID)
	}
	s.mu.Unlock()
	if err != nil {
		return group, err
	}
	go func() { _ = s.SyncShare(s.ctx, group.ID, true) }()
	return group, nil
}
