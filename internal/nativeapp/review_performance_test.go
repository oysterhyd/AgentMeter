package nativeapp

import (
	"context"
	"encoding/json"
	"testing"

	"local.codex.monitor/internal/monitor"
)

// The identical source also runs on 2.8.1: consume its JSON result there,
// and consume the detached Go result from the optimized native client here.
func BenchmarkReviewClient(b *testing.B) {
	a := fixtureApp(b)
	c, err := startNativeClient(monitor.Config{Data: a.options.Data, Home: a.options.Data, Offline: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(c.Close)
	for _, page := range []string{"overview", "history", "calls", "widget"} {
		b.Run(page, func(b *testing.B) {
			f := Object{"page": page, "range": "all", "pageSize": 50}
			method := "snapshot"
			if page == "calls" {
				f["page"], f["historyMode"] = "history", "calls"
			}
			if page == "widget" {
				method, f["now"] = "widget", 1791360000000.
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				value, err := c.Call(context.Background(), method, f)
				if err != nil {
					b.Fatal(err)
				}
				var result Object
				if raw, ok := any(value).(json.RawMessage); ok {
					if err := json.Unmarshal(raw, &result); err != nil {
						b.Fatal(err)
					}
				} else {
					result = obj(value)
				}
				normalizeLists(result)
				if len(result) == 0 {
					b.Fatal("empty snapshot")
				}
			}
		})
	}
}

func BenchmarkReviewChartSegments(b *testing.B) {
	points := make([]chartPoint, 20000)
	for i := range points {
		points[i] = chartPoint{Time: float64(i * 1000), Value: float64(i), Gap: i%1000 == 0}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := chartSegments(points, false); len(got) != 20 {
			b.Fatal(len(got))
		}
	}
}
