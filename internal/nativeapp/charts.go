package nativeapp

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

type chartPoint struct {
	Time, Value float64
	Name        string
	Reset       float64
	Gap         bool
}

const chartStrokeWidth = 2.5 // DIPs

// Explicit segments preserve observations; a reset/gap must never become a slope.
func chartSegments(points []chartPoint, quota bool) [][]chartPoint {
	var segments [][]chartPoint
	start := 0
	for i, p := range points {
		reset := false
		if i > 0 && quota {
			previous := points[i-1]
			reset = (previous.Reset > previous.Time/1000 && previous.Reset <= p.Time/1000) || (previous.Reset != 0 && p.Reset != 0 && math.Abs(previous.Reset-p.Reset) > 1)
		}
		if i > 0 && (p.Gap || reset) {
			segments = append(segments, points[start:i])
			start = i
		}
	}
	if len(points) > 0 {
		segments = append(segments, points[start:])
	}
	return segments
}
func (a *App) chart(c *ui.Context, key string, points []chartPoint, percent, quota bool, unit string) {
	if len(points) == 0 {
		a.releaseChart()
		ui.Text(c, a.tr("这个时间范围内还没有记录")).Padding(28).TextColor(c.Theme().TextMuted)
		return
	}
	column := ui.Column(c).Key("chart-" + key + a.snapshotKey).Height(203).Gap(3)
	renderer := ui.Local(column, "renderer", func() chartRenderer { return chartRenderer{} })
	if a.chartRenderer != nil && a.chartRenderer != renderer && a.chartRenderer.name == key {
		a.releaseChart()
	}
	a.chartRenderer = renderer
	renderer.name = key
	renderer.prepare(points, percent, quota, unit, a.snapshotRevision)
	maximum := renderer.maximum
	first, last := points[0].Time, points[len(points)-1].Time
	if str(a.filter["range"]) != "all" {
		start := dateValue(str(obj(a.data["range"])["start"]))
		end := dateValue(str(obj(a.data["range"])["end"]))
		if !start.IsZero() {
			first = float64(start.UnixMilli())
		}
		if !end.IsZero() {
			last = float64(end.UnixMilli())
		}
	}
	selected := -1
	if a.chartSelection == nil {
		a.chartSelection = map[string]int{}
	}
	if value, ok := a.chartSelection[key]; ok && value < len(points) {
		selected = value
	}
	format := func(v float64) string {
		if percent {
			return fmt.Sprintf("%.1f%%", v)
		}
		if unit == "USD" {
			return money(v)
		}
		return full(v) + " tokens"
	}
	column.Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Width(34).Height(150).Justify(ui.SpaceBetween).Children(func() {
				for _, v := range []float64{maximum, maximum / 2, 0} {
					label := compact(v)
					if percent {
						label = fmt.Sprintf("%.0f%%", v)
					} else if unit == "USD" {
						label = money(v)
					}
					ui.Text(c, label).FontSize(10).TextColor(c.Theme().TextMuted)
				}
			})
			plot := ui.Box(c).Key(key + a.snapshotKey).Grow(1).Height(150).Focusable().Label(a.tr("用量趋势") + " " + key)
			started := ui.Local(plot, "reveal-start", func() time.Time { return c.Now() })
			plot.Draw(func(p *ui.Painter, r ui.Rect) {
				x := func(t float64) float32 {
					if last <= first {
						return r.X + r.W/2
					}
					return r.X + float32((t-first)/(last-first))*r.W
				}
				y := func(v float64) float32 { return r.Y + 6 + float32(1-v/maximum)*(r.H-12) }
				for _, fraction := range []float64{0, .5, 1} {
					dashed(p, r.X, y(maximum*fraction), r.X+r.W, y(maximum*fraction), 1, c.Theme().Border, 4, 5)
				}
				for i := 0; i <= 6; i++ {
					t := first + (last-first)*float64(i)/6
					p.Line(x(t), y(maximum), x(t), y(0), 1, c.Theme().Border.Alpha(.35))
					if last <= first {
						break
					}
				}
				fraction := float32(1)
				if !c.Preferences().ReduceMotion {
					fraction = motionEase(float32(p.Now().Sub(*started)) / float32(motionReveal))
					if fraction < 1 {
						p.AnimationFrame()
					}
				}
				p.Clip(ui.Rect{X: r.X - 6, Y: r.Y - 6, W: (r.W + 12) * fraction, H: r.H + 12}, 0, func() {
					renderer.draw(p, r, first, last, c.Theme().Accent, c.Invalidate)
				})
				if selected >= 0 {
					point := points[selected]
					p.Line(x(point.Time), r.Y, x(point.Time), r.Y+r.H, 1, c.Theme().Accent.Alpha(.4))
					p.Fill(ui.Rect{X: x(point.Time) - 4, Y: y(point.Value) - 4, W: 8, H: 8}, c.Theme().Accent, 4)
				}
			})
			if plot.Hovered() {
				x, _, _ := plot.PointerPosition()
				box := plot.Bounds()
				nearest := 0
				if last > first && box.W > 0 {
					nearest = chartNearest(points, first+(last-first)*float64(x)/float64(box.W))
				}
				selected = nearest
				a.chartSelection[key] = nearest
			}
			if plot.Focused() {
				for _, pair := range []struct {
					key   ui.Key
					delta int
				}{{ui.KeyLeft, -1}, {ui.KeyRight, 1}} {
					if plot.Shortcut(0, pair.key) {
						selected = max(0, min(len(points)-1, selected+pair.delta))
						a.chartSelection[key] = selected
					}
				}
				if plot.Shortcut(0, ui.KeyHome) {
					selected = 0
					a.chartSelection[key] = selected
				}
				if plot.Shortcut(0, ui.KeyEnd) {
					selected = len(points) - 1
					a.chartSelection[key] = selected
				}
			}
		})
		ui.Row(c).Padding(0, 0, 0, 46).Height(20).Justify(ui.SpaceBetween).Children(func() {
			for i := 0; i <= 6; i++ {
				t := time.UnixMilli(int64(first + (last-first)*float64(i)/6)).Local()
				format := "15:04"
				if last-first > 2*86400000 {
					format = "01/02"
				}
				ui.Text(c, t.Format(format)).FontSize(10).TextColor(c.Theme().TextMuted)
			}
		})
		caption := " "
		if selected >= 0 {
			caption = points[selected].Name + " · " + format(points[selected].Value)
		}
		ui.Text(c, caption).FontSize(10).TextColor(c.Theme().TextMuted)
	})
}
func (a *App) timelinePoints(metric string) []chartPoint {
	return a.chartPoints("timeline-"+metric, func() []chartPoint {
		rows := objects(a.data["timeline"])
		result := make([]chartPoint, 0, len(rows))
		gap := false
		for _, row := range rows {
			if metric == "cost" && number(row["unpriced"]) == number(row["requests"]) && number(row["requests"]) > 0 {
				gap = true
				continue
			}
			result = append(result, chartPoint{Time: number(row["time"]), Value: number(row[metric]), Name: str(row["name"]), Gap: gap})
			gap = false
		}
		return result
	})
}

func dashed(p *ui.Painter, x1, y1, x2, y2, width float32, color ui.Color, dash, gap float32) {
	dx, dy := x2-x1, y2-y1
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length == 0 {
		return
	}
	for start := float32(0); start < length; start += dash + gap {
		end := min(length, start+dash)
		p.Line(x1+dx*start/length, y1+dy*start/length, x1+dx*end/length, y1+dy*end/length, width, color)
	}
}
