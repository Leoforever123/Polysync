package service

import (
	"errors"
	"polysync/internal/update"
)

func (s *Service) SetUpdater(manager *update.Manager) { s.updater = manager }

// PrepareUpdate closes the task admission gate atomically with the idle check.
func (s *Service) PrepareUpdate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updating || s.ctx.Err() != nil {
		return errors.New("应用正在退出或安装更新")
	}
	if s.activeSync > 0 || s.activeTransferWork > 0 {
		return errors.New("仍有同步或文件传输正在进行，请完成后再安装")
	}
	s.transfers.mu.Lock()
	defer s.transfers.mu.Unlock()
	for _, t := range s.transfers.tasks {
		if transferActive(t.State) {
			return errors.New("仍有文件传输正在进行，请完成后再安装")
		}
	}
	s.updating = true
	return nil
}
func (s *Service) CancelUpdate() { s.mu.Lock(); s.updating = false; s.mu.Unlock() }
func (s *Service) beginSyncWork() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updating {
		return errors.New("正在安装更新，请稍后再试")
	}
	if e := s.ctx.Err(); e != nil {
		return e
	}
	s.activeSync++
	s.wg.Add(1)
	return nil
}
func (s *Service) endSyncWork() { s.mu.Lock(); s.activeSync--; s.mu.Unlock(); s.wg.Done() }
