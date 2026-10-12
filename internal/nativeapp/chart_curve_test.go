package nativeapp

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestChartTangentsKeepObservedBounds(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 73))
	for run := 0; run < 200; run++ {
		points := make([]chartPoint, 25)
		for i := range points {
			points[i].Value = float64(rng.IntN(101))
			if i > 0 {
				points[i].Time = points[i-1].Time + float64(1+rng.IntN(10000))
				if i%4 == 0 {
					points[i].Value = points[i-1].Value
				}
			}
		}
		for i := 1; i < len(points); i++ {
			a, b := points[i-1], points[i]
			h := b.Time - a.Time
			m0, m1 := chartTangent(points, i-1)*h, chartTangent(points, i)*h
			for j := 0; j <= 100; j++ {
				f := float64(j) / 100
				// Independently evaluate Hermite interpolation, not a rendered path.
				v := (2*f*f*f-3*f*f+1)*a.Value + (f*f*f-2*f*f+f)*m0 + (-2*f*f*f+3*f*f)*b.Value + (f*f*f-f*f)*m1
				if math.IsNaN(v) || v < min(a.Value, b.Value)-1e-9 || v > max(a.Value, b.Value)+1e-9 {
					t.Fatalf("run %d, interval %d: curve %.8f outside [%.1f, %.1f]", run, i, v, a.Value, b.Value)
				}
			}
		}
	}
	line := []chartPoint{{Time: 0, Value: 20}, {Time: 3, Value: 17}, {Time: 8, Value: 12}}
	for i := range line {
		if chartTangent(line, i) != -1 {
			t.Fatal("constant rate did not keep its slope")
		}
	}
	peak := []chartPoint{{Time: 0, Value: 0}, {Time: 1, Value: 100}, {Time: 2, Value: 20}}
	if chartTangent(peak, 1) != 0 {
		t.Fatal("peak is not a smooth horizontal tangent")
	}
}

func TestChartDisplayPointsKeepSamplesExtremaAndChordBound(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 57))
	for run := 0; run < 30; run++ {
		points := make([]chartPoint, 1200)
		byTime := map[float64]int{}
		for i := range points {
			points[i] = chartPoint{Time: float64(i), Value: 100, Name: fmt.Sprintf("sample-%d", i)}
			if i > 0 {
				points[i].Value = points[i-1].Value
				if i%3 == 0 {
					delta := float64(rng.IntN(4)) / 10
					if i/300%2 == 0 {
						delta = -delta
					}
					points[i].Value += delta
				}
			}
			byTime[points[i].Time] = i
		}
		original := slices.Clone(points)
		const tolerance = .58
		display := chartDisplayPoints(points, tolerance)
		if !slices.Equal(points, original) || display[0] != points[0] || display[len(display)-1] != points[len(points)-1] {
			t.Fatal("samples mutated or endpoints lost")
		}
		if len(display) >= len(points)/10 {
			t.Fatalf("dense staircase retained %d of %d drawing points", len(display), len(points))
		}
		retained := map[float64]bool{}
		for i, point := range display {
			if point != points[byTime[point.Time]] {
				t.Fatal("drawing point is not an original observation")
			}
			retained[point.Time] = true
			if i == 0 {
				continue
			}
			a, b := display[i-1], point
			for j := byTime[a.Time] + 1; j < byTime[b.Time]; j++ {
				chord := a.Value + (b.Value-a.Value)*(points[j].Time-a.Time)/(b.Time-a.Time)
				if math.Abs(chord-points[j].Value) > tolerance+1e-9 {
					t.Fatalf("sample %d: chord error exceeds tolerance", j)
				}
			}
		}
		direction := float64(0)
		for i := 1; i < len(points); i++ {
			delta := points[i].Value - points[i-1].Value
			if delta == 0 {
				continue
			}
			if direction != 0 && math.Signbit(delta) != math.Signbit(direction) && !retained[points[i-1].Time] {
				t.Fatal("local peak/trough lost")
			}
			direction = delta
		}
	}
}

func TestChartDuplicateTimesAndSmallSeries(t *testing.T) {
	for _, points := range [][]chartPoint{nil, {{Time: 1, Value: 35}}, {{Time: 1, Value: 35}, {Time: 2, Value: 70}}} {
		if !slices.Equal(points, chartDisplayPoints(points, .58)) {
			t.Fatal("short series changed")
		}
	}
	points := []chartPoint{{Time: 1, Value: 100}, {Time: 2, Value: 90}, {Time: 2, Value: 40}, {Time: 3, Value: 20}, {Time: 4, Value: 0}}
	display := chartDisplayPoints(points, .58)
	if !slices.Equal(display[:3], points[:3]) {
		t.Fatal("same-time change lost", display)
	}
	for i := range points {
		if v := chartTangent(points, i); math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatal("duplicate time produced an invalid tangent")
		}
	}
}

func TestChartHoverAndKeyboardUseAllObservations(t *testing.T) {
	a := NewApp()
	a.filter["range"] = "all"
	points := make([]chartPoint, 101)
	for i := range points {
		points[i] = chartPoint{Time: float64(i), Value: 100 - float64(i)*.3, Name: fmt.Sprintf("sample-%d", i)}
	}
	if len(chartDisplayPoints(points, .58)) != 2 {
		t.Fatal("fixture must have omitted drawing samples")
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.skin(c)
		ui.Column(c).Padding(20).Children(func() { a.chart(c, "quota", points, true, true, "") })
	}, 640, 260)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	r, ok := tt.Find("用量趋势 quota")
	if !ok {
		t.Fatal("missing focusable chart")
	}
	tt.Move(r.X+r.W*.77, r.Y+40)
	if a.chartSelection["quota"] != 77 || !tt.HasText("sample-77 · 76.9%") {
		t.Fatal("hover selected a simplified observation", tt.Texts())
	}
	tt.ClickAt(r.X+r.W*.77, r.Y+40)
	tt.Move(2, 2)
	tt.Key(0, ui.KeyLeft)
	if !tt.HasText("sample-76 · 77.2%") {
		t.Fatal("keyboard skipped an observation", tt.Texts())
	}
	tt.Key(0, ui.KeyHome)
	if !tt.HasText("sample-0 · 100.0%") {
		t.Fatal("Home lost first sample")
	}
	tt.Key(0, ui.KeyEnd)
	if !tt.HasText("sample-100 · 70.0%") {
		t.Fatal("End lost last sample")
	}
}

func TestChartCurveAndFillDoNotBridgeGaps(t *testing.T) {
	for _, quota := range []bool{false, true} {
		a := NewApp()
		a.filter["range"] = "all"
		points := []chartPoint{{Time: 0, Value: 60}, {Time: 1, Value: 60}, {Time: 9, Value: 60, Gap: true}, {Time: 10, Value: 60}}
		tt := ui.NewTester(func(c *ui.Context) {
			a.skin(c)
			ui.Column(c).Padding(20).Children(func() { a.chart(c, "gap", points, true, quota, "") })
		}, 640, 260)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			r, _ := tt.Find("用量趋势 gap")
			ink := func(fraction float32) bool {
				x := int(r.X + r.W*fraction)
				y := int(r.Y + 6 + .4*(r.H-12))
				for py := y - 2; py <= y+2; py++ {
					color := tt.Image().RGBAAt(x, py)
					if int(color.G)-int(color.R) > 40 && int(color.G)-int(color.B) > 10 {
						return true
					}
				}
				return false
			}
			if !ink(.05) || !ink(.95) || ink(.5) {
				t.Fatalf("quota=%t dark=%t: missing curve or connected gap", quota, dark)
			}
		}
	}
}

// Synthetic curves reproduce the sparse peaks and dense, quantized quota
// observations in the reported screenshots without reading personal data.
func chartPreviewPoints() ([]chartPoint, []chartPoint) {
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)
	tokens := []chartPoint{}
	for i, hour := range []int{10, 13, 14, 15, 16, 17, 18, 20, 21, 22} {
		at := start.Add(time.Duration(hour) * time.Hour)
		tokens = append(tokens, chartPoint{Time: float64(at.UnixMilli()), Value: []float64{4486709, 13350000, 12800000, 22100000, 13500000, 11050000, 9000000, 3850000, 7800000, 800000}[i], Name: at.Format("2006-01-02 15:04")})
	}
	quota := []chartPoint{}
	for _, knots := range [][][2]float64{
		{{9.8, 100}, {10.55, 100}, {10.98, 89}, {13.35, 89}, {13.42, 80}, {13.9, 59}, {14.7, 38}, {15, 26}, {15.42, 0}},
		{{15.5, 100}, {15.6, 100}, {16, 68}, {16.6, 47}, {17, 43}, {18.1, 23}, {18.5, 12}, {19.25, 0}, {20.55, 0}},
		{{20.6, 100}, {20.7, 100}, {22, 71}},
	} {
		reset := float64(start.Unix()) + (knots[len(knots)-1][0]+.02)*3600
		for i := 1; i < len(knots); i++ {
			a, b := knots[i-1], knots[i]
			for hour := a[0]; hour < b[0]; hour += 1.0 / 60 {
				value := a[1] + (b[1]-a[1])*(hour-a[0])/(b[0]-a[0])
				at := start.Add(time.Duration(hour * float64(time.Hour)))
				quota = append(quota, chartPoint{Time: float64(at.UnixMilli()), Value: math.Round(value), Name: at.Format("01/02 15:04"), Reset: reset})
			}
		}
		last := knots[len(knots)-1]
		at := start.Add(time.Duration(last[0] * float64(time.Hour)))
		quota = append(quota, chartPoint{Time: float64(at.UnixMilli()), Value: last[1], Name: at.Format("01/02 15:04"), Reset: reset})
	}
	return tokens, quota
}

func TestChartSmoothPreview(t *testing.T) {
	tokens, quota := chartPreviewPoints()
	retained := 0
	for _, segment := range chartSegments(quota, true) {
		retained += len(chartDisplayPoints(segment, 100*(chartStrokeWidth/2)/138))
	}
	t.Logf("quota display: %d of %d original observations", retained, len(quota))
	for _, config := range []struct {
		width int
		scale float32
		dark  bool
	}{{1100, 1.25, false}, {760, 2, false}, {1100, 1.5, true}} {
		t.Run(fmt.Sprint(config), func(t *testing.T) {
			a := NewApp()
			start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)
			a.data["range"] = Object{"start": start.Format(time.RFC3339), "end": start.Add(24 * time.Hour).Format(time.RFC3339)}
			a.chartSelection = map[string]int{"tokens": 0, "quota": 0}
			tt := ui.NewTester(func(c *ui.Context) {
				a.skin(c)
				ui.Column(c).Padding(16).Gap(16).Children(func() {
					surface(c, 0, 300, func() {
						panelHeading(c, "Token 使用趋势", "")
						ui.Text(c, "输入（含缓存）   97.63M        输出   783.72K").FontSize(11).TextColor(c.Theme().TextMuted)
						a.chart(c, "tokens", tokens, false, false, "tokens")
					})
					surface(c, 0, 305, func() {
						panelHeading(c, "剩余额度历史", "")
						ui.Text(c, "5 小时窗口").FontSize(11).TextColor(c.Theme().Accent)
						a.chart(c, "quota", quota, true, true, "")
					})
				})
			}, config.width, 655)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.SetScale(config.scale)
			tt.SetDark(config.dark)
			if !tt.HasText("4,486,709 tokens") || !tt.HasText("100.0%") {
				t.Fatal("real observations missing from captions")
			}
			captureActivity(t, tt, fmt.Sprintf("charts-smooth-%d-%.2f-%t", config.width, config.scale, config.dark))
		})
	}
}

func BenchmarkChartDenseQuota(b *testing.B) {
	_, points := chartPreviewPoints()
	x := func(v float64) float32 {
		return float32((v-points[0].Time)/(points[len(points)-1].Time-points[0].Time)) * 1000
	}
	y := func(v float64) float32 { return float32(1-v/100) * 138 }
	for _, smooth := range []bool{false, true} {
		b.Run(fmt.Sprint(smooth), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if smooth {
					for _, segment := range chartSegments(points, true) {
						display := chartDisplayPoints(segment, 100*(chartStrokeWidth/2)/138)
						path := chartCurve(display, x, y)
						area := path
						area.LineTo(x(segment[len(segment)-1].Time), y(0)).LineTo(x(segment[0].Time), y(0)).Close()
						runtime.KeepAlive(path)
						runtime.KeepAlive(area)
					}
				} else {
					for _, segment := range chartSegments(points, true) {
						var line, area ui.Path
						for i, point := range segment {
							if i == 0 {
								line.MoveTo(x(point.Time), y(point.Value))
								area.MoveTo(x(point.Time), y(point.Value))
							} else {
								line.LineTo(x(point.Time), y(segment[i-1].Value)).LineTo(x(point.Time), y(point.Value))
								area.LineTo(x(point.Time), y(segment[i-1].Value)).LineTo(x(point.Time), y(point.Value))
							}
						}
						area.LineTo(x(segment[len(segment)-1].Time), y(0)).LineTo(x(segment[0].Time), y(0)).Close()
						runtime.KeepAlive(line)
						runtime.KeepAlive(area)
					}
				}
			}
		})
	}
}
