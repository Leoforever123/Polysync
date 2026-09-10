//go:build windows

package tray

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	unregisterClass  = user32.NewProc("UnregisterClassW")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	registerMessage  = user32.NewProc("RegisterWindowMessageW")
	createIcon       = user32.NewProc("CreateIconFromResourceEx")
	destroyIcon      = user32.NewProc("DestroyIcon")
	createMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu       = user32.NewProc("AppendMenuW")
	destroyMenu      = user32.NewProc("DestroyMenu")
	trackMenu        = user32.NewProc("TrackPopupMenu")
	cursorPos        = user32.NewProc("GetCursorPos")
	foreground       = user32.NewProc("SetForegroundWindow")
	notifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	messageBox       = user32.NewProc("MessageBoxW")
	nativeWindows    sync.Map
	windowCallback   = syscall.NewCallback(windowProc)
)

const (
	trayMessage   = 0x8001
	resultMessage = 0x8002
	wmClose       = 0x0010
	wmDestroy     = 0x0002
)

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	ID             uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Callback                           uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type notifyData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}
type nativeTray struct {
	menuStyle      *menuStyle
	ctx            context.Context
	options        Options
	window, icon   uintptr
	taskbarMessage uint32
	mu             sync.Mutex
	result         string
	resultRoute    string
	shownRoute     string
}

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return p
}
func setText(dest []uint16, s string) {
	clear(dest)
	chars := windows.StringToUTF16(strings.ReplaceAll(s, "\x00", ""))
	n := min(len(chars)-1, len(dest)-1)
	// Do not split a UTF-16 surrogate pair.
	if n > 0 && chars[n-1] >= 0xD800 && chars[n-1] <= 0xDBFF {
		n--
	}
	copy(dest, chars[:n])
}

func ShowError(text string) {
	messageBox.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("PolySync"))), 0x10)
}

// Run owns a native Windows message loop on its calling OS thread.
// All icon and menu mutations take place on that thread.
func Run(ctx context.Context, options Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPI, dpiErr := enablePerMonitorDPI()
	if dpiErr != nil {
		return dpiErr
	}
	defer restoreDPI()
	instance, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	if instance == 0 {
		return err
	}
	className := utf16("PolySync.Tray")
	wc := windowClass{Callback: windowCallback, Instance: uintptr(instance), ClassName: className}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if ok, _, e := registerClass.Call(uintptr(unsafe.Pointer(&wc))); ok == 0 {
		return fmt.Errorf("register tray window: %w", e)
	}
	defer unregisterClass.Call(uintptr(unsafe.Pointer(className)), uintptr(instance))
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16("PolySync"))), 0, 0, 0, 0, 0, 0, 0, uintptr(instance), 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %w", err)
	}
	defer destroyWindow.Call(hwnd)
	data := iconDIBSize(64)
	icon, _, err := createIcon.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, 64, 64, 0)
	runtime.KeepAlive(data)
	if icon == 0 {
		return fmt.Errorf("create tray icon: %w", err)
	}
	defer destroyIcon.Call(icon)
	taskbar, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	t := &nativeTray{ctx: ctx, options: options, window: hwnd, icon: icon, taskbarMessage: uint32(taskbar)}
	nativeWindows.Store(hwnd, t)
	defer nativeWindows.Delete(hwnd)
	if err := t.addIcon(); err != nil {
		return err
	}
	defer t.removeIcon()
	done := make(chan struct{})
	defer close(done)
	go func() {
		notifications := options.Notifications
		for {
			select {
			case <-ctx.Done():
				postMessage.Call(hwnd, wmClose, 0, 0)
				return
			case <-done:
				return
			case note, ok := <-notifications:
				if !ok {
					notifications = nil
					continue
				}
				t.reportRoute(note.Text, note.Route)
			}
		}
	}()
	var msg message
	for {
		result, _, e := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("tray message loop: %w", e)
		}
		if result == 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	runtime.KeepAlive(className)
	return nil
}

func (t *nativeTray) data() notifyData {
	d := notifyData{Window: t.window, ID: 1, Flags: 1 | 2 | 4, Callback: trayMessage, Icon: t.icon}
	d.Size = uint32(unsafe.Sizeof(d))
	setText(d.Tip[:], "PolySync · 双击打开 · 右键同步或退出")
	return d
}
func (t *nativeTray) addIcon() error {
	d := t.data()
	if ok, _, e := notifyIcon.Call(0, uintptr(unsafe.Pointer(&d))); ok == 0 {
		return fmt.Errorf("add tray icon: %w", e)
	}
	return nil
}
func (t *nativeTray) removeIcon() {
	d := t.data()
	notifyIcon.Call(2, uintptr(unsafe.Pointer(&d)))
}
func (t *nativeTray) showResult() {
	t.mu.Lock()
	text := t.result
	t.shownRoute = t.resultRoute
	t.mu.Unlock()
	d := t.data()
	d.Flags = 0x10
	setText(d.InfoTitle[:], "PolySync")
	setText(d.Info[:], text)
	d.InfoFlags = 1
	notifyIcon.Call(1, uintptr(unsafe.Pointer(&d)))
}
func (t *nativeTray) report(text string) { t.reportRoute(text, "") }
func (t *nativeTray) reportRoute(text, route string) {
	t.mu.Lock()
	t.result = text
	t.resultRoute = route
	t.mu.Unlock()
	if t.ctx.Err() == nil {
		postMessage.Call(t.window, resultMessage, 0, 0)
	}
}

func windowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	value, ok := nativeWindows.Load(hwnd)
	if !ok {
		result, _, _ := defWindowProc.Call(hwnd, uintptr(msg), wp, lp)
		return result
	}
	t := value.(*nativeTray)
	switch msg {
	case 0x002c: // WM_MEASUREITEM
		if t.menuStyle != nil && t.menuStyle.measure((*measureItem)(messagePointer(lp))) {
			return 1
		}
	case 0x002b: // WM_DRAWITEM
		if t.menuStyle != nil && t.menuStyle.draw((*drawItem)(messagePointer(lp)), t.icon) {
			return 1
		}
	case trayMessage:
		switch uint32(lp) {
		case 0x0203:
			go t.options.OpenUI() // WM_LBUTTONDBLCLK
		case 0x0405: // NIN_BALLOONUSERCLICK
			t.mu.Lock()
			route := t.shownRoute
			t.mu.Unlock()
			t.openRoute(route)
		case 0x0205, 0x007b: // WM_RBUTTONUP / WM_CONTEXTMENU
			if err := t.showMenu(); err != nil {
				t.report("无法打开托盘菜单：" + err.Error())
			}
		}
		return 0
	case resultMessage:
		t.showResult()
		return 0
	case wmClose:
		t.options.Quit()
		t.removeIcon()
		destroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		postQuitMessage.Call(0)
		return 0
	}
	if t.taskbarMessage != 0 && msg == t.taskbarMessage {
		if err := t.addIcon(); err != nil {
			ShowError("无法恢复系统托盘图标：" + err.Error())
			t.options.Quit()
		}
		return 0
	}
	result, _, _ := defWindowProc.Call(hwnd, uintptr(msg), wp, lp)
	return result
}

func addItem(menu uintptr, flags uintptr, id uintptr, text string) error {
	ok, _, e := appendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(utf16(text))))
	if ok == 0 {
		return fmt.Errorf("append menu: %w", e)
	}
	return nil
}

func (t *nativeTray) buildMenu() (menu uintptr, actions map[uintptr]func(), err error) {
	menu, _, e := createMenu.Call()
	if menu == 0 {
		return 0, nil, e
	}
	ownedMenu := menu
	defer func() {
		if err != nil {
			destroyMenu.Call(ownedMenu)
		}
	}()
	actions = map[uintptr]func(){}
	next := uintptr(1)
	add := func(parent uintptr, label string, disabled bool, action func()) error {
		id := next
		next++
		flags := uintptr(0)
		if disabled {
			flags = 3
		} else if action != nil {
			actions[id] = action
		}
		return addItem(parent, flags, id, label)
	}
	if err := add(menu, "打开 PolySync", false, func() { go t.options.OpenUI() }); err != nil {
		return 0, nil, err
	}
	if err := addItem(menu, 0x800, 0, ""); err != nil {
		return 0, nil, err
	}
	if err := add(menu, "近期同步的文件夹", true, nil); err != nil {
		return 0, nil, err
	}
	folders := Recent(t.options.Folders(), 8)
	if len(folders) == 0 {
		if err := add(menu, "尚未添加 · 打开控制台开始", false, func() { go t.options.OpenUI() }); err != nil {
			return 0, nil, err
		}
	}
	for _, folder := range folders {
		sub, _, err := createMenu.Call()
		if sub == 0 {
			return 0, nil, err
		}
		if err := addItem(menu, 0x10, sub, menuLabel(folder.Name)); err != nil {
			destroyMenu.Call(sub)
			return 0, nil, err
		}
		if err := add(sub, folder.Status(), true, nil); err != nil {
			return 0, nil, err
		}
		if err := add(sub, "立即同步", !folder.CanSync(), func() {
			go func() {
				t.report("正在同步 “" + folder.Name + "”…")
				if err := t.options.Sync(t.ctx, folder.ID); err != nil {
					t.report("“" + folder.Name + "” 同步失败：" + err.Error())
				} else {
					t.report("“" + folder.Name + "” 同步完成")
				}
			}()
		}); err != nil {
			return 0, nil, err
		}
		if err := add(sub, "打开文件夹", false, func() {
			go func() {
				if err := t.options.OpenFolder(folder.Path); err != nil {
					t.report("无法打开文件夹：" + err.Error())
				}
			}()
		}); err != nil {
			return 0, nil, err
		}
	}
	if err := addItem(menu, 0x800, 0, ""); err != nil {
		return 0, nil, err
	}
	if t.options.Transfers != nil {
		snapshot := t.options.Transfers()
		if err := add(menu, "随传", true, nil); err != nil {
			return 0, nil, err
		}
		if err := add(menu, "发送文件…", false, func() { t.openRoute("#transfer/send") }); err != nil {
			return 0, nil, err
		}
		if err := add(menu, "打开随传工作台", false, func() { t.openRoute("#transfer") }); err != nil {
			return 0, nil, err
		}
		sub, _, e := createMenu.Call()
		if sub == 0 {
			return 0, nil, e
		}
		if err := addItem(menu, 0x10, sub, fmt.Sprintf("正在传输 · %d", len(snapshot.Active))); err != nil {
			destroyMenu.Call(sub)
			return 0, nil, err
		}
		if len(snapshot.Active) == 0 {
			if err := add(sub, "当前没有传输", true, nil); err != nil {
				return 0, nil, err
			}
		}
		for _, task := range snapshot.Active {
			child, _, e := createMenu.Call()
			if child == 0 {
				return 0, nil, e
			}
			if err := addItem(sub, 0x10, child, menuLabel(task.Label)); err != nil {
				destroyMenu.Call(child)
				return 0, nil, err
			}
			if err := add(child, "查看进度", false, func() { t.openRoute("#transfer/task/" + task.ID) }); err != nil {
				return 0, nil, err
			}
			if err := add(child, "取消传输", false, func() {
				go func() {
					if err := t.options.CancelTransfer(task.ID); err != nil {
						t.report(err.Error())
					} else {
						t.report("正在取消传输")
					}
				}()
			}); err != nil {
				return 0, nil, err
			}
		}
		recent, _, e := createMenu.Call()
		if recent == 0 {
			return 0, nil, e
		}
		if err := addItem(menu, 0x10, recent, "最近收到"); err != nil {
			destroyMenu.Call(recent)
			return 0, nil, err
		}
		if len(snapshot.Recent) == 0 {
			if err := add(recent, "尚未收到文件", true, nil); err != nil {
				return 0, nil, err
			}
		}
		for _, file := range snapshot.Recent {
			child, _, e := createMenu.Call()
			if child == 0 {
				return 0, nil, e
			}
			if err := addItem(recent, 0x10, child, menuLabel(file.Name)); err != nil {
				destroyMenu.Call(child)
				return 0, nil, err
			}
			if err := add(child, menuLabel("来自 "+file.PeerName), true, nil); err != nil {
				return 0, nil, err
			}
			if file.Missing {
				if err := add(child, "暂存文件已不存在", true, nil); err != nil {
					return 0, nil, err
				}
			}
			if err := add(child, "另存为…", file.Missing, func() {
				go func() {
					path, err := t.options.SaveFile(file.TaskID, file.Index)
					if err != nil {
						t.report("另存失败：" + err.Error())
					} else if path != "" {
						t.report("已另存到 " + path)
					}
				}()
			}); err != nil {
				return 0, nil, err
			}
			if err := add(child, "打开所在文件夹", file.Missing, func() {
				go func() {
					if err := t.options.OpenReceived(file.TaskID, file.Index); err != nil {
						t.report("无法打开：" + err.Error())
					}
				}()
			}); err != nil {
				return 0, nil, err
			}
			if err := add(child, "查看详情", false, func() { t.openRoute("#transfer/task/" + file.TaskID) }); err != nil {
				return 0, nil, err
			}
		}
		if err := add(menu, "随传设置…", false, func() { t.openRoute("#transfer/settings") }); err != nil {
			return 0, nil, err
		}
		if err := addItem(menu, 0x800, 0, ""); err != nil {
			return 0, nil, err
		}
	}
	if err := add(menu, "使用引导", false, func() { t.openRoute("#tour") }); err != nil {
		return 0, nil, err
	}
	if err := add(menu, "查看全部同步与记录", false, func() { go t.options.OpenUI() }); err != nil {
		return 0, nil, err
	}
	if err := add(menu, "退出 PolySync", false, t.requestQuit); err != nil {
		return 0, nil, err
	}
	return menu, actions, nil
}

func (t *nativeTray) showMenu() error {
	menu, actions, err := t.buildMenu()
	if err != nil {
		return err
	}
	defer destroyMenu.Call(menu)
	var p point
	if ok, _, e := cursorPos.Call(uintptr(unsafe.Pointer(&p))); ok == 0 {
		return e
	}
	style, err := newMenuStyle(t.menuDPI(p))
	if err != nil {
		return err
	}
	defer style.close()
	if err := style.decorate(menu); err != nil {
		return err
	}
	t.menuStyle = style
	defer func() { t.menuStyle = nil }()
	foreground.Call(t.window)
	// TPM_RETURNCMD | TPM_RIGHTBUTTON: nested actions also accept right clicks.
	command, _, _ := trackMenu.Call(menu, 0x100|0x2, uintptr(p.X), uintptr(p.Y), 0, t.window, 0)
	postMessage.Call(t.window, 0, 0, 0)
	if action := actions[command]; action != nil {
		action()
	}
	return nil
}

func (t *nativeTray) openRoute(route string) {
	if t.options.OpenRoute != nil {
		go t.options.OpenRoute(route)
	} else {
		go t.options.OpenUI()
	}
}
func (t *nativeTray) requestQuit() {
	if t.options.Transfers != nil && len(t.options.Transfers().Active) > 0 {
		answer, _, _ := messageBox.Call(t.window, uintptr(unsafe.Pointer(utf16("仍有文件正在传输。退出会中断这些任务，已完成的文件会保留。确定退出？"))), uintptr(unsafe.Pointer(utf16("退出 PolySync"))), 0x4|0x30|0x100)
		if answer != 6 {
			return
		}
	}
	t.options.Quit()
}
