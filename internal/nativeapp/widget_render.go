package nativeapp

import (
	"fmt"
	"image"
	"time"

	"github.com/egoist/mygo/ui"
	"local.codex.monitor/internal/monitor"
)

// The widget is drawn with the app's design system: the same palette, type
// ramp, grouped surfaces and badges, in light or dark like the app.

// widgetLight is the soft highlight that follows the pointer over a tile.
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
func (m *widgetModel) palette() *palette {
	if m.Dark {
		return &darkPalette
	}
	return &lightPalette
}

// metricColor is each tile's system color, matching the app's KPI tiles.
func (m *widgetModel) metricColor(i int) ui.Color {
	p := m.palette()
	return [...]ui.Color{p.green, p.blue, p.orange, p.purple}[i%4]
}
func (p *widgetPainter) theme(c *ui.Context) *palette {
	pal := p.model.palette()
	t := ui.LightTheme()
	if pal.dark {
		t = ui.DarkTheme()
	}
	t.Background = ui.Transparent
	t.Font = fontText
	t.Text = pal.label
	c.SetTheme(t)
	return pal
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

// widgetBadge is the app's badge: a white glyph on a system-color tile.
func widgetBadge(c *ui.Context, name string, x, y, size float32, color ui.Color) ui.Element {
	b := widgetBox(c, x, y, size, size).Radius(size * .27).Center().
		LinearGradient(ui.LinearGradient{From: color.Mix(ui.RGB(255, 255, 255), .16), To: color, Angle: 180})
	b.Children(func() { ui.Icon(c, icons[name]).Size(size*.6, size*.6).TextColor(ui.RGB(255, 255, 255)) })
	return b
}

// widgetAgentMark is the app's agent tile at an absolute position.
func widgetAgentMark(c *ui.Context, pal *palette, agent string, x, y, size float32) ui.Element {
	tile, mark, glyph := ui.RGB(0, 0, 0), ui.RGB(255, 255, 255), "openai"
	if pal.dark {
		tile, mark = mark, tile
	}
	if agent == agentClaude {
		tile, mark, glyph = pal.agent(agentClaude), ui.RGB(255, 255, 255), "claude"
	}
	b := widgetBox(c, x, y, size, size).Radius(size * .27).Center().Background(tile)
	b.Children(func() { ui.Icon(c, icons[glyph]).Size(size*.68, size*.68).TextColor(mark) })
	return b
}

// widgetStatus is the collection state, as the app's tinted pill.
func (m *widgetModel) status(now time.Time, short bool) (string, ui.Color) {
	p := m.palette()
	if m.collecting(now) {
		if short {
			return m.t("实时", "Live"), p.green
		}
		return m.t("采集中", "Live"), p.green
	}
	return m.t("等待中", "Waiting"), p.orange
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
	pal := p.theme(c)
	m := p.model
	now := m.clock
	r := m.glass(now)
	shape := m.shape.value(now)
	// Native window alpha composes visibility without rebuilding this scene.
	alpha := float32(1)
	// The corner grows with the morph until the surface is the orb's circle.
	radius := 24 + 36*shape
	near, shadow := ui.RGBA(0, 0, 0, .06), ui.RGBA(0, 0, 0, .16)
	edge, highlight := ui.RGBA(0, 0, 0, .08), ui.RGBA(255, 255, 255, .7)
	surface := ui.LinearGradient{From: ui.RGBA(252, 252, 253, .95), To: ui.RGBA(242, 242, 246, .93), Angle: 180}
	if pal.dark {
		near, shadow = ui.RGBA(0, 0, 0, .25), ui.RGBA(0, 0, 0, .45)
		edge, highlight = ui.RGBA(255, 255, 255, .12), ui.RGBA(255, 255, 255, .05)
		surface = ui.LinearGradient{From: ui.RGBA(40, 40, 43, .94), To: ui.RGBA(26, 26, 28, .95), Angle: 180}
	}
	if m.ReduceTransparency {
		surface = ui.LinearGradient{From: pal.bg, To: pal.bg}
	}
	outer := widgetBox(c, r.X, r.Y, r.W, r.H).Radius(radius).Opacity(alpha)
	outer.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Shadow(r, radius, 0, 1, 3, 0, near)
		p.Shadow(r, radius, 0, 5, 11, 0, shadow)
		p.FillGradient(r, surface, radius)
		p.Stroke(r, edge, radius, 1)
	})
	// A light inner rim, as on Apple's materials.
	widgetBox(c, r.X+1, r.Y+1, r.W-2, r.H-2).Radius(max(0, radius-1)).Opacity(alpha).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Stroke(r, highlight, max(0, radius-1), 1)
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
		w, h := 536*contentScale, 356*contentScale
		x := 12 + 476*(1-contentScale)
		y := 12 + 60*(1-contentScale)
		ui.Image(c, p.filteredCard).Absolute().Left(x).Top(y+7*(1-alpha)).Size(w, h).Fit(ui.FillBox).Opacity(cardOpacity * alpha)
	}
	if orbOpacity > 0 {
		s := 120 * orbScale
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
		switch {
		case m.Collapsed:
			radius = 63
		case m.focus == 1:
			radius = 21
		case m.focus >= 2 && m.focus <= 5:
			radius = 17
		}
		widgetBox(c, focus.X-3, focus.Y-3, focus.W+6, focus.H+6).Radius(radius).Border(2, pal.accent).Opacity(alpha)
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

// panel is a tile on the widget, the app's card: white (raised gray in dark
// mode), lifted by a soft shadow that deepens under the pointer.
func (p *widgetPainter) panel(c *ui.Context, r ui.Rect, radius float32, index int) {
	m := p.model
	pal := m.palette()
	hover := float32(0)
	if index >= 0 {
		hover = m.hoverMotion[index].value(m.clock)
	}
	box := widgetBox(c, r.X, r.Y, r.W, r.H).Radius(radius)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if pal.dark {
			p.Shadow(r, radius, 0, 2+6*hover, 6+12*hover, 0, ui.RGBA(0, 0, 0, .18+.22*hover))
		} else {
			p.Shadow(r, radius, 0, 1, 2, 0, ui.RGBA(0, 0, 0, .04))
			p.Shadow(r, radius, 0, 3+6*hover, 12+12*hover, 0, ui.RGBA(0, 0, 0, .05+.06*hover))
		}
		p.Fill(r, pal.card, radius)
		if pal.dark {
			p.Stroke(r, pal.hairline, radius, 1)
		}
	})
	if hover > 0 {
		size := max(r.W, r.H) * 1.6
		x, y := m.light[index][0]*r.W-size/2, m.light[index][1]*r.H-size/2
		glow := float32(.5)
		if pal.dark {
			glow = .1
		}
		box.Clip().Children(func() {
			ui.Image(c, widgetLight).Absolute().Left(x).Top(y).Size(size, size).Fit(ui.FillBox).Opacity(glow * hover)
		})
	}
}
func (p *widgetPainter) cardView(c *ui.Context) {
	pal := p.theme(c)
	m := p.model
	now := m.clock
	if p.brand != nil {
		ui.Image(c, p.brand).Absolute().Left(18).Top(20.5).Size(40, 40).Radius(10).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, .14))
	}
	widgetText(c, monitor.AppName, 68, 19, 290, 18, 22, pal.label, 650).Font(fontDisplay).LetterSpacing(-.3)
	widgetText(c, m.t("Codex 与 Claude Code · 全部账号", "Codex & Claude Code · all accounts"), 68, 44.7, 300, 12, 17.6, pal.secondary, 400)
	status, color := m.status(now, false)
	ui.Row(c).Absolute().Right(64).Top(28.5).Height(24).Padding(0, 10).Gap(6).Radius(999).AlignItems(ui.Center).
		Background(color.Alpha(.14)).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(color)
		ui.Text(c, status).FontSize(11).FontWeight(600).TextColor(color).NoWrap()
	})
	menu, ink := pal.fill, pal.secondary
	if m.hover == 1 {
		menu, ink = pal.fill.Mix(pal.label, .08), pal.label
	}
	widgetBox(c, 484, 24.5, 32, 32).Radius(16).Background(menu)
	widgetIcon(c, "dots", 490, 30.5, 20, ink)
	for i := 0; i < 4; i++ {
		r := widgetMetricRect(i)
		hover := m.hoverMotion[i].value(now)
		if m.ReduceMotion {
			hover = 0
		}
		ui.Image(c, p.metricBitmaps[i]).Absolute().Left(r.X-12-20).Top(r.Y-12-20-3*hover).Size(159, 160).Fit(ui.FillBox)
	}
	p.panel(c, ui.Rect{X: 18, Y: 211, W: 500, H: 105}, 14, 4)
	widgetBox(c, 181, 227, 1, 73).Background(pal.separator)
	widgetBadge(c, "stack-fill", 32, 225, 22, pal.blue)
	widgetText(c, m.t("今日 Token", "Today’s tokens"), 62, 226.4, 116, 12, 19.2, pal.secondary, 500)
	widgetText(c, widgetCompact(m.Data["total"]), 32, 254, 146, 30, 34, pal.label, 650).Font(fontDisplay).LetterSpacing(-.6).FontFeatures("tnum")
	ui.Row(c).Absolute().Left(32).Top(292.9).Height(17.6).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.t("较昨日", "vs yest.")).FontSize(11).FixedLineHeight(17.6).TextColor(pal.tertiary).NoWrap()
		if m.Data["change"] == nil {
			ui.Text(c, "—").FontSize(11).TextColor(pal.tertiary)
			return
		}
		change := number(m.Data["change"])
		tint := pal.green
		if change < 0 {
			tint = pal.red
		}
		ui.Row(c).Gap(1).AlignItems(ui.Center).Children(func() {
			arrow := ui.Icon(c, icons["up-right"]).Size(12, 12).TextColor(tint)
			if change < 0 {
				arrow.Rotate(90)
			}
			ui.Text(c, fmt.Sprintf("%+.1f%%", change*100)).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(tint).NoWrap()
		})
	})
	ui.Row(c).Absolute().Left(195).Top(225).Height(19.2).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.t("Token 使用趋势", "Token usage")).FontSize(13).FontWeight(600).Font(fontDisplay).TextColor(pal.label)
		ui.Text(c, m.t("近 24 小时", "Last 24h")).FontSize(11).TextColor(pal.tertiary)
	})
	timeline := objects(m.Data["timeline"])
	p.spark(c, timeline, ui.Rect{X: 195, Y: 252.2, W: 280, H: 43}, true, pal.blue)
	maxValue := 0.
	for _, point := range timeline {
		maxValue = max(maxValue, number(point["total"]))
	}
	widgetText(c, widgetCompact(maxValue), 480, 246.2, 36, 10, 14.4, pal.tertiary, 400).FontFeatures("tnum")
	for j, i := range []int{0, 48, 95} {
		label := "—"
		if i < len(timeline) {
			label = parseTime(timeline[i]["time"]).Local().Format("15:04")
		}
		x := 195 + float32(j)*140
		if j == 1 {
			x -= 14
		}
		if j == 2 {
			x -= 28
		}
		widgetText(c, label, x, 298.2, 34, 10, 14.4, pal.tertiary, 400).FontFeatures("tnum")
	}
	widgetIcon(c, "clock", 20, 331.5, 14, pal.tertiary)
	footer := m.t("最后更新：", "Updated: ") + "—"
	if last := parseTime(obj(m.Data["scan"])["lastScan"]); !last.IsZero() {
		footer = m.t("最后更新：", "Updated: ") + last.Local().Format("01/02 15:04")
	}
	footerColor := pal.secondary
	if m.Error != "" {
		footer = m.t("连接中断 · 自动重试", "Disconnected · retrying")
		footerColor = pal.orange
	}
	widgetText(c, footer, 40, 329.7, 340, 11, 17.6, footerColor, 400).FontFeatures("tnum")
	refresh := m.t("自动刷新中", "Auto-refreshing")
	if m.Busy {
		refresh = m.t("刷新中", "Refreshing")
	}
	button := ui.Row(c).Absolute().Right(19).Top(325).Height(27).Padding(0, 9).Gap(5).Radius(8).AlignItems(ui.Center)
	if m.hover == 6 {
		button.Background(pal.fill)
	}
	button.Children(func() {
		spin := ui.Icon(c, icons["refresh"]).Size(14, 14).TextColor(pal.secondary)
		if m.Busy && !m.ReduceMotion {
			spin.Rotate(float32(now.UnixMilli()%1200) * .3)
		}
		label := ui.Text(c, refresh).FontSize(11).FixedLineHeight(17.6).TextColor(pal.secondary).NoWrap()
		if m.Busy {
			spin.Opacity(.5)
			label.Opacity(.5)
		}
	})
}
func (p *widgetPainter) orbView(c *ui.Context) {
	pal := p.theme(c)
	m := p.model
	widgetBox(c, 4, 4, 112, 112).Radius(56).Draw(func(p *ui.Painter, r ui.Rect) {
		if !pal.dark {
			p.Shadow(r, 56, 0, 1, 3, 0, ui.RGBA(0, 0, 0, .06))
		}
		p.Fill(r, pal.card, 56)
		if pal.dark {
			p.Stroke(r, pal.hairline, 56, 1)
		}
	})
	widgetBadge(c, "stack-fill", 49, 15, 22, pal.blue)
	widgetText(c, m.t("今日 Token", "Today’s tokens"), 0, 40.3, 120, 10, 16, pal.secondary, 500).TextAlign(ui.Center)
	widgetText(c, widgetCompact(m.Data["total"]), 0, 55.3, 120, 23, 30, pal.label, 650).Font(fontDisplay).LetterSpacing(-.5).FontFeatures("tnum").TextAlign(ui.Center)
	status, color := m.status(m.clock, true)
	ui.Row(c).Absolute().Left(0).Top(87).Width(120).Height(16).Gap(4).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(5, 5).Radius(2.5).Background(color)
		ui.Text(c, status).FontSize(10).FontWeight(600).TextColor(color).NoWrap()
	})
}

// spark is the app's area chart in miniature: hairline rules, a line in the
// series color and the same fading fill.
func (p *widgetPainter) spark(c *ui.Context, points []Object, r ui.Rect, large bool, color ui.Color) {
	pal := p.model.palette()
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
			for _, v := range []float32{0, .5} {
				p.Line(r.X, r.Y+r.H*v, r.X+r.W, r.Y+r.H*v, 1, pal.hairline)
			}
			p.Line(r.X, r.Y+r.H, r.X+r.W, r.Y+r.H, 1, pal.separator)
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
		p.FillPathGradient(fill, chartFill(color))
		width := float32(2)
		if !large {
			width = 2.5
		}
		p.StrokePath(line, width, color)
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

// metricView is one tile, laid out as the app's KPI tiles: a badge over the
// label, the value in the display face, and a capsule gauge or sparkline.
func (p *widgetPainter) metricView(c *ui.Context, i int) {
	pal := p.theme(c)
	m := p.model
	now := m.clock
	metric := m.metrics(now)[i]
	r := ui.Rect{X: 20, Y: 20, W: 118.25, H: 120}
	hover := m.hoverMotion[i].value(now)
	if m.ReduceMotion {
		hover = 0
	}
	p.panel(c, r, 14, i)
	color := m.metricColor(i)
	widgetBadge(c, metric.Icon, r.X+12, r.Y+12-hover, 24, color).Rotate(-6 * hover)
	if i < 2 {
		// Quota tiles name the agent whose account they show.
		widgetAgentMark(c, pal, m.quotaAgent(), r.X+r.W-30, r.Y+15-hover, 18)
	}
	widgetText(c, metric.Label, r.X+12, r.Y+43, r.W-22, 12, 17, pal.secondary, 500)
	value := widgetPercent(metric.Value)
	if metric.Speed {
		value = "—"
		if metric.Value != nil {
			value = fmt.Sprintf("%.1f", number(metric.Value))
		}
	}
	valueRow := ui.Row(c).Absolute().Left(r.X + 12).Top(r.Y + 63).Gap(3).Height(32).AlignItems(ui.End)
	valueRow.Children(func() {
		ui.Text(c, value).FontSize(26).FixedLineHeight(32).Font(fontDisplay).FontWeight(650).TextColor(pal.label).LetterSpacing(-.6).FontFeatures("tnum")
		if metric.Speed {
			ui.Text(c, "tok/s").FontSize(10).FixedLineHeight(12).TextColor(pal.tertiary).FontWeight(500).Margin(0, 0, 5, 0)
		}
	})
	if metric.Speed {
		p.spark(c, objects(m.Data["speed"]), ui.Rect{X: r.X + 12, Y: r.Y + 98, W: r.W - 24, H: 14}, false, color)
		return
	}
	bar := ui.Rect{X: r.X + 12, Y: r.Y + 104, W: r.W - 24, H: 6}
	widgetBox(c, bar.X, bar.Y, bar.W, bar.H).Radius(3).Background(pal.fill)
	if level := bar.W * m.bars[i].value(now) / 100; level > 0 {
		widgetBox(c, bar.X, bar.Y, max(bar.H, level), bar.H).Radius(3).
			LinearGradient(ui.LinearGradient{From: color.Mix(ui.RGB(255, 255, 255), .2), To: color, Angle: 90})
	}
}
