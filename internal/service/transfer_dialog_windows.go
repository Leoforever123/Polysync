//go:build windows

package service

import (
	"os/exec"
	"syscall"
)

func hideTransferHost(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
