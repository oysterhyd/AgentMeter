//go:build windows

package nativeapp

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// A per-pixel-alpha Win32 window. All painting and input remain in the Go
// process. UpdateLayeredWindow needs premultiplied BGRA, supplied by MyGO's
// memory renderer; no web view, Chromium, child surface or extra runtime.
var widgetUser = windows.NewLazySystemDLL("user32.dll")
var widgetGDI = windows.NewLazySystemDLL("gdi32.dll")
var widgetKernel = windows.NewLazySystemDLL("kernel32.dll")
var widgetDWM = windows.NewLazySystemDLL("dwmapi.dll")
var wRegister = widgetUser.NewProc("RegisterClassExW")
var wCreate = widgetUser.NewProc("CreateWindowExW")
var wDef = widgetUser.NewProc("DefWindowProcW")
var wDestroy = widgetUser.NewProc("DestroyWindow")
var wShow = widgetUser.NewProc("ShowWindow")
var wSetPos = widgetUser.NewProc("SetWindowPos")
var wGetRect = widgetUser.NewProc("GetWindowRect")
var wGetCursor = widgetUser.NewProc("GetCursorPos")
var wGetDPI = widgetUser.NewProc("GetDpiForWindow")
var wGetLong = widgetUser.NewProc("GetWindowLongPtrW")
var wSetLong = widgetUser.NewProc("SetWindowLongPtrW")
var wSetTimer = widgetUser.NewProc("SetTimer")
var wPost = widgetUser.NewProc("PostMessageW")
var wKillTimer = widgetUser.NewProc("KillTimer")
var wCapture = widgetUser.NewProc("SetCapture")
var wRelease = widgetUser.NewProc("ReleaseCapture")
var wSetFocus = widgetUser.NewProc("SetFocus")
var wLoadCursor = widgetUser.NewProc("LoadCursorW")
var wSetCursor = widgetUser.NewProc("SetCursor")
var wSend = widgetUser.NewProc("SendMessageW")
var wMonitor = widgetUser.NewProc("MonitorFromPoint")
var wMonitorInfo = widgetUser.NewProc("GetMonitorInfoW")
var wSPI = widgetUser.NewProc("SystemParametersInfoW")
var wKeyState = widgetUser.NewProc("GetKeyState")
var wMenuCreate = widgetUser.NewProc("CreatePopupMenu")
var wMenuAppend = widgetUser.NewProc("AppendMenuW")
var wMenuTrack = widgetUser.NewProc("TrackPopupMenu")
var wMenuDestroy = widgetUser.NewProc("DestroyMenu")
var wForeground = widgetUser.NewProc("SetForegroundWindow")
var wLayered = widgetUser.NewProc("UpdateLayeredWindow")
var wDIB = widgetGDI.NewProc("CreateDIBSection")
var wCompatibleDC = widgetGDI.NewProc("CreateCompatibleDC")
var wSelect = widgetGDI.NewProc("SelectObject")
var wDelete = widgetGDI.NewProc("DeleteObject")
var wDeleteDC = widgetGDI.NewProc("DeleteDC")
var wModule = widgetKernel.NewProc("GetModuleHandleW")
var widgetWindows = map[uintptr]*widgetWindow{}
var widgetRegister sync.Once
var widgetRegisterErr error
var widgetCallback uintptr

const widgetClass = "CodexMonitorNativeWidget"

type widgetPoint struct{ X, Y int32 }
type widgetRect struct{ Left, Top, Right, Bottom int32 }
type widgetClassEx struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type widgetBitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, Bits           uint16
	Compression, ImageSize uint32
	XPels, YPels           int32
	Used, Important        uint32
	Colors                 [4]byte
}
type widgetMonitorInfo struct {
	Size          uint32
	Monitor, Work widgetRect
	Flags         uint32
}
type widgetToolInfo struct {
	Size, Flags     uint32
	Window, ID      uintptr
	Rect            widgetRect
	Instance        uintptr
	Text            *uint16
	Param, Reserved uintptr
}
type widgetWindow struct {
	host                                 *widgetHost
	ctx                                  context.Context
	hwnd, dc, bitmap, oldBitmap, tooltip uintptr
	pixels                               unsafe.Pointer
	pixelWidth, pixelHeight              int
	dpi                                  float32
	tipText                              []uint16
	tipRegion                            int
	tipSince                             time.Time
	tipShowing                           bool
	dragging, moved, down                bool
	downRegion                           int
	grab, origin                         widgetPoint
	frame                                *image.RGBA
	renderError                          error
	alpha                                byte
	timerInterval                        uintptr
	highResolution                       bool
	frameStop                            chan struct{}
	framePending                         atomic.Bool
	frameGeneration                      uintptr
}

func widgetUTF16(s string) *uint16 { return windows.StringToUTF16Ptr(s) }
func widgetSigned(v int) uintptr   { return uintptr(int64(v)) }
func newWidgetWindow(host *widgetHost, ctx context.Context) (*widgetWindow, error) {
	widgetRegister.Do(func() {
		widgetCallback = syscall.NewCallback(widgetWndProc)
		instance, _, _ := wModule.Call(0)
		cursor, _, _ := wLoadCursor.Call(0, 32512)
		class := widgetClassEx{Style: 8, Proc: widgetCallback, Instance: instance, Cursor: cursor, Name: widgetUTF16(widgetClass)}
		class.Size = uint32(unsafe.Sizeof(class))
		result, _, err := wRegister.Call(uintptr(unsafe.Pointer(&class)))
		if result == 0 {
			widgetRegisterErr = err
		}
	})
	if widgetRegisterErr != nil {
		return nil, widgetRegisterErr
	}
	area := mygo.Screen.PrimaryDisplay().WorkArea
	x, y := host.saved.X, host.saved.Y
	if !host.saved.PositionKnown {
		x, y = area.X+area.Width-widgetWidth-24, area.Y+48
	}
	// Match MyGO/Electron DIP coordinates, then read actual window DPI.
	display := mygo.Screen.DisplayNearestPoint(mygo.Point{X: x, Y: y})
	area = display.WorkArea
	s := float32(display.ScaleFactor)
	if s <= 0 {
		s = 1
	}
	x, y = widgetClamp(x, y, ui.Rect{X: float32(area.X), Y: float32(area.Y), W: float32(area.Width), H: float32(area.Height)})
	instance, _, _ := wModule.Call(0)
	handle, _, err := wCreate.Call(0x80000|0x80, uintptr(unsafe.Pointer(widgetUTF16(widgetClass))), uintptr(unsafe.Pointer(widgetUTF16("Codex Monitor Widget"))), 0x80000000, widgetSigned(int(float32(x)*s)), widgetSigned(int(float32(y)*s)), uintptr(int(widgetWidth*s)), uintptr(int(widgetHeight*s)), 0, 0, instance, 0)
	if handle == 0 {
		return nil, fmt.Errorf("无法创建原生小窗口：%w", err)
	}
	w := &widgetWindow{host: host, ctx: ctx, hwnd: handle, dpi: s, tipRegion: -1, downRegion: -1}
	widgetWindows[handle] = w
	for _, kind := range []uintptr{0, 1} {
		icon, _, _ := wSend.Call(host.app.win.NativeHandle(), 0x7f, kind, 0)
		if icon != 0 {
			wSend.Call(handle, 0x80, kind, icon)
		}
	}
	actual, _, _ := wGetDPI.Call(handle)
	if actual > 0 {
		w.dpi = float32(actual) / 96
	}
	host.saved.X, host.saved.Y = x, y
	w.place(x, y)
	w.topmost(host.saved.Topmost)
	// Remove the system's extra shadow: the original surface draws its own.
	attr := uint32(1)
	widgetDWM.NewProc("DwmSetWindowAttribute").Call(handle, 2, uintptr(unsafe.Pointer(&attr)), 4)
	w.createTooltip()
	w.preferences()
	return w, nil
}
func (w *widgetWindow) context() context.Context { return w.ctx }
func (w *widgetWindow) scale() float32           { return w.dpi }
func (w *widgetWindow) wake() {
	w.host.model.dirty = true
	w.schedule(true)
}
func (w *widgetWindow) schedule(animated bool) {
	interval := uintptr(100)
	if animated {
		interval = 15
	}
	if w.hwnd == 0 || interval == w.timerInterval {
		return
	}
	if animated != w.highResolution {
		proc := "timeEndPeriod"
		if animated {
			proc = "timeBeginPeriod"
		}
		windows.NewLazySystemDLL("winmm.dll").NewProc(proc).Call(1)
		w.highResolution = animated
	}
	wKillTimer.Call(w.hwnd, 1)
	w.stopFrames()
	if animated {
		// WM_TIMER is dispatched only after higher-priority GUI work. Post a
		// coalesced frame message so paint traffic cannot starve a crossfade.
		stop := make(chan struct{})
		w.frameStop = stop
		hwnd, generation := w.hwnd, w.frameGeneration
		go func() {
			ticker := time.NewTicker(15 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					if w.framePending.CompareAndSwap(false, true) {
						if ok, _, _ := wPost.Call(hwnd, 0x803b, generation, 0); ok == 0 {
							w.framePending.Store(false)
						}
					}
				}
			}
		}()
	} else {
		wSetTimer.Call(w.hwnd, 1, interval, 0)
	}
	w.timerInterval = interval
}
func (w *widgetWindow) stopFrames() {
	if w.frameStop != nil {
		close(w.frameStop)
		w.frameStop = nil
	}
	w.frameGeneration++
}
func (w *widgetWindow) show() {
	w.taskbar(true)
	w.place(w.host.saved.X, w.host.saved.Y)
	wShow.Call(w.hwnd, 4)
	w.wake()
}
func (w *widgetWindow) hide() {
	w.hideTooltip()
	wShow.Call(w.hwnd, 0)
	w.taskbar(false)
	wKillTimer.Call(w.hwnd, 1)
	w.stopFrames()
	w.timerInterval = 0
	if w.highResolution {
		windows.NewLazySystemDLL("winmm.dll").NewProc("timeEndPeriod").Call(1)
		w.highResolution = false
	}
	w.host.releaseWhenHidden()
}
func (w *widgetWindow) taskbar(show bool) {
	style, _, _ := wGetLong.Call(w.hwnd, widgetSigned(-20))
	if show {
		style = (style &^ 0x80) | 0x40000
	} else {
		style = (style &^ 0x40000) | 0x80
	}
	wSetLong.Call(w.hwnd, widgetSigned(-20), style)
}
func (w *widgetWindow) topmost(on bool) {
	insert := widgetSigned(-2)
	if on {
		insert = widgetSigned(-1)
	}
	wSetPos.Call(w.hwnd, insert, 0, 0, 0, 0, 0x1|0x2|0x10)
}
func (w *widgetWindow) place(x, y int) {
	wSetPos.Call(w.hwnd, 0, widgetSigned(int(math.Round(float64(float32(x)*w.dpi)))), widgetSigned(int(math.Round(float64(float32(y)*w.dpi)))), uintptr(math.Round(widgetWidth*float64(w.dpi))), uintptr(math.Round(widgetHeight*float64(w.dpi))), 0x4|0x10)
	w.host.saved.X, w.host.saved.Y = x, y
}
func (w *widgetWindow) bounds() widgetRect {
	var r widgetRect
	wGetRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}
func widgetCursor() widgetPoint {
	var p widgetPoint
	wGetCursor.Call(uintptr(unsafe.Pointer(&p)))
	return p
}
func widgetWork(p widgetPoint, s float32) ui.Rect {
	packed := uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32)
	monitor, _, _ := wMonitor.Call(packed, 2)
	info := widgetMonitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	wMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	return ui.Rect{X: float32(info.Work.Left) / s, Y: float32(info.Work.Top) / s, W: float32(info.Work.Right-info.Work.Left) / s, H: float32(info.Work.Bottom-info.Work.Top) / s}
}
func (w *widgetWindow) anchor() {
	r := w.bounds()
	work := widgetWork(widgetPoint{X: r.Left, Y: r.Top}, w.dpi)
	x := max(int(work.X), min(w.host.saved.X, int(work.X+work.W)-widgetWidth))
	y := max(int(work.Y), min(w.host.saved.Y, int(work.Y+work.H)-widgetHeight))
	w.place(x, y)
	w.host.persist()
}
func (w *widgetWindow) releasePixels() {
	if w.dc != 0 {
		wSelect.Call(w.dc, w.oldBitmap)
		wDelete.Call(w.bitmap)
		wDeleteDC.Call(w.dc)
	}
	w.dc, w.bitmap, w.oldBitmap, w.pixels = 0, 0, 0, nil
	w.pixelWidth, w.pixelHeight, w.frame = 0, 0, nil
}
func (w *widgetWindow) pixelsReleased() bool { return w.dc == 0 && w.frame == nil }
func (w *widgetWindow) close() {
	w.hide()
	if w.tooltip != 0 {
		wDestroy.Call(w.tooltip)
		w.tooltip = 0
	}
	w.releasePixels()
	delete(widgetWindows, w.hwnd)
	wDestroy.Call(w.hwnd)
	w.hwnd = 0
}
func (w *widgetWindow) present(frame *image.RGBA) {
	w.frame = frame
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	if w.pixelWidth != width || w.pixelHeight != height {
		if w.dc != 0 {
			wSelect.Call(w.dc, w.oldBitmap)
			wDelete.Call(w.bitmap)
			wDeleteDC.Call(w.dc)
		}
		w.dc, _, _ = wCompatibleDC.Call(0)
		info := widgetBitmapInfo{Width: int32(width), Height: -int32(height), Planes: 1, Bits: 32}
		info.Size = 40
		w.bitmap, _, _ = wDIB.Call(w.dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&w.pixels)), 0, 0)
		if w.bitmap == 0 {
			w.renderError = fmt.Errorf("无法创建透明绘制位图")
			return
		}
		w.oldBitmap, _, _ = wSelect.Call(w.dc, w.bitmap)
		w.pixelWidth, w.pixelHeight = width, height
	}
	pixels := unsafe.Slice((*byte)(w.pixels), width*height*4)
	for y := 0; y < height; y++ {
		row := frame.Pix[y*frame.Stride : y*frame.Stride+width*4]
		out := pixels[y*width*4 : (y+1)*width*4]
		for x := 0; x < len(row); x += 4 {
			out[x], out[x+1], out[x+2], out[x+3] = row[x+2], row[x+1], row[x], row[x+3]
		}
	}
	w.alpha = byte(math.Round(float64(w.host.model.visibility.value(time.Now()) * 255)))
	w.compose()
}
func (w *widgetWindow) fade(alpha float32) {
	value := byte(math.Round(float64(max(0, min(1, alpha)) * 255)))
	if w.alpha != value {
		w.alpha = value
		w.compose()
	}
}
func (w *widgetWindow) compose() {
	if w.bitmap == 0 {
		return
	}
	size := widgetPoint{X: int32(w.pixelWidth), Y: int32(w.pixelHeight)}
	source := widgetPoint{}
	blend := [4]byte{0, 0, w.alpha, 1}
	result, _, err := wLayered.Call(w.hwnd, 0, 0, uintptr(unsafe.Pointer(&size)), w.dc, uintptr(unsafe.Pointer(&source)), 0, uintptr(unsafe.Pointer(&blend[0])), 2)
	if result == 0 {
		w.renderError = fmt.Errorf("原生透明绘制失败：%w", err)
	}
}
func (w *widgetWindow) preferences() {
	var enabled int32 = 1
	wSPI.Call(0x1042, 0, uintptr(unsafe.Pointer(&enabled)), 0)
	w.host.model.ReduceMotion = w.host.app.reduceMotion || enabled == 0
	// Respect Windows' transparency preference as the old CSS media query did.
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		value, _, e := key.GetIntegerValue("EnableTransparency")
		w.host.model.ReduceTransparency = e == nil && value == 0
	}
}
func (w *widgetWindow) poll(now time.Time) {
	cursor := widgetCursor()
	r := w.bounds()
	x, y := float32(cursor.X-r.Left)/w.dpi, float32(cursor.Y-r.Top)/w.dpi
	inside := w.host.model.glass(now).Contains(x, y)
	style, _, _ := wGetLong.Call(w.hwnd, widgetSigned(-20))
	next := style
	if !inside && !w.down {
		next |= 0x20
	} else {
		next &^= 0x20
	}
	if next != style {
		wSetLong.Call(w.hwnd, widgetSigned(-20), next)
	}
	if !w.down && (x != w.host.model.pointerX || y != w.host.model.pointerY) {
		if w.host.model.pointer(x, y, now) {
			w.host.painter.invalidateInteraction()
			w.schedule(true)
		}
	}
	w.updateTooltip(now, cursor)
	w.host.tick(now)
	if w.host.model.Visible || w.host.model.animating(now) || w.host.app.mainFade.active(now) {
		w.schedule(w.host.model.animating(now) || w.host.app.mainFade.active(now))
	}
}
func (w *widgetWindow) pointerMove() {
	cursor := widgetCursor()
	w.movePointer(cursor)
}
func (w *widgetWindow) movePointer(cursor widgetPoint) {
	r := w.bounds()
	if w.down && w.dragging {
		dx, dy := cursor.X-w.grab.X, cursor.Y-w.grab.Y
		w.moved = w.moved || math.Hypot(float64(dx), float64(dy)) > 5*float64(w.dpi)
		if w.moved {
			x, y := int(math.Round(float64(w.origin.X+dx)/float64(w.dpi))), int(math.Round(float64(w.origin.Y+dy)/float64(w.dpi)))
			work := widgetWork(cursor, w.dpi)
			x, y = widgetClamp(x, y, work)
			w.place(x, y)
		}
	}
	if w.host.model.pointer(float32(cursor.X-r.Left)/w.dpi, float32(cursor.Y-r.Top)/w.dpi, time.Now()) {
		w.host.painter.invalidateInteraction()
		w.schedule(true)
	}
}
func (w *widgetWindow) buttonDown() {
	cursor := widgetCursor()
	r := w.bounds()
	x, y := float32(cursor.X-r.Left)/w.dpi, float32(cursor.Y-r.Top)/w.dpi
	w.downRegion = w.host.model.hit(x, y, time.Now())
	w.down = true
	w.moved = false
	w.grab = cursor
	w.origin = widgetPoint{X: r.Left, Y: r.Top}
	w.dragging = w.downRegion == 7 || (!w.host.model.Collapsed && y < 75 && w.downRegion != 0 && w.downRegion != 1)
	w.host.model.pressed = true
	if w.downRegion >= 0 && w.downRegion <= 7 {
		w.host.model.focus = w.downRegion
	}
	w.host.model.keyboard = false
	w.hideTooltip()
	wCapture.Call(w.hwnd)
	wSetFocus.Call(w.hwnd)
}
func (w *widgetWindow) buttonUp() {
	if !w.down {
		return
	}
	w.pointerMove()
	w.down = false
	wRelease.Call()
	w.host.model.pressed = false
	if w.moved {
		w.host.persist()
		return
	}
	if w.downRegion == 7 || w.host.model.hover == w.downRegion {
		w.clickRegion(w.downRegion)
	}
}
func (w *widgetWindow) clickRegion(region int) {
	switch region {
	case 0:
		w.host.activate("restore")
	case 1:
		w.host.activate("collapse")
	case 6:
		if !w.host.model.Busy {
			w.host.activate("refresh")
		}
	case 7:
		w.host.activate("orb")
	}
}
func (w *widgetWindow) menu() {
	w.hideTooltip()
	menu, _, _ := wMenuCreate.Call()
	defer wMenuDestroy.Call(menu)
	for i, label := range []string{"打开主窗口 / Open monitor", "置顶 / Always on top", "刷新数据 / Refresh", "", "隐藏小组件 / Hide widget"} {
		flags := uintptr(0)
		if i == 1 && w.host.saved.Topmost {
			flags = 8
		}
		if i == 3 {
			flags = 0x800
		}
		wMenuAppend.Call(menu, flags, uintptr(i+1), uintptr(unsafe.Pointer(widgetUTF16(label))))
	}
	cursor := widgetCursor()
	wForeground.Call(w.hwnd)
	id, _, _ := wMenuTrack.Call(menu, 0x100|0x2, widgetSigned(int(cursor.X)), widgetSigned(int(cursor.Y)), 0, w.hwnd, 0)
	switch id {
	case 1:
		w.host.activate("restore")
	case 2:
		w.host.activate("topmost")
	case 3:
		w.host.activate("refresh")
	case 5:
		w.host.activate("hide")
	}
}
func (w *widgetWindow) key(key uintptr) {
	m := &w.host.model
	m.keyboard = true
	switch key {
	case 9:
		direction := 1
		shift, _, _ := wKeyState.Call(16)
		if int16(shift) < 0 {
			direction = -1
		}
		if m.Collapsed {
			m.focus = 7
		} else {
			m.focus = (m.focus + direction + 7) % 7
		}
		m.pointer(m.pointerX, m.pointerY, time.Now())
		w.host.painter.invalidate()
	case 13, 32:
		w.clickRegion(m.focus)
	case 93:
		w.menu()
	case 121:
		shift, _, _ := wKeyState.Call(16)
		if int16(shift) < 0 {
			w.menu()
		}
	}
	w.wake()
}
func (w *widgetWindow) createTooltip() {
	w.tooltip, _, _ = wCreate.Call(8, uintptr(unsafe.Pointer(widgetUTF16("tooltips_class32"))), 0, 0x80000000|1|2, 0, 0, 0, 0, w.hwnd, 0, 0, 0)
	if w.tooltip == 0 {
		return
	}
	w.tipText = windows.StringToUTF16(" ")
	info := w.toolInfo()
	wSend.Call(w.tooltip, 0x432, 0, uintptr(unsafe.Pointer(&info)))
	wSend.Call(w.tooltip, 0x418, 0, uintptr(440*w.dpi))
}
func (w *widgetWindow) toolInfo() widgetToolInfo {
	info := widgetToolInfo{Flags: 0x20 | 0x80, Window: w.hwnd, ID: 1, Text: &w.tipText[0]}
	info.Size = uint32(unsafe.Sizeof(info))
	return info
}
func (w *widgetWindow) hideTooltip() {
	if w.tooltip != 0 && w.tipShowing {
		info := w.toolInfo()
		wSend.Call(w.tooltip, 0x411, 0, uintptr(unsafe.Pointer(&info)))
	}
	w.tipShowing = false
}
func (w *widgetWindow) updateTooltip(now time.Time, cursor widgetPoint) {
	region := w.host.model.hover
	if region != w.tipRegion {
		w.hideTooltip()
		w.tipRegion = region
		w.tipSince = now
	}
	if region < 0 || w.down || now.Sub(w.tipSince) < 600*time.Millisecond || w.tooltip == 0 {
		return
	}
	text := w.host.model.tooltip(region, now)
	if text == "" {
		return
	}
	if !w.tipShowing {
		w.tipText = windows.StringToUTF16(text)
		info := w.toolInfo()
		wSend.Call(w.tooltip, 0x439, 0, uintptr(unsafe.Pointer(&info)))
		pos := uintptr(uint32(uint16(cursor.X+12)) | uint32(uint16(cursor.Y+24))<<16)
		wSend.Call(w.tooltip, 0x412, 0, pos)
		wSend.Call(w.tooltip, 0x411, 1, uintptr(unsafe.Pointer(&info)))
		w.tipShowing = true
	}
}
func widgetWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	w := widgetWindows[hwnd]
	if w != nil {
		switch msg {
		case 0x803b:
			w.framePending.Store(false)
			if wp == w.frameGeneration && w.highResolution {
				w.poll(time.Now())
			}
			return 0
		case 0x113:
			w.poll(time.Now())
			return 0
		case 0x200:
			w.pointerMove()
			return 0
		case 0x201:
			w.buttonDown()
			return 0
		case 0x202:
			w.buttonUp()
			return 0
		case 0x203:
			if !w.host.model.Collapsed {
				w.host.activate("restore")
			}
			return 0
		case 0x205:
			w.menu()
			return 0
		case 0x215:
			if w.down {
				w.down = false
				w.host.model.pressed = false
				w.host.persist()
			}
			return 0
		case 0x100:
			w.key(wp)
			return 0
		case 0x10:
			w.host.activate("hide")
			return 0
		case 0x8:
			w.host.model.focus = -1
			w.host.painter.invalidate()
			return 0
		case 0x20:
			cursorID := uintptr(32512)
			if w.host.model.Collapsed || w.host.model.hover == 0 || w.host.model.hover == 1 || w.host.model.hover == 6 {
				cursorID = 32649
			}
			cursor, _, _ := wLoadCursor.Call(0, cursorID)
			wSetCursor.Call(cursor)
			return 1
		case 0x2e0:
			w.dpi = float32(wp&0xffff) / 96
			var r widgetRect
			if err := windows.ReadProcessMemory(windows.CurrentProcess(), lp, (*byte)(unsafe.Pointer(&r)), unsafe.Sizeof(r), nil); err != nil {
				r = w.bounds()
			}
			w.host.saved.X, w.host.saved.Y = int(float32(r.Left)/w.dpi), int(float32(r.Top)/w.dpi)
			w.place(w.host.saved.X, w.host.saved.Y)
			w.host.painter.invalidate()
			w.wake()
			return 0
		case 0x1a, 0x7e:
			w.preferences()
			w.anchor()
			w.host.painter.invalidate()
			w.wake()
			return 0
		}
	}
	result, _, _ := wDef.Call(hwnd, uintptr(msg), wp, lp)
	return result
}

func (w *widgetWindow) renderFailure() error { return w.renderError }
func (w *widgetWindow) verify() Object {
	r := w.bounds()
	style, _, _ := wGetLong.Call(w.hwnd, widgetSigned(-20))
	child, _, _ := widgetUser.NewProc("GetWindow").Call(w.hwnd, 5)
	alpha := false
	if w.frame != nil {
		alpha = w.frame.RGBAAt(0, 0).A == 0 && w.frame.RGBAAt(int(30*w.dpi), int(40*w.dpi)).A > 0
	}
	return Object{"layered": style&0x80000 != 0, "fixedSize": int(r.Right-r.Left) == int(math.Round(widgetWidth*float64(w.dpi))) && int(r.Bottom-r.Top) == int(math.Round(widgetHeight*float64(w.dpi))), "rendered": w.frame != nil && w.renderError == nil, "perPixelAlpha": alpha, "noBrowserChildren": child == 0, "dpi": w.dpi}
}
func (w *widgetWindow) verifyDrag(checks Object) {
	r := w.bounds()
	w.down, w.dragging, w.moved = true, true, false
	w.origin = widgetPoint{X: r.Left, Y: r.Top}
	w.grab = widgetPoint{X: r.Left + int32(488*w.dpi), Y: r.Top + int32(72*w.dpi)}
	w.movePointer(widgetPoint{X: w.grab.X - 40, Y: w.grab.Y + 20})
	next := w.bounds()
	checks["dragWorked"] = next.Left != r.Left && next.Top != r.Top && next.Right-next.Left == r.Right-r.Left
	checks["dragDidNotExpand"] = w.host.model.Collapsed
	w.down = false
	w.dragging = false
	w.place(int(float32(r.Left)/w.dpi), int(float32(r.Top)/w.dpi))
	w.host.persist()
}
