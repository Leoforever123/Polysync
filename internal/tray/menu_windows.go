//go:build windows

package tray

import (
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

func enablePerMonitorDPI() (func(), error) {
	proc := user32.NewProc("SetThreadDpiAwarenessContext")
	if err := proc.Find(); err != nil {
		return func() {}, fmt.Errorf("Windows 10 1703 or later is required for the tray: %w", err)
	}
	previous, _, err := proc.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4.
	if previous == 0 {
		return func() {}, fmt.Errorf("enable per-monitor DPI: %w", err)
	}
	return func() { proc.Call(previous) }, nil
}

// Move the hidden owner to the cursor's monitor before Windows measures its menus.
// GetDpiForWindow then returns that monitor's effective DPI, including mixed-DPI setups.
func (t *nativeTray) menuDPI(p point) uint32 {
	user32.NewProc("SetWindowPos").Call(t.window, 0, uintptr(p.X), uintptr(p.Y), 0, 0, 0x1|0x4|0x10)
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(t.window)
	if dpi == 0 {
		return 96
	}
	return uint32(dpi)
}
func scaleMenu(value int32, dpi uint32) int32 { return (value*int32(dpi) + 48) / 96 }

type menuRect struct{ Left, Top, Right, Bottom int32 }
type measureItem struct {
	Type, ControlID, ItemID, Width, Height uint32
	Data                                   uintptr
}
type drawItem struct {
	Type, ControlID, ItemID, Action, State uint32
	Window, DC                             uintptr
	Rect                                   menuRect
	Data                                   uintptr
}
type menuItemInfo struct {
	Size, Mask, Type, State, ID       uint32
	Submenu, Checked, Unchecked, Data uintptr
	Text                              *uint16
	Length                            uint32
	Bitmap                            uintptr
}
type menuInfo struct {
	Size, Mask, Style, MaxHeight uint32
	Background                   uintptr
	HelpID                       uint32
	Data                         uintptr
}
type menuVisualItem struct {
	text                                       string
	section, separator, submenu, danger, brand bool
}
type menuStyle struct {
	dpi                                uint32
	regular, caption, bold, background uintptr
	items                              map[uintptr]menuVisualItem
}

var gdi32 = windows.NewLazySystemDLL("gdi32.dll")

func newMenuStyle(dpi uint32) (*menuStyle, error) {
	s := &menuStyle{dpi: dpi, items: make(map[uintptr]menuVisualItem)}
	font := func(size int32, weight uintptr) uintptr {
		name := utf16("Microsoft YaHei UI")
		h, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(-scaleMenu(size, dpi)), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(name)))
		runtime.KeepAlive(name)
		return h
	}
	s.regular = font(13, 400)
	s.caption = font(11, 400)
	s.bold = font(14, 600)
	s.background, _, _ = gdi32.NewProc("CreateSolidBrush").Call(0xf6faf8)
	if s.regular == 0 || s.caption == 0 || s.bold == 0 || s.background == 0 {
		s.close()
		return nil, fmt.Errorf("create menu fonts or brush")
	}
	return s, nil
}
func (s *menuStyle) close() {
	for _, h := range []uintptr{s.regular, s.caption, s.bold, s.background} {
		if h != 0 {
			gdi32.NewProc("DeleteObject").Call(h)
		}
	}
}
func (s *menuStyle) decorate(menu uintptr) error {
	info := menuInfo{Mask: 0x2 | 0x10, Style: 0x80000000, Background: s.background}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, e := user32.NewProc("SetMenuInfo").Call(menu, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return e
	}
	count, _, _ := user32.NewProc("GetMenuItemCount").Call(menu)
	for i := uintptr(0); i < count; i++ {
		var text [256]uint16
		item := menuItemInfo{Mask: 0x100 | 0x4 | 0x40, Text: &text[0], Length: 255}
		item.Size = uint32(unsafe.Sizeof(item))
		if ok, _, e := user32.NewProc("GetMenuItemInfoW").Call(menu, i, 1, uintptr(unsafe.Pointer(&item))); ok == 0 {
			return e
		}
		label := windows.UTF16ToString(text[:])
		token := uintptr(len(s.items) + 1)
		s.items[token] = menuVisualItem{text: label, separator: item.Type&0x800 != 0, submenu: item.Submenu != 0,
			section: label == "近期同步的文件夹" || label == "随传", danger: label == "退出 PolySync", brand: label == "打开 PolySync"}
		item.Mask = 0x100 | 0x20
		item.Type |= 0x100 // MFT_OWNERDRAW; retain native labels for menu accessibility.
		item.Data = token
		if ok, _, e := user32.NewProc("SetMenuItemInfoW").Call(menu, i, 1, uintptr(unsafe.Pointer(&item))); ok == 0 {
			return e
		}
		if item.Submenu != 0 {
			if err := s.decorate(item.Submenu); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *menuStyle) measure(m *measureItem) bool {
	item, ok := s.items[m.Data]
	if !ok {
		return false
	}
	height := int32(38)
	if item.section {
		height = 30
	}
	if item.separator {
		height = 10
	}
	if item.brand {
		height = 48
	}
	m.Width = uint32(scaleMenu(276, s.dpi))
	m.Height = uint32(scaleMenu(height, s.dpi))
	return true
}
func fill(dc uintptr, r menuRect, color uintptr, round int32) {
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(color)
	defer gdi32.NewProc("DeleteObject").Call(brush)
	if round == 0 {
		user32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&r)), brush)
		return
	}
	oldBrush, _, _ := gdi32.NewProc("SelectObject").Call(dc, brush)
	pen, _, _ := gdi32.NewProc("GetStockObject").Call(8) // NULL_PEN
	oldPen, _, _ := gdi32.NewProc("SelectObject").Call(dc, pen)
	gdi32.NewProc("RoundRect").Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(round), uintptr(round))
	gdi32.NewProc("SelectObject").Call(dc, oldPen)
	gdi32.NewProc("SelectObject").Call(dc, oldBrush)
}
func (s *menuStyle) draw(d *drawItem, icon uintptr) bool {
	item, ok := s.items[d.Data]
	if !ok {
		return false
	}
	saved, _, _ := gdi32.NewProc("SaveDC").Call(d.DC)
	defer gdi32.NewProc("RestoreDC").Call(d.DC, saved)
	px := func(v int32) int32 { return scaleMenu(v, s.dpi) }
	fill(d.DC, d.Rect, 0xf6faf8, 0)
	r := d.Rect
	if item.separator {
		r.Left += px(16)
		r.Right -= px(16)
		r.Top = (r.Top + r.Bottom) / 2
		r.Bottom = r.Top + px(1)
		fill(d.DC, r, 0xdee7e0, 0)
		return true
	}
	selected := d.State&1 != 0 && d.State&(2|4) == 0
	if selected {
		r.Left += px(5)
		r.Right -= px(5)
		r.Top += px(2)
		r.Bottom -= px(2)
		fill(d.DC, r, 0xe3f0e6, px(12))
	}
	color := uintptr(0x2d3521)
	font := s.regular
	if item.section {
		color = 0x5c6754
		font = s.caption
	}
	if d.State&(2|4) != 0 && !item.section {
		color = 0x7c887d
	}
	if item.danger && d.State&(2|4) == 0 {
		color = 0x3647a0
	}
	if item.brand {
		font = s.bold
	}
	gdi32.NewProc("SetBkMode").Call(d.DC, 1)
	gdi32.NewProc("SetTextColor").Call(d.DC, color)
	gdi32.NewProc("SelectObject").Call(d.DC, font)
	r = d.Rect
	r.Left += px(18)
	r.Right -= px(24)
	if item.brand {
		user32.NewProc("DrawIconEx").Call(d.DC, uintptr(r.Left), uintptr(r.Top+(r.Bottom-r.Top-px(24))/2), icon, uintptr(px(24)), uintptr(px(24)), 0, 0, 3)
		r.Left += px(34)
	}
	text := utf16(item.text)
	// Native mnemonic handling keeps escaped ampersands literal.
	user32.NewProc("DrawTextW").Call(d.DC, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x4|0x20|0x8000)
	runtime.KeepAlive(text)
	return true
}

// messagePointer decodes a native LPARAM union. Windows owns this memory and
// keeps it valid only for the duration of WM_MEASUREITEM/WM_DRAWITEM callbacks.
func messagePointer(value uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&value))
}
