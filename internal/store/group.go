package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"polysync/internal/model"
)

func (s *Store) GroupState(id string) (model.GroupState, error) {
	state := model.GroupState{Records: make(model.Revisions)}
	data, err := os.ReadFile(filepath.Join(s.dir, "state", safeName(id), "revisions-v1.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &state)
	}
	if err == nil && state.Records == nil {
		err = errors.New("文件版本记录无效")
	}
	return state, err
}

func (s *Store) SaveGroupState(id string, state model.GroupState) error {
	dir := filepath.Join(s.dir, "state", safeName(id))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "revisions-v1.json"), data, 0600)
}

// Receive an advertised object into the existing content-addressed cache without
// making any working-tree changes until every required object has been verified.
func (s *Store) ReceiveGroupObject(ctx context.Context, entry model.Entry, copyTo func(io.Writer, int64) error) error {
	if !validObjectHash(entry.Hash) || entry.Size < 0 || entry.Size > 16<<30 {
		return errors.New("无效的文件版本")
	}
	dest := s.ObjectPath(entry.Hash)
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(dest), ".group-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	if err := copyTo(io.MultiWriter(file, hash), entry.Size); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != entry.Hash {
		return fmt.Errorf("文件内容核对失败：%s", entry.Path)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), dest); err != nil {
		// An identical object can arrive via another space concurrently.
		if _, statErr := os.Stat(dest); statErr != nil {
			return err
		}
	}
	return nil
}
