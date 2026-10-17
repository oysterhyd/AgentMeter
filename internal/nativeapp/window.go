package nativeapp

import (
	"github.com/egoist/mygo"
	"local.codex.monitor/internal/monitor"
	"time"
)

// Sizes are outer window DIPs, including the caption and resize borders.
func fitWindowBounds(bounds, work mygo.Rectangle) mygo.Rectangle {
	margin := 16
	if work.Width <= 0 || work.Height <= 0 {
		return bounds
	}
	width, height := max(1, work.Width-2*margin), max(1, work.Height-2*margin)
	bounds.Width, bounds.Height = min(bounds.Width, width), min(bounds.Height, height)
	bounds.X = max(work.X+margin, min(bounds.X, work.X+work.Width-margin-bounds.Width))
	bounds.Y = max(work.Y+margin, min(bounds.Y, work.Y+work.Height-margin-bounds.Height))
	return bounds
}

func mainWindowOptions(work mygo.Rectangle, hidden bool, view mygo.Content) mygo.WindowOptions {
	bounds := fitWindowBounds(mygo.Rectangle{X: work.X + (work.Width-1380)/2, Y: work.Y + (work.Height-960)/2, Width: 1380, Height: 960}, work)
	return mygo.WindowOptions{Title: monitor.AppName, X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height,
		MinWidth: min(980, max(1, work.Width-32)), MinHeight: min(700, max(1, work.Height-32)),
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancySidebar, Hidden: hidden, Content: view, StateKey: "main"}
}

// The SDK restores remembered bounds after applying WindowOptions. Repair an
// oversized/off-screen restore too, without moving a valid window to another display.
func (a *App) keepWindowVisible() {
	if a.win.IsFullScreen() {
		a.win.SetFullScreen(false)
	}
	if a.win.IsMaximized() {
		return
	}
	bounds := a.win.Bounds()
	work := mygo.Screen.DisplayMatching(bounds).WorkArea
	if next := fitWindowBounds(bounds, work); next != bounds {
		a.win.SetBounds(next)
	}
}

func insideWorkArea(bounds, work mygo.Rectangle) bool {
	return bounds.X >= work.X && bounds.Y >= work.Y && bounds.X+bounds.Width <= work.X+work.Width && bounds.Y+bounds.Height <= work.Y+work.Height
}

// Exercise actual native operations in the isolated acceptance window.
func (a *App) verifyWindowControls() {
	var original mygo.Rectangle
	checks := Object{}
	a.updateWait(func() {
		original = a.win.Bounds()
		work := mygo.Screen.DisplayMatching(original).WorkArea
		content := a.win.ContentBounds()
		checks["bounds"], checks["workArea"] = original, work
		checks["insideWorkArea"] = insideWorkArea(original, work)
		// The title bar is hidden: the window controls sit over the page,
		// so they show while the window's top edge is on screen.
		checks["captionVisible"] = content.Y >= original.Y && original.Y >= work.Y && content.Height > 0
		checks["resizable"], checks["movable"], checks["minimizable"], checks["maximizable"], checks["closable"] = a.win.IsResizable(), a.win.IsMovable(), a.win.IsMinimizable(), a.win.IsMaximizable(), a.win.IsClosable()
		moved := original
		moved.X += 18
		moved.Y += 10
		moved.Width -= 36
		moved.Height -= 32
		a.win.SetBounds(moved)
		next := a.win.Bounds()
		checks["moveWorked"] = next.X != original.X || next.Y != original.Y
		checks["resizeWorked"] = next.Width != original.Width && next.Height != original.Height
		a.win.SetBounds(original)
		a.win.Minimize()
		checks["minimizeWorked"] = a.win.IsMinimized()
		a.win.Restore()
		a.win.Maximize()
		checks["maximizeWorked"] = a.win.IsMaximized()
		a.win.Unmaximize()
		a.win.Close()
	})
	closed := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.updateWait(func() { closed = !a.win.IsVisible() })
		if closed {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	a.updateWait(func() {
		checks["closeToTrayWorked"] = closed
		a.restore()
		a.win.SetBounds(original)
		a.windowVerification = checks
		for _, key := range []string{"insideWorkArea", "captionVisible", "resizable", "movable", "minimizable", "maximizable", "closable", "moveWorked", "resizeWorked", "minimizeWorked", "maximizeWorked", "closeToTrayWorked"} {
			if !truth(checks[key]) {
				a.errorText = "窗口验收失败：" + key
			}
		}
	})
}
