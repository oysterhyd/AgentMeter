package nativeapp

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestWindowFitsWorkAreaAndRepairsRestoredBounds(t *testing.T) {
	for _, work := range []mygo.Rectangle{{Width: 1707, Height: 912}, {Width: 1280, Height: 680}, {X: -1920, Y: -120, Width: 1920, Height: 1080}, {X: 1920, Width: 800, Height: 600}} {
		o := mainWindowOptions(work, false, nil)
		bounds := mygo.Rectangle{X: o.X, Y: o.Y, Width: o.Width, Height: o.Height}
		if !insideWorkArea(bounds, work) {
			t.Fatalf("initial bounds %+v outside %+v", bounds, work)
		}
		if o.UseContentSize || o.Frameless || o.TitleBarStyle != mygo.TitleBarHidden || o.Vibrancy != mygo.VibrancySidebar || o.DisableResize || o.DisableMove || o.DisableClose || o.DisableMinimize || o.DisableMaximize {
			t.Fatal("native window frame disabled")
		}
		if o.MinWidth > bounds.Width || o.MinHeight > bounds.Height {
			t.Fatal("minimum size exceeds work area")
		}
		bad := mygo.Rectangle{X: work.X - 20, Y: work.Y - 42, Width: 2000, Height: 1300}
		if fixed := fitWindowBounds(bad, work); !insideWorkArea(fixed, work) {
			t.Fatalf("restored bounds %+v", fixed)
		}
		if next := fitWindowBounds(bounds, work); next != bounds {
			t.Fatal("valid placement moved")
		}
	}
}

func TestPageDirectionAndReducedMotionKeepNavigationResponsive(t *testing.T) {
	a := fixtureApp(t)
	tt := ui.NewTester(a.View, 1380, 880)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Key(ui.Alt, ui.Key3)
	if a.page != 2 || a.pageEnterX != -16 || !tt.HasText("任务运行记录") {
		t.Fatal("forward transition/navigation")
	}
	a.mainScroll.Y = 200
	tt.Key(ui.Alt, ui.Key1)
	if a.page != 0 || a.pageEnterX != 16 || a.mainScroll.Y != 0 {
		t.Fatal("reverse transition/scroll reset")
	}
	tt.Key(ui.Alt, ui.Key5)
	if !tt.HasText("外观") {
		t.Fatal("settings with reduced motion")
	}
	if err := tt.Click("模型价格"); err != nil || a.settingsSection != 2 {
		t.Fatal("settings tab input blocked", err)
	}
}
