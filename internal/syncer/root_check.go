package syncer

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

type rootCheckKey struct{}
type workingVolume struct {
	dir   *os.Root
	check func() error
}

// WithRootCheck carries the verified directory through the whole transfer.
// File operations use its handle even if the original path is later reused.
func WithRootCheck(ctx context.Context, dir *os.Root, check func() error) context.Context {
	return context.WithValue(ctx, rootCheckKey{}, workingVolume{dir, check})
}

func checkRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if volume, ok := ctx.Value(rootCheckKey{}).(workingVolume); ok {
		return volume.check()
	}
	return nil
}

func workingRoot(ctx context.Context, path string) (*os.Root, func(), error) {
	if volume, ok := ctx.Value(rootCheckKey{}).(workingVolume); ok {
		if filepath.Clean(path) != filepath.Clean(volume.dir.Name()) {
			return nil, nil, errors.New("同步操作与已确认的文件夹不符")
		}
		return volume.dir, func() {}, nil
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	return root, func() { root.Close() }, nil
}

// Create exclusively within the open root. os.CreateTemp and os.MkdirTemp
// would resolve the configured pathname again after a device change.
func createTemp(root *os.Root, parent, prefix string) (string, *os.File, error) {
	for {
		name := filepath.Join(parent, prefix+rand.Text())
		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if !errors.Is(err, os.ErrExist) {
			return name, file, err
		}
	}
}

func mkdirTemp(root *os.Root, parent, prefix string) (string, error) {
	for {
		name := filepath.Join(parent, prefix+rand.Text())
		err := root.Mkdir(name, 0700)
		if !errors.Is(err, os.ErrExist) {
			return name, err
		}
	}
}
