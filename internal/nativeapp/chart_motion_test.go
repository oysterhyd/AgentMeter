package nativeapp

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestRangeRevealAnimatesSparseUsageAndKeepsLayerAndFocus(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%t", dark), func(t *testing.T) {
			a := NewApp()
			start := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
			a.filter["range"] = "today"
			a.snapshotKey = "today"
			a.data["range"] = Object{"start": start.Format(time.RFC3339), "end": start.AddDate(0, 0, 1).Format(time.RFC3339)}
			// Two hours of observations on a 24-hour axis, like sparse usage
			// just after midnight. Revealing the whole axis hides the motion.
			points := []chartPoint{{Time: float64(start.UnixMilli()), Value: 60}, {Time: float64(start.Add(2 * time.Hour).UnixMilli()), Value: 60}}
			tt := ui.NewTester(func(c *ui.Context) {
				a.skin(c)
				ui.Column(c).Padding(20).Children(func() { a.chart(c, "usage-total", points, true, false, "tokens") })
			}, 760, 260)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			awaitChartLayer(t, tt, a.chartRenderer)
			tt.Key(0, ui.KeyTab)
			label := "用量趋势 usage-total"
			if !tt.Focused(label) {
				t.Fatal("chart did not receive keyboard focus")
			}
			plot, _ := tt.Find(label)
			// An independent pixel probe near the last observation must be
			// visible only after the curve has unfolded across its data span.
			x := int(plot.X + (plot.W-chartAxis)*.9/12)
			y := int(plot.Y + 6 + .4*(plot.H-12))
			ink := func() bool {
				for py := y - 2; py <= y+2; py++ {
					pixel := tt.Image().RGBAAt(x, py)
					if int(pixel.B)-int(pixel.R) > 40 && int(pixel.B)-int(pixel.G) > 10 {
						return true
					}
				}
				return false
			}
			if !ink() {
				t.Fatal("settled sparse curve is missing")
			}
			renderer, layer := a.chartRenderer, a.chartRenderer.bitmap
			tt.SetPreferences(ui.Preferences{})
			for _, key := range []string{"7d", "30d", "all"} {
				a.snapshotKey = key
				tt.Frame()
				if ink() {
					t.Fatal("range change skipped the reveal or animated the empty axis")
				}
				if a.chartRenderer != renderer || renderer.bitmap != layer || !tt.Focused(label) {
					t.Fatal("reveal discarded its cached layer or keyboard focus")
				}
			}
			time.Sleep(motionReveal + 60*time.Millisecond)
			tt.Frame()
			if !ink() {
				t.Fatal("latest range animation did not finish")
			}
			// Background refreshes keep the current animation state; only a
			// filter/metric change replays it. Reduced motion still snaps.
			points = append([]chartPoint(nil), points...)
			a.snapshotRevision++
			tt.Frame()
			if !ink() || renderer.bitmap != layer {
				t.Fatal("background refresh replayed or rebuilt the unchanged curve")
			}
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			a.snapshotKey = "custom"
			tt.Frame()
			if !ink() {
				t.Fatal("reduced motion did not show the full curve immediately")
			}
		})
	}
}
