//go:build windows

package tray

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestTrayDPIContextIsPerMonitorV2(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, _, _ := user32.NewProc("GetThreadDpiAwarenessContext").Call()
	restore, err := enablePerMonitorDPI()
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ := user32.NewProc("GetThreadDpiAwarenessContext").Call()
	same, _, _ := user32.NewProc("AreDpiAwarenessContextsEqual").Call(current, ^uintptr(3))
	restore()
	after, _, _ := user32.NewProc("GetThreadDpiAwarenessContext").Call()
	restored, _, _ := user32.NewProc("AreDpiAwarenessContextsEqual").Call(before, after)
	if same == 0 || restored == 0 {
		t.Fatal("per-monitor DPI context not applied/restored")
	}
}

func TestMenuScalesFontsAndRows(t *testing.T) {
	for _, dpi := range []uint32{96, 120, 144, 168, 192, 240} {
		style, err := newMenuStyle(dpi)
		if err != nil {
			t.Fatal(err)
		}
		style.items[1] = menuVisualItem{text: "立即同步"}
		measure := measureItem{Data: 1}
		if !style.measure(&measure) {
			t.Fatal("item not measured")
		}
		if measure.Height != uint32(scaleMenu(38, dpi)) || measure.Width != uint32(scaleMenu(276, dpi)) {
			t.Fatalf("wrong dimensions at %d DPI", dpi)
		}
		var font [92]byte
		got, _, _ := gdi32.NewProc("GetObjectW").Call(style.regular, 92, uintptr(unsafe.Pointer(&font[0])))
		if got == 0 || int32(binary.LittleEndian.Uint32(font[:])) != -scaleMenu(13, dpi) {
			t.Fatalf("font not rendered at native %d DPI", dpi)
		}
		style.close()
	}
}

func TestStyledMenuKeepsNativeLabelsAndActions(t *testing.T) {
	n := &nativeTray{options: Options{Folders: func() []Folder { return []Folder{{ID: "1", Name: "工作文档", Active: true}} }}}
	menu, actions, err := n.buildMenu()
	if err != nil {
		t.Fatal(err)
	}
	defer destroyMenu.Call(menu)
	style, err := newMenuStyle(168)
	if err != nil {
		t.Fatal(err)
	}
	defer style.close()
	if err := style.decorate(menu); err != nil {
		t.Fatal(err)
	}
	var text [128]uint16
	item := menuItemInfo{Mask: 0x100 | 0x20 | 0x40 | 0x2, Text: &text[0], Length: 127}
	item.Size = uint32(unsafe.Sizeof(item))
	ok, _, _ := user32.NewProc("GetMenuItemInfoW").Call(menu, 0, 1, uintptr(unsafe.Pointer(&item)))
	if ok == 0 || windows.UTF16ToString(text[:]) != "打开 PolySync" || item.Type&0x100 == 0 || item.Data == 0 {
		t.Fatal("styled menu lost its native label or paint data")
	}
	if actions[uintptr(item.ID)] == nil {
		t.Fatal("styled menu lost action dispatch")
	}
}
func TestHighResolutionIconResource(t *testing.T) {
	data := iconDIBSize(64)
	if binary.LittleEndian.Uint32(data[4:]) != 64 || len(data) != 40+64*64*4+64*8 {
		t.Fatal("invalid high resolution icon")
	}
}
