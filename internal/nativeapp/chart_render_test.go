package nativeapp

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func awaitChartLayer(t *testing.T, tt *ui.Tester, renderer *chartRenderer) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if renderer.bitmap != nil && renderer.pending == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chart layer did not finish")
}

func TestChartNearestMatchesAllOriginalObservations(t *testing.T) {
	rng := rand.New(rand.NewPCG(71, 19))
	points := make([]chartPoint, 2000)
	for i := 1; i < len(points); i++ {
		points[i].Time = points[i-1].Time + float64(rng.IntN(6))
	}
	for i := 0; i < 10000; i++ {
		at := float64(rng.IntN(int(points[len(points)-1].Time)+100)) - 50 + float64(i%2)/2
		want, distance := 0, math.Inf(1)
		for j, point := range points {
			if d := math.Abs(point.Time - at); d < distance {
				want, distance = j, d
			}
		}
		if got := chartNearest(points, at); got != want {
			t.Fatalf("at %.1f: got sample %d, want %d", at, got, want)
		}
	}
	if chartNearest(nil, 0) != -1 || chartNearest([]chartPoint{{Time: 5}}, 500) != 0 {
		t.Fatal("empty/single-point lookup changed")
	}
}

func TestChartSourcesRefreshAndKeepCostGaps(t *testing.T) {
	a := NewApp()
	a.data["timeline"] = []Object{
		{"time": 1., "total": 20., "cost": 1., "requests": 1., "unpriced": 0.},
		{"time": 2., "total": 30., "requests": 1., "unpriced": 1.},
		{"time": 3., "total": 40., "cost": 2., "requests": 1., "unpriced": 0.},
	}
	first := a.timelinePoints("total")
	if next := a.timelinePoints("total"); &first[0] != &next[0] {
		t.Fatal("unchanged snapshot copied the source")
	}
	cost := a.timelinePoints("cost")
	if len(cost) != 2 || cost[0].Gap || !cost[1].Gap || cost[1].Value != 2 {
		t.Fatal("unpriced interval lost", cost)
	}
	a.data["timeline"] = []Object{{"time": 4., "total": 100., "cost": 3.}}
	a.snapshotRevision++
	if next := a.timelinePoints("cost"); len(next) != 1 || next[0].Time != 4 || next[0].Value != 3 {
		t.Fatal("new snapshot kept the old source", next)
	}
	// Quota slots/accounts share the bounded source cache, without mixing data.
	if next := a.chartPoints("quota-a:primary", func() []chartPoint { return []chartPoint{{Value: 90}} }); next[0].Value != 90 {
		t.Fatal(next)
	}
	if next := a.chartPoints("quota-b:secondary", func() []chartPoint { return []chartPoint{{Value: 40}} }); next[0].Value != 40 {
		t.Fatal(next)
	}
}

func TestChartLayerInvalidationAndPositionReuse(t *testing.T) {
	points := []chartPoint{{Time: 0, Value: 100}, {Time: 1, Value: 40}, {Time: 2, Value: 70}}
	var renderer chartRenderer
	var revision uint64
	left := float32(18)
	width := float32(340)
	first, last := float64(0), float64(2)
	tt := ui.NewTester(func(c *ui.Context) {
		renderer.prepare(points, true, false, "", revision)
		ui.Box(c).Absolute().Left(left).Top(18).Width(width).Height(150).Draw(func(p *ui.Painter, box ui.Rect) {
			renderer.draw(p, box, first, last, c.Theme().Accent, c.Invalidate)
		})
	}, 400, 200)
	awaitChartLayer(t, tt, &renderer)
	image := renderer.bitmap
	beforeMove := bytes.Clone(tt.Image().Pix)
	left += 7
	tt.Frame()
	if renderer.bitmap != image || bytes.Equal(beforeMove, tt.Image().Pix) {
		t.Fatal("moving a chart rebuilt its layer or failed to move it")
	}
	left -= 7
	tt.Frame()
	image = renderer.bitmap
	tt.Frame()
	if renderer.bitmap != image || !bytes.Equal(beforeMove, tt.Image().Pix) {
		t.Fatal("unchanged chart rebuilt or changed appearance")
	}
	points = slices.Clone(points)
	points[1].Name = "updated caption"
	revision++
	tt.Frame()
	if renderer.bitmap != image || renderer.points[1].Name != "updated caption" {
		t.Fatal("identical geometry rebuilt or lost new source metadata")
	}
	assertRebuilt := func(change func()) {
		t.Helper()
		previous := renderer.bitmap
		change()
		awaitChartLayer(t, tt, &renderer)
		if renderer.bitmap == previous {
			t.Fatal("changed chart kept a stale image")
		}
		cached := bytes.Clone(tt.Image().Pix)
		renderer.bitmap = nil
		awaitChartLayer(t, tt, &renderer)
		if !bytes.Equal(cached, tt.Image().Pix) {
			t.Fatal("cached image differs from a fresh render")
		}
	}
	assertRebuilt(func() { tt.SetScale(2) })
	assertRebuilt(func() { tt.SetDark(true) })
	assertRebuilt(func() { width += 50; tt.SetSize(450, 200) })
	assertRebuilt(func() { last = 3 })
	assertRebuilt(func() { points[1].Value = 60; revision++ })
	// Several changes can overtake a worker; only the final state may be used.
	renderer.bitmap = nil
	tt.Frame()
	for i := 0; i < 8; i++ {
		points = slices.Clone(points)
		points[1].Value = 25 + float64(i)*4
		revision++
		last = 4 + float64(i%2)
		tt.SetScale(1 + float32(i%2)*.5)
		tt.SetDark(i%2 == 0)
	}
	awaitChartLayer(t, tt, &renderer)
	final := bytes.Clone(tt.Image().Pix)
	renderer.bitmap = nil
	awaitChartLayer(t, tt, &renderer)
	if !bytes.Equal(final, tt.Image().Pix) {
		t.Fatal("an obsolete worker replaced the final curve")
	}
}

func TestChartLayerMatchesOriginalDrawing(t *testing.T) {
	for _, quota := range []bool{false, true} {
		points := chartHistoryPoints(quota)
		original := slices.Clone(points)
		var renderer chartRenderer
		renderer.prepare(points, false, quota, "tokens", 0)
		for _, scale := range []float32{1, 1.5, 2} {
			cached := false
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Box(c).Absolute().Left(18).Top(18).Size(340, 150).Draw(func(p *ui.Painter, box ui.Rect) {
					if cached {
						renderer.draw(p, box, points[0].Time, points[len(points)-1].Time, c.Theme().Accent, c.Invalidate)
					} else {
						renderer.paint(p, box, points[0].Time, points[len(points)-1].Time, c.Theme().Accent)
					}
				})
			}, 400, 200)
			tt.SetScale(scale)
			captureActivity(t, tt, fmt.Sprintf("chart-direct-%t-%.1f", quota, scale))
			direct := bytes.Clone(tt.Image().Pix)
			cached = true
			awaitChartLayer(t, tt, &renderer)
			captureActivity(t, tt, fmt.Sprintf("chart-cached-%t-%.1f", quota, scale))
			pixels := tt.Image().Pix
			var difference int64
			for i, value := range direct {
				difference += int64(math.Abs(float64(int(value) - int(pixels[i]))))
			}
			// Independently rasterized masks can round antialiased edges
			// differently (MyGO 0.5 antialiases a little softer). Allow <0.6%
			// mean channel error; source/geometry
			// and discontinuities are verified separately.
			if mean := float64(difference) / float64(len(direct)); mean > 1.5 {
				t.Fatalf("quota=%t scale=%.1f: mean channel error %.3f", quota, scale, mean)
			}
		}
		if !slices.Equal(points, original) {
			t.Fatal("rendering changed source observations")
		}
	}
}

func TestChartWorkerPreservesGapsAndReleasesRetiredLayers(t *testing.T) {
	for _, quota := range []bool{false, true} {
		points := []chartPoint{{Time: 0, Value: 60}, {Time: 1, Value: 60}, {Time: 9, Value: 60, Gap: true}, {Time: 10, Value: 60}}
		var renderer chartRenderer
		renderer.prepare(points, true, quota, "", 0)
		pixels := renderer.raster(chartImageKey{width: 500, height: 150, scale: 2, first: 0, last: 10, accent: ui.Hex("#00785e")})
		for _, fraction := range []float32{.05, .5, .95} {
			x := int((6 + 500*fraction) * 2)
			ink := pixels.RGBAAt(x, int(math.Round((12+.4*138)*2))).A > 0
			if ink != (fraction != .5) {
				t.Fatalf("quota=%t: missing curve or filled gap at %.2f", quota, fraction)
			}
		}
	}
	a := NewApp()
	a.filter["range"] = "all"
	points, _ := chartPreviewPoints()
	key := "total"
	tt := ui.NewTester(func(c *ui.Context) {
		a.chart(c, key, a.chartPoints(key, func() []chartPoint { return points }), false, false, "tokens")
	}, 640, 260)
	awaitChartLayer(t, tt, a.chartRenderer)
	retired := a.chartRenderer
	key = "output"
	tt.Frame()
	if retired.bitmap != nil || retired.pending != nil || len(retired.points) != 0 || a.chartRenderer == retired {
		t.Fatal("changing metric retained a retired chart layer")
	}
	retired = a.chartRenderer
	a.snapshotKey = "changed-range"
	tt.Frame()
	if a.chartRenderer != retired {
		t.Fatal("changing range discarded the chart renderer")
	}
	points = []chartPoint{{Time: 1, Value: 10}, {Time: 2, Value: 20}}
	a.snapshotRevision++
	tt.Frame()
	if a.chartRenderer != retired || len(retired.points) != 2 || retired.points[1].Value != 20 {
		t.Fatal("stable chart identity kept the previous range's data")
	}
	points = nil
	a.snapshotRevision++
	tt.Frame()
	if a.chartRenderer != nil {
		t.Fatal("empty data retained the previous chart")
	}
	a.releaseChart()
	if a.chartRenderer != nil {
		t.Fatal("leaving the chart retained its renderer")
	}
}
