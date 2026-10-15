package nativeapp

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	heatGap     = 3  // DIPs between cells
	heatLabels  = 26 // weekday column
	heatHeader  = 18 // month row
	heatMaxCell = 15
)

// heatmap is the year laid out by week, built once per snapshot: painting
// and pointer hit-testing index into it in O(1) instead of creating an
// element for each of the 371 cells.
type heatmap struct {
	revision uint64
	year     int
	metric   string
	start    time.Time
	weeks    int
	cells    []heatCell // week*7 + weekday
	months   [12]int    // week each month starts in
	maximum  float64
}

type heatCell struct {
	date  string
	row   Object
	level int
	in    bool
}

func (a *App) heat(activity Object, metric string) *heatmap {
	year := int(number(activity["year"]))
	h := &a.heatmap
	if h.revision == a.snapshotRevision && h.year == year && h.metric == metric && h.cells != nil {
		return h
	}
	days := map[string]Object{}
	maximum := 0.0
	for _, day := range objects(activity["days"]) {
		days[str(day["date"])] = day
		maximum = max(maximum, number(day[metric]))
	}
	start := time.Date(year, 1, 1, 12, 0, 0, 0, time.Local)
	start = start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
	end := time.Date(year+1, 1, 1, 12, 0, 0, 0, time.Local)
	weeks := (int(math.Round(end.Sub(start).Hours()/24)) + 6) / 7
	*h = heatmap{revision: a.snapshotRevision, year: year, metric: metric, start: start, weeks: weeks, cells: make([]heatCell, weeks*7), maximum: maximum}
	for m := 0; m < 12; m++ {
		d := time.Date(year, time.Month(m+1), 1, 12, 0, 0, 0, time.Local)
		h.months[m] = int(math.Round(d.Sub(start).Hours()/24)) / 7
	}
	for i := range h.cells {
		d := start.AddDate(0, 0, (i/7)*7+i%7)
		cell := &h.cells[i]
		cell.date, cell.in = d.Format("2006-01-02"), d.Year() == year
		cell.row = days[cell.date]
		if v := number(cell.row[metric]); v > 0 && maximum > 0 {
			cell.level = min(4, max(1, int(math.Ceil(math.Sqrt(v/maximum)*4))))
		}
	}
	return h
}

func (h *heatmap) index(date string) int {
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return -1
	}
	i := int(math.Round(t.Add(12*time.Hour).Sub(h.start).Hours() / 24))
	if i < 0 || i >= len(h.cells) {
		return -1
	}
	return i
}

func (h *heatmap) cellSize(width float32) float32 {
	return float32(bounded(float64((width-heatLabels-heatGap*float32(h.weeks-1))/float32(h.weeks)), 6, heatMaxCell))
}

// at finds the cell under a point of the grid in O(1).
func (h *heatmap) at(x, y, size float32) int {
	x, y = x-heatLabels, y-heatHeader
	if x < 0 || y < 0 {
		return -1
	}
	week, day := int(x/(size+heatGap)), int(y/(size+heatGap))
	if week >= h.weeks || day >= 7 {
		return -1
	}
	return week*7 + day
}

// center is where cell i sits in the grid.
func (h *heatmap) center(i int, size float32) (float32, float32) {
	return heatLabels + float32(i/7)*(size+heatGap) + size/2, heatHeader + float32(i%7)*(size+heatGap) + size/2
}

func (a *App) heatColor(level int) ui.Color {
	p := a.pal
	if level == 0 {
		return p.fill
	}
	return p.green.Alpha([]float32{0, .3, .52, .76, 1}[level])
}

func (a *App) activity(c *ui.Context, compactView bool) {
	activity := obj(a.data["activity"])
	if len(activity) == 0 {
		return
	}
	p := a.pal
	year := int(number(activity["year"]))
	stats := obj(activity["stats"])
	metric := []string{"total", "requests", "cost"}[a.activityMetric]
	h := a.heat(activity, metric)
	today := str(activity["today"])
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			a.header(c, a.tr("活动"), "", func() {
				if a.iconButton(c, "caret-left", a.tr("上一年")).Disabled(year <= int(number(activity["firstYear"]))).Clicked() {
					a.change("activityYear", year-1)
				}
				ui.Text(c.Key("year"), fmt.Sprint(year)).FontSize(13).FontWeight(600).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
				if a.iconButton(c, "caret-right", a.tr("下一年")).Disabled(year >= time.Now().Year()).Clicked() {
					a.change("activityYear", year+1)
				}
				ui.Box(c).Width(4)
				a.segmented(c, &a.activityMetric, []string{"Token", a.tr("用量记录"), "USD"}, 210).Label(a.tr("活动指标"))
				if compactView {
					if a.iconButton(c, "arrow-up-right", a.tr("查看活动")).Clicked() {
						a.navigate(1)
					}
				}
			})
			if compactView {
				ui.Row(c).Gap(24).Children(func() {
					for i, key := range []string{"activeDays", "currentStreak", "longestStreak"} {
						ui.Row(c).Gap(6).AlignItems(ui.End).Children(func() {
							if i == 1 {
								a.glyph(c, "flame-fill", 15, p.orange).Margin(0, 0, 3, 0)
							}
							ui.Text(c, full(stats[key])).FontSize(17).FontWeight(650).Font(fontDisplay).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
							ui.Text(c, a.tr([]string{"活跃天数", "当前连续", "最长连续"}[i])).FontSize(12).TextColor(p.secondary).Padding(0, 0, 2, 0)
						})
					}
					ui.Spacer(c)
					a.caption(c, compact(stats["total"])+" tokens · "+full(stats["requests"])+" "+a.tr("用量记录"))
				})
			}
			a.heatGrid(c, h, today)
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				detail := a.tr("按本地日期统计 · 点击日期查看记录")
				if i := h.index(a.hoverDay); i >= 0 {
					detail = a.dayLabel(h.cells[i])
				}
				ui.Text(c.Key("detail"), detail).FontSize(11).TextColor(p.secondary).Grow(1).SingleLine()
				ui.Text(c, a.tr("少")).FontSize(10).TextColor(p.tertiary)
				for level := 0; level <= 4; level++ {
					ui.Box(c).Size(10, 10).Radius(3).Background(a.heatColor(level))
				}
				ui.Text(c, a.tr("多")).FontSize(10).TextColor(p.tertiary)
			})
			if i := h.index(a.selectedDay); i >= 0 && a.selectedDay != "" {
				cell := h.cells[i]
				ui.Row(c.Key("day-"+a.selectedDay)).Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Y: -6}}).Padding(10, 12).Gap(14).Radius(10).Background(p.fill).AlignItems(ui.Center).Children(func() {
					a.badge(c, "calendar-dots-fill", p.red, 26)
					ui.Column(c).Grow(1).Basis(0).Gap(1).Children(func() {
						ui.Text(c, a.selectedDay).FontSize(13).FontWeight(600)
						a.caption(c, compact(cell.row["total"])+" tokens · "+full(cell.row["requests"])+" "+a.tr("用量记录")+" · "+full(cell.row["sessions"])+" "+a.tr("个任务")+" · "+a.rowCost(cell.row))
					})
					if a.primary(c, a.tr("查看当天记录")).Clicked() {
						a.openDay(a.selectedDay)
					}
					if a.iconButton(c, "x", a.tr("收起日期详情")).Clicked() {
						a.selectedDay = ""
					}
				})
			}
		})
	})
	if !compactView {
		a.activityDetails(c, activity)
	}
}

func (a *App) dayLabel(cell heatCell) string {
	value := full(cell.row["total"]) + " tokens"
	switch a.activityMetric {
	case 1:
		value = full(cell.row["requests"]) + " " + a.tr("用量记录")
	case 2:
		value = a.rowCost(cell.row)
	}
	return cell.date + " · " + value
}

// heatGrid paints the calendar as one element. Cells ripple in week by week,
// the hovered one lifts, and arrow keys walk the days.
func (a *App) heatGrid(c *ui.Context, h *heatmap, today string) {
	p := a.pal
	grid := ui.Box(c.Key(fmt.Sprintf("heat-%d-%s", h.year, h.metric))).FillWidth().Focusable().Label(a.tr("活动热力图"))
	width := grid.Bounds().W
	if width <= 0 {
		width = 900
	}
	size := h.cellSize(width)
	grid.Height(heatHeader + 7*size + 6*heatGap)
	ui.Local(grid, "init", func() bool { grid.Jump("reveal", 0); return true })
	reveal := grid.AnimateTo("reveal", 1, ui.Animation{Duration: 900 * time.Millisecond})
	at := func(x, y float32) int {
		if i := h.at(x, y, size); i >= 0 && h.cells[i].in && h.cells[i].date <= today {
			return i
		}
		return -1
	}
	x, y, over := grid.PointerPosition()
	over = over && grid.Hovered()
	hovered := -1
	if over {
		hovered = at(x, y)
	}
	focus := h.index(a.hoverDay)
	if grid.Focused() {
		if focus < 0 {
			focus = h.index(today)
		}
		for _, move := range []struct {
			key   ui.Key
			delta int
		}{{ui.KeyLeft, -7}, {ui.KeyRight, 7}, {ui.KeyUp, -1}, {ui.KeyDown, 1}} {
			if grid.Shortcut(0, move.key) && focus >= 0 {
				if next := focus + move.delta; next >= 0 && next < len(h.cells) && h.cells[next].in && h.cells[next].date <= today {
					focus = next
				}
			}
		}
		if grid.Shortcut(0, ui.KeyEnter) && focus >= 0 {
			a.selectedDay = h.cells[focus].date
		}
	}
	switch {
	case hovered >= 0:
		a.hoverDay = h.cells[hovered].date
	case grid.Focused() && focus >= 0:
		a.hoverDay = h.cells[focus].date
	default:
		a.hoverDay = ""
	}
	if grid.Clicked() && hovered >= 0 {
		a.selectedDay = h.cells[hovered].date
	}
	lit := h.index(a.hoverDay)
	monthNames := [12]string{"1月", "2月", "3月", "4月", "5月", "6月", "7月", "8月", "9月", "10月", "11月", "12月"}
	for i := range monthNames {
		monthNames[i] = a.tr(monthNames[i])
	}
	weekdays := [3]string{a.tr("一"), a.tr("三"), a.tr("五")}
	selected := h.index(a.selectedDay)
	grid.Draw(func(pt *ui.Painter, r ui.Rect) {
		for m, week := range h.months {
			pt.Text(r.X+heatLabels+float32(week)*(size+heatGap), r.Y, monthNames[m], 10, p.tertiary)
		}
		for i, label := range weekdays {
			pt.Text(r.X, r.Y+heatHeader+float32(i*2)*(size+heatGap)+size/2-7, label, 10, p.tertiary)
		}
		radius := max(2, size*.24)
		for i, cell := range h.cells {
			if !cell.in {
				continue
			}
			week, day := i/7, i%7
			t := bounded(float64(reveal)*1.6-float64(week)/float64(h.weeks)*.6, 0, 1)
			box := ui.Rect{X: r.X + heatLabels + float32(week)*(size+heatGap), Y: r.Y + heatHeader + float32(day)*(size+heatGap), W: size, H: size}
			grow := float32(.4 + .6*t)
			if i == lit {
				grow = 1.22
			}
			inset := box.W * (1 - grow) / 2
			box = ui.Rect{X: box.X + inset, Y: box.Y + inset, W: box.W - 2*inset, H: box.H - 2*inset}
			color := a.heatColor(cell.level)
			if cell.date > today {
				color = color.Alpha(.35)
			}
			pt.Fill(box, color.Alpha(float32(t)*float32(color.A)/255), radius)
			if cell.date == today {
				pt.Stroke(box, p.green, radius, 1.5)
			}
			if i == selected || i == lit {
				pt.Stroke(ui.Rect{X: box.X - 2, Y: box.Y - 2, W: box.W + 4, H: box.H + 4}, map[bool]ui.Color{true: p.label, false: p.green}[i == selected], radius+2, 1.5)
			}
		}
	})
}

func (a *App) activityDetails(c *ui.Context, activity Object) {
	p := a.pal
	stats := obj(activity["stats"])
	ui.Row(a.kc(c)).Gap(16).Children(func() {
		for i, item := range []struct{ label, value, icon string }{
			{"活跃天数", full(stats["activeDays"]), "calendar-dots-fill"},
			{"当前连续", full(stats["currentStreak"]), "flame-fill"},
			{"最长连续", full(stats["longestStreak"]), "timer-fill"},
			{"年度 Token", compact(stats["total"]), "stack-fill"},
		} {
			a.enter(a.card(c).Grow(1).Basis(0).MinWidth(0).Padding(14, 16).Gap(8)).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					a.badge(c, item.icon, []ui.Color{p.red, p.orange, p.indigo, p.blue}[i], 22)
					ui.Text(c, a.tr(item.label)).FontSize(12).TextColor(p.secondary)
				})
				ui.Text(c, item.value).FontSize(24).FontWeight(650).Font(fontDisplay).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
			})
		}
	})
	ui.Row(a.kc(c)).Gap(16).AlignItems(ui.Stretch).Children(func() {
		a.section(c, func() {
			a.header(c, a.tr("活跃时段"), a.tr("本地时间 · 年度汇总"), nil)
			a.activityHours(c, activity)
			a.separator(c, 0)
			ui.Row(c).Children(func() {
				ui.Text(c, a.tr("高峰日期")+"  "+str(obj(stats["peak"])["date"])).FontSize(12).Grow(1)
				a.caption(c, compact(obj(stats["peak"])["total"])+" tokens")
			})
		}).Grow(1).Basis(0).MinWidth(320)
		a.section(c, func() {
			a.header(c, a.tr("最近活动日"), a.tr("{0} 个任务", stats["sessions"]), nil)
			list := objects(activity["days"])
			peak := max(1, number(obj(stats["peak"])["total"]))
			ui.Column(c.Key("recent-days")).Label(a.tr("最近活动日列表")).Gap(0).Children(func() {
				for i := len(list) - 1; i >= max(0, len(list)-8); i-- {
					day := list[i]
					date := str(day["date"])
					row := ui.Row(c.Key(date)).Height(36).Gap(12).Padding(0, 8).Margin(0, -8).Radius(8).AlignItems(ui.Center).Focusable().Label(date)
					if row.Hovered() || row.FocusVisible() {
						row.Background(p.hover)
					}
					row.Children(func() {
						ui.Text(c, date).Width(84).FontSize(12).FontFeatures("tnum").NoWrap()
						bar := ui.Box(c).Height(6).Grow(1).Basis(0).Radius(3).Background(p.fill)
						fraction := float32(number(day["total"]) / peak)
						bar.Draw(func(pt *ui.Painter, r ui.Rect) {
							pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.H, r.W*fraction), H: r.H}, p.green, 3)
						})
						ui.Text(c, compact(day["total"])).Width(56).FontSize(12).FontWeight(600).FontFeatures("tnum").TextAlign(ui.End).NoWrap()
						a.glyph(c, "caret-right", 12, p.tertiary)
					})
					if row.Clicked() || row.Shortcut(0, ui.KeyEnter) {
						a.openDay(date)
					}
				}
			})
			if len(list) == 0 {
				a.empty(c, a.tr("这个时间范围内还没有记录"))
			}
		}).Label(a.tr("最近活动日卡片")).Grow(1).Basis(0).MinWidth(320)
	})
}

// The same geometry drives painting and pointer selection, including gutters.
func activityHourAt(x, width float32) int {
	if width <= 0 || x < 0 || x >= width {
		return -1
	}
	return min(23, int(x/width*24))
}

func (a *App) activityHours(c *ui.Context, activity Object) {
	p := a.pal
	var hours [24]Object
	maximum := 1.0
	for _, h := range objects(activity["hours"]) {
		if i := int(number(h["hour"])); i >= 0 && i < len(hours) {
			hours[i] = h
			maximum = max(maximum, number(h["total"]))
		}
	}
	plot := ui.Box(c.Key(fmt.Sprintf("hours-%v", activity["year"]))).Height(170).Shrink(0).Focusable().Label(a.tr("活跃时段柱状图"))
	keyboard := ui.Local(plot, "hour", func() int { return 0 })
	ui.Local(plot, "init", func() bool { plot.Jump("grow", 0); return true })
	grow := plot.AnimateTo("grow", 1, springGentle)
	x, _, over := plot.PointerPosition()
	selected := -1
	if over {
		selected = activityHourAt(x, plot.Bounds().W)
	}
	if plot.Clicked() && selected >= 0 {
		*keyboard = selected
	}
	if plot.Focused() {
		for _, move := range []struct {
			key ui.Key
			to  int
		}{{ui.KeyLeft, *keyboard - 1}, {ui.KeyRight, *keyboard + 1}, {ui.KeyHome, 0}, {ui.KeyEnd, 23}} {
			if plot.Shortcut(0, move.key) {
				*keyboard = max(0, min(23, move.to))
			}
		}
		if !over || plot.FocusVisible() {
			selected = *keyboard
		}
	}
	focus := plot.AnimateTo("focus", float32(max(selected, 0)), springSnappy)
	shown := plot.Animate("shown", map[bool]float32{true: 1, false: 0}[selected >= 0], motionQuick)
	plot.Draw(func(pt *ui.Painter, r ui.Rect) {
		step := r.W / 24
		gap := min(float32(6), step*.32)
		barHeight := r.H - 22
		if shown > 0 {
			pt.Fill(ui.Rect{X: r.X + step*focus, Y: r.Y, W: step, H: barHeight}, p.hover.Alpha(float32(p.hover.A)/255*shown*1.6), 6)
		}
		for i, h := range hours {
			t := bounded(float64(grow)*1.4-float64(i)/24*.4, 0, 1)
			track := ui.Rect{X: r.X + step*float32(i) + gap/2, Y: r.Y, W: step - gap, H: barHeight}
			pt.Fill(track, p.fill.Alpha(float32(p.fill.A)/255*.55), min(4, track.W/2))
			value := number(h["total"]) / maximum
			if value <= 0 {
				continue
			}
			height := max(track.W, barHeight*float32(value*t))
			bar := ui.Rect{X: track.X, Y: r.Y + barHeight - height, W: track.W, H: height}
			color := p.blue
			if i != selected && selected >= 0 {
				color = color.Alpha(.45)
			}
			pt.FillGradient(bar, ui.LinearGradient{From: color.Mix(ui.RGB(255, 255, 255), .25), To: color, Angle: 180}, min(4, bar.W/2))
		}
		for i := 0; i < 24; i += 6 {
			pt.Text(r.X+step*float32(i)+gap/2, r.Y+barHeight+8, fmt.Sprintf("%02d:00", i), 10, p.tertiary)
		}
	})
	caption := a.tr("移动鼠标或使用方向键查看每小时用量")
	if selected >= 0 {
		h := hours[selected]
		caption = fmt.Sprintf("%02d:00–%02d:00 · %s tokens · %s %s", selected, selected+1, compact(number(h["total"])), full(number(h["requests"])), a.tr("用量记录"))
	}
	ui.Text(c.Key("hour-caption"), caption).FontSize(12).TextColor(p.secondary).SingleLine()
}
