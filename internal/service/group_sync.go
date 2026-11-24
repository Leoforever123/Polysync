package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
	"polysync/internal/syncer"
)

type groupFrame struct {
	Resolved []string        `json:"resolved,omitempty"`
	Type     string          `json:"type"`
	Message  string          `json:"message,omitempty"`
	Records  model.Revisions `json:"records,omitempty"`
	Need     []string        `json:"need,omitempty"`
}

func readGroup(f *protocol.Framer, stage string) (groupFrame, error) {
	var frame groupFrame
	if err := f.ReadJSON(&frame); err != nil {
		return frame, err
	}
	if frame.Type != stage {
		if frame.Type == "result" && frame.Message != "" {
			return frame, errors.New(frame.Message)
		}
		return frame, fmt.Errorf("文件夹同步阶段不一致，预期 %s", stage)
	}
	return frame, nil
}

func (s *Service) syncGroup(ctx context.Context, group model.Share, manual bool) error {
	s.updateStatus(group.ID, func(st *model.RuntimeStatus) {
		st.State = "syncing"
		st.Message = "正在联系文件夹成员…"
		st.LastAttempt = time.Now()
	})
	var failures []error
	count := 0
	for _, member := range group.Members {
		if member.State != "active" {
			continue
		}
		if ctx.Err() != nil {
			return s.syncFailed(group.ID, ctx.Err())
		}
		peer, ok := s.store.PairedDevice(member.DeviceID)
		if !ok {
			failures = append(failures, errors.New("成员尚未配对"))
			continue
		}
		var err error
		if member.Legacy {
			legacy := group
			legacy.LocalRole = ""
			legacy.Members = nil
			legacy.PeerDeviceID = member.DeviceID
			legacy.PeerAddress = member.Address
			err = s.syncLegacyLocked(ctx, legacy, manual)
		} else {
			err = s.dialGroup(ctx, group, member, peer)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s：%w", peer.Name, err))
		} else {
			count++
		}
	}
	if len(failures) > 0 {
		return s.syncFailed(group.ID, fmt.Errorf("已与 %d 台设备同步，其他连接等待重试：%w", count, errors.Join(failures...)))
	}
	if count == 0 {
		return s.syncFailed(group.ID, errors.New("等待其他设备接受文件夹邀请"))
	}
	s.updateStatus(group.ID, func(st *model.RuntimeStatus) {
		st.State = "synced"
		st.LastSync = time.Now()
		st.Message = fmt.Sprintf("已与 %d 台设备同步", count)
	})
	return nil
}

func (s *Service) dialGroup(ctx context.Context, sh model.Share, m model.ShareMember, peer model.PairedDevice) error {
	var addrs []string
	for _, addr := range append([]string{s.bestDeviceAddress(peer), m.Address}, peer.Addresses...) {
		if addr != "" && !slices.Contains(addrs, addr) {
			addrs = append(addrs, addr)
		}
	}
	var conn *tls.Conn
	var err error
	for _, addr := range addrs {
		conn, err = s.dialTLS(ctx, addr, &peer)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if conn == nil {
		if err != nil {
			return err
		}
		return errors.New("设备尚未提供连接地址")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	stopService := context.AfterFunc(s.ctx, func() { conn.Close() })
	defer stopService()
	conn.SetDeadline(time.Now().Add(sessionTimeout))
	f := protocol.NewFramer(conn)
	cfg := s.store.Config()
	if err := f.WriteJSON(protocol.Hello{Type: "hello", Operation: "group_sync", Protocol: model.ProtocolVersion, ShareID: sh.ID, DeviceID: cfg.DeviceID, DeviceName: cfg.DeviceName, ListenPort: s.listenPort}); err != nil {
		return err
	}
	if _, err := readGroup(f, "group_ready"); err != nil {
		return err
	}
	return s.groupSession(ctx, f, sh, m, peer, true)
}

func (s *Service) receiveGroup(ctx context.Context, conn *tls.Conn, f *protocol.Framer, hello protocol.Hello) error {
	peer, err := s.requirePaired(conn, hello.DeviceID)
	if err != nil {
		return err
	}
	if _, ok := s.store.Share(hello.ShareID); !ok {
		return errors.New("此设备未获准同步这个文件夹")
	}
	lock := s.lockFor(hello.ShareID)
	if !lock.TryLock() {
		return ErrBusy
	}
	defer lock.Unlock()
	sh, ok := s.store.Share(hello.ShareID)
	m, member := shareMember(sh, peer.ID)
	if !ok || sh.LocalRole == "" || sh.State != "active" || !member || m.State != "active" || m.Legacy {
		return errors.New("此设备未获准同步这个文件夹")
	}
	conn.SetDeadline(time.Now().Add(sessionTimeout))
	if err := f.WriteJSON(groupFrame{Type: "group_ready"}); err != nil {
		return err
	}
	err = s.groupSession(ctx, f, sh, m, peer, false)
	if err != nil {
		return s.syncFailed(sh.ID, err)
	}
	s.updateStatus(sh.ID, func(st *model.RuntimeStatus) {
		st.State = "synced"
		st.LastSync = time.Now()
		st.PeerName = peer.Name
		st.Message = "已与 " + peer.Name + " 同步"
	})
	return nil
}

func groupObjects(records model.Revisions) map[string]model.Entry {
	objects := make(map[string]model.Entry)
	for _, r := range records {
		if r.Hash != "" {
			objects[r.Hash] = r.Entry
		}
	}
	return objects
}

func (s *Service) sendGroupObjects(ctx context.Context, f *protocol.Framer, records model.Revisions) error {
	request, err := readGroup(f, "group_need")
	if err != nil {
		return err
	}
	offered := groupObjects(records)
	for _, hash := range request.Need {
		entry, ok := offered[hash]
		if !ok {
			return errors.New("请求的内容不在此文件夹公开的版本中")
		}
		delete(offered, hash)
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := os.Open(s.store.ObjectPath(hash))
		if err != nil {
			return err
		}
		err = f.CopyFrom(file, entry.Size)
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) fetchGroupObjects(ctx context.Context, f *protocol.Framer, required, remote map[string]model.Entry) error {
	var need []string
	for hash := range required {
		_, err := os.Stat(s.store.ObjectPath(hash))
		if err == nil {
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, ok := remote[hash]; !ok {
			return errors.New("需要的文件版本暂不可用，请重新同步")
		}
		need = append(need, hash)
	}
	slices.Sort(need)
	if err := f.WriteJSON(groupFrame{Type: "group_need", Need: need}); err != nil {
		return err
	}
	for _, h := range need {
		if err := s.store.ReceiveGroupObject(ctx, remote[h], f.CopyTo); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) groupSession(ctx context.Context, f *protocol.Framer, sh model.Share, member model.ShareMember, peer model.PairedDevice, client bool) error {
	self := s.store.Config()
	localRO, peerRO, err := s.groupPolicy(f, sh, member, client)
	if err != nil {
		return err
	}
	manifest, err := syncer.Scan(ctx, sh.Path)
	if err != nil {
		return err
	}
	files := revisionManifest(manifest)
	state, err := s.store.GroupState(sh.ReplicaID)
	if err != nil {
		return err
	}
	if err = observeGroup(&state, files, sh.ReplicaID, !localRO); err != nil {
		return err
	}
	if err = s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
		return err
	}
	if !localRO {
		if err = s.cacheObjects(sh.Path, manifest); err != nil {
			return err
		}
	}
	offer := state.Records
	var resolutions []string
	if localRO {
		offer = nil
	} else {
		resolutions, err = s.groupResolvedIDs(sh, state)
		if err != nil {
			return err
		}
	}
	send := func() error {
		return f.WriteJSON(groupFrame{Type: "group_offer", Records: offer, Resolved: resolutions})
	}
	if client {
		if err = send(); err != nil {
			return err
		}
	}
	frame, err := readGroup(f, "group_offer")
	if err != nil {
		return err
	}
	if err = validateGroupDecisions(frame.Resolved, peerRO); err != nil {
		return err
	}
	if err = validateGroupOffer(frame.Records, peerRO); err != nil {
		return err
	}
	if !client {
		if err = send(); err != nil {
			return err
		}
	}
	var desired model.Revisions
	var conflicts []model.PlanConflict
	switch {
	case localRO:
		desired = frame.Records
		if desired == nil {
			desired = make(model.Revisions)
		}
	case peerRO:
		desired = state.Records
	default:
		desired, conflicts, err = mergeGroup(state.Records, frame.Records)
		if err != nil {
			return err
		}
	}
	required := make(map[string]model.Entry)
	expected := make(map[string]string, len(files))
	for n, e := range files {
		expected[n] = e.Hash
	}
	for n, rev := range desired {
		// A follower may keep a private divergence until its upstream publishes
		// a new revision. Private files are never advertised or requested.
		prior := state.Records[n]
		if localRO && prior.Hash == rev.Hash {
			continue
		}
		if rev.Hash == "" {
			delete(expected, n)
		} else {
			expected[n] = rev.Hash
			if files[n].Hash != rev.Hash {
				required[rev.Hash] = rev.Entry
			}
		}
	}
	fetch := func() error { return s.fetchGroupObjects(ctx, f, required, groupObjects(frame.Records)) }
	if client {
		if err = fetch(); err == nil {
			err = s.sendGroupObjects(ctx, f, offer)
		}
	} else {
		if err = s.sendGroupObjects(ctx, f, offer); err == nil {
			err = fetch()
		}
	}
	if err != nil {
		return err
	}

	state.Pending = desired
	if err = s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
		return err
	}
	history := filepath.Join(s.store.Dir(), "history", sh.ID)
	for _, name := range revisionPaths(desired) {
		if _, exists := expected[name]; !exists && files[name].Hash != "" {
			if err = syncer.DeletePlannedFile(ctx, sh.Path, name, history, files[name].Hash); err != nil {
				return err
			}
		}
	}
	for _, name := range revisionPaths(desired) {
		hash := expected[name]
		if hash == "" || files[name].Hash == hash {
			continue
		}
		e := desired[name].Entry
		file, err := os.Open(s.store.ObjectPath(hash))
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		e.Size = info.Size()
		err = syncer.InstallPlannedContent(ctx, sh.Path, history, e, files[name].Hash, func(dst io.Writer, n int64) error { _, err := io.CopyN(dst, file, n); return err })
		file.Close()
		if err != nil {
			return err
		}
	}
	final, err := syncer.Scan(ctx, sh.Path)
	if err != nil {
		return err
	}
	if len(final) != len(expected) {
		return errors.New("同步期间目录有新变化，请重新同步")
	}
	for _, e := range final {
		if expected[e.Path] != e.Hash {
			return fmt.Errorf("同步期间文件有变化：%s", e.Path)
		}
	}
	for _, entry := range final {
		if rev, ok := desired[entry.Path]; ok && rev.Hash == entry.Hash && rev.Size != entry.Size {
			rev.Size = entry.Size
			desired[entry.Path] = rev
		}
	}
	if err = s.saveGroupConflicts(sh, conflicts, model.PairedDevice{ID: self.DeviceID, Name: self.DeviceName}, peer); err != nil {
		return err
	}
	state.Records = desired
	state.Pending = nil
	if err = s.store.SaveGroupState(sh.ReplicaID, state); err != nil {
		return err
	}
	if err = s.store.SaveBaseline(sh.ID, peer.ID, final); err != nil {
		return err
	}
	if client {
		if err = f.WriteJSON(groupFrame{Type: "group_applied"}); err != nil {
			return err
		}
		if _, err = readGroup(f, "group_done"); err != nil {
			return err
		}
	} else {
		if _, err = readGroup(f, "group_applied"); err != nil {
			return err
		}
	}
	if !client {
		if err = f.WriteJSON(groupFrame{Type: "group_done"}); err != nil {
			return err
		}
	}
	if !localRO {
		if err = s.autoResolveConflicts(sh.ID, final); err != nil {
			return err
		}
	}
	if err = s.commitGroupDecisions(sh, state, frame.Resolved); err != nil {
		return err
	}
	// The peer must finish its durable status update before the caller reports
	// success, even when no file content was transferred for a keep decision.
	if client {
		if err = f.WriteJSON(groupFrame{Type: "group_committed"}); err != nil {
			return err
		}
		_, err = readGroup(f, "group_committed")
		return err
	}
	if _, err = readGroup(f, "group_committed"); err != nil {
		return err
	}
	return f.WriteJSON(groupFrame{Type: "group_committed"})
}
