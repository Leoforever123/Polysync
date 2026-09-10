//go:build windows

package tray

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNativeTrayDoubleClickAndExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan struct{}, 1)
	actions := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(4 * time.Second)
		var hwnd uintptr
		for time.Now().Before(deadline) {
			nativeWindows.Range(func(key, value any) bool { hwnd = key.(uintptr); return false })
			if hwnd != 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if hwnd == 0 {
			actions <- fmt.Errorf("tray window not created")
			cancel()
			return
		}
		postMessage.Call(hwnd, trayMessage, 0, 0x0203)
		select {
		case <-opened:
		case <-time.After(2 * time.Second):
			actions <- fmt.Errorf("double click did not open console")
			cancel()
			return
		}
		postMessage.Call(hwnd, wmClose, 0, 0)
		actions <- nil
	}()
	if err := Run(ctx, Options{OpenUI: func() { opened <- struct{}{} }, Quit: cancel, Folders: func() []Folder { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := <-actions; err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("exit did not cancel application")
	}
	remaining := 0
	nativeWindows.Range(func(_, _ any) bool { remaining++; return true })
	if remaining != 0 {
		t.Fatal("native tray window retained after exit")
	}
}

func TestNativeFolderMenuDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	synced := make(chan string, 1)
	opened := make(chan string, 1)
	n := &nativeTray{ctx: ctx, options: Options{
		Folders: func() []Folder {
			return []Folder{
				{ID: "active", Name: "A 文档", Path: "C:/Documents", Active: true},
				{ID: "pending", Name: "B 待接受"},
				{ID: "busy", Name: "C 同步中", Active: true, State: "syncing"},
			}
		},
		Sync:       func(_ context.Context, id string) error { synced <- id; return nil },
		OpenFolder: func(path string) error { opened <- path; return nil },
		OpenUI:     func() {}, Quit: cancel,
	}}
	menu, actions, err := n.buildMenu()
	if err != nil {
		t.Fatal(err)
	}
	defer destroyMenu.Call(menu)
	submenu := user32.NewProc("GetSubMenu")
	itemID := user32.NewProc("GetMenuItemID")
	itemState := user32.NewProc("GetMenuState")
	sub, _, _ := submenu.Call(menu, 3)
	syncID, _, _ := itemID.Call(sub, 1)
	openID, _, _ := itemID.Call(sub, 2)
	if actions[syncID] == nil || actions[openID] == nil {
		t.Fatal("folder actions missing")
	}
	actions[syncID]()
	select {
	case id := <-synced:
		if id != "active" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("sync not dispatched")
	}
	actions[openID]()
	select {
	case path := <-opened:
		if path != "C:/Documents" {
			t.Fatal(path)
		}
	case <-time.After(time.Second):
		t.Fatal("folder open not dispatched")
	}
	for _, index := range []uintptr{4, 5} {
		sub, _, _ := submenu.Call(menu, index)
		state, _, _ := itemState.Call(sub, 1, 0x400)
		id, _, _ := itemID.Call(sub, 1)
		if state&3 == 0 || actions[id] != nil {
			t.Fatal("pending/busy sync must be disabled")
		}
	}
}
