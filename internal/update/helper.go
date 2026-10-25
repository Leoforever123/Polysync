package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Job struct {
	ParentPID int      `json:"parentPid"`
	Package   string   `json:"package"`
	Digest    string   `json:"digest"`
	Cache     string   `json:"cache"`
	Installed string   `json:"installed"`
	Previous  string   `json:"previous"`
	Args      []string `json:"args"`
}

func readJob(path string) (Job, error) {
	var j Job
	data, e := os.ReadFile(path)
	if e != nil {
		return j, e
	}
	if len(data) > 64<<10 {
		return j, errors.New("更新任务文件过大")
	}
	if e = json.Unmarshal(data, &j); e != nil {
		return j, e
	}
	if j.ParentPID <= 0 || !filepath.IsAbs(j.Package) || !filepath.IsAbs(j.Cache) || !filepath.IsAbs(j.Installed) || !filepath.IsAbs(j.Previous) {
		return j, errors.New("更新任务路径无效")
	}
	return j, nil
}
func saveResult(j Job, r InstallResult) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(j.Cache, "last-result.json"), b, 0600)
}

// apply is isolated from process APIs so failure, restart and rollback are testable.
func apply(j Job, wait func() error, install func(string) (bool, error), start func(string, []string) error) error {
	if e := wait(); e != nil {
		_ = saveResult(j, InstallResult{Message: e.Error()})
		return e
	}
	if e := VerifyFile(j.Package, j.Digest); e != nil {
		_ = saveResult(j, InstallResult{Message: e.Error()})
		if restartErr := start(j.Previous, j.Args); restartErr != nil {
			return fmt.Errorf("%v；恢复旧版失败：%w", e, restartErr)
		}
		return e
	}
	reboot, e := install(j.Package)
	if e != nil {
		_ = saveResult(j, InstallResult{Message: "更新未完成，已恢复原版本：" + e.Error()})
		if restartErr := start(j.Previous, j.Args); restartErr != nil {
			return fmt.Errorf("%v；恢复旧版失败：%w", e, restartErr)
		}
		return e
	}
	message := "更新安装完成"
	if reboot {
		message = "更新安装完成，Windows 要求重启电脑"
	}
	if e = saveResult(j, InstallResult{Success: true, Message: message}); e != nil {
		// A log write failure must not leave a successfully updated application stopped.
		if restartErr := start(j.Installed, j.Args); restartErr != nil {
			return fmt.Errorf("保存安装结果失败：%v；启动新版失败：%w", e, restartErr)
		}
		return fmt.Errorf("更新已完成并重启，但保存结果失败：%w", e)
	}
	if e = start(j.Installed, j.Args); e != nil {
		_ = saveResult(j, InstallResult{Message: "安装完成，但自动启动失败，请从开始菜单打开 PolySync：" + e.Error()})
		return fmt.Errorf("安装完成，但自动启动失败，请从开始菜单打开 PolySync：%w", e)
	}
	return nil
}
