package nativeapp

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) overview(c *ui.Context) {
	a.kpis(c)
	ui.Row(a.kc(c)).Gap(16).AlignItems(ui.Stretch).Children(func() {
		a.trend(c).Grow(2).Basis(0).MinWidth(0)
		a.quotaRings(c).Grow(1).Basis(0).MinWidth(280)
	})
	a.activity(c, true)
	ui.Row(a.kc(c)).Gap(16).AlignItems(ui.Stretch).Children(func() {
		a.models(c).Grow(3).Basis(0).MinWidth(0)
		a.speed(c).Grow(2).Basis(0).MinWidth(260)
	})
	a.metrics(c)
}

func (a *App) primaryQuota() Object {
	for _, q := range objects(a.data["quotas"]) {
		if number(q["minutes"]) == 300 {
			return q
		}
	}
	return nil
}

// kpis is the row of headline figures; each tile opens the page behind it.
func (a *App) kpis(c *ui.Context) {
	p := a.pal
	s := obj(a.data["sums"])
	quota := a.primaryQuota()
	remaining, quotaFoot := "—", a.tr("尚未获得额度快照")
	fraction := -1.0
	if quota["used"] != nil {
		fraction = bounded(1-number(quota["used"])/100, 0, 1)
		remaining = fmt.Sprintf("%.0f%%", fraction*100)
		quotaFoot = a.quotaWindow(quota)
	}
	tokenLabel := "Token 消耗"
	if str(a.filter["range"]) == "today" {
		tokenLabel = "今日 Token"
	}
	cost := money(s["cost"])
	if number(s["unpriced"]) == number(s["requests"]) && number(s["requests"]) > 0 {
		cost = "—"
	}
	costFoot := a.tr("按标准短上下文价格估算")
	if number(s["unpriced"]) > 0 {
		costFoot = a.tr("{0} 条未定价 · 金额不完整", s["unpriced"])
	}
	tiles := []struct {
		label, value, foot, icon string
		color                    ui.Color
		ring                     float64
		open                     func()
	}{
		{"账户剩余额度", remaining, quotaFoot, "gauge-fill", p.green, fraction, func() { a.navigate(3) }},
		{tokenLabel, compact(s["total"]), a.tr("{0} 次用量记录", full(s["requests"])), "stack-fill", p.blue, -1, func() { a.viewIndex = 2; a.navigate(2) }},
		{"API 等值估算 · USD", cost, costFoot, "coins-fill", p.orange, -1, func() { a.settingsSection = 2; a.navigate(4) }},
		{"缓存命中率", ratio(s["cacheRate"]), a.tr("{0} 缓存输入 tokens", compact(s["cached"])), "lightning-fill", p.purple, -1, func() { a.viewIndex = 0; a.navigate(2) }},
	}
	ui.Row(a.kc(c)).Gap(16).AlignItems(ui.Stretch).Children(func() {
		for i, t := range tiles {
			tile := a.enter(a.card(c.Key(i))).Grow(1).Basis(0).MinWidth(0).Padding(16, 18).Gap(10).Focusable().Label(a.tr(t.label) + " " + t.value)
			a.lift(tile)
			tile.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					a.badge(c, t.icon, t.color, 24)
					ui.Text(c, a.tr(t.label)).FontSize(12).FontWeight(500).TextColor(p.secondary).Grow(1).SingleLine()
					a.glyph(c, "caret-right", 12, p.tertiary).Opacity(tile.Animate("chevron", map[bool]float32{true: 1, false: 0}[tile.Hovered()], motionQuick))
				})
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, t.value).FontSize(30).FontWeight(650).Font(fontDisplay).LetterSpacing(-.6).FontFeatures("tnum").NoWrap().Grow(1).ContentTransition(ui.ContentTransition{Numeric: true})
					if t.ring >= 0 {
						a.ring(c, "kpi", []ringValue{{t.ring, t.color}}, 40, 6)
					}
				})
				ui.Text(c, t.foot).FontSize(11).TextColor(p.tertiary).SingleLine()
			})
			if tile.Clicked() || tile.Shortcut(0, ui.KeyEnter) {
				t.open()
			}
		}
	})
}

// lift raises a clickable card under the pointer, as tiles on iPadOS do.
func (a *App) lift(e ui.Element) {
	hover := e.Hovered() || e.FocusVisible()
	y, scale := float32(0), float32(1)
	if hover {
		y, scale = -2, 1.008
	}
	if e.Pressed() {
		y, scale = 0, .985
	}
	e.AnimatePose(ui.Pose{Y: y, Scale: scale}, springSnappy)
	if hover && !a.pal.dark {
		e.Shadow(0, 12, 28, 0, ui.RGBA(0, 0, 0, .08))
	}
}

func (a *App) quotaWindow(q Object) string {
	if number(q["minutes"]) >= 1440 {
		return a.tr("{0} 天窗口", number(q["minutes"])/1440)
	}
	return a.tr("{0} 小时窗口", number(q["minutes"])/60)
}

func (a *App) trend(c *ui.Context) ui.Element {
	p := a.pal
	s := obj(a.data["sums"])
	return a.section(c, func() {
		a.header(c, a.tr("Token 使用趋势"), "", func() {
			a.segmented(c, &a.metricIndex, []string{a.tr("总量"), a.tr("输出"), "USD"}, 190).Label(a.tr("趋势指标"))
		})
		ui.Row(c).Gap(24).Children(func() {
			for i, item := range []struct{ label, value string }{{"输入（含缓存）", compact(s["input"])}, {"输出", compact(s["output"])}, {"推理输出", compact(s["reasoning"])}} {
				ui.Column(c).Gap(2).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(4).Background(p.series(i))
						ui.Text(c, a.tr(item.label)).FontSize(11).TextColor(p.secondary)
					})
					ui.Text(c, item.value).FontSize(17).FontWeight(600).Font(fontDisplay).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
				})
			}
		})
		metric := []string{"total", "output", "cost"}[a.metricIndex]
		unit := "tokens"
		if metric == "cost" {
			unit = "USD"
		}
		a.chart(c, "usage-"+metric, a.timelinePoints(metric), false, false, unit)
	})
}

type ringValue struct {
	fraction float64
	color    ui.Color
}

// ring draws concentric activity rings, outermost first, that wind up to
// their values with a spring.
func (a *App) ring(c *ui.Context, key string, values []ringValue, size, width float32) ui.Element {
	p := a.pal
	box := ui.Box(c.Key("ring-"+key)).Size(size, size).Shrink(0)
	shown := make([]float32, len(values))
	for i, v := range values {
		k := fmt.Sprint("ring", i)
		ui.Local(box, k, func() bool { box.Jump(k, 0); return true })
		motion := springGentle
		motion.Delay = time.Duration(i) * 90 * time.Millisecond
		shown[i] = box.AnimateTo(k, float32(v.fraction), motion)
	}
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		for i, v := range values {
			radius := r.W/2 - width/2 - float32(i)*(width+2)
			if radius <= width/2 {
				break
			}
			var track ui.Path
			track.Circle(cx, cy, radius)
			pt.StrokePath(&track, width, v.color.Alpha(map[bool]float32{true: .26, false: .18}[p.dark]))
			if f := shown[i]; f > .001 {
				arc := arcPath(cx, cy, radius, min(f, 1))
				pt.StrokePathGradient(&arc, width, ui.LinearGradient{From: v.color.Mix(ui.RGB(255, 255, 255), .22), To: v.color, Angle: 90})
			}
		}
	})
	return box
}

// arcPath runs clockwise from twelve o'clock through fraction of a turn, in
// quarter-turn Bézier pieces.
func arcPath(cx, cy, r, fraction float32) ui.Path {
	var path ui.Path
	start := -math.Pi / 2
	sweep := 2 * math.Pi * float64(fraction)
	point := func(t float64) (float32, float32) {
		return cx + r*float32(math.Cos(t)), cy + r*float32(math.Sin(t))
	}
	path.MoveTo(point(start))
	pieces := int(math.Ceil(sweep / (math.Pi / 2)))
	for i := 0; i < pieces; i++ {
		a0 := start + sweep*float64(i)/float64(pieces)
		a1 := start + sweep*float64(i+1)/float64(pieces)
		k := 4.0 / 3 * math.Tan((a1-a0)/4) * float64(r)
		x0, y0 := point(a0)
		x1, y1 := point(a1)
		path.CubeTo(x0-float32(k*math.Sin(a0)), y0+float32(k*math.Cos(a0)), x1+float32(k*math.Sin(a1)), y1-float32(k*math.Cos(a1)), x1, y1)
	}
	return path
}

func (a *App) quotaRings(c *ui.Context) ui.Element {
	p := a.pal
	quotas := objects(a.data["quotas"])
	return a.section(c, func() {
		status := a.tr("在线查询正常")
		if reason := str(obj(a.data["quotaStatus"])["reason"]); reason != "" {
			status = a.system(reason)
		}
		a.header(c, a.tr("账户额度"), "", func() {
			if a.iconButton(c, "arrow-up-right", a.tr("查看账户额度")).Clicked() {
				a.navigate(3)
			}
		})
		if len(quotas) == 0 {
			a.empty(c, a.tr("尚未获得额度快照"))
			return
		}
		values := make([]ringValue, 0, len(quotas))
		for i, q := range quotas {
			values = append(values, ringValue{bounded(1-number(q["used"])/100, 0, 1), a.quotaColor(i)})
		}
		ui.Row(c).Gap(20).AlignItems(ui.Center).Grow(1).Children(func() {
			a.ring(c, "quota", values[:min(3, len(values))], 132, 14)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(12).Children(func() {
				for i, q := range quotas {
					ui.Column(c).Gap(1).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(8, 8).Radius(4).Background(values[i].color)
							ui.Text(c, a.quotaWindow(q)).FontSize(12).TextColor(p.secondary).SingleLine()
						})
						ui.Row(c).Gap(3).AlignItems(ui.End).Children(func() {
							ui.Text(c, fmt.Sprintf("%.0f", values[i].fraction*100)).FontSize(22).FontWeight(650).Font(fontDisplay).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
							ui.Text(c, a.tr("% 剩余")).FontSize(11).TextColor(p.secondary).Padding(0, 0, 4, 0)
						})
						ui.Text(c, a.resetText(q)).FontSize(11).TextColor(p.tertiary).SingleLine()
					})
				}
			})
		})
		if at := obj(a.data["quotaStatus"])["attempted"]; at != nil {
			status += " · " + shortStamp(at)
		}
		ui.Text(c, status).FontSize(11).TextColor(p.tertiary).SingleLine()
	})
}

func (a *App) quotaColor(i int) ui.Color {
	return [...]ui.Color{a.pal.green, a.pal.blue, a.pal.purple, a.pal.orange}[i%4]
}

func (a *App) resetText(q Object) string {
	if q["resets"] == nil {
		return a.tr("重置 ") + "—"
	}
	reset := number(q["resets"]) * 1000
	if reset < float64(time.Now().UnixMilli()) {
		return a.tr("已过重置时间，等待新快照")
	}
	return a.tr("重置 ") + shortStamp(reset)
}

func (a *App) rowCost(row Object) string {
	cost := any(row["cost"])
	if number(row["requests"]) > 0 && number(row["requests"]) == number(row["unpriced"]) {
		cost = nil
	}
	result := money(cost)
	if number(row["unpriced"]) > 0 {
		result += a.tr(" + 未定价")
	}
	return result
}

// models shows each model's share as one stacked capsule, then as rows.
func (a *App) models(c *ui.Context) ui.Element {
	p := a.pal
	rows := objects(a.data["models"])
	total := max(1, number(obj(a.data["sums"])["total"]))
	return a.section(c, func() {
		a.header(c, a.tr("模型分布"), a.tr("{0} 个模型", len(rows)), nil)
		if len(rows) == 0 {
			a.empty(c, a.tr("这个时间范围内还没有记录"))
			return
		}
		bar := ui.Box(c.Key("share")).Height(10).FillWidth().Radius(5).Background(p.fill).Clip()
		ui.Local(bar, "init", func() bool { bar.Jump("grow", 0); return true })
		grow := bar.AnimateTo("grow", 1, springGentle)
		bar.Draw(func(pt *ui.Painter, r ui.Rect) {
			x := r.X
			for i, row := range rows {
				w := float32(number(row["total"])/total) * r.W * grow
				if w <= 0 {
					continue
				}
				pt.Fill(ui.Rect{X: x, Y: r.Y, W: max(w-2, 1), H: r.H}, p.series(i), 0)
				x += w
			}
		})
		ui.Column(c).Gap(0).Children(func() {
			for i, row := range rows[:min(6, len(rows))] {
				name := str(row["name"])
				line := ui.Row(c.Key(name)).Height(40).Padding(0, 8).Margin(0, -8).Gap(10).Radius(8).AlignItems(ui.Center).Focusable().Label(name)
				if line.Hovered() {
					line.Background(p.hover)
				}
				line.Children(func() {
					ui.Box(c).Size(10, 10).Radius(3).Background(p.series(i))
					ui.Text(c, name).FontSize(13).FontWeight(500).Grow(1).Basis(0).SingleLine()
					ui.Text(c, a.rowCost(row)).FontSize(12).TextColor(p.secondary).NoWrap()
					ui.Text(c, compact(row["total"])).FontSize(13).FontWeight(600).FontFeatures("tnum").Width(64).TextAlign(ui.End)
					ui.Text(c, fmt.Sprintf("%.0f%%", number(row["total"])/total*100)).FontSize(12).TextColor(p.tertiary).FontFeatures("tnum").Width(40).TextAlign(ui.End)
				})
				if line.Clicked() || line.Shortcut(0, ui.KeyEnter) {
					a.change("model", name)
					a.navigate(2)
				}
			}
		})
	})
}

func (a *App) speed(c *ui.Context) ui.Element {
	p := a.pal
	perf := obj(a.data["performance"])
	return a.section(c, func() {
		a.header(c, a.tr("任务平均输出速率"), "", nil)
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			value := "—"
			if perf["latestTps"] != nil {
				value = fmt.Sprintf("%.1f", number(perf["latestTps"]))
			}
			ui.Text(c, value).FontSize(46).FontWeight(650).Font(fontDisplay).LetterSpacing(-1.5).FontFeatures("tnum").TextColor(p.accent).ContentTransition(ui.ContentTransition{Numeric: true})
			ui.Text(c, "tokens / sec").FontSize(12).TextColor(p.secondary).Padding(0, 0, 10, 0)
		})
		context := a.tr("尚无带完整耗时的已完成任务")
		if perf["latestAt"] != nil {
			context = a.tr("最近完成 · ") + shortStamp(perf["latestAt"])
		}
		a.caption(c, context)
		ui.Spacer(c)
		a.separator(c, 0)
		ui.Row(c).Gap(16).Children(func() {
			a.stat(c, a.tr("范围内加权平均"), fmt.Sprintf("%.1f tok/s", number(perf["taskTps"])))
			a.stat(c, a.tr("近期活跃任务"), full(perf["active"]))
		})
	})
}

func (a *App) metrics(c *ui.Context) {
	p, s := obj(a.data["performance"]), obj(a.data["sums"])
	items := [][2]string{{"请求次数", full(s["requests"])}, {"任务成功率", ratio(p["successRate"])}, {"失败任务", full(p["failed"])}, {"平均首 Token 延迟", a.duration(p["ttft"])},
		{"平均任务耗时", a.duration(p["avgDuration"])}, {"缓存节省估算", money(s["saved"])}, {"推理输出", compact(s["reasoning"])}, {"缓存写入", compact(s["cache_write"])}}
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			a.header(c, a.tr("运行指标"), "", nil)
			ui.Grid(c).Columns(4).GapX(16).GapY(18).Children(func() {
				for _, item := range items {
					a.stat(c, a.tr(item[0]), item[1])
				}
			})
		})
	})
}
