// Package rootidentity identifies a local sync folder independently of its pathname.
package rootidentity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Older clients already exclude regular files with the .polysync-backup- prefix.
const markerPrefix = ".polysync-backup-root-id-"

var ErrDifferentRoot = errors.New("当前位置不是原来的磁盘或文件夹")

func Reserved(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), markerPrefix)
}

type identity struct {
	Token string `json:"token"`
	// Volume and Directory are optional on filesystems without persistent IDs.
	Kind      string `json:"kind,omitempty"`
	Volume    string `json:"volume,omitempty"`
	Directory string `json:"directory,omitempty"`
}

func validToken(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16 && strings.ToLower(s) == s
}

// Bind enrolls an existing directory. Only this operation creates a marker.
// An empty file with a random name is complete as soon as exclusive creation
// succeeds, including on filesystems without hard links or atomic replace.
func Bind(path string) (string, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	return BindRoot(root)
}

// BindRoot enrolls the exact open directory already checked during migration.
func BindRoot(root *os.Root) (string, error) {
	path := root.Name()
	dir, err := root.Open(".")
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := samePath(dir, path); err != nil {
		return "", err
	}
	id, err := nativeIdentity(dir)
	if err != nil {
		return "", err
	}
	for {
		names, err := dir.Readdirnames(128)
		for _, name := range names {
			token, found := strings.CutPrefix(name, markerPrefix)
			if !found || !validToken(token) {
				continue
			}
			if err := checkMarker(root, name); err != nil {
				return "", err
			}
			id.Token = token
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		if id.Token != "" || errors.Is(err, io.EOF) {
			break
		}
	}
	if id.Token == "" {
		var secret [16]byte
		rand.Read(secret[:])
		id.Token = hex.EncodeToString(secret[:])
		marker, err := root.OpenFile(markerPrefix+id.Token, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		if err := errors.Join(hideMarker(marker), marker.Close()); err != nil {
			return "", err
		}
	}
	if err := samePath(dir, path); err != nil {
		return "", err
	}
	data, err := json.Marshal(id)
	return string(data), err
}

// RemoveUnusedBinding rolls back an app-created directory after an invitation
// failed. Leave folders containing any user files or a different marker intact.
func RemoveUnusedBinding(path, saved string) error {
	var id identity
	if err := json.Unmarshal([]byte(saved), &id); err != nil || !validToken(id.Token) {
		return errors.New("同步文件夹身份记录无效")
	}
	root, err := Open(path, saved)
	if err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return err
	}
	held, err := dir.Stat()
	names, readErr := dir.Readdirnames(2)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	err = errors.Join(err, readErr, dir.Close())
	marker := markerPrefix + id.Token
	if err != nil || len(names) != 1 || names[0] != marker {
		return errors.Join(err, root.Close())
	}
	err = root.Remove(marker)
	if err := errors.Join(err, root.Close()); err != nil {
		return err
	}
	// Windows needs the directory handle closed before removal. The final
	// nonrecursive remove also refuses a new file arriving during cleanup.
	live, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(held, live) {
		return ErrDifferentRoot
	}
	return os.Remove(path)
}

// Check never creates or changes the directory or its identity marker.
func Check(path, saved string) error {
	root, err := Open(path, saved)
	if err != nil {
		return err
	}
	return root.Close()
}

// Open returns a handle anchored to the verified folder. The caller closes it.
func Open(path, saved string) (*os.Root, error) {
	var expected identity
	if err := json.Unmarshal([]byte(saved), &expected); err != nil || !validToken(expected.Token) {
		return nil, errors.New("同步文件夹身份记录无效")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	if err := verify(root, expected); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func verify(root *os.Root, expected identity) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := samePath(dir, root.Name()); err != nil {
		return err
	}
	current, err := nativeIdentity(dir)
	if err != nil {
		return err
	}
	if current.Kind != expected.Kind || (expected.Volume != "" && current.Volume != expected.Volume) ||
		(expected.Directory != "" && current.Directory != expected.Directory) {
		return ErrDifferentRoot
	}
	if err := checkMarker(root, markerPrefix+expected.Token); err != nil {
		return err
	}
	return samePath(dir, root.Name())
}

func checkMarker(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("无法确认同步文件夹标记: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		return errors.New("同步文件夹标记已改变")
	}
	return nil
}

func samePath(dir *os.File, path string) error {
	held, err := dir.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !named.IsDir() || !os.SameFile(held, named) {
		return ErrDifferentRoot
	}
	return nil
}
