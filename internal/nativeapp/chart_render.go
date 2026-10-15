package nativeapp

import (
	"math"
	"slices"
	"sort"

	"github.com/egoist/mygo/ui"
)

type chartDataKey struct {
	first          *chartPoint
	count          int
	revision       uint64
	percent, quota bool
	unit           string
}

// This belongs to the visible chart element, so leaving the page or changing
// its metric releases it. Keep only the final bitmap, not a second UI engine,
// its glyph atlas, or a separate copy for every animation frame.
type chartRenderer struct {
	name       string
	data       chartDataKey
	maximum    float64
	points     []chartPoint
	segments   [][]chartPoint
	bitmap     *ui.Bitmap
	imageKey   chartImageKey
	generation uint64
	pending    chan chartImage
}

type chartImage struct {
	key        chartImageKey
	generation uint64
	bitmap     *ui.Bitmap
}

type chartImageKey struct {
	width, height, scale float32
	first, last          float64
	accent               ui.Color
}

func (a *App) releaseChart() {
	if a.chartRenderer != nil {
		*a.chartRenderer = chartRenderer{}
		a.chartRenderer = nil
	}
}

func (r *chartRenderer) prepare(points []chartPoint, percent, quota bool, unit string, revision uint64) {
	key := chartDataKey{&points[0], len(points), revision, percent, quota, unit}
	if r.data == key {
		return
	}
	unchanged := r.data.first != key.first && r.data.count == key.count && r.data.percent == percent && r.data.quota == quota && r.data.unit == unit
	if unchanged {
		for i, point := range points {
			previous := r.points[i]
			if point.Time != previous.Time || point.Value != previous.Value || point.Reset != previous.Reset || point.Gap != previous.Gap {
				unchanged = false
				break
			}
		}
	}
	r.data, r.points = key, points
	if unchanged {
		// A service refresh can change captions/settings without changing
		// this curve. Borrow the new records and keep its existing layer.
		offset := 0
		for i, segment := range r.segments {
			r.segments[i] = points[offset : offset+len(segment)]
			offset += len(segment)
		}
		return
	}
	r.bitmap = nil
	r.generation++
	r.maximum = 1
	if unit == "USD" {
		r.maximum = .01
	}
	if percent {
		r.maximum = 100
	} else {
		for _, point := range points {
			r.maximum = max(r.maximum, point.Value)
		}
	}
	r.segments = chartSegments(points, quota)
}

func (r *chartRenderer) draw(p *ui.Painter, box ui.Rect, first, last float64, accent ui.Color, wake func()) {
	if box.W <= 0 || box.H <= 0 || len(r.points) == 0 {
		return
	}
	key := chartImageKey{box.W, box.H, p.Scale(), first, last, accent}
	width, height := int(math.Ceil(float64(box.W+12))), int(math.Ceil(float64(box.H+12)))
	if r.pending != nil {
		select {
		case ready := <-r.pending:
			r.pending = nil
			if ready.generation == r.generation && ready.key == key {
				r.bitmap, r.imageKey = ready.bitmap, ready.key
			}
		default:
		}
	}
	if r.bitmap == nil || r.imageKey != key {
		if r.pending == nil {
			// Coalesce resize/refresh requests while a job is running. It owns
			// its records; the GUI thread alone installs the completed image.
			job := chartRenderer{data: r.data, maximum: r.maximum, points: slices.Clone(r.points)}
			job.segments = chartSegments(job.points, r.data.quota)
			generation := r.generation
			ready := make(chan chartImage, 1)
			r.pending = ready
			go func() {
				ready <- chartImage{key, generation, ui.NewBitmap(job.raster(key))}
				wake()
			}()
		}
		// Display current data immediately. Building the cache must not
		// turn the first frame or a live resize into a synchronous stall.
		r.paint(p, box, first, last, accent)
		return
	}
	p.Image(r.bitmap, ui.Rect{X: box.X - 6, Y: box.Y - 6, W: float32(width), H: float32(height)}, ui.FillBox)
}

func (r *chartRenderer) paint(p *ui.Painter, box ui.Rect, first, last float64, accent ui.Color) {
	x := func(t float64) float32 {
		if last <= first {
			return box.X + box.W/2
		}
		return box.X + float32((t-first)/(last-first))*box.W
	}
	y := func(v float64) float32 { return box.Y + 6 + float32(1-v/r.maximum)*(box.H-12) }
	pointsFirst := r.segments[0][0].Time
	final := r.segments[len(r.segments)-1]
	pointsLast := final[len(final)-1].Time
	for segmentIndex, segment := range r.segments {
		if r.data.quota && segmentIndex > 0 && !segment[0].Gap {
			previous := r.segments[segmentIndex-1]
			lastPoint := previous[len(previous)-1]
			dashed(p, x(lastPoint.Time), y(lastPoint.Value), x(segment[0].Time), y(segment[0].Value), 1.2, accent.Alpha(.45), 3, 5)
		}
		display := segment
		if r.data.quota {
			display = chartDisplayPoints(segment, r.maximum*(chartStrokeWidth/2)/float64(max(1, box.H-12)))
		}
		path := chartCurve(display, x, y)
		if last > first && (pointsLast-pointsFirst)/(last-first) >= .1 && len(segment) > 1 && segment[len(segment)-1].Time > segment[0].Time {
			area := path
			area.LineTo(x(segment[len(segment)-1].Time), y(0)).LineTo(x(segment[0].Time), y(0)).Close()
			p.FillPathGradient(&area, chartFill(accent))
		}
		p.StrokePath(&path, chartStrokeWidth, accent)
		if r.data.count < 15 {
			radius := float32(2.5)
			if r.data.count < 4 {
				radius = 4.5
			}
			for _, point := range segment {
				p.Fill(ui.Rect{X: x(point.Time) - radius, Y: y(point.Value) - radius, W: radius * 2, H: radius * 2}, accent, radius)
			}
		}
		if len(segment) == 1 {
			point := segment[0]
			p.Fill(ui.Rect{X: x(point.Time) - 3, Y: y(point.Value) - 3, W: 6, H: 6}, accent, 3)
		}
	}
}

// Records arrive in time order. Binary search keeps interaction O(log n),
// including the old rule that ties and duplicate timestamps select the first.
func chartNearest(points []chartPoint, at float64) int {
	if len(points) == 0 {
		return -1
	}
	i := sort.Search(len(points), func(i int) bool { return points[i].Time >= at })
	if i == 0 {
		return 0
	}
	if i == len(points) || at-points[i-1].Time <= points[i].Time-at {
		at = points[i-1].Time
		return sort.Search(i, func(j int) bool { return points[j].Time >= at })
	}
	return i
}

// Snapshot records are immutable. Retain only the currently selected source;
// repeatedly formatting all-history timestamps during animation is needless.
type chartSourceCache struct {
	key      string
	revision uint64
	points   []chartPoint
}

func (a *App) chartPoints(key string, load func() []chartPoint) []chartPoint {
	if a.chartSource.key != key || a.chartSource.revision != a.snapshotRevision {
		if a.chartSource.key != "" && a.chartSource.key != key {
			a.releaseChart()
		}
		a.chartSource = chartSourceCache{key, a.snapshotRevision, load()}
	}
	return a.chartSource.points
}
