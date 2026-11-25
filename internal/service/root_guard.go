package service

import (
	"context"
	"errors"

	"os"
	"time"

	"polysync/internal/model"
	"polysync/internal/rootidentity"
	"polysync/internal/syncer"
)

var errRootPaused = errors.New("文件夹同步已暂停")
var errUnverifiedRoot = errors.New("升级后无法确认原文件夹。请接回原盘；若仍未恢复，请核对两端文件，再重新邀请此文件夹。")
var errRemoteRootPaused = errors.New("本机文件夹可用，正在等待对端接回原盘。")

type rootPause struct{ cause error }

func (e rootPause) Error() string {
	switch {
	case errors.Is(e.cause, errUnverifiedRoot):
		return errUnverifiedRoot.Error()
	case errors.Is(e.cause, rootidentity.ErrDifferentRoot):
		return "此位置未找到原同步文件夹。接回本机原盘后会自动恢复。"
	case errors.Is(e.cause, os.ErrNotExist):
		return "正在等待本机原盘。原文件夹回到此路径后会自动恢复。"
	default:
		return "暂时无法核验本机的同步文件夹。可以读取原盘后会自动恢复。"
	}
}
func (e rootPause) Unwrap() error        { return e.cause }
func (e rootPause) Is(target error) bool { return target == errRootPaused }
func rootPauseError(err error) error     { return rootPause{err} }

type migrationFailure struct {
	directory os.FileInfo
	recheck   time.Time
}

// A migrated share has no disk identifier. Check its last known live contents
// before enrolling the directory. In particular, never enroll an empty mount
// point merely because a directory of the right name exists.
func (s *Service) migrateRoot(ctx context.Context, sh model.Share) (string, error) {
	root, err := os.OpenRoot(sh.Path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	dir, err := root.Stat(".")
	if err != nil {
		return "", err
	}
	s.mu.RLock()
	failed := s.migrationFailures[sh.ID]
	s.mu.RUnlock()
	if failed.directory != nil && time.Now().Before(failed.recheck) && os.SameFile(dir, failed.directory) {
		return "", errUnverifiedRoot
	}
	unverified := func() (string, error) {
		s.mu.Lock()
		s.migrationFailures[sh.ID] = migrationFailure{dir, time.Now().Add(30 * time.Second)}
		s.mu.Unlock()
		return "", errUnverifiedRoot
	}
	want := make(map[string]string)
	var groupHistory bool
	// A receive-only folder can keep private edits. Its revision journal
	// describes the upstream, while its successful baseline describes the
	// actual local files from that same round.
	receiveOnly := sh.LocalRole == model.RoleFollower && len(sh.Members) == 1 && !sh.Members[0].AllowWriteBack
	if sh.ReplicaID != "" && !receiveOnly {
		state, err := s.checkedGroupState(sh)
		if err != nil {
			return "", err
		}
		groupHistory = len(state.Records) != 0 || len(state.Pending) != 0
		for name, rev := range state.Records {
			if rev.Hash != "" {
				want[name] = rev.Hash
			}
		}
	}
	if !groupHistory && sh.PeerDeviceID != "" {
		baseline, err := s.store.LoadBaseline(sh.ID, sh.PeerDeviceID)
		if err != nil {
			return "", err
		}
		for _, e := range baseline {
			want[e.Path] = e.Hash
		}
	}
	if len(want) == 0 {
		return unverified()
	}
	// Cheaply reject a missing file before hashing a large original folder.
	for name := range want {
		if _, err := root.Lstat(name); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return unverified()
			}
			return "", err
		}
	}
	entries, err := syncer.ScanRoot(ctx, root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if want[e.Path] == e.Hash {
			delete(want, e.Path)
		}
	}
	if len(want) != 0 {
		return unverified()
	}
	id, err := rootidentity.BindRoot(root)
	if err != nil {
		return "", err
	}
	return id, s.store.BindMigratedRoot(sh.ID, sh.Path, id)
}

// Called under the per-share lock. Newly registered groups can have a local
// value predating AddShare's persisted identity, so always consult the store.
func (s *Service) rootID(ctx context.Context, sh model.Share) (string, error) {
	if sh.RootID != "" {
		return sh.RootID, nil
	}
	current, ok := s.store.Share(sh.ID)
	if !ok || current.Path != sh.Path {
		return "", errors.New("同步文件夹配置已变更")
	}
	if current.RootID != "" {
		return current.RootID, nil
	}
	return s.migrateRoot(ctx, current)
}

func (s *Service) noteRootPause(id string, retry bool, err error) error {
	if !errors.Is(err, errRootPaused) {
		err = rootPauseError(err)
	}
	s.mu.Lock()
	prior, paused := s.rootRetry[id]
	s.rootRetry[id] = prior || retry
	status := s.statuses[id]
	status.ShareID, status.State, status.Message = id, "paused", err.Error()
	s.statuses[id] = status
	s.mu.Unlock()
	if retry && !prior {
		if saveErr := s.store.SetVolumeRetry(id, true); saveErr != nil {
			s.log("warning", id, "无法保存待恢复的同步请求："+saveErr.Error())
		}
	}
	if !paused {
		s.log("warning", id, err.Error())
	}
	return err
}

func (s *Service) clearRootPause(id string) {
	s.mu.Lock()
	retry, paused := s.rootRetry[id]
	if paused {
		delete(s.rootRetry, id)
		status := s.statuses[id]
		if status.State == "paused" {
			status.State, status.Message = "idle", "原盘已接回"
		}
		s.statuses[id] = status
	}
	s.mu.Unlock()
	if retry {
		if err := s.store.SetVolumeRetry(id, false); err != nil {
			s.log("warning", id, "无法更新待恢复的同步请求："+err.Error())
		}
	}
	if paused {
		s.log("info", id, "原同步文件夹已恢复")
	}
}

func (s *Service) checkLocalRoot(ctx context.Context, sh model.Share, retry bool) error {
	id, err := s.rootID(ctx, sh)
	if err == nil {
		err = rootidentity.Check(sh.Path, id)
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.noteRootPause(sh.ID, retry, err)
}

type syncVolume struct {
	path string
	dir  *os.Root
	info os.FileInfo
}

func (v *syncVolume) check() error {
	now, err := os.Lstat(v.path)
	if err == nil && !os.SameFile(v.info, now) {
		err = rootidentity.ErrDifferentRoot
	}
	if err != nil {
		return rootPauseError(err)
	}
	return nil
}

func (s *Service) openVolume(ctx context.Context, sh model.Share, retry bool) (context.Context, *syncVolume, error) {
	id, err := s.rootID(ctx, sh)
	if err == nil {
		dir, openErr := rootidentity.Open(sh.Path, id)
		err = openErr
		if err == nil {
			info, statErr := dir.Stat(".")
			if statErr == nil {
				v := &syncVolume{sh.Path, dir, info}
				return syncer.WithRootCheck(ctx, dir, v.check), v, nil
			}
			dir.Close()
			err = statErr
		}
	}
	if ctx.Err() != nil {
		return ctx, nil, ctx.Err()
	}
	return ctx, nil, s.noteRootPause(sh.ID, retry, err)
}

func (v *syncVolume) scan(ctx context.Context) ([]model.Entry, error) {
	if err := v.check(); err != nil {
		return nil, err
	}
	entries, err := syncer.ScanRoot(ctx, v.dir)
	if checkErr := v.check(); checkErr != nil {
		return nil, checkErr
	}
	return entries, err
}

// Probe each folder independently. A slow or missing mount never holds up the
// scheduler for other folders. The share lock also bounds outstanding probes.
func (s *Service) checkSyncFolders(ctx context.Context) {
	config := s.store.Config()
	for _, sh := range config.Shares {
		if sh.State != "active" || sh.PeerDeviceID == "" {
			continue
		}
		lock := s.lockFor(sh.ID)
		if !lock.TryLock() {
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer lock.Unlock()
			if ctx.Err() != nil {
				return
			}
			sh, exists := s.store.Share(sh.ID)
			if !exists || sh.State != "active" {
				return
			}
			if err := s.checkLocalRoot(ctx, sh, false); err != nil {
				return
			}
			s.mu.RLock()
			retry, resumed := s.rootRetry[sh.ID]
			status := s.statuses[sh.ID]
			s.mu.RUnlock()
			auto := sh.AutoSync && !s.options.DisableAutoSync
			if !retry && !auto {
				if resumed {
					s.clearRootPause(sh.ID)
				}
				return
			}
			interval := time.Duration(sh.IntervalSeconds) * time.Second
			if interval < 5*time.Second {
				interval = 30 * time.Second
			}
			jitter := time.Duration(deviceJitter(config.DeviceID, sh.ID)) * time.Millisecond
			if !retry && !resumed && !status.LastAttempt.IsZero() && time.Since(status.LastAttempt) < interval+jitter {
				return
			}
			if err := s.beginSyncWork(); err != nil {
				return
			}
			defer s.endSyncWork()
			_ = s.syncShareLocked(ctx, sh, false)
		}()
	}
}

// OpenWorkspaceRoot opens the mobile workspace previously checked by the
// native bridge as belonging to app storage. The caller closes the handle.
func (s *Service) OpenWorkspaceRoot(id, expectedPath string) (*os.Root, error) {
	sh, ok := s.store.Share(id)
	if !ok || sh.Path != expectedPath {
		return nil, errors.New("工作区已变更")
	}
	if sh.RootID == "" {
		// Only first-use migration needs serialization with synchronization.
		lock := s.lockFor(id)
		lock.Lock()
		defer lock.Unlock()
	}
	_, volume, err := s.openVolume(s.ctx, sh, false)
	if err != nil {
		return nil, err
	}
	return volume.dir, nil
}
