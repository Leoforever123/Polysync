//go:build windows

package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func LaunchHelper(ctx context.Context, j Job) error {
	if err := VerifyFile(j.Package, j.Digest); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(j.Cache, "helper-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	source, err := os.Open(j.Previous)
	if err != nil {
		return err
	}
	defer source.Close()
	helper := filepath.Join(dir, "polysync-update.exe")
	out, err := os.OpenFile(helper, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, source)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	jobPath := filepath.Join(dir, "job.json")
	if err = os.WriteFile(jobPath, data, 0600); err != nil {
		return err
	}
	command := exec.Command(helper, "-apply-update", jobPath)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = command.Start(); err != nil {
		return fmt.Errorf("无法启动更新程序：%w", err)
	}
	ended := make(chan error, 1)
	go func() { ended <- command.Wait() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = command.Process.Kill()
			<-ended
			return ctx.Err()
		case <-timer.C:
			_ = command.Process.Kill()
			<-ended
			return errors.New("更新程序启动超时，当前应用继续运行")
		case err := <-ended:
			return fmt.Errorf("更新程序未就绪：%v", err)
		case <-tick.C:
			if _, err = os.Stat(jobPath + ".ready"); err == nil {
				cleanup = false
				return nil
			}
		}
	}
}
func RunHelper(path string) error {
	j, err := readJob(path)
	if err != nil {
		return err
	}
	if err = VerifyFile(j.Package, j.Digest); err != nil {
		return err
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(j.ParentPID))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	if err = os.WriteFile(path+".ready", []byte("ready"), 0600); err != nil {
		return err
	}
	wait := func() error {
		event, e := windows.WaitForSingleObject(parent, 60000)
		if e != nil {
			return e
		}
		if event != windows.WAIT_OBJECT_0 {
			return errors.New("等待应用退出超时，未安装更新")
		}
		return nil
	}
	install := func(packagePath string) (bool, error) {
		installer := filepath.Join(os.Getenv("SystemRoot"), "System32", "msiexec.exe")
		command := exec.Command(installer, "/i", packagePath, "/passive", "/norestart", "/L*v", filepath.Join(j.Cache, "install.log"))
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		err := command.Run()
		if err == nil {
			return false, nil
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			switch exit.ExitCode() {
			case 3010:
				return true, nil
			case 1602:
				return false, errors.New("已取消安装")
			case 1618:
				return false, errors.New("另一个安装正在进行，请稍后重试")
			}
		}
		return false, fmt.Errorf("Windows Installer 安装失败：%w；日志：%s", err, filepath.Join(j.Cache, "install.log"))
	}
	restart := func(exe string, args []string) error {
		cmd := exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if e := cmd.Start(); e != nil {
			return e
		}
		return cmd.Process.Release()
	}
	return apply(j, wait, install, restart)
}
func InstalledPath() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\PolySync`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		dir, _, readErr := key.GetStringValue("InstallDir")
		if readErr != nil {
			return "", readErr
		}
		if !filepath.IsAbs(dir) {
			return "", errors.New("已安装程序的路径无效")
		}
		return filepath.Join(dir, "polysync.exe"), nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return "", err
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Programs", "PolySync", "polysync.exe"), nil
}
