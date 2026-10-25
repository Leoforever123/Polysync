package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"

	"polysync/internal/model"
)

const serviceName = "_polysync._tcp"

type Service struct {
	external bool
	deviceID string
	mu       sync.RWMutex
	nearby   map[string]model.NearbyDevice
	server   *zeroconf.Server
	resolver *zeroconf.Resolver
}

// NewExternal lets a platform DNS-SD implementation supply event-driven results.
// It owns no sockets or goroutines. Entries live until the host reports loss.
func NewExternal(deviceID string) *Service {
	return &Service{deviceID: deviceID, nearby: make(map[string]model.NearbyDevice), external: true}
}

func (s *Service) ReplaceExternal(devices []model.NearbyDevice) error {
	if !s.external || len(devices) > 128 {
		return fmt.Errorf("invalid external discovery snapshot")
	}
	next := make(map[string]model.NearbyDevice)
	rejected := 0
devicesLoop:
	for _, d := range devices {
		if d.ID == s.deviceID {
			continue
		}
		key, err := base64.RawStdEncoding.DecodeString(d.PublicKey)
		if err != nil || len(key) != 32 || d.ID == "" || len(d.ID) > 128 || len(d.Name) > 255 || len(d.Addresses) == 0 || len(d.Addresses) > 16 {
			rejected++
			continue
		}
		for _, address := range d.Addresses {
			host, port, err := net.SplitHostPort(address)
			n, portErr := strconv.Atoi(port)
			// NSD supplies literal addresses. Keep discovery from triggering DNS lookups.
			ip := net.ParseIP(strings.Split(host, "%")[0])
			if err != nil || portErr != nil || n < 1 || n > 65535 || ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
				rejected++
				continue devicesLoop
			}
		}
		d.Addresses = append([]string(nil), d.Addresses...)
		d.Fingerprint, d.LastSeen, d.Online = Fingerprint(d.PublicKey), time.Now(), true
		next[d.ID] = d
	}
	s.mu.Lock()
	s.nearby = next
	s.mu.Unlock()
	if rejected > 0 {
		return fmt.Errorf("已忽略 %d 条无效发现记录", rejected)
	}
	return nil
}

func Start(ctx context.Context, deviceID, deviceName, publicKey string, port int) (*Service, error) {
	fingerprint := Fingerprint(publicKey)
	instanceName := deviceName
	if len(deviceID) >= 6 {
		instanceName += "-" + deviceID[:6]
	}
	server, err := zeroconf.Register(
		instanceName, serviceName, "local.", port,
		[]string{"id=" + deviceID, "name=" + deviceName, "version=" + strconv.Itoa(model.ProtocolVersion), "key=" + publicKey, "fp=" + fingerprint}, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("register mDNS service: %w", err)
	}
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		server.Shutdown()
		return nil, fmt.Errorf("create mDNS resolver: %w", err)
	}
	service := &Service{deviceID: deviceID, nearby: make(map[string]model.NearbyDevice), server: server, resolver: resolver}
	entries := make(chan *zeroconf.ServiceEntry)
	go service.collect(ctx, entries)
	go service.browse(ctx, entries)
	return service, nil
}

func (s *Service) Close() {
	if s.server != nil {
		s.server.Shutdown()
	}
}

func (s *Service) Nearby(paired map[string]bool) []model.NearbyDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	result := make([]model.NearbyDevice, 0, len(s.nearby))
	for _, device := range s.nearby {
		device.Paired = paired[device.ID]
		device.Online = s.external || now.Sub(device.LastSeen) < 90*time.Second
		result = append(result, device)
	}
	return result
}

func (s *Service) Device(id string) (model.NearbyDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	device, ok := s.nearby[id]
	if ok {
		device.Online = s.external || time.Since(device.LastSeen) < 90*time.Second
	}
	return device, ok
}

func (s *Service) collect(ctx context.Context, entries <-chan *zeroconf.ServiceEntry) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}
			s.update(entry)
		}
	}
}

func (s *Service) browse(ctx context.Context, entries chan<- *zeroconf.ServiceEntry) {
	_ = s.resolver.Browse(ctx, serviceName, "local.", entries)
}

func (s *Service) update(entry *zeroconf.ServiceEntry) {
	values := parseText(entry.Text)
	id := values["id"]
	if id == "" || id == s.deviceID || values["key"] == "" {
		return
	}
	addresses := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
	for _, address := range entry.AddrIPv4 {
		addresses = append(addresses, net.JoinHostPort(address.String(), strconv.Itoa(entry.Port)))
	}
	for _, address := range entry.AddrIPv6 {
		if address.IsLinkLocalUnicast() {
			continue
		}
		addresses = append(addresses, net.JoinHostPort(address.String(), strconv.Itoa(entry.Port)))
	}
	if len(addresses) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nearby[id] = model.NearbyDevice{
		ID: id, Name: values["name"], PublicKey: values["key"], Fingerprint: values["fp"], Addresses: addresses, LastSeen: time.Now(), Online: true,
	}
}

func Fingerprint(publicKey string) string {
	hash := sha256.Sum256([]byte(publicKey))
	return strings.ToUpper(hex.EncodeToString(hash[:8]))
}

func parseText(items []string) map[string]string {
	result := make(map[string]string)
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}
