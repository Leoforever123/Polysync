package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"polysync/internal/model"
)

type Store struct {
	dir        string
	configPath string
	mu         sync.RWMutex
	config     model.Config
}

func Open(dir, listenAddr, uiAddr string) (*Store, error) {
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("find config directory: %w", err)
		}
		dir = filepath.Join(base, "Polysync")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	s := &Store{dir: dir, configPath: filepath.Join(dir, "config.json")}
	data, err := os.ReadFile(s.configPath)
	if err == nil {
		if err := json.Unmarshal(data, &s.config); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config: %w", err)
	} else {
		host, _ := os.Hostname()
		s.config = model.Config{
			DeviceID:   randomHex(16),
			DeviceName: host,
			ListenAddr: listenAddr,
			UIAddr:     uiAddr,
			CreatedAt:  time.Now(),
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}

	if listenAddr != "" {
		s.config.ListenAddr = listenAddr
	}
	if uiAddr != "" {
		s.config.UIAddr = uiAddr
	}
	if s.config.DeviceID == "" {
		s.config.DeviceID = randomHex(16)
	}
	if s.config.DeviceName == "" {
		s.config.DeviceName = runtime.GOOS + "-device"
	}
	if err := s.ensureIdentityLocked(); err != nil {
		return nil, err
	}
	for i := range s.config.Shares {
		if s.config.Shares[i].State == "" {
			s.config.Shares[i].State = "legacy"
		}
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) Config() model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

func (s *Store) Share(id string) (model.Share, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, share := range s.config.Shares {
		if share.ID == id {
			return share, true
		}
	}
	return model.Share{}, false
}

func (s *Store) AddShare(share model.Share) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.config.Shares {
		if existing.ID == share.ID {
			return fmt.Errorf("share %q already exists", share.ID)
		}
	}
	s.config.Shares = append(s.config.Shares, share)
	return s.saveLocked()
}

func (s *Store) UpdateShare(share model.Share) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Shares {
		if s.config.Shares[i].ID == share.ID {
			s.config.Shares[i] = share
			return s.saveLocked()
		}
	}
	return fmt.Errorf("share %q not found", share.ID)
}

func (s *Store) SetPeerAddress(id, address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Shares {
		if s.config.Shares[i].ID == id {
			s.config.Shares[i].PeerAddress = address
			return s.saveLocked()
		}
	}
	return fmt.Errorf("share %q not found", id)
}

func (s *Store) DeleteShare(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Shares {
		if s.config.Shares[i].ID == id {
			s.config.Shares = append(s.config.Shares[:i], s.config.Shares[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("share %q not found", id)
}

func (s *Store) SetDeviceName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("device name cannot be empty")
	}
	s.config.DeviceName = name
	return s.saveLocked()
}

func (s *Store) Identity() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	publicKey, err := base64.RawStdEncoding.DecodeString(s.config.IdentityPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, nil, errors.New("invalid device public key")
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(s.config.IdentityPrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return nil, nil, errors.New("invalid device private key")
	}
	return ed25519.PublicKey(publicKey), ed25519.PrivateKey(privateKey), nil
}

func (s *Store) PairedDevice(id string) (model.PairedDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, device := range s.config.PairedDevices {
		if device.ID == id {
			return device, true
		}
	}
	return model.PairedDevice{}, false
}

func (s *Store) SavePairedDevice(device model.PairedDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.PairedDevices {
		if s.config.PairedDevices[i].ID == device.ID {
			if device.PairedAt.IsZero() {
				device.PairedAt = s.config.PairedDevices[i].PairedAt
			}
			s.config.PairedDevices[i] = device
			return s.saveLocked()
		}
	}
	if device.PairedAt.IsZero() {
		device.PairedAt = time.Now()
	}
	s.config.PairedDevices = append(s.config.PairedDevices, device)
	return s.saveLocked()
}

func (s *Store) RemovePairedDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, share := range s.config.Shares {
		if share.PeerDeviceID == id {
			return errors.New("device still has configured sync folders")
		}
	}
	for i := range s.config.PairedDevices {
		if s.config.PairedDevices[i].ID == id {
			s.config.PairedDevices = append(s.config.PairedDevices[:i], s.config.PairedDevices[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("paired device %q not found", id)
}

func (s *Store) SaveShareInvitation(invitation model.ShareInvitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.ShareInvitations {
		if s.config.ShareInvitations[i].ID == invitation.ID {
			s.config.ShareInvitations[i] = invitation
			return s.saveLocked()
		}
	}
	s.config.ShareInvitations = append(s.config.ShareInvitations, invitation)
	return s.saveLocked()
}

func (s *Store) RemoveShareInvitation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.ShareInvitations {
		if s.config.ShareInvitations[i].ID == id {
			s.config.ShareInvitations = append(s.config.ShareInvitations[:i], s.config.ShareInvitations[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) SaveConflict(conflict model.Conflict) error {
	dir := filepath.Join(s.dir, "conflicts", safeName(conflict.ShareID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(conflict, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, safeName(conflict.ID)+".json"), data, 0o600)
}

func (s *Store) Conflicts() ([]model.Conflict, error) {
	root := filepath.Join(s.dir, "conflicts")
	var result []model.Conflict
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var conflict model.Conflict
		if err := json.Unmarshal(data, &conflict); err != nil {
			return err
		}
		result = append(result, conflict)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return result, err
}

func (s *Store) Conflict(id string) (model.Conflict, bool, error) {
	conflicts, err := s.Conflicts()
	if err != nil {
		return model.Conflict{}, false, err
	}
	for _, conflict := range conflicts {
		if conflict.ID == id {
			return conflict, true, nil
		}
	}
	return model.Conflict{}, false, nil
}

func (s *Store) PutObject(root string, entry model.Entry) error {
	if !validObjectHash(entry.Hash) {
		return errors.New("invalid object hash")
	}
	destination := s.ObjectPath(entry.Hash)
	if _, err := os.Stat(destination); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	source := filepath.Join(root, filepath.FromSlash(entry.Path))
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".object-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), input); err != nil {
		tmp.Close()
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != entry.Hash {
		tmp.Close()
		return errors.New("file changed while caching object")
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destination); err != nil && !errors.Is(err, os.ErrExist) {
		if _, statErr := os.Stat(destination); statErr != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ObjectPath(hash string) string {
	if !validObjectHash(hash) {
		return filepath.Join(s.dir, "objects", "invalid")
	}
	return filepath.Join(s.dir, "objects", hash[:2], hash[2:])
}

func validObjectHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func (s *Store) LoadBaseline(shareID, peerID string) ([]model.Entry, error) {
	path := s.baselinePath(shareID, peerID)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var baseline model.Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, err
	}
	return baseline.Entries, nil
}

func (s *Store) SaveBaseline(shareID, peerID string, entries []model.Entry) error {
	dir := filepath.Join(s.dir, "state", safeName(shareID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	baseline := model.Baseline{PeerID: peerID, Entries: entries, SavedAt: time.Now()}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.baselinePath(shareID, peerID), data, 0o600)
}

func (s *Store) baselinePath(shareID, peerID string) string {
	return filepath.Join(s.dir, "state", safeName(shareID), safeName(peerID)+".json")
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.configPath, data, 0o600)
}

func (s *Store) ensureIdentityLocked() error {
	if s.config.IdentityPublicKey != "" && s.config.IdentityPrivateKey != "" {
		return nil
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	s.config.IdentityPublicKey = base64.RawStdEncoding.EncodeToString(publicKey)
	s.config.IdentityPrivateKey = base64.RawStdEncoding.EncodeToString(privateKey)
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func RandomID() string     { return randomHex(12) }
func RandomSecret() string { return randomHex(32) }

func safeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func cloneConfig(config model.Config) model.Config {
	copyConfig := config
	copyConfig.Shares = append([]model.Share(nil), config.Shares...)
	copyConfig.PairedDevices = append([]model.PairedDevice(nil), config.PairedDevices...)
	copyConfig.ShareInvitations = append([]model.ShareInvitation(nil), config.ShareInvitations...)
	for i := range copyConfig.PairedDevices {
		copyConfig.PairedDevices[i].Addresses = append([]string(nil), copyConfig.PairedDevices[i].Addresses...)
	}
	return copyConfig
}
