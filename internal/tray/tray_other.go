//go:build !windows

package tray

import "context"

func Run(ctx context.Context, _ Options) error { <-ctx.Done(); return nil }
func ShowError(_ string)                       {}
