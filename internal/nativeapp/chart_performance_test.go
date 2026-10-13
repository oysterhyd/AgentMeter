package nativeapp

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A long history includes many quota resets as well as dense token peaks.
// Both revisions use these exact observations and the real chart renderer.
func chartHistoryPoints(quota bool) []chartPoint {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := make([]chartPoint, 2400)
	for i := range points {
		at := start.Add(time.Duration(i) * time.Hour)
		value := 40 + 25*math.Sin(float64(i)*.13) + 10*math.Sin(float64(i)*1.7)
		reset := float64(0)
		if quota {
			value = 100 - float64(i%5)*23
			reset = float64(start.Add(time.Duration(i/5*5+5) * time.Hour).Unix())
		}
		points[i] = chartPoint{Time: float64(at.UnixMilli()), Value: value, Name: at.Format("01/02 15:04"), Reset: reset}
	}
	return points
}

// Frame includes UI construction, geometry, mask generation/cache lookup and
// software composition. It is not a measurement of native GPU presentation.
func BenchmarkChartHistoryFrame(b *testing.B) {
	benchmarkChartHistoryFrames(b, false)
}

func BenchmarkChartHistoryMovingFrame(b *testing.B) {
	benchmarkChartHistoryFrames(b, true)
}

func benchmarkChartHistoryFrames(b *testing.B, moving bool) {
	for _, quota := range []bool{false, true} {
		name := "tokens"
		if quota {
			name = "quota"
		}
		b.Run(name, func(b *testing.B) {
			a := NewApp()
			a.filter["range"] = "all"
			points := chartHistoryPoints(quota)
			shift := float32(0)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Absolute().Left(shift).Top(0).Width(1100).Padding(20).Children(func() { a.chart(c, name, points, false, quota, "tokens") })
			}, 1100, 260)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.SetScale(1.5)
			// Identical warm-up on both revisions; the candidate prepares its
			// immutable layer in the background instead of blocking the GUI.
			for i := 0; i < 4; i++ {
				time.Sleep(100 * time.Millisecond)
				tt.Frame()
			}
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				if moving {
					shift = float32(i%120) / 10
					i++
				}
				tt.Frame()
			}
		})
	}
}

func BenchmarkChartHistoryColdFrame(b *testing.B) {
	for _, quota := range []bool{false, true} {
		name := "tokens"
		if quota {
			name = "quota"
		}
		b.Run(name, func(b *testing.B) {
			a := NewApp()
			a.filter["range"] = "all"
			points := chartHistoryPoints(quota)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Padding(20).Children(func() { a.chart(c, name, points, false, quota, "tokens") })
			}, 1100, 260)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.SetScale(1.5)
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				a.snapshotRevision++
				points[i%len(points)].Value += .5
				i++
				tt.Frame()
			}
		})
	}
}
