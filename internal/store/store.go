package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	return copyConfig
}
