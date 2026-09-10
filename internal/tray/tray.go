// Package tray provides the optional desktop control surface.
package tray

import (
	"context"
	"sort"
	"strings"
	"time"
)

type Folder struct {
	ID, Name, Path, State, Message string
	LastSync                       time.Time
	Active                         bool
}

type ReceivedFile struct {
	Received               time.Time
	TaskID, Name, PeerName string
	Index                  int
	Missing                bool
}
type TransferSummary struct {
	ID, Label string
}
type Notification struct{ Text, Route string }
type TransferSnapshot struct {
	Active []TransferSummary
	Recent []ReceivedFile
}
type Options struct {
	OpenRoute      func(string)
	Transfers      func() TransferSnapshot
	SaveFile       func(string, int) (string, error)
	OpenReceived   func(string, int) error
	CancelTransfer func(string) error
	Notifications  <-chan Notification
	Folders        func() []Folder
	OpenUI         func()
	OpenFolder     func(string) error
	Sync           func(context.Context, string) error
	Quit           func()
}

// Recent puts the most recently completed syncs first without modifying the snapshot.
func Recent(folders []Folder, limit int) []Folder {
	result := append([]Folder(nil), folders...)
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].LastSync.Equal(result[j].LastSync) {
			return result[i].LastSync.After(result[j].LastSync)
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	if limit < 0 {
		limit = 0
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func (f Folder) CanSync() bool { return f.Active && f.State != "syncing" }
func (f Folder) Status() string {
	if !f.Active {
		return "等待配对或接受邀请"
	}
	if f.State == "syncing" {
		return "正在同步…"
	}
	if f.State == "error" {
		return "同步失败 · 请在控制台查看"
	}
	if f.LastSync.IsZero() {
		return "等待首次同步"
	}
	return "最近同步 · " + f.LastSync.Local().Format("01-02 15:04")
}

func menuLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 48 {
		s = string(r[:47]) + "…"
	}
	return strings.ReplaceAll(s, "&", "&&")
}
