package nativeapp

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

// Acceptance drives the real native widget, in a synthetic offline profile.
func (a *App) verifyNativeWidget() {
	checks := Object{}
	capture := func(name string) {
		a.updateWait(func() {
			h := a.widget
			if h == nil {
				return
			}
			h.tick(time.Now())
			f, err := os.Create(filepath.Join(a.options.Capture, "widget-"+name+".png"))
			if err != nil {
				a.errorText = err.Error()
				return
			}
			err = png.Encode(f, h.painter.render(time.Now(), h.window.scale()))
			f.Close()
			if err != nil {
				a.errorText = err.Error()
			}
		})
	}
	time.Sleep(500 * time.Millisecond)
	a.updateWait(func() { checks = a.widget.window.verify(); a.widget.model.keyboard = false })
	capture("card")
	// Exercise the app theme API and the real widget's repaint path. No
	// system settings or personal profile preferences are changed.
	for _, source := range []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight} {
		a.updateWait(func() {
			mygo.Theme.SetSource(source)
			a.widget.syncTheme()
			checks["theme-"+string(source)] = a.widget.model.Dark == (source == mygo.ThemeDark)
		})
		capture("card-" + string(source))
	}
	a.updateWait(func() { mygo.Theme.SetSource(mygo.ThemeSystem); a.widget.syncTheme() })
	a.updateWait(func() { a.widget.model.pointer(75, 120, time.Now()); a.widget.painter.invalidate() })
	time.Sleep(360 * time.Millisecond)
	capture("hover")
	a.updateWait(func() {
		a.widget.model.pointer(-1, -1, time.Now())
		a.widget.model.ReduceMotion = false
		a.widget.activate("collapse")
	})
	time.Sleep(150 * time.Millisecond)
	capture("morph")
	time.Sleep(620 * time.Millisecond)
	capture("orb")
	a.updateWait(func() { mygo.Theme.SetSource(mygo.ThemeDark); a.widget.syncTheme() })
	capture("orb-dark")
	a.updateWait(func() { mygo.Theme.SetSource(mygo.ThemeSystem); a.widget.syncTheme() })
	a.updateWait(func() {
		h := a.widget
		checks["orbHit"] = h.model.hit(488, 72, time.Now()) == 7
		checks["transparentHit"] = h.model.hit(30, 210, time.Now()) == -1
		h.window.verifyDrag(checks)
		h.window.key(13)
	})
	time.Sleep(760 * time.Millisecond)
	// Drain real fetches and pause automatic polling while injecting the
	// expired/disconnected scenes. A fast response must not overwrite them.
	deadline := time.Now().Add(3 * time.Second)
	idle := false
	for time.Now().Before(deadline) {
		a.updateWait(func() {
			h := a.widget
			h.lastPoll = time.Now().Add(time.Hour)
			idle = !h.fetching && !h.pending
			if idle {
				h.tick(time.Now())
			}
		})
		if idle {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !idle {
		a.updateWait(func() { a.errorText = "原生小窗口验收等待数据刷新超时" })
		return
	}
	a.updateWait(func() {
		h := a.widget
		checks["keyboardExpand"] = !h.model.Collapsed
		h.model.Data["language"] = "en"
		h.model.Data["quotas"] = []any{map[string]any{"minutes": 300., "used": 20., "resets": float64(time.Now().Add(-time.Minute).Unix()), "ts": time.Now().Add(-6 * time.Minute).Format(time.RFC3339), "bucket": "codex"}}
		h.model.Data["quotaStatus"] = map[string]any{"ok": false}
		value, tip := h.model.quota(300, time.Now())
		checks["expiredUnknown"] = value == nil && len(tip) > 0
		h.painter.invalidate()
	})
	capture("english-expired")
	a.updateWait(func() { a.widget.model.Error = "Synthetic disconnect"; a.widget.painter.invalidate() })
	capture("disconnected")
	a.updateWait(func() {
		h := a.widget
		checks["errorVisible"] = h.painter.card.HasText("Disconnected · retrying")
		h.model.ReduceMotion = true
		h.model.toggle(time.Now())
		checks["reducedMotion"] = !h.model.morphing(time.Now())
		h.model.toggle(time.Now())
		h.model.Error = ""
		h.update()
	})
	time.Sleep(120 * time.Millisecond)
	a.updateWait(func() {
		h := a.widget
		checks["retryRecovered"] = h.model.Error == "" && a.widgetVerified
		h.activate("topmost")
		checks["topmostToggle"] = !h.saved.Topmost
		h.activate("topmost")
		h.activate("hide")
		checks["hidePersisted"] = !h.saved.Mode
		h.show()
		if h.window.renderFailure() != nil {
			a.errorText = h.window.renderFailure().Error()
		}
	})
	// Wait past the cache grace period, then drive a real cold restore.
	a.updateWait(func() { a.widget.activate("hide") })
	time.Sleep(2200 * time.Millisecond)
	a.updateWait(func() {
		h := a.widget
		checks["idleBuffersReleased"] = h.painter.frame == nil && h.painter.card == nil && h.window.pixelsReleased()
		h.show()
	})
	time.Sleep(400 * time.Millisecond)
	capture("cold")
	a.updateWait(func() {
		h := a.widget
		checks["coldRestored"] = h.painter.frame != nil && h.painter.card != nil && !h.window.pixelsReleased() && h.window.renderFailure() == nil && a.widgetVerified
		b, _ := json.Marshal(checks)
		_ = os.WriteFile(filepath.Join(a.options.Capture, "widget-result.json"), b, 0600)
		for _, key := range []string{"layered", "fixedSize", "rendered", "perPixelAlpha", "noBrowserChildren", "orbHit", "transparentHit", "keyboardExpand", "expiredUnknown", "errorVisible", "reducedMotion", "retryRecovered", "topmostToggle", "hidePersisted", "dragWorked", "idleBuffersReleased", "coldRestored"} {
			if !truth(checks[key]) {
				a.errorText = "原生小窗口验收失败：" + key
			}
		}
	})
}
