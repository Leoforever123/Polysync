package service

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
	"polysync/internal/store"
)

func (s *Service) ShareInvitations() []model.ShareInvitation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.ShareInvitation, 0, len(s.shareInvitations))
	for _, invitation := range s.shareInvitations {
		result = append(result, invitation)
	}
	return result
}

func (s *Service) InviteShare(ctx context.Context, deviceID, name, path string, auto bool, interval int) (model.Share, error) {
	device, paired := s.store.PairedDevice(deviceID)
	if !paired {
		return model.Share{}, errors.New("只能邀请已配对设备")
	}
	address := s.bestDeviceAddress(device)
	if address == "" {
		return model.Share{}, errors.New("配对设备当前没有可用地址")
	}
	share := model.Share{ID: store.RandomID(), Name: name, Path: path, PeerDeviceID: deviceID, PeerAddress: address, State: "pending", AutoSync: auto, IntervalSeconds: interval}
	if err := s.store.AddShare(share); err != nil {
		return model.Share{}, err
	}
	s.RegisterShare(share.ID)
	committed := false
	defer func() {
		if !committed {
			_ = s.store.DeleteShare(share.ID)
			s.RemoveShare(share.ID)
		}
	}()
	invitationID := randomToken(16)
	connection, err := s.dialTLS(ctx, address, &device)
	if err != nil {
		return model.Share{}, err
	}
	defer connection.Close()
	framer := protocol.NewFramer(connection)
	config := s.store.Config()
	hello := protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "share_invite", DeviceID: config.DeviceID, DeviceName: config.DeviceName, ListenPort: s.listenPort}
	if err := framer.WriteJSON(hello); err != nil {
		return model.Share{}, err
	}
	if err := framer.WriteJSON(protocol.ShareInvite{Type: "share_invite", ID: invitationID, ShareID: share.ID, Name: share.Name}); err != nil {
		return model.Share{}, err
	}
	var result protocol.Result
	if err := framer.ReadJSON(&result); err != nil {
		return model.Share{}, err
	}
	if !result.OK {
		return model.Share{}, errors.New(result.Message)
	}
	committed = true
	s.log("info", share.ID, "已向 “"+device.Name+"” 发送文件夹同步邀请")
	return share, nil
}

func (s *Service) AcceptShareInvitation(ctx context.Context, invitationID, path string, auto bool, interval int) (model.Share, error) {
	s.mu.RLock()
	invitation, exists := s.shareInvitations[invitationID]
	s.mu.RUnlock()
	if !exists {
		return model.Share{}, errors.New("同步邀请不存在")
	}
	device, paired := s.store.PairedDevice(invitation.DeviceID)
	if !paired {
		return model.Share{}, errors.New("邀请设备不再处于配对状态")
	}
	address := invitation.Address
	if address == "" {
		address = s.bestDeviceAddress(device)
	}
	share := model.Share{ID: invitation.ShareID, Name: invitation.Name, Path: path, PeerDeviceID: invitation.DeviceID, PeerAddress: address, State: "active", AutoSync: auto, IntervalSeconds: interval}
	if err := s.store.AddShare(share); err != nil {
		return model.Share{}, err
	}
	s.RegisterShare(share.ID)
	committed := false
	defer func() {
		if !committed {
			_ = s.store.DeleteShare(share.ID)
			s.RemoveShare(share.ID)
		}
	}()
	connection, err := s.dialTLS(ctx, address, &device)
	if err != nil {
		return model.Share{}, err
	}
	defer connection.Close()
	framer := protocol.NewFramer(connection)
	config := s.store.Config()
	hello := protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "share_accept", DeviceID: config.DeviceID, DeviceName: config.DeviceName, ListenPort: s.listenPort}
	if err := framer.WriteJSON(hello); err != nil {
		return model.Share{}, err
	}
	if err := framer.WriteJSON(protocol.ShareAccept{Type: "share_accept", ID: invitation.ID, ShareID: invitation.ShareID}); err != nil {
		return model.Share{}, err
	}
	var result protocol.Result
	if err := framer.ReadJSON(&result); err != nil {
		return model.Share{}, err
	}
	if !result.OK {
		return model.Share{}, errors.New(result.Message)
	}
	committed = true
	s.mu.Lock()
	delete(s.shareInvitations, invitationID)
	s.mu.Unlock()
	_ = s.store.RemoveShareInvitation(invitationID)
	s.log("success", share.ID, "已接受来自 “"+device.Name+"” 的同步邀请")
	go func() { _ = s.SyncShare(context.Background(), share.ID, true) }()
	return share, nil
}

func (s *Service) handleShareInvite(connection *tls.Conn, framer *protocol.Framer, hello protocol.Hello) {
	device, err := s.requirePaired(connection, hello.DeviceID)
	if err != nil {
		_ = writeError(framer, "unpaired", err.Error())
		return
	}
	var request protocol.ShareInvite
	if err := framer.ReadJSON(&request); err != nil {
		return
	}
	if request.Type != "share_invite" || request.ID == "" || request.ShareID == "" || request.Name == "" {
		_ = writeError(framer, "invite", "无效的文件夹同步邀请")
		return
	}
	if _, exists := s.store.Share(request.ShareID); exists {
		_ = writeError(framer, "invite", "同步空间已经存在")
		return
	}
	address := remoteAddress(connection.RemoteAddr(), hello.ListenPort)
	invitation := model.ShareInvitation{ID: request.ID, ShareID: request.ShareID, Name: request.Name, DeviceID: hello.DeviceID, DeviceName: device.Name, Address: address, CreatedAt: time.Now()}
	if err := s.store.SaveShareInvitation(invitation); err != nil {
		_ = writeError(framer, "invite", err.Error())
		return
	}
	s.mu.Lock()
	s.shareInvitations[request.ID] = invitation
	s.mu.Unlock()
	s.log("info", "", "收到来自 “"+device.Name+"” 的文件夹同步邀请")
	_ = framer.WriteJSON(protocol.Result{Type: "result", OK: true})
}

func (s *Service) handleShareAccept(connection *tls.Conn, framer *protocol.Framer, hello protocol.Hello) {
	device, err := s.requirePaired(connection, hello.DeviceID)
	if err != nil {
		_ = writeError(framer, "unpaired", err.Error())
		return
	}
	var request protocol.ShareAccept
	if err := framer.ReadJSON(&request); err != nil {
		return
	}
	share, exists := s.store.Share(request.ShareID)
	if !exists || share.PeerDeviceID != hello.DeviceID || share.State != "pending" {
		_ = writeError(framer, "share", "没有对应的待处理同步空间")
		return
	}
	share.State = "active"
	share.PeerAddress = remoteAddress(connection.RemoteAddr(), hello.ListenPort)
	if err := s.store.UpdateShare(share); err != nil {
		_ = writeError(framer, "share", err.Error())
		return
	}
	_ = framer.WriteJSON(protocol.Result{Type: "result", OK: true})
	s.log("success", share.ID, "“"+device.Name+"” 已接受文件夹同步邀请")
}

func (s *Service) requirePaired(connection *tls.Conn, deviceID string) (model.PairedDevice, error) {
	device, paired := s.store.PairedDevice(deviceID)
	if !paired {
		return model.PairedDevice{}, errors.New("设备尚未配对")
	}
	peerKey, err := peerIdentity(connection)
	if err != nil || peerKey != device.PublicKey {
		return model.PairedDevice{}, errors.New("设备身份不匹配")
	}
	return device, nil
}

func (s *Service) bestDeviceAddress(device model.PairedDevice) string {
	if s.discovery != nil {
		if nearby, exists := s.discovery.Device(device.ID); exists && nearby.Online && len(nearby.Addresses) > 0 {
			return nearby.Addresses[0]
		}
	}
	return pairedAddress(device)
}
