package service

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"sort"
	"time"

	"polysync/internal/model"
	"polysync/internal/protocol"
)

type pendingPair struct {
	Request     model.PairingRequest
	ClientNonce string
	ServerNonce string
	Attempts    int
	Approved    bool
}

type outboundPair struct {
	SessionID   string
	RequestID   string
	Address     string
	DeviceID    string
	DeviceName  string
	PublicKey   string
	ClientNonce string
	ServerNonce string
	ExpiresAt   time.Time
}

func (s *Service) NearbyDevices() []model.NearbyDevice {
	paired := make(map[string]bool)
	for _, device := range s.store.Config().PairedDevices {
		paired[device.ID] = true
	}
	if s.discovery == nil {
		return nil
	}
	devices := s.discovery.Nearby(paired)
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	return devices
}

func (s *Service) PairingRequests() []model.PairingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	result := make([]model.PairingRequest, 0, len(s.pairingRequests))
	for id, request := range s.pairingRequests {
		if now.After(request.Request.ExpiresAt) {
			delete(s.pairingRequests, id)
			continue
		}
		result = append(result, request.Request)
		if !request.Approved {
			result[len(result)-1].Code = ""
		}
	}
	return result
}

func (s *Service) ApprovePairingRequest(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	request, exists := s.pairingRequests[id]
	if !exists || time.Now().After(request.Request.ExpiresAt) {
		return errors.New("配对请求不存在或已经过期")
	}
	request.Approved = true
	s.pairingRequests[id] = request
	return nil
}

func (s *Service) RejectPairingRequest(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pairingRequests, id)
}

func (s *Service) StartPairing(ctx context.Context, deviceID, address string) (string, error) {
	if deviceID != "" {
		if _, paired := s.store.PairedDevice(deviceID); paired {
			return "", errors.New("该设备已经配对")
		}
	}
	var nearby model.NearbyDevice
	var exists bool
	if deviceID != "" && s.discovery != nil {
		nearby, exists = s.discovery.Device(deviceID)
	}
	if !exists && address != "" {
		var err error
		nearby, err = s.identifyDevice(ctx, address)
		if err != nil {
			return "", err
		}
		exists = true
	}
	if !exists {
		return "", errors.New("附近设备已离线，请重新发现或填写地址")
	}
	if _, paired := s.store.PairedDevice(nearby.ID); paired {
		return "", errors.New("该设备已经配对")
	}
	return s.startPairingDevice(ctx, nearby, address)
}

func (s *Service) identifyDevice(ctx context.Context, address string) (model.NearbyDevice, error) {
	connection, err := s.dialTLS(ctx, address, nil)
	if err != nil {
		return model.NearbyDevice{}, fmt.Errorf("连接设备失败: %w", err)
	}
	defer connection.Close()
	peerKey, err := peerIdentity(connection)
	if err != nil {
		return model.NearbyDevice{}, err
	}
	framer := protocol.NewFramer(connection)
	config := s.store.Config()
	if err := framer.WriteJSON(protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "identify", DeviceID: config.DeviceID, DeviceName: config.DeviceName, ListenPort: s.listenPort}); err != nil {
		return model.NearbyDevice{}, err
	}
	var result protocol.PairResult
	if err := framer.ReadJSON(&result); err != nil {
		return model.NearbyDevice{}, err
	}
	if !result.OK || result.PublicKey != peerKey || result.DeviceID == "" {
		return model.NearbyDevice{}, errors.New("对端没有返回有效的 PolySync 设备身份")
	}
	return model.NearbyDevice{ID: result.DeviceID, Name: result.DeviceName, PublicKey: result.PublicKey, Addresses: []string{address}, Online: true, LastSeen: time.Now()}, nil
}

func (s *Service) startPairingDevice(ctx context.Context, nearby model.NearbyDevice, address string) (string, error) {
	if address == "" && len(nearby.Addresses) > 0 {
		address = nearby.Addresses[0]
	}
	if address == "" {
		return "", errors.New("设备没有可连接地址")
	}
	connection, err := s.dialTLS(ctx, address, nil)
	if err != nil {
		return "", fmt.Errorf("连接配对设备失败: %w", err)
	}
	defer connection.Close()
	peerKey, err := peerIdentity(connection)
	if err != nil {
		return "", err
	}
	if peerKey != nearby.PublicKey {
		return "", errors.New("mDNS 身份与设备证书不一致")
	}
	framer := protocol.NewFramer(connection)
	config := s.store.Config()
	hello := protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "pair_request", DeviceID: config.DeviceID, DeviceName: config.DeviceName, ListenPort: s.listenPort}
	if err := framer.WriteJSON(hello); err != nil {
		return "", err
	}
	requestID := randomToken(16)
	clientNonce := randomToken(32)
	if err := framer.WriteJSON(protocol.PairRequest{Type: "pair_request", RequestID: requestID, ClientNonce: clientNonce, DeviceID: config.DeviceID, DeviceName: config.DeviceName, PublicKey: config.IdentityPublicKey}); err != nil {
		return "", err
	}
	var challenge protocol.PairChallenge
	if err := framer.ReadJSON(&challenge); err != nil {
		return "", err
	}
	if challenge.Type != "pair_challenge" || challenge.RequestID != requestID || challenge.DeviceID != nearby.ID || challenge.PublicKey != peerKey {
		return "", errors.New("对端返回了无效的配对挑战")
	}
	sessionID := randomToken(16)
	s.mu.Lock()
	s.outboundPairs[sessionID] = outboundPair{
		SessionID: sessionID, RequestID: requestID, Address: address, DeviceID: nearby.ID, DeviceName: challenge.DeviceName,
		PublicKey: peerKey, ClientNonce: clientNonce, ServerNonce: challenge.ServerNonce, ExpiresAt: time.Unix(challenge.ExpiresAt, 0),
	}
	s.mu.Unlock()
	s.log("info", "", "已向 “"+challenge.DeviceName+"” 发起配对请求")
	return sessionID, nil
}

func (s *Service) ConfirmPairing(ctx context.Context, sessionID, code string) error {
	s.mu.RLock()
	session, exists := s.outboundPairs[sessionID]
	s.mu.RUnlock()
	if !exists || time.Now().After(session.ExpiresAt) {
		return errors.New("配对请求不存在或已经过期")
	}
	config := s.store.Config()
	if len(code) != 6 {
		return errors.New("请输入六位配对验证码")
	}
	connection, err := s.dialTLS(ctx, session.Address, nil)
	if err != nil {
		return err
	}
	defer connection.Close()
	peerKey, err := peerIdentity(connection)
	if err != nil || peerKey != session.PublicKey {
		return errors.New("配对设备身份发生变化")
	}
	framer := protocol.NewFramer(connection)
	hello := protocol.Hello{Type: "hello", Protocol: model.ProtocolVersion, Operation: "pair_confirm", DeviceID: config.DeviceID, DeviceName: config.DeviceName, ListenPort: s.listenPort}
	if err := framer.WriteJSON(hello); err != nil {
		return err
	}
	_, privateKey, err := s.store.Identity()
	if err != nil {
		return err
	}
	transcript := pairingTranscript(config.IdentityPublicKey, session.PublicKey, session.ClientNonce, session.ServerNonce, code)
	confirm := protocol.PairConfirm{Type: "pair_confirm", RequestID: session.RequestID, Code: code, Signature: signPairing(privateKey, transcript, "client")}
	if err := framer.WriteJSON(confirm); err != nil {
		return err
	}
	var result protocol.PairResult
	if err := framer.ReadJSON(&result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New(result.Message)
	}
	if result.DeviceID != session.DeviceID || result.PublicKey != session.PublicKey || !verifyPairing(session.PublicKey, result.Signature, transcript, "server") {
		return errors.New("无法验证对端的配对确认")
	}
	if err := s.store.SavePairedDevice(model.PairedDevice{ID: session.DeviceID, Name: result.DeviceName, PublicKey: session.PublicKey, Addresses: []string{session.Address}, PairedAt: time.Now(), LastSeen: time.Now()}); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.outboundPairs, sessionID)
	s.mu.Unlock()
	s.log("success", "", "已与 “"+result.DeviceName+"” 完成安全配对")
	return nil
}

func (s *Service) handlePairRequest(connection *tls.Conn, framer *protocol.Framer, hello protocol.Hello) {
	var request protocol.PairRequest
	if err := framer.ReadJSON(&request); err != nil {
		return
	}
	peerKey, err := peerIdentity(connection)
	if err != nil || request.Type != "pair_request" || request.DeviceID != hello.DeviceID || request.PublicKey != peerKey || request.RequestID == "" {
		_ = writeError(framer, "pairing", "无效的配对请求")
		return
	}
	if _, exists := s.store.PairedDevice(request.DeviceID); exists {
		_ = writeError(framer, "paired", "设备已经配对")
		return
	}
	config := s.store.Config()
	serverNonce := randomToken(32)
	code := randomPairCode()
	expires := time.Now().Add(60 * time.Second)
	pending := pendingPair{Request: model.PairingRequest{
		ID: request.RequestID, DeviceID: request.DeviceID, DeviceName: request.DeviceName, PublicKey: request.PublicKey,
		Address: remoteAddress(connection.RemoteAddr(), hello.ListenPort), Code: code, ExpiresAt: expires,
	}, ClientNonce: request.ClientNonce, ServerNonce: serverNonce}
	s.mu.Lock()
	s.pairingRequests[request.RequestID] = pending
	s.mu.Unlock()
	s.log("info", "", "收到来自 “"+request.DeviceName+"” 的配对请求")
	_ = framer.WriteJSON(protocol.PairChallenge{Type: "pair_challenge", RequestID: request.RequestID, ServerNonce: serverNonce, DeviceID: config.DeviceID, DeviceName: config.DeviceName, PublicKey: config.IdentityPublicKey, ExpiresAt: expires.Unix()})
}

func (s *Service) handlePairConfirm(connection *tls.Conn, framer *protocol.Framer, hello protocol.Hello) {
	var confirm protocol.PairConfirm
	if err := framer.ReadJSON(&confirm); err != nil {
		return
	}
	s.mu.Lock()
	pending, exists := s.pairingRequests[confirm.RequestID]
	if exists {
		pending.Attempts++
		s.pairingRequests[confirm.RequestID] = pending
	}
	s.mu.Unlock()
	if !exists || !pending.Approved || time.Now().After(pending.Request.ExpiresAt) || pending.Attempts > 3 {
		_ = framer.WriteJSON(protocol.PairResult{Type: "pair_result", OK: false, Message: "配对请求不存在、已过期或尝试次数过多"})
		return
	}
	peerKey, err := peerIdentity(connection)
	if err != nil || peerKey != pending.Request.PublicKey || hello.DeviceID != pending.Request.DeviceID {
		_ = framer.WriteJSON(protocol.PairResult{Type: "pair_result", OK: false, Message: "发起设备身份不匹配"})
		return
	}
	config := s.store.Config()
	expectedCode := pending.Request.Code
	transcript := pairingTranscript(pending.Request.PublicKey, config.IdentityPublicKey, pending.ClientNonce, pending.ServerNonce, confirm.Code)
	if subtle.ConstantTimeCompare([]byte(confirm.Code), []byte(expectedCode)) != 1 || !verifyPairing(pending.Request.PublicKey, confirm.Signature, transcript, "client") {
		_ = framer.WriteJSON(protocol.PairResult{Type: "pair_result", OK: false, Message: "验证码或设备签名无效"})
		return
	}
	device := model.PairedDevice{ID: pending.Request.DeviceID, Name: pending.Request.DeviceName, PublicKey: pending.Request.PublicKey, Addresses: []string{pending.Request.Address}, PairedAt: time.Now(), LastSeen: time.Now()}
	if err := s.store.SavePairedDevice(device); err != nil {
		_ = framer.WriteJSON(protocol.PairResult{Type: "pair_result", OK: false, Message: err.Error()})
		return
	}
	_, privateKey, _ := s.store.Identity()
	result := protocol.PairResult{Type: "pair_result", OK: true, DeviceID: config.DeviceID, DeviceName: config.DeviceName, PublicKey: config.IdentityPublicKey, Signature: signPairing(privateKey, transcript, "server")}
	_ = framer.WriteJSON(result)
	s.mu.Lock()
	delete(s.pairingRequests, confirm.RequestID)
	s.mu.Unlock()
	s.log("success", "", "已与 “"+pending.Request.DeviceName+"” 完成安全配对")
}

func pairedAddress(device model.PairedDevice) string {
	if len(device.Addresses) == 0 {
		return ""
	}
	return device.Addresses[0]
}
