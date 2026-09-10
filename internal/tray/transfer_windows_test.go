//go:build windows

package tray

import (
	"context"
	"golang.org/x/sys/windows"
	"testing"
	"time"
	"unsafe"
)

func menuEntry(t *testing.T, menu uintptr, label string) menuItemInfo {
	t.Helper()
	count, _, _ := user32.NewProc("GetMenuItemCount").Call(menu)
	for i := uintptr(0); i < count; i++ {
		var text [256]uint16
		item := menuItemInfo{Mask: 0x100 | 0x4 | 0x40 | 0x2 | 0x1, Text: &text[0], Length: 255}
		item.Size = uint32(unsafe.Sizeof(item))
		ok, _, _ := user32.NewProc("GetMenuItemInfoW").Call(menu, i, 1, uintptr(unsafe.Pointer(&item)))
		if ok != 0 && windows.UTF16ToString(text[:]) == label {
			return item
		}
	}
	t.Fatalf("menu item missing: %s", label)
	return menuItemInfo{}
}
func TestTransferMenuRoutesAndFileActions(t *testing.T) {
	routes := make(chan string, 8)
	saved := make(chan string, 1)
	cancelled := make(chan string, 1)
	n := &nativeTray{ctx: context.Background(), options: Options{
		Folders: func() []Folder { return nil }, OpenRoute: func(route string) { routes <- route },
		Transfers: func() TransferSnapshot {
			return TransferSnapshot{Active: []TransferSummary{{ID: "active", Label: "电脑 · 接收中"}}, Recent: []ReceivedFile{{TaskID: "received", Name: "report.txt", PeerName: "电脑", Index: 2}, {TaskID: "missing", Name: "missing.txt", Missing: true}}}
		},
		SaveFile: func(id string, index int) (string, error) {
			if index != 2 {
				t.Error("wrong file index")
			}
			saved <- id
			return "", nil
		},
		OpenReceived:   func(id string, index int) error { saved <- id; return nil },
		CancelTransfer: func(id string) error { cancelled <- id; return nil },
	}}
	menu, actions, err := n.buildMenu()
	if err != nil {
		t.Fatal(err)
	}
	defer destroyMenu.Call(menu)
	actions[uintptr(menuEntry(t, menu, "发送文件…").ID)]()
	select {
	case route := <-routes:
		if route != "#transfer/send" {
			t.Fatal(route)
		}
	case <-time.After(time.Second):
		t.Fatal("send not dispatched")
	}
	recent := menuEntry(t, menu, "最近收到").Submenu
	file := menuEntry(t, recent, "report.txt").Submenu
	actions[uintptr(menuEntry(t, file, "另存为…").ID)]()
	select {
	case id := <-saved:
		if id != "received" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("save not dispatched")
	}
	missing := menuEntry(t, recent, "missing.txt").Submenu
	item := menuEntry(t, missing, "另存为…")
	if item.State&3 == 0 || actions[uintptr(item.ID)] != nil {
		t.Fatal("missing file action enabled")
	}
	active := menuEntry(t, menu, "正在传输 · 1").Submenu
	task := menuEntry(t, active, "电脑 · 接收中").Submenu
	actions[uintptr(menuEntry(t, task, "取消传输").ID)]()
	select {
	case id := <-cancelled:
		if id != "active" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel not dispatched")
	}
}
