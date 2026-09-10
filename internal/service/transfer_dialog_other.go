//go:build !windows

package service

import "os/exec"

func hideTransferHost(_ *exec.Cmd) {}
