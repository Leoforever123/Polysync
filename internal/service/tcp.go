package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
	"polysync/internal/syncer"
)

const sessionTimeout = 2 * time.Hour

type responseFrame struct {
	Type             string     `json:"type"`
	Nonce            string     `json:"nonce"`
	ServerDeviceID   string     `json:"serverDeviceId"`
	ServerDeviceName string     `json:"serverDeviceName"`
	Plan             model.Plan `json:"plan"`
	OK               bool       `json:"ok"`
	Code             string     `json:"code"`
	Message          string     `json:"message"`
	Sent             int        `json:"sent"`
	Received         int        `json:"received"`
	Conflicts        int        `json:"conflicts"`
}

func (s *Service) SyncShare(ctx context.Context, shareID string, manual bool) error {
	share, exists := s.store.Share(shareID)
	if !exists {
		return fmt.Errorf("sync folder not found")
	}
	lock := s.lockFor(shareID)
	if !lock.TryLock() {
		return ErrBusy
	}
	defer lock.Unlock()

	now := time.Now()
	s.updateStatus(shareID, func(status *model.RuntimeStatus) {
		status.State = "syncing"
		status.Message = "正在连接设备…"
		status.LastAttempt = now
	})
	if share.PeerAddress == "" {
		return s.syncFailed(shareID, errors.New("尚未设置对端地址"))
	}

	dialer := net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", share.PeerAddress)
	if err != nil {
		return s.syncFailed(shareID, fmt.Errorf("连接 %s 失败: %w", share.PeerAddress, err))
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(sessionTimeout))
	framer := protocol.NewFramer(connection)
	config := s.store.Config()
	hello := protocol.Hello{
		Type: "hello", Protocol: model.ProtocolVersion, ShareID: share.ID, DeviceID: config.DeviceID,
		DeviceName: config.DeviceName, ListenPort: s.listenPort, Manual: manual,
	}
	if err := framer.WriteJSON(hello); err != nil {
		return s.syncFailed(shareID, err)
	}
	var response responseFrame
	if err := framer.ReadJSON(&response); err != nil {
		return s.syncFailed(shareID, err)
	}
	if response.Type != "challenge" {
		return s.syncFailed(shareID, remoteError(response))
	}

	s.updateStatus(shareID, func(status *model.RuntimeStatus) {
		status.Message = "正在扫描文件…"
		status.PeerName = response.ServerDeviceName
	})
	manifest, err := syncer.Scan(ctx, share.Path)
	if err != nil {
		return s.syncFailed(shareID, err)
	}
	request := protocol.SyncRequest{
		Type: "sync_request", Auth: protocol.Authentication(share.Secret, response.Nonce, share.ID, config.DeviceID), Manifest: manifest,
	}
	if err := framer.WriteJSON(request); err != nil {
		return s.syncFailed(shareID, err)
	}
	response = responseFrame{}
	if err := framer.ReadJSON(&response); err != nil {
		return s.syncFailed(shareID, err)
	}
	if response.Type != "plan" {
		return s.syncFailed(shareID, remoteError(response))
	}
	plan := response.Plan
	historyRoot := filepath.Join(s.store.Dir(), "history", share.ID)
	s.updateStatus(shareID, func(status *model.RuntimeStatus) { status.Message = "正在传输文件…" })

	for _, transfer := range plan.ClientSends {
		if err := syncer.SendFile(ctx, framer, share.Path, transfer); err != nil {
			return s.syncFailed(shareID, err)
		}
	}
	if err := framer.WriteJSON(protocol.Marker{Type: "uploads_done"}); err != nil {
		return s.syncFailed(shareID, err)
	}
	for _, filePath := range plan.ClientDeletes {
		if err := syncer.DeleteFile(share.Path, filePath, historyRoot); err != nil {
			return s.syncFailed(shareID, err)
		}
	}
	for _, transfer := range plan.ServerSends {
		if err := syncer.ReceiveFile(ctx, framer, share.Path, historyRoot, transfer); err != nil {
			return s.syncFailed(shareID, err)
		}
	}
	var complete protocol.Marker
	if err := framer.ReadJSON(&complete); err != nil || complete.Type != "transfers_done" {
		if err == nil {
			err = errors.New("unexpected transfer completion frame")
		}
		return s.syncFailed(shareID, err)
	}
	finalManifest, err := syncer.Scan(ctx, share.Path)
	if err != nil {
		return s.syncFailed(shareID, err)
	}
	if err := framer.WriteJSON(protocol.Ack{Type: "ack", Manifest: finalManifest}); err != nil {
		return s.syncFailed(shareID, err)
	}
	response = responseFrame{}
	if err := framer.ReadJSON(&response); err != nil {
		return s.syncFailed(shareID, err)
	}
	if response.Type != "result" || !response.OK {
		return s.syncFailed(shareID, remoteError(response))
	}
	if err := s.store.SaveBaseline(share.ID, response.ServerDeviceID, finalManifest); err != nil {
		return s.syncFailed(shareID, err)
	}
	s.syncSucceeded(shareID, response.ServerDeviceName, len(plan.ClientSends), len(plan.ServerSends), len(plan.Conflicts))
	return nil
}

func (s *Service) handleConnection(ctx context.Context, connection net.Conn) {
	_ = connection.SetDeadline(time.Now().Add(sessionTimeout))
	framer := protocol.NewFramer(connection)
	var hello protocol.Hello
	if err := framer.ReadJSON(&hello); err != nil {
		return
	}
	if hello.Type != "hello" || hello.Protocol != model.ProtocolVersion {
		_ = writeError(framer, "protocol", "协议版本不兼容")
		return
	}
	share, exists := s.store.Share(hello.ShareID)
	if !exists {
		_ = writeError(framer, "unknown_share", "此设备未配置该同步文件夹")
		return
	}
	nonce := randomNonce()
	config := s.store.Config()
	if err := framer.WriteJSON(protocol.Challenge{
		Type: "challenge", Nonce: nonce, ServerDeviceID: config.DeviceID, ServerDeviceName: config.DeviceName,
	}); err != nil {
		return
	}
	var request protocol.SyncRequest
	if err := framer.ReadJSON(&request); err != nil {
		return
	}
	if request.Type != "sync_request" || !protocol.VerifyAuthentication(request.Auth, share.Secret, nonce, share.ID, hello.DeviceID) {
		_ = writeError(framer, "auth", "同步码认证失败")
		return
	}
	s.learnPeer(share, hello, connection.RemoteAddr())
	lock := s.lockFor(share.ID)
	if !lock.TryLock() {
		_ = writeError(framer, "busy", "对端正在执行另一轮同步，请稍后重试")
		return
	}
	defer lock.Unlock()

	s.updateStatus(share.ID, func(status *model.RuntimeStatus) {
		status.State = "syncing"
		status.Message = "正在与 " + hello.DeviceName + " 同步…"
		status.PeerName = hello.DeviceName
		status.LastAttempt = time.Now()
	})
	serverManifest, err := syncer.Scan(ctx, share.Path)
	if err != nil {
		_ = writeError(framer, "scan", err.Error())
		s.syncFailed(share.ID, err)
		return
	}
	baseline, err := s.store.LoadBaseline(share.ID, hello.DeviceID)
	if err != nil {
		_ = writeError(framer, "state", err.Error())
		s.syncFailed(share.ID, err)
		return
	}
	plan := syncer.BuildPlan(serverManifest, request.Manifest, baseline, hello.DeviceName, time.Now())
	historyRoot := filepath.Join(s.store.Dir(), "history", share.ID)
	if err := framer.WriteJSON(protocol.PlanMessage{Type: "plan", Plan: plan}); err != nil {
		s.syncFailed(share.ID, err)
		return
	}
	for _, transfer := range plan.ClientSends {
		if err := syncer.ReceiveFile(ctx, framer, share.Path, historyRoot, transfer); err != nil {
			s.syncFailed(share.ID, err)
			return
		}
	}
	var uploadsDone protocol.Marker
	if err := framer.ReadJSON(&uploadsDone); err != nil || uploadsDone.Type != "uploads_done" {
		if err == nil {
			err = errors.New("unexpected upload completion frame")
		}
		s.syncFailed(share.ID, err)
		return
	}
	for _, filePath := range plan.ServerDeletes {
		if err := syncer.DeleteFile(share.Path, filePath, historyRoot); err != nil {
			_ = writeError(framer, "delete", err.Error())
			s.syncFailed(share.ID, err)
			return
		}
	}
	for _, transfer := range plan.ServerSends {
		if err := syncer.SendFile(ctx, framer, share.Path, transfer); err != nil {
			s.syncFailed(share.ID, err)
			return
		}
	}
	if err := framer.WriteJSON(protocol.Marker{Type: "transfers_done"}); err != nil {
		s.syncFailed(share.ID, err)
		return
	}
	var ack protocol.Ack
	if err := framer.ReadJSON(&ack); err != nil || ack.Type != "ack" {
		if err == nil {
			err = errors.New("unexpected sync acknowledgement")
		}
		s.syncFailed(share.ID, err)
		return
	}
	finalManifest, err := syncer.Scan(ctx, share.Path)
	if err != nil {
		_ = writeError(framer, "scan", err.Error())
		s.syncFailed(share.ID, err)
		return
	}
	if !manifestsEqual(finalManifest, ack.Manifest) {
		err := errors.New("同步期间文件发生变化，将在下一轮重试")
		_ = writeError(framer, "changed", err.Error())
		s.syncFailed(share.ID, err)
		return
	}
	if err := s.store.SaveBaseline(share.ID, hello.DeviceID, finalManifest); err != nil {
		_ = writeError(framer, "state", err.Error())
		s.syncFailed(share.ID, err)
		return
	}
	result := protocol.Result{
		Type: "result", OK: true, Message: "同步完成", Sent: len(plan.ServerSends), Received: len(plan.ClientSends), Conflicts: len(plan.Conflicts),
	}
	// Include peer identity in the final frame for baseline storage.
	if err := framer.WriteJSON(struct {
		protocol.Result
		ServerDeviceID   string `json:"serverDeviceId"`
		ServerDeviceName string `json:"serverDeviceName"`
	}{Result: result, ServerDeviceID: config.DeviceID, ServerDeviceName: config.DeviceName}); err != nil {
		return
	}
	s.syncSucceeded(share.ID, hello.DeviceName, len(plan.ServerSends), len(plan.ClientSends), len(plan.Conflicts))
}

func (s *Service) learnPeer(share model.Share, hello protocol.Hello, remote net.Addr) {
	address, ok := remote.(*net.TCPAddr)
	if !ok || hello.ListenPort <= 0 || hello.ListenPort > 65535 {
		return
	}
	peer := net.JoinHostPort(address.IP.String(), fmt.Sprintf("%d", hello.ListenPort))
	if share.PeerAddress == peer {
		return
	}
	share.PeerAddress = peer
	if err := s.store.SetPeerAddress(share.ID, peer); err == nil {
		s.log("info", share.ID, "已记住对端地址 "+peer)
	}
}

func (s *Service) syncFailed(shareID string, err error) error {
	s.updateStatus(shareID, func(status *model.RuntimeStatus) {
		status.State = "error"
		status.Message = err.Error()
	})
	s.log("error", shareID, err.Error())
	return err
}

func (s *Service) syncSucceeded(shareID, peerName string, sent, received, conflicts int) {
	now := time.Now()
	s.updateStatus(shareID, func(status *model.RuntimeStatus) {
		status.State = "synced"
		status.Message = "同步完成"
		status.LastSync = now
		status.FilesSent = sent
		status.FilesReceived = received
		status.Conflicts = conflicts
		status.PeerName = peerName
	})
	message := fmt.Sprintf("与 %s 同步完成：发送 %d，接收 %d", peerName, sent, received)
	if conflicts > 0 {
		message += fmt.Sprintf("，冲突 %d", conflicts)
	}
	s.log("success", shareID, message)
}

func writeError(framer *protocol.Framer, code, message string) error {
	return framer.WriteJSON(protocol.Result{Type: "result", OK: false, Code: code, Message: message})
}

func remoteError(response responseFrame) error {
	if response.Message == "" {
		return errors.New("对端返回了未知错误")
	}
	return errors.New(response.Message)
}

func randomNonce() string {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

func manifestsEqual(left, right []model.Entry) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]model.Entry(nil), left...)
	right = append([]model.Entry(nil), right...)
	sort.Slice(left, func(i, j int) bool { return left[i].Path < left[j].Path })
	sort.Slice(right, func(i, j int) bool { return right[i].Path < right[j].Path })
	for i := range left {
		if left[i].Path != right[i].Path || left[i].Hash != right[i].Hash || left[i].Size != right[i].Size {
			return false
		}
	}
	return true
}
