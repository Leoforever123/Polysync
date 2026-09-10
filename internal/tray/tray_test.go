package tray

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestRecentFolders(t *testing.T) {
	now := time.Now()
	folders := []Folder{{ID: "old", Name: "A", LastSync: now.Add(-time.Hour)}, {ID: "new", Name: "B", LastSync: now}, {ID: "never", Name: "C"}}
	recent := Recent(folders, 2)
	if len(recent) != 2 || recent[0].ID != "new" || recent[1].ID != "old" {
		t.Fatalf("unexpected recent folders: %#v", recent)
	}
	if folders[0].ID != "old" {
		t.Fatal("snapshot mutated")
	}
	if len(Recent(folders, 0)) != 0 || len(Recent(nil, 8)) != 0 {
		t.Fatal("empty/zero limit must be empty")
	}
}
func TestFolderActions(t *testing.T) {
	for _, tc := range []struct {
		f      Folder
		can    bool
		status string
	}{
		{Folder{}, false, "等待配对或接受邀请"},
		{Folder{Active: true}, true, "等待首次同步"},
		{Folder{Active: true, State: "syncing"}, false, "正在同步…"},
		{Folder{Active: true, State: "error"}, true, "同步失败 · 请在控制台查看"},
	} {
		if tc.f.CanSync() != tc.can || tc.f.Status() != tc.status {
			t.Fatalf("wrong actions for %#v", tc.f)
		}
	}
}
func TestMenuEscapesNames(t *testing.T) {
	if got := menuLabel("R&D\n同步\t"); got != "R&&D 同步" {
		t.Fatal(got)
	}
}
func TestIconResource(t *testing.T) {
	data := iconDIB()
	if binary.LittleEndian.Uint32(data[4:]) != 32 || binary.LittleEndian.Uint32(data[8:]) != 64 {
		t.Fatal("invalid icon dimensions")
	}
	visible := 0
	for i := 43; i < 40+32*32*4; i += 4 {
		if data[i] > 0 {
			visible++
		}
	}
	if visible < 100 || visible > 800 {
		t.Fatalf("invalid icon coverage: %d", visible)
	}
}
