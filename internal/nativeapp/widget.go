package nativeapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

// The widget uses the parent's service and GUI thread, without another process.
type widgetHost struct {
	app                               *App
	window                            *widgetWindow
	model                             widgetModel
	painter                           *widgetPainter
	saved                             widgetPlacement
	cancel                            context.CancelFunc
	closed, fetching, pending, manual bool
	wasAnimating                      bool
	lastPoll                          time.Time
	hiddenGeneration                  uint64
	measurement                       *switchMeasurement
	staged                            *widgetResult
}
type widgetResult struct {
	data Object
	err  error
}
type widgetPlacement struct {
	PositionKnown bool `json:"-"`
	X             int  `json:"x"`
	Y             int  `json:"y"`
	Width         int  `json:"width"`
	Height        int  `json:"height"`
	Topmost       bool `json:"topmost"`
	TopmostChoice bool `json:"topmostChoice"`
	Mode          bool `json:"mode"`
}

func (h *widgetHost) persist() {
	h.saved.Width, h.saved.Height = widgetWidth, widgetHeight
	h.saved.TopmostChoice = true
	b, _ := json.Marshal(h.saved)
	_ = os.WriteFile(filepath.Join(h.app.options.Data, "widget-window.json"), b, 0600)
}
func (h *widgetHost) show() bool {
	h.hiddenGeneration++
	started := time.Now()
	h.model.setVisible(true, time.Now())
	h.saved.Mode = true
	h.tick(time.Now()) // Present a valid surface before exposing the window.
	if h.closed {
		return false
	}
	if h.measurement != nil {
		h.measurement.stage("prepareScene", started)
	}
	started = time.Now()
	h.window.show()
	if h.measurement != nil {
		h.measurement.stage("showWindow", started)
	}
	started = time.Now()
	h.persist()
	h.update()
	if h.measurement != nil {
		h.measurement.stage("persistAndRequest", started)
	}
	return true
}
func (h *widgetHost) hide(animate ...bool) {
	h.model.setVisible(false, time.Now())
	h.saved.Mode = false
	h.persist()
	if len(animate) > 0 && animate[0] {
		h.window.wake()
	} else {
		h.model.visibility.duration = 0
		h.window.hide()
	}
}
func (h *widgetHost) close() {
	if h.closed {
		return
	}
	h.closed = true
	h.cancel()
	h.window.close()
	h.painter.release()
}

// Preserve buffers for rapid reversals, then let an unused widget's complete
// scene and native DIB go. Data, placement, focus and animation state live in
// the model, so showing it again rebuilds the same pixels.
func (h *widgetHost) releaseWhenHidden() {
	if h.closed {
		return
	}
	h.hiddenGeneration++
	generation := h.hiddenGeneration
	time.AfterFunc(2*time.Second, func() {
		if h.window.context().Err() != nil {
			return
		}
		h.app.win.Update(func() {
			if h.closed || h.model.Visible || generation != h.hiddenGeneration {
				return
			}
			h.painter.release()
			h.window.releasePixels()
			reclaimGUIMemory()
		})
	})
}
func (h *widgetHost) update() { h.request(false) }
func (h *widgetHost) request(manual bool) {
	if h.closed || !h.model.Visible {
		return
	}
	if h.fetching {
		h.pending = true
		h.manual = h.manual || manual
		if manual {
			h.model.Busy = true
			h.window.wake()
		}
		return
	}
	h.fetching = true
	h.model.Busy = manual
	h.lastPoll = time.Now()
	h.window.wake()
	go func() {
		ctx, cancel := context.WithTimeout(h.window.context(), 2*time.Minute)
		defer cancel()
		var data Object
		var err error
		if manual {
			_, err = h.app.client.Call(ctx, "refresh", nil)
		}
		if err == nil {
			data, err = h.app.client.Object(ctx, "widget", nil)
		}
		h.app.win.Update(func() {
			if h.closed {
				return
			}
			h.fetching = false
			h.staged = &widgetResult{data: data, err: err}
			h.window.wake()
			if h.pending {
				manual := h.manual
				h.pending, h.manual = false, false
				h.request(manual)
			}
		})
	}()
}

// syncTheme follows the app into light or dark appearance.
func (h *widgetHost) syncTheme() {
	if h.closed || h.app.win == nil {
		return
	}
	if dark := mygo.Theme.IsDark(); dark != h.model.Dark {
		h.model.Dark = dark
		h.painter.invalidate()
		h.window.wake()
	}
}
func (h *widgetHost) tick(now time.Time) {
	if h.closed {
		return
	}
	started := time.Now()
	// Commit incoming content after the short crossfade. Heavy scene work
	// must not occupy the GUI thread between its native alpha steps.
	if h.staged != nil && !h.model.visibility.active(now) && !h.app.mainFade.active(now) {
		result := h.staged
		h.staged = nil
		h.model.Busy = false
		if result.err != nil {
			h.model.Error = result.err.Error()
		} else {
			h.model.Data, h.model.Error = result.data, ""
			h.app.widgetVerified = true
		}
		h.painter.invalidate()
	}
	if h.model.Visible && now.Sub(h.lastPoll) >= 3*time.Second {
		h.syncTheme()
		h.update()
	}
	active := h.model.animating(now)
	painting := active && !h.model.visibility.active(now) || h.model.morphing(now)
	// Visibility is composed by Windows; it does not invalidate the scene.
	for _, motion := range h.model.bars {
		painting = painting || motion.active(now)
	}
	for _, motion := range h.model.hoverMotion {
		painting = painting || motion.active(now)
	}
	painting = painting || h.model.Busy && !h.model.ReduceMotion
	if h.model.dirty || painting || h.wasAnimating {
		if h.measurement != nil {
			h.measurement.sceneRenders++
		}
		h.window.present(h.painter.render(now, h.window.scale()))
		if err := h.window.renderFailure(); err != nil {
			h.app.errorText = err.Error()
			h.close()
			h.app.widget = nil
			h.app.restore()
			return
		}
		h.model.dirty = false
	}
	h.window.fade(h.model.visibility.value(now))
	h.wasAnimating = painting
	h.app.tickMainFade(now)
	if h.measurement != nil && (active || h.app.mainFade.active(now)) {
		h.measurement.record(started, time.Since(started))
	}
	if !h.model.Visible && !active && !h.app.mainFade.active(now) {
		h.window.hide()
	}
}
func (h *widgetHost) activate(action string) {
	switch action {
	case "restore":
		h.app.restore()
	case "collapse", "orb":
		if h.model.toggleBusy(time.Now()) {
			return
		}
		h.window.anchor()
		h.model.toggle(time.Now())
		h.window.wake()
	case "refresh":
		h.request(true)
	case "topmost":
		h.saved.Topmost = !h.saved.Topmost
		h.window.topmost(h.saved.Topmost)
		h.persist()
	case "hide":
		h.hide(true)
		h.app.widgetMode = false
		h.app.updateTray()
	}
}
func (a *App) toggleWidget() {
	if a.widgetMode {
		a.restore()
		return
	}
	a.showWidget()
}
func (a *App) showWidget() {
	started := time.Now()
	if a.widgetMode {
		a.restore()
		return
	}
	if a.widget == nil {
		h := &widgetHost{app: a, saved: widgetPlacement{Topmost: true}}
		b, _ := os.ReadFile(filepath.Join(a.options.Data, "widget-window.json"))
		_ = json.Unmarshal(b, &h.saved)
		var oldPlacement map[string]any
		if json.Unmarshal(b, &oldPlacement) == nil {
			_, x := oldPlacement["x"]
			_, y := oldPlacement["y"]
			h.saved.PositionKnown = x && y
		}
		if !h.saved.TopmostChoice {
			h.saved.Topmost = true
		}
		h.model = newWidgetModel()
		h.model.ReduceMotion = a.reduceMotion
		h.model.Dark = a.pal != nil && a.pal.dark
		h.painter = newWidgetPainter(&h.model, a.brand)
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		w, err := newWidgetWindow(h, ctx)
		if err != nil {
			cancel()
			a.errorText = err.Error()
			return
		}
		h.window = w
		a.widget = h
	}
	if !a.widget.show() {
		return
	}
	a.widgetMode = true
	if a.reduceMotion || !a.win.IsVisible() {
		a.win.Hide()
		a.mainFade = widgetMotion{target: 0}
	} else {
		a.widgetClosing = true
		a.mainFade.to(0, time.Now(), motionQuick)
		a.widget.window.wake()
	}
	a.updateTray()
	if a.widget.measurement != nil {
		a.widget.measurement.stage("showWidgetTotal", started)
	}
}

func (a *App) tickMainFade(now time.Time) {
	if a.win == nil || a.mainFade.start.IsZero() {
		return
	}
	a.win.SetOpacity(float64(a.mainFade.value(now)))
	if !a.mainFade.active(now) {
		if a.mainFade.target == 0 {
			a.win.Hide()
			a.widgetClosing = false
			a.win.SetOpacity(1)
		}
		a.mainFade.start = time.Time{}
		if a.mainRefreshPending && a.mainFade.target == 1 {
			a.mainRefreshPending = false
			a.load()
		}
	}
}
