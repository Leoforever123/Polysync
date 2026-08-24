package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"polysync/internal/discovery"
	"polysync/internal/model"
	"polysync/internal/store"
)

var ErrBusy = errors.New("sync already in progress")

type Service struct {
	store            *store.Store
	mu               sync.RWMutex
	statuses         map[string]model.RuntimeStatus
	activities       []model.Activity
	locks            map[string]*sync.Mutex
	listener         net.Listener
	listenPort       int
	wg               sync.WaitGroup
	discovery        *discovery.Service
	certificate      tls.Certificate
	pairingRequests  map[string]pendingPair
	outboundPairs    map[string]outboundPair
	shareInvitations map[string]model.ShareInvitation
}

func New(dataStore *store.Store) *Service {
	service := &Service{
		store: dataStore, statuses: make(map[string]model.RuntimeStatus), locks: make(map[string]*sync.Mutex),
		pairingRequests: make(map[string]pendingPair), outboundPairs: make(map[string]outboundPair), shareInvitations: make(map[string]model.ShareInvitation),
	}
	for _, share := range dataStore.Config().Shares {
		service.statuses[share.ID] = model.RuntimeStatus{ShareID: share.ID, State: "idle"}
		service.locks[share.ID] = &sync.Mutex{}
	}
	for _, invitation := range dataStore.Config().ShareInvitations {
		service.shareInvitations[invitation.ID] = invitation
	}
	return service
}

func (s *Service) Store() *store.Store { return s.store }

func (s *Service) Start(ctx context.Context) error {
	config := s.store.Config()
	certificate, err := s.identityCertificate()
	if err != nil {
		return err
	}
	s.certificate = certificate
	listener, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.ListenAddr, err)
	}
	s.listener = listener
	if address, ok := listener.Addr().(*net.TCPAddr); ok {
		s.listenPort = address.Port
	}
	s.log("info", "", fmt.Sprintf("TCP 同步服务已监听 %s", listener.Addr()))
	discoveryService, discoveryErr := discovery.Start(ctx, config.DeviceID, config.DeviceName, config.IdentityPublicKey, s.listenPort)
	if discoveryErr != nil {
		s.log("error", "", "mDNS 发现不可用: "+discoveryErr.Error())
	} else {
		s.discovery = discoveryService
		s.log("info", "", "mDNS 设备发现已启动")
	}

	s.wg.Add(2)
	go s.acceptLoop(ctx)
	go s.autoLoop(ctx)
	return nil
}

func (s *Service) Stop() {
	if s.discovery != nil {
		s.discovery.Close()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
}

func (s *Service) ListenPort() int { return s.listenPort }

func (s *Service) Statuses() map[string]model.RuntimeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]model.RuntimeStatus, len(s.statuses))
	for key, value := range s.statuses {
		result[key] = value
	}
	return result
}

func (s *Service) Activities() []model.Activity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]model.Activity(nil), s.activities...)
	return result
}

func (s *Service) RegisterShare(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.locks[id]; !exists {
		s.locks[id] = &sync.Mutex{}
	}
	if _, exists := s.statuses[id]; !exists {
		s.statuses[id] = model.RuntimeStatus{ShareID: id, State: "idle"}
	}
}

func (s *Service) RemoveShare(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.statuses, id)
	delete(s.locks, id)
}

func (s *Service) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, exists := s.locks[id]
	if !exists {
		lock = &sync.Mutex{}
		s.locks[id] = lock
	}
	return lock
}

func (s *Service) updateStatus(id string, update func(*model.RuntimeStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.statuses[id]
	status.ShareID = id
	update(&status)
	s.statuses[id] = status
}

func (s *Service) log(level, shareID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activities = append([]model.Activity{{Time: time.Now(), ShareID: shareID, Level: level, Message: message}}, s.activities...)
	if len(s.activities) > 200 {
		s.activities = s.activities[:200]
	}
}

func (s *Service) acceptLoop(ctx context.Context) {
	defer s.wg.Done()
	go func() {
		<-ctx.Done()
		if s.listener != nil {
			_ = s.listener.Close()
		}
	}()
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			s.log("error", "", "接受 TCP 连接失败: "+err.Error())
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer connection.Close()
			tlsConnection := tls.Server(connection, s.serverTLSConfig())
			if err := tlsConnection.HandshakeContext(ctx); err != nil {
				return
			}
			s.handleConnection(ctx, tlsConnection)
		}()
	}
}

func (s *Service) autoLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			config := s.store.Config()
			statuses := s.Statuses()
			for _, share := range config.Shares {
				if !share.AutoSync || share.State != "active" || share.PeerDeviceID == "" {
					continue
				}
				interval := time.Duration(share.IntervalSeconds) * time.Second
				if interval < 5*time.Second {
					interval = 30 * time.Second
				}
				status := statuses[share.ID]
				jitter := time.Duration(deviceJitter(config.DeviceID, share.ID)) * time.Millisecond
				if !status.LastAttempt.IsZero() && time.Since(status.LastAttempt) < interval+jitter {
					continue
				}
				go func(id string) {
					_ = s.SyncShare(ctx, id, false)
				}(share.ID)
			}
		}
	}
}

func deviceJitter(deviceID, shareID string) int {
	value := 0
	for _, char := range deviceID + shareID {
		value = (value*31 + int(char)) % 3000
	}
	return value
}

func listenPort(address string) int {
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portText)
	return port
}
