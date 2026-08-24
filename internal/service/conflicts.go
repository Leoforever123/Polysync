package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"polysync/internal/model"
	"polysync/internal/syncer"
)

const maxMergeTextSize = 5 << 20

type ConflictContent struct {
	Conflict model.Conflict `json:"conflict"`
	Text     bool           `json:"text"`
	Base     string         `json:"base,omitempty"`
	Local    string         `json:"local,omitempty"`
	Remote   string         `json:"remote,omitempty"`
	Merged   string         `json:"merged,omitempty"`
}

func (s *Service) cacheObjects(root string, entries []model.Entry) error {
	for _, entry := range entries {
		if err := s.store.PutObject(root, entry); err != nil {
			return fmt.Errorf("cache object %s: %w", entry.Path, err)
		}
	}
	return nil
}

func (s *Service) savePlanConflicts(share model.Share, plan model.Plan, localIsServer bool, localDevice, remoteDevice string) error {
	for _, detail := range plan.ConflictDetails {
		localHash, remoteHash := detail.ClientHash, detail.ServerHash
		localExists, remoteExists := detail.ClientExists, detail.ServerExists
		if localIsServer {
			localHash, remoteHash = detail.ServerHash, detail.ClientHash
			localExists, remoteExists = detail.ServerExists, detail.ClientExists
		}
		idHash := sha256.Sum256([]byte(share.ID + "\x00" + detail.Path + "\x00" + detail.BaseHash + "\x00" + detail.ServerHash + "\x00" + detail.ClientHash))
		conflict := model.Conflict{
			ID: hex.EncodeToString(idHash[:16]), ShareID: share.ID, Path: detail.Path, Kind: detail.Kind,
			BaseHash: detail.BaseHash, LocalHash: localHash, RemoteHash: remoteHash,
			LocalExists: localExists, RemoteExists: remoteExists, LocalDevice: localDevice, RemoteDevice: remoteDevice,
			ConflictCopyPath: detail.ConflictCopyPath, Status: "pending", CreatedAt: time.Now(),
		}
		if err := s.store.SaveConflict(conflict); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) autoResolveConflicts(shareID string, entries []model.Entry) error {
	conflicts, err := s.store.Conflicts()
	if err != nil {
		return err
	}
	paths := make(map[string]bool, len(entries))
	for _, entry := range entries {
		paths[entry.Path] = true
	}
	for _, conflict := range conflicts {
		if conflict.ShareID != shareID || conflict.Status != "pending" || conflict.ConflictCopyPath == "" || paths[conflict.ConflictCopyPath] {
			continue
		}
		conflict.Status = "resolved"
		conflict.ResolvedAt = time.Now()
		if err := s.store.SaveConflict(conflict); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) resolvedConflictIDs(shareID string) []string {
	conflicts, _ := s.store.Conflicts()
	var result []string
	for _, conflict := range conflicts {
		if conflict.ShareID == shareID && conflict.Status == "resolved" {
			result = append(result, conflict.ID)
		}
	}
	return result
}

func (s *Service) markConflictsResolved(ids []string) error {
	for _, id := range ids {
		conflict, exists, err := s.store.Conflict(id)
		if err != nil {
			return err
		}
		if !exists || conflict.Status == "resolved" {
			continue
		}
		conflict.Status = "resolved"
		conflict.ResolvedAt = time.Now()
		if err := s.store.SaveConflict(conflict); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ConflictContent(id string) (ConflictContent, error) {
	conflict, exists, err := s.store.Conflict(id)
	if err != nil {
		return ConflictContent{}, err
	}
	if !exists {
		return ConflictContent{}, errors.New("冲突不存在")
	}
	base, baseOK := s.readTextObject(conflict.BaseHash)
	local, localOK := s.readTextObject(conflict.LocalHash)
	remote, remoteOK := s.readTextObject(conflict.RemoteHash)
	text := (conflict.BaseHash == "" || baseOK) && (!conflict.LocalExists || localOK) && (!conflict.RemoteExists || remoteOK)
	content := ConflictContent{Conflict: conflict, Text: text}
	if text {
		content.Base, content.Local, content.Remote = base, local, remote
		content.Merged = defaultMerge(base, local, remote, conflict.LocalDevice, conflict.RemoteDevice)
	}
	return content, nil
}

func (s *Service) ResolveConflict(ctx context.Context, id, choice, merged string) error {
	conflict, exists, err := s.store.Conflict(id)
	if err != nil || !exists {
		if err != nil {
			return err
		}
		return errors.New("冲突不存在")
	}
	if conflict.Status != "pending" {
		return errors.New("冲突已经解决")
	}
	share, exists := s.store.Share(conflict.ShareID)
	if !exists {
		return errors.New("同步空间不存在")
	}
	historyRoot := filepath.Join(s.store.Dir(), "history", share.ID)
	switch choice {
	case "delete":
		if err := syncer.DeleteFile(share.Path, conflict.Path, historyRoot); err != nil {
			return err
		}
	case "local", "remote":
		hash := conflict.LocalHash
		exists := conflict.LocalExists
		if choice == "remote" {
			hash, exists = conflict.RemoteHash, conflict.RemoteExists
		}
		if !exists {
			if err := syncer.DeleteFile(share.Path, conflict.Path, historyRoot); err != nil {
				return err
			}
			break
		}
		data, err := os.ReadFile(s.store.ObjectPath(hash))
		if err != nil {
			return errors.New("冲突版本内容不可用")
		}
		if err := syncer.WriteResolvedFile(share.Path, conflict.Path, historyRoot, data, 0o644); err != nil {
			return err
		}
	case "merged":
		if len(merged) > maxMergeTextSize || !utf8.ValidString(merged) || strings.ContainsRune(merged, '\x00') {
			return errors.New("合并结果不是受支持的文本文件")
		}
		if err := syncer.WriteResolvedFile(share.Path, conflict.Path, historyRoot, []byte(merged), 0o644); err != nil {
			return err
		}
	default:
		return errors.New("未知的冲突解决方式")
	}
	if conflict.ConflictCopyPath != "" {
		_ = syncer.DeleteFile(share.Path, conflict.ConflictCopyPath, historyRoot)
	}
	conflict.Status = "resolved"
	conflict.ResolvedAt = time.Now()
	if err := s.store.SaveConflict(conflict); err != nil {
		return err
	}
	s.log("success", share.ID, "已解决冲突 “"+conflict.Path+"”")
	go func() { _ = s.SyncShare(context.Background(), share.ID, true) }()
	return nil
}

func (s *Service) readTextObject(hash string) (string, bool) {
	if hash == "" {
		return "", true
	}
	info, err := os.Stat(s.store.ObjectPath(hash))
	if err != nil || info.Size() > maxMergeTextSize {
		return "", false
	}
	data, err := os.ReadFile(s.store.ObjectPath(hash))
	if err != nil || !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return "", false
	}
	return string(data), true
}

func defaultMerge(base, local, remote, localName, remoteName string) string {
	if local == remote {
		return local
	}
	if local == base {
		return remote
	}
	if remote == base {
		return local
	}
	return "<<<<<<< " + localName + "\n" + local + ensureNewline(local) + "=======\n" + remote + ensureNewline(remote) + ">>>>>>> " + remoteName + "\n"
}

func ensureNewline(value string) string {
	if value == "" || strings.HasSuffix(value, "\n") {
		return ""
	}
	return "\n"
}
