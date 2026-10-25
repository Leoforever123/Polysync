//go:build !windows

package update

import (
	"context"
	"errors"
)

func LaunchHelper(context.Context, Job) error {
	return errors.New("当前系统暂不支持应用内安装")
}
func RunHelper(string) error { return errors.New("当前系统暂不支持应用内安装") }
func InstalledPath() (string, error) {
	return "", errors.New("当前系统暂不支持应用内安装")
}
