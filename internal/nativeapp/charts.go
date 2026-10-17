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
	a.chartColored(c, key, points, percent, quota, unit, a.pal.accent)
}

const chartHeight = 168 // DIPs of plot

// chartColored is a line chart in the manner of Swift Charts: a gradient
// under the curve, hairline rules, trailing axis labels, and a lollipop with
// a callout under the pointer.
func (a *App) chartColored(c *ui.Context, key string, points []chartPoint, percent, quota bool, unit string, accent ui.Color) {
	pal := a.pal
	if len(points) == 0 {
		a.releaseChart()
		a.empty(c, a.tr("这个时间范围内还没有记录"))
		return
	}
	// A new filter changes the data, not this chart's identity. Keep its
	// reveal/focus state so a range switch does not replay the 780ms entrance.
	column := ui.Column(c.Key("chart-" + key)).Gap(6)
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
		if start := dateValue(str(obj(a.data["range"])["start"])); !start.IsZero() {
			first = float64(start.UnixMilli())
		}
		if end := dateValue(str(obj(a.data["range"])["end"])); !end.IsZero() {
			last = float64(end.UnixMilli())
		}
	}
	if a.chartSelection == nil {
		a.chartSelection = map[string]int{}
	}
	selected := -1
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
	axis := func(v float64) string {
		if percent {
			return fmt.Sprintf("%.0f%%", v)
		} else if unit == "USD" {
			return money(v)
		}
		return compact(v)
	}
	column.Children(func() {
		plot := ui.Box(c.Key(key)).FillWidth().Height(chartHeight).Focusable().Label(a.tr("用量趋势") + " " + key)
		ui.Local(plot, "init", func() bool { plot.Jump("reveal", 0); return true })
		filterKey := ui.Local(plot, "filter", func() string { return a.snapshotKey })
		if *filterKey != a.snapshotKey {
			plot.Jump("reveal", 1)
			*filterKey = a.snapshotKey
		}
		reveal := plot.AnimateTo("reveal", 1, ui.Animation{Duration: motionReveal, Ease: motionEase})
		active := false
		if plot.Hovered() {
			x, _, _ := plot.PointerPosition()
			box := plot.Bounds()
			if w := box.W - chartAxis; last > first && w > 0 {
				selected = chartNearest(points, first+(last-first)*float64(min(x, w))/float64(w))
			} else {
				selected = 0
			}
			a.chartSelection[key] = selected
			active = true
		}
		if plot.Focused() {
			active = true
			for _, move := range []struct {
				key ui.Key
				to  int
			}{{ui.KeyLeft, selected - 1}, {ui.KeyRight, selected + 1}, {ui.KeyHome, 0}, {ui.KeyEnd, len(points) - 1}} {
				if plot.Shortcut(0, move.key) {
					selected = max(0, min(len(points)-1, move.to))
					a.chartSelection[key] = selected
				}
			}
			selected = max(selected, 0)
		}
		show := plot.Animate("callout", map[bool]float32{true: 1, false: 0}[active && selected >= 0], motionQuick)
		labels := [3]string{axis(maximum), axis(maximum / 2), axis(0)}
		plot.Draw(func(p *ui.Painter, r ui.Rect) {
			r.W -= chartAxis
			y := func(v float64) float32 { return r.Y + 6 + float32(1-v/maximum)*(r.H-12) }
			for i, fraction := range []float64{1, .5, 0} {
				line := y(maximum * fraction)
				p.Fill(ui.Rect{X: r.X, Y: line - .5, W: r.W, H: 1}, pal.separator.Alpha(float32(pal.separator.A)/255*map[bool]float32{true: 1, false: .6}[fraction == 0]), 0)
				p.Text(r.X+r.W+10, line-7, labels[i], 10, pal.tertiary)
			}
			p.Clip(ui.Rect{X: r.X - 6, Y: r.Y - 6, W: (r.W + 12) * reveal, H: r.H + 12}, 0, func() {
				renderer.draw(p, r, first, last, accent, c.Invalidate)
			})
		})
		plot.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if show <= 0 || selected < 0 {
				return
			}
			r.W -= chartAxis
			point := points[selected]
			px := r.X + r.W/2
			if last > first {
				px = r.X + float32((point.Time-first)/(last-first))*r.W
			}
			py := r.Y + 6 + float32(1-point.Value/maximum)*(r.H-12)
			p.Fill(ui.Rect{X: px - .5, Y: r.Y, W: 1, H: r.H}, pal.label.Alpha(.18*show), 0)
			ring := 5 * show
			p.Shadow(ui.Rect{X: px - ring, Y: py - ring, W: ring * 2, H: ring * 2}, ring, 0, 1, 4, 0, ui.RGBA(0, 0, 0, .25*show))
			p.Fill(ui.Rect{X: px - ring, Y: py - ring, W: ring * 2, H: ring * 2}, pal.card, ring)
			p.Fill(ui.Rect{X: px - ring + 2, Y: py - ring + 2, W: ring*2 - 4, H: ring*2 - 4}, accent, ring-2)
			title, value := point.Name, format(point.Value)
			tw, _ := p.MeasureText(0, ui.Span{Text: title, Size: 11})
			vw, _ := p.MeasureText(0, ui.Span{Text: value, Size: 13, Weight: 600})
			w := max(tw, vw) + 20
			bx := max(r.X, min(r.X+r.W-w, px-w/2))
			by := py - 58
			if by < r.Y-10 {
				by = py + 14
			}
			bubble := ui.Rect{X: bx, Y: by + (1-show)*6, W: w, H: 44}
			p.Shadow(bubble, 10, 0, 4, 14, 0, ui.RGBA(0, 0, 0, map[bool]float32{true: .45, false: .14}[pal.dark]*show))
			p.Fill(bubble, pal.control.Alpha(show), 10)
			p.Stroke(bubble, pal.hairline.Alpha(float32(pal.hairline.A)/255*show), 10, 1)
			p.Text(bubble.X+10, bubble.Y+6, title, 11, pal.secondary.Alpha(show))
			p.RichText(bubble.X+10, bubble.Y+21, 0, ui.Span{Text: value, Size: 13, Weight: 600, Color: pal.label.Alpha(show)})
		})
		ui.Row(c).Padding(0, chartAxis, 0, 0).Justify(ui.SpaceBetween).Children(func() {
			ticks := 6
			if last <= first {
				ticks = 0
			}
			for i := 0; i <= ticks; i++ {
				t := time.UnixMilli(int64(first + (last-first)*float64(i)/float64(max(ticks, 1)))).Local()
				layout := "15:04"
				if last-first > 2*86400000 {
					layout = "01/02"
				}
				ui.Text(c, t.Format(layout)).FontSize(10).TextColor(pal.tertiary).FontFeatures("tnum")
			}
		})
	})
}

const chartAxis = 48 // DIPs right of the plot for value labels

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
