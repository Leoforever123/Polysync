package service

import (
	"errors"
	"path/filepath"

	"polysync/internal/model"
)

// Serialize changes to sync roots with changes to the transfer inbox.
func (s *Service) saveTransferAwareShare(share model.Share, update bool) error {
	s.transferConfigMu.Lock()
	defer s.transferConfigMu.Unlock()
	if err := s.checkTransferOverlap(share.Path); err != nil {
		return err
	}
	if update {
		if saved, ok := s.store.Share(share.ID); ok && saved.LocalRole != "" {
			// Settings forms never grant, drop or modify memberships.
			if share.Path != saved.Path {
				return errors.New("多设备文件夹的位置固定，请新建共享以更换目录")
			}
			share.LocalRole, share.Members, share.State = saved.LocalRole, saved.Members, saved.State
			share.PeerDeviceID, share.PeerAddress = saved.PeerDeviceID, saved.PeerAddress
		}
		return s.store.UpdateShare(share)
	}
	for _, existing := range s.store.Config().Shares {
		if pathsOverlap(existing.Path, share.Path) {
			return errors.New("此目录已用于其他同步文件夹")
		}
	}
	return s.store.AddShare(share)
}
func (s *Service) checkTransferOverlap(path string) error {
	if actual, err := filepath.EvalSymlinks(path); err == nil {
		path = actual
	}
	inbox := s.transfers.Settings().Directory
	if actual, err := filepath.EvalSymlinks(inbox); err == nil {
		inbox = actual
	}
	if pathsOverlap(path, inbox) {
		return errors.New("同步文件夹不能与随传暂存目录重叠")
	}
	for _, task := range s.transfers.List() {
		if pathsOverlap(path, task.StorageDir) {
			return errors.New("同步文件夹不能包含随传暂存文件")
		}
	}
	return nil
}
