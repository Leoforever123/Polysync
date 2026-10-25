package service

import (
	"errors"
	"net/http"

	"polysync/internal/model"
)

var errUnpairBusy = errors.New("仍有设备操作或文件任务正在进行，请完成或取消任务后重试")

// UnpairDevice revokes local trust only. No network connection to the peer is
// needed, and no files, historical versions or transfer records are deleted.
func (s *Service) UnpairDevice(id string) error {
	// Control operations can persist trust, invitations or share configuration.
	// TryLock avoids waiting behind a stalled peer while holding the HTTP request.
	if !s.peerControlMu.TryLock() {
		return errUnpairBusy
	}
	defer s.peerControlMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updating || s.ctx.Err() != nil {
		return errors.New("应用正在退出或安装更新，请稍后再试")
	}
	// Admission uses the same mutex, so no authenticated operation can start
	// between this check and the durable removal of the pinned identity.
	if s.activeSync > 0 || s.activeTransferWork > 0 {
		return errUnpairBusy
	}
	s.transfers.mu.Lock()
	defer s.transfers.mu.Unlock()
	for _, task := range s.transfers.tasks {
		if task.PeerID == id && transferActive(task.State) {
			return errUnpairBusy
		}
	}
	if err := s.store.RemovePairedDevice(id); err != nil {
		return err
	}
	for key, invitation := range s.shareInvitations {
		if invitation.DeviceID == id {
			delete(s.shareInvitations, key)
		}
	}
	for key, pending := range s.pairingRequests {
		if pending.Request.DeviceID == id {
			delete(s.pairingRequests, key)
		}
	}
	for key, pending := range s.outboundPairs {
		if pending.DeviceID == id {
			delete(s.outboundPairs, key)
		}
	}
	for _, share := range s.store.Config().Shares {
		if share.PeerDeviceID == id {
			s.statuses[share.ID] = model.RuntimeStatus{ShareID: share.ID, State: "idle", Message: "已取消配对 · 本地文件保留"}
		}
	}
	return nil
}

func (s *Service) handleUnpairDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.UnpairDevice(r.PathValue("id")); err != nil {
		writeAPIError(w, http.StatusConflict, err)
		return
	}
	s.log("info", "", "已取消设备配对，关联同步已停用，本地文件与历史记录保留")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
