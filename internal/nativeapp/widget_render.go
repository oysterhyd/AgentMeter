package nativeapp

import (
	"fmt"
	"image"
	"time"

	"github.com/egoist/mygo/ui"
)

var widgetInk = ui.Hex("#10243f")
var widgetMuted = ui.Hex("#506783")
var widgetGreen = ui.Hex("#008f80")

// The original CSS surface, with its three gradient layers and alpha values.
var widgetGlass = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 536 356"><defs><linearGradient id="base" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#eff7ff" stop-opacity=".91"/><stop offset="1" stop-color="#dbe9f6" stop-opacity=".85"/></linearGradient><radialGradient id="cyan" cx="0" cy="0" r=".8485" gradientTransform="matrix(1 0 0 1 0 0)"><stop stop-color="#c2f6ff" stop-opacity=".631"/><stop offset="1" stop-color="#c2f6ff" stop-opacity="0"/></radialGradient><radialGradient id="purple" cx="1" cy=".15" r=".9192"><stop stop-color="#d9ccf5" stop-opacity=".612"/><stop offset="1" stop-color="#d9ccf5" stop-opacity="0"/></radialGradient></defs><rect width="536" height="356" fill="url(#base)"/><rect width="536" height="356" fill="url(#purple)"/><rect width="536" height="356" fill="url(#cyan)"/></svg>`))
var widgetGlint = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 112 112"><defs><radialGradient id="white" cx=".3" cy=".08" r=".6"><stop stop-color="#ffffff" stop-opacity=".79"/><stop offset="1" stop-color="#ffffff" stop-opacity="0"/></radialGradient><radialGradient id="mint" cx=".8" cy=".95" r=".67"><stop stop-color="#9fdfdb" stop-opacity=".4"/><stop offset="1" stop-color="#9fdfdb" stop-opacity="0"/></radialGradient></defs><rect width="112" height="112" fill="url(#mint)"/><rect width="112" height="112" fill="url(#white)"/></svg>`))
var widgetLight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><radialGradient id="light"><stop stop-color="#ffffff" stop-opacity=".78"/><stop offset="1" stop-color="#ffffff" stop-opacity="0"/></radialGradient></defs><rect width="100" height="100" fill="url(#light)"/></svg>`))

type widgetPainter struct {
	model                 *widgetModel
	brand                 *ui.Bitmap
	card, orb, frame      *ui.Tester
	metricLayers          [4]*ui.Tester
	metricBitmaps         [4]*ui.Bitmap
	cardBitmap, orbBitmap *ui.Bitmap
	filteredCard          *ui.Bitmap
	cardImage             *image.RGBA
	frameImage            *image.RGBA
	lastShape             float32
	blur                  widgetBlurWorkspace
	scale                 float32
	dirty                 bool
	interactionDirty      bool
	metricState           [4]widgetMetricPaint
}

type widgetMetricPaint struct {
	bar, hover float32
	light      [2]float32
}

func newWidgetPainter(model *widgetModel, brand *ui.Bitmap) *widgetPainter {
	return &widgetPainter{model: model, brand: brand, dirty: true}
}
func (p *widgetPainter) invalidate()            { p.dirty = true; p.model.dirty = true }
func (p *widgetPainter) invalidateInteraction() { p.interactionDirty = true; p.model.dirty = true }
func (p *widgetPainter) releaseCard() {
	p.card, p.cardImage, p.cardBitmap, p.filteredCard = nil, nil, nil, nil
	p.metricLayers = [4]*ui.Tester{}
	p.metricBitmaps = [4]*ui.Bitmap{}
	p.metricState = [4]widgetMetricPaint{}
	p.blur = widgetBlurWorkspace{}
}
func (p *widgetPainter) release() {
	*p = widgetPainter{model: p.model, brand: p.brand, dirty: true}
	p.model.dirty = true
}
func widgetTheme(c *ui.Context) {
	t := ui.LightTheme()
	t.Background = ui.Transparent
	t.Font = "Segoe UI Variable, Segoe UI, Microsoft YaHei UI"
	t.Text = widgetInk
	c.SetTheme(t)
}
func widgetBox(c *ui.Context, x, y, w, h float32) ui.Element {
	return ui.Box(c).Absolute().Left(x).Top(y).Size(w, h)
}
func widgetText(c *ui.Context, s string, x, y, w, size, line float32, color ui.Color, weight int) ui.Element {
	return ui.Text(c, s).Absolute().Left(x).Top(y).Width(w).FontSize(size).FixedLineHeight(line).TextColor(color).FontWeight(weight).NoWrap()
}
func widgetIcon(c *ui.Context, name string, x, y, size float32, color ui.Color) ui.Element {
	return ui.Icon(c, icons[name]).Absolute().Left(x).Top(y).Size(size, size).TextColor(color)
}
func (p *widgetPainter) render(now time.Time, scale float32) *image.RGBA {
	m := p.model
	m.clock = now
	if p.scale != scale {
		p.release()
		p.scale = scale
	}
	for i, metric := range m.metrics(now) {
		target := float32(min(100., max(0., number(metric.Value))))
		if m.bars[i].target != target {
			d := 750 * time.Millisecond
			if m.ReduceMotion {
				d = 0
			}
			m.bars[i].to(target, now, d)
			p.dirty = true
		}
	}
	needCard, needOrb := !m.Collapsed || m.morphing(now), m.Collapsed || m.morphing(now)
	if !needCard {
		p.releaseCard()
	}
	if needCard && p.card == nil {
		p.card = ui.NewTester(p.cardView, 536, 356)
		p.card.SetScale(scale)
		p.dirty = true
	}
	if needOrb && p.orb == nil {
		p.orb = ui.NewTester(p.orbView, 120, 120)
		p.orb.SetScale(scale)
		p.dirty = true
	}
	changed := p.dirty || p.interactionDirty
	cardChanged := needCard && (changed || m.Busy && !m.ReduceMotion)
	for i := 0; i < 4; i++ {
		if !needCard {
			break
		}
		state := widgetMetricPaint{bar: m.bars[i].value(now), hover: m.hoverMotion[i].value(now), light: m.light[i]}
		if p.dirty || state != p.metricState[i] {
			if p.metricLayers[i] == nil {
				index := i
				p.metricLayers[i] = ui.NewTester(func(c *ui.Context) { p.metricView(c, index) }, 159, 160)
				p.metricLayers[i].SetScale(scale)
			} else {
				p.metricLayers[i].Frame()
			}
			img := p.metricLayers[i].Image()
			hover := m.hoverMotion[i].value(now)
			if hover > 0 && !m.ReduceMotion {
				img = widgetTilt(img, m, widgetMetricRect(i), hover, scale)
			}
			p.metricBitmaps[i] = ui.NewBitmap(img)
			p.metricState[i] = state
			cardChanged = true
		}
	}
	if cardChanged {
		p.card.Frame()
		p.cardImage = p.card.Image()
		p.cardBitmap = ui.NewBitmap(p.cardImage)
	}
	orbChanged := needOrb && (changed || m.hoverMotion[4].active(now))
	if orbChanged {
		p.orb.Frame()
		p.orbBitmap = ui.NewBitmap(p.orb.Image())
	}
	p.dirty = false
	p.interactionDirty = false
	if !m.morphing(now) {
		p.blur = widgetBlurWorkspace{}
	}
	shape := m.shape.value(now)
	if p.frame != nil && !changed && !cardChanged && !orbChanged && shape == p.lastShape && !m.morphing(now) {
		return p.frameImage
	}
	p.filteredCard = p.cardBitmap
	if !m.ReduceMotion && m.morphing(now) {
		elapsed := float32(now.Sub(m.shape.start)) / float32(time.Millisecond)
		sigma := float64(7 * widgetEase(elapsed/300) * scale)
		visible := elapsed < 170
		if !m.Collapsed {
			sigma = float64(7 * (1 - widgetEase(elapsed/360)) * scale)
			visible = elapsed > 180
		}
		if visible && sigma > .35 {
			p.filteredCard = ui.NewBitmap(p.blur.apply(p.cardImage, sigma))
		}
	}
	if p.frame == nil {
		p.frame = ui.NewTester(p.frameView, widgetWidth, widgetHeight)
		p.frame.SetScale(scale)
	} else {
		p.frame.Frame()
	}
	p.lastShape, p.frameImage = shape, p.frame.Image()
	return p.frameImage
}
func (p *widgetPainter) frameView(c *ui.Context) {
	widgetTheme(c)
	m := p.model
	now := m.clock
	r := m.glass(now)
	shape := m.shape.value(now)
	// Native window alpha composes visibility without rebuilding this scene.
	alpha, size := float32(1), float32(1)
	radius := (30 + 30*shape) * size
	outer := widgetBox(c, r.X, r.Y, r.W, r.H).Radius(radius).Opacity(alpha)
	outer.Draw(func(p *ui.Painter, r ui.Rect) { p.Shadow(r, radius, 0, 5, 11, 0, ui.Hex("#19335730")) })
	outer.Children(func() {
		if m.ReduceTransparency {
			ui.Box(c).Fill().Background(ui.Hex("#e4edf8"))
		} else {
			ui.Image(c, widgetGlass).Fill().Fit(ui.FillBox)
		}
	}).Border(1, ui.Hex("#ffffffde")).Clip()
	// Inner top/bottom highlights are independent from the drop shadow.
	widgetBox(c, r.X+1, r.Y+1, r.W-2, r.H-2).Radius(max(0, radius-1)).Opacity(alpha).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Stroke(r, ui.Hex("#ffffff55"), max(0, radius-1), 1)
	})
	elapsed := float32(now.Sub(m.shape.start)) / float32(time.Millisecond)
	cardOpacity, orbOpacity, contentScale, orbScale := float32(1), float32(0), float32(1), float32(.65)
	if !m.shape.start.IsZero() {
		if m.Collapsed {
			cardOpacity = 1 - widgetEase(elapsed/170)
			orbOpacity = widgetEase((elapsed - 220) / 180)
			contentScale = 1 - .72*motionEase(elapsed/650)
			orbScale = .65 + .35*motionEase(max(0, elapsed-220)/420)
		} else {
			cardOpacity = widgetEase((elapsed - 180) / 240)
			orbOpacity = 1 - widgetEase(elapsed/180)
			contentScale = .28 + .72*motionEase(elapsed/650)
			orbScale = 1 - .35*motionEase(elapsed/420)
		}
	}
	if m.ReduceMotion {
		if m.Collapsed {
			cardOpacity, orbOpacity, contentScale, orbScale = 0, 1, .28, 1
		} else {
			cardOpacity, orbOpacity, contentScale, orbScale = 1, 0, 1, .65
		}
	}
	if cardOpacity > 0 {
		w, h := 536*contentScale*size, 356*contentScale*size
		x := 12 + 476*(1-contentScale)
		y := 12 + 60*(1-contentScale)
		ui.Image(c, p.filteredCard).Absolute().Left(x).Top(y+7*(1-alpha)).Size(w, h).Fit(ui.FillBox).Opacity(cardOpacity * alpha)
	}
	if orbOpacity > 0 {
		s := 120 * orbScale * size
		box := widgetBox(c, 428+(120-s)/2, 12+(120-s)/2+7*(1-alpha), s, s).Opacity(orbOpacity * alpha)
		box.Children(func() { ui.Image(c, p.orbBitmap).Fill().Fit(ui.FillBox) })
	}
	if m.focus >= 0 && m.keyboard {
		var focus ui.Rect
		if m.Collapsed {
			focus = ui.Rect{X: 428, Y: 12, W: 120, H: 120}
		} else {
			focus = widgetRegionRect(m.focus)
		}
		radius := float32(12)
		if m.Collapsed {
			radius = 63
		} else if m.focus >= 1 && m.focus <= 5 {
			radius = 21
		}
		widgetBox(c, focus.X-3, focus.Y-3, focus.W+6, focus.H+6).Radius(radius).Border(2, ui.Hex("#008d88")).Opacity(alpha)
	}
}
func widgetEase(t float32) float32 {
	t = max(0, min(1, t))
	if t == 0 || t == 1 {
		return t
	}
	lo, hi := float32(0), float32(1)
	for i := 0; i < 14; i++ {
		m := (lo + hi) / 2
		u := 1 - m
		x := 3*u*u*m*.25 + 3*u*m*m*.25 + m*m*m
		if x < t {
			lo = m
		} else {
			hi = m
		}
	}
	m := (lo + hi) / 2
	u := 1 - m
	return 3*u*u*m*.1 + 3*u*m*m + m*m*m
}
func (p *widgetPainter) panel(c *ui.Context, r ui.Rect, radius float32, index int) {
	m := p.model
	hover := float32(0)
	if index >= 0 {
		hover = m.hoverMotion[index].value(m.clock)
	}
	bg1, bg2 := ui.Hex("#ffffffa8"), ui.Hex("#f4faff60")
	if m.ReduceTransparency {
		bg1, bg2 = ui.Hex("#f1f6fc"), ui.Hex("#f1f6fc")
	}
	box := widgetBox(c, r.X, r.Y, r.W, r.H).Radius(radius).Border(1, ui.Hex("#ffffff9c"))
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Shadow(r, radius, 0, 2+7*hover, 3+15*hover, 0, ui.Hex("#346580").Alpha(.02+.10*hover))
		p.FillGradient(r, ui.LinearGradient{From: bg1, To: bg2, Angle: 135}, radius)
	})
	if hover > 0 {
		size := max(r.W, r.H) * 1.6
		x, y := m.light[index][0]*r.W-size/2, m.light[index][1]*r.H-size/2
		box.Clip().Children(func() {
			ui.Image(c, widgetLight).Absolute().Left(x).Top(y).Size(size, size).Fit(ui.FillBox).Opacity(.55 * hover)
		})
	}
}
func (p *widgetPainter) cardView(c *ui.Context) {
	widgetTheme(c)
	m := p.model
	now := m.clock
	if p.brand != nil {
		ui.Image(c, p.brand).Absolute().Left(18).Top(20.5).Size(40, 40).Radius(12).Shadow(0, 4, 10, 0, ui.Hex("#698fa32b"))
	}
	widgetText(c, "Codex Monitor", 68, 16.367, 290, 20, 24, widgetInk, 700).LetterSpacing(-.6)
	widgetText(c, m.t("本机用量 · 全部账号", "Local usage · all accounts"), 68, 45.7, 300, 11, 17.6, widgetMuted, 400)
	live := m.collecting(now)
	status := m.t("等待中", "Waiting")
	color := ui.Hex("#795c25")
	dot := ui.Hex("#bc9444")
	if live {
		status = m.t("采集中", "Live")
		color = ui.Hex("#00776a")
		dot = ui.Hex("#00a58a")
	}
	statusW := float32(64)
	if m.en() {
		statusW = 57
		if !live {
			statusW = 78
		}
	}
	sx := 482 - statusW - 10
	widgetBox(c, sx, 25, statusW, 32).Radius(20).Background(ui.Hex("#ffffff60")).Border(1, ui.Hex("#ffffff80"))
	widgetBox(c, sx+10, 38, 6, 6).Radius(3).Background(dot).Shadow(0, 0, 0, 3, ui.Hex("#5bdbba32"))
	widgetText(c, status, sx+22, 32.2, statusW-25, 11, 17.6, color, 400)
	menu := widgetBox(c, 482, 22.5, 36, 36).Radius(18).Background(ui.Hex("#ffffff60"))
	if m.hover == 1 {
		menu.Background(ui.Hex("#ffffffdc")).Shadow(0, 3, 12, 0, ui.Hex("#6589a62b"))
	}
	widgetIcon(c, "dots", 487, 27.5, 26, widgetInk)
	for i := 0; i < 4; i++ {
		r := widgetMetricRect(i)
		hover := m.hoverMotion[i].value(now)
		if m.ReduceMotion {
			hover = 0
		}
		ui.Image(c, p.metricBitmaps[i]).Absolute().Left(r.X-12-20).Top(r.Y-12-20-3*hover).Size(159, 160).Fit(ui.FillBox)
	}
	p.panel(c, ui.Rect{X: 18, Y: 211, W: 500, H: 105}, 19, 4)
	widgetBox(c, 181, 225, 1, 77).Background(ui.Hex("#6c90bb20"))
	widgetBox(c, 32, 225, 26, 26).Radius(8).Background(ui.Hex("#4ddbc32d"))
	widgetIcon(c, "cube", 35, 228, 20, widgetGreen)
	widgetText(c, m.t("今日 Token", "Today’s tokens"), 65, 228.4, 126, 12, 19.2, widgetMuted, 400)
	widgetText(c, widgetCompact(m.Data["total"]), 32, 254, 146, 29, 31.9, widgetInk, 700).LetterSpacing(-1).FontFeatures("tnum")
	change := m.t("较昨日 —", "vs yest. —")
	if m.Data["change"] != nil {
		change = m.t("较昨日 ", "vs yest. ") + fmt.Sprintf("%+.1f%%", number(m.Data["change"])*100)
	}
	ui.Row(c).Absolute().Left(32).Top(293.9).Height(17.6).Gap(2).AlignItems(ui.Center).Children(func() {
		ui.Text(c, change).FontSize(11).FixedLineHeight(17.6).TextColor(widgetMuted).NoWrap()
		if m.Data["change"] != nil {
			arrow := ui.Icon(c, icons["up-right"]).Size(13, 13).TextColor(ui.Hex("#009f8a"))
			if number(m.Data["change"]) < 0 {
				arrow.Rotate(90)
			}
		}
	})
	heading := ui.Row(c).Absolute().Left(195).Top(225).Height(19.2).Gap(4).AlignItems(ui.Center)
	heading.Children(func() {
		ui.Text(c, m.t("Token 使用趋势", "Token usage")).FontSize(12).FontWeight(500).TextColor(widgetMuted)
		ui.Text(c, m.t("(近 24 小时)", "(last 24h)")).FontSize(10).TextColor(widgetMuted)
	})
	timeline := objects(m.Data["timeline"])
	chart := ui.Rect{X: 195, Y: 252.2, W: 285, H: 43}
	p.spark(c, timeline, chart, true)
	maxValue := 0.
	for _, point := range timeline {
		maxValue = max(maxValue, number(point["total"]))
	}
	widgetText(c, widgetCompact(maxValue), 482, 249.2, 30, 9, 14.4, widgetMuted, 400)
	for j, i := range []int{0, 48, 95} {
		label := "—"
		if i < len(timeline) {
			label = parseTime(timeline[i]["time"]).Local().Format("15:04")
		}
		x := 195 + float32(j)*142.5
		if j == 1 {
			x -= 14
		}
		if j == 2 {
			x -= 28
		}
		widgetText(c, label, x, 299.2, 34, 9, 14.4, widgetMuted, 400)
	}
	widgetIcon(c, "clock", 19, 330.5, 16, widgetMuted)
	footer := m.t("最后更新：", "Updated: ") + "—"
	if last := parseTime(obj(m.Data["scan"])["lastScan"]); !last.IsZero() {
		footer = m.t("最后更新：", "Updated: ") + last.Local().Format("01/02 15:04")
	}
	if m.Error != "" {
		footer = m.t("连接中断 · 自动重试", "Disconnected · retrying")
	}
	widgetText(c, footer, 40, 329.7, 340, 11, 17.6, widgetMuted, 400)
	refresh := m.t("自动刷新中", "Auto-refreshing")
	if m.Busy {
		refresh = m.t("刷新中", "Refreshing")
	}
	refreshW := float32(91)
	if m.en() {
		refreshW = 120
	}
	x := 517 - refreshW
	if m.hover == 6 {
		widgetBox(c, x, 325, refreshW, 27).Radius(9).Background(ui.Hex("#ffffffa0"))
	}
	spin := widgetIcon(c, "refresh", x+6, 330.5, 16, widgetMuted)
	if m.Busy && !m.ReduceMotion {
		spin.Rotate(float32(now.UnixMilli()%1200) * .3)
	}
	label := widgetText(c, refresh, x+27, 329.7, refreshW-27, 11, 17.6, widgetMuted, 400)
	if m.Busy {
		spin.Opacity(.5)
		label.Opacity(.5)
	}
}
func (p *widgetPainter) orbView(c *ui.Context) {
	widgetTheme(c)
	m := p.model
	widgetBox(c, 4, 4, 112, 112).Radius(56).Clip().Children(func() { ui.Image(c, widgetGlint).Fill().Fit(ui.FillBox) }).Border(1, ui.Hex("#ffffffb3"))
	widgetIcon(c, "cube", 50.5, 17.333, 19, widgetGreen)
	widgetText(c, m.t("今日 Token", "Today’s tokens"), 0, 39.333, 120, 10, 16, widgetMuted, 400).TextAlign(ui.Center)
	widgetText(c, widgetCompact(m.Data["total"]), 0, 55.333, 120, 23, 32.2, widgetInk, 700).LetterSpacing(-.8).FontFeatures("tnum").TextAlign(ui.Center)
	live := m.collecting(m.clock)
	status := m.t("等待中", "Waiting")
	color := ui.Hex("#795c25")
	dot := ui.Hex("#bc9444")
	if live {
		status = m.t("实时", "Live")
		color = ui.Hex("#00776a")
		dot = ui.Hex("#00a58a")
	}
	x := float32(40)
	if !live {
		x = 35
	}
	widgetBox(c, x, 92.533, 4, 4).Radius(2).Background(dot).Shadow(0, 0, 0, 3, ui.Hex("#5bdbba32"))
	widgetText(c, status, x+9, 87.333, 50, 9, 14.4, color, 400)
}
func (p *widgetPainter) spark(c *ui.Context, points []Object, r ui.Rect, large bool) {
	box := widgetBox(c, r.X, r.Y, r.W, r.H)
	if !large {
		box.Clip()
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		maximum := 1.
		for _, point := range points {
			maximum = max(maximum, number(point["total"]))
		}
		if large {
			for _, v := range []float32{0, .5, 1} {
				for x := r.X; x < r.X+r.W; x += 8 {
					p.Line(x, r.Y+r.H*v, min(x+3, r.X+r.W), r.Y+r.H*v, 1, ui.Hex("#6785aa").Alpha(.16))
				}
			}
			for _, v := range []float32{0, .25, .5, .75, 1} {
				for y := r.Y; y < r.Y+r.H; y += 8 {
					p.Line(r.X+r.W*v, y, r.X+r.W*v, min(y+3, r.Y+r.H), 1, ui.Hex("#6785aa").Alpha(.12))
				}
			}
		}
		if len(points) == 0 {
			return
		}
		if large {
			r.H *= 82. / 84
		} else {
			r.H *= 30. / 32
		}
		line := &ui.Path{}
		fill := &ui.Path{}
		for i, point := range points {
			x := r.X + float32(i)*r.W/float32(max(1, len(points)-1))
			y := r.Y + r.H - float32(number(point["total"])/maximum)*(r.H-3*r.H/82)
			if !large {
				y = r.Y + r.H - float32(number(point["total"])/maximum)*(r.H-3*r.H/30)
			}
			if i == 0 {
				line.MoveTo(x, y)
				fill.MoveTo(x, y)
			} else {
				line.LineTo(x, y)
				fill.LineTo(x, y)
			}
		}
		fill.LineTo(r.X+r.W, r.Y+r.H).LineTo(r.X, r.Y+r.H).Close()
		p.FillPath(fill, ui.Hex("#00a88e").Alpha(.13))
		width := float32(2)
		if !large {
			width = 3.5
		}
		p.StrokePath(line, width, ui.Hex("#00a58e"))
	})
}

// These are also the Win32 hit-test, tooltip and keyboard-focus regions.
func widgetRegionRect(index int) ui.Rect {
	switch index {
	case 0:
		return ui.Rect{X: 30, Y: 32.5, W: 40, H: 40}
	case 1:
		return ui.Rect{X: 494, Y: 34.5, W: 36, H: 36}
	case 2, 3, 4, 5:
		return widgetMetricRect(index - 2)
	case 6:
		return ui.Rect{X: 419, Y: 337, W: 110, H: 27}
	case 7:
		return ui.Rect{X: 428, Y: 12, W: 120, H: 120}
	}
	return ui.Rect{}
}
func (m *widgetModel) hit(x, y float32, now time.Time) int {
	if !m.glass(now).Contains(x, y) {
		return -1
	}
	if m.Collapsed {
		if widgetRegionRect(7).Contains(x, y) {
			return 7
		}
		return 9
	}
	for _, i := range []int{0, 1, 2, 3, 4, 5, 6} {
		if widgetRegionRect(i).Contains(x, y) {
			return i
		}
	}
	if (ui.Rect{X: 30, Y: 223, W: 500, H: 105}).Contains(x, y) {
		if x < 194 {
			return 8
		}
		return 12
	}
	if (ui.Rect{X: 80, Y: 57.7, W: 330, H: 17.6}).Contains(x, y) {
		return 10
	}
	if (ui.Rect{X: 31, Y: 337, W: 330, H: 27}).Contains(x, y) {
		return 11
	}
	return 9
}
func (m *widgetModel) pointer(x, y float32, now time.Time) bool {
	old := m.hover
	m.hover = m.hit(x, y, now)
	m.pointerX, m.pointerY = x, y
	if m.hover >= 2 && m.hover <= 5 {
		index := m.hover - 2
		r := widgetMetricRect(index)
		m.light[index] = [2]float32{(x - r.X) / r.W, (y - r.Y) / r.H}
	} else if m.hover == 8 || m.hover == 12 {
		m.light[4] = [2]float32{(x - 30) / 500, (y - 223) / 105}
	}
	if old < 0 && m.hover < 0 && (!m.keyboard || m.focus < 2 || m.focus > 5) {
		return false
	}
	m.dirty = true
	for i := range m.hoverMotion {
		target := float32(0)
		if m.hover == i+2 || (m.keyboard && m.focus == i+2) || (i == 4 && (m.hover == 8 || m.hover == 12)) {
			target = 1
		}
		if m.hoverMotion[i].target != target {
			d := 350 * time.Millisecond
			if m.ReduceMotion {
				d = 0
			}
			m.hoverMotion[i].to(target, now, d)
		}
	}
	if old != m.hover {
		m.pressed = false
	}
	return true
}
func (m *widgetModel) tooltip(region int, now time.Time) string {
	switch region {
	case 0:
		return m.t("打开主窗口", "Open monitor")
	case 1:
		return m.t("收起为圆球 · 右键打开选项", "Collapse to orb · Right-click for options")
	case 2, 3, 4, 5:
		return m.metrics(now)[region-2].Tip
	case 6:
		return m.t("立即刷新采集与额度", "Refresh usage and quota now")
	case 7, 8, 10:
		return m.scope()
	case 11:
		if m.Error != "" {
			return m.Error
		}
		return m.t("本机采集更新时间；额度采样时间见指标提示", "Local collection time; hover quotas for their sample times")
	}
	return ""
}

func (p *widgetPainter) metricView(c *ui.Context, i int) {
	widgetTheme(c)
	m := p.model
	now := m.clock
	metric := m.metrics(now)[i]
	r := ui.Rect{X: 20, Y: 20, W: 118.25, H: 120}
	hover := m.hoverMotion[i].value(now)
	if m.ReduceMotion {
		hover = 0
	}
	p.panel(c, r, 18, i)
	iconColor, iconBG := widgetGreen, ui.Hex("#4ddbc32d")
	if metric.Speed {
		iconColor, iconBG = ui.Hex("#1671ff"), ui.Hex("#67a8ff29")
	}
	widgetBox(c, r.X+11, r.Y+13, 26, 26).Radius(8).Background(iconBG)
	widgetIcon(c, metric.Icon, r.X+14, r.Y+16-hover, 20, iconColor).Rotate(-6 * hover)
	widgetText(c, metric.Label, r.X+11, r.Y+44, r.W-22, 12, 19.2, ui.Hex("#465f7d"), 400)
	value := widgetPercent(metric.Value)
	if metric.Speed {
		value = "—"
		if metric.Value != nil {
			value = fmt.Sprintf("%.1f", number(metric.Value))
		}
	}
	valueRow := ui.Row(c).Absolute().Left(r.X + 11).Top(r.Y + 73.2).Gap(3).Height(32.4).AlignItems(ui.End)
	valueRow.Children(func() {
		ui.Text(c, value).FontSize(27).FixedLineHeight(32.4).TextColor(widgetInk).Bold().LetterSpacing(-1).FontFeatures("tnum")
		if metric.Speed {
			ui.Text(c, "tok/s").FontSize(10).FixedLineHeight(12).TextColor(widgetMuted).FontWeight(500).Margin(0, 0, 4, 0)
		}
	})
	if metric.Speed {
		p.spark(c, objects(m.Data["speed"]), ui.Rect{X: r.X + 11, Y: r.Y + 101.6, W: r.W - 22, H: 14}, false)
	} else {
		bar := ui.Rect{X: r.X + 11, Y: r.Y + 109.6, W: r.W - 22, H: 5}
		widgetBox(c, bar.X, bar.Y, bar.W, bar.H).Radius(6).Background(ui.Hex("#b8c9df66"))
		widgetBox(c, bar.X, bar.Y, bar.W*m.bars[i].value(now)/100, bar.H).Radius(6).Draw(func(p *ui.Painter, r ui.Rect) {
			p.FillGradient(r, ui.LinearGradient{From: ui.Hex("#00a58b"), To: ui.Hex("#43c7ab"), Angle: 90}, 6)
		})
	}
}
