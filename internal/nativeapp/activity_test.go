package nativeapp

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func activityFixture() Object {
	var days, hours []Object
	for i := 0; i < 16; i++ {
		days = append(days, Object{"date": fmt.Sprintf("2026-10-%02d", i+1), "total": float64((i + 1) * 10000), "requests": float64(i + 1), "sessions": 2.})
	}
	for i := 0; i < 24; i++ {
		hours = append(hours, Object{"hour": i, "total": float64((i + 1) * 1000), "requests": float64(i + 1)})
	}
	return Object{"year": 2026, "firstYear": 2026, "today": "2026-10-16", "days": days, "hours": hours, "stats": Object{"sessions": 32., "peak": days[15]}}
}

func captureActivity(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if out := os.Getenv("MONITOR_NATIVE_CAPTURE"); out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(out, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecentActivityContainsAllEightDays(t *testing.T) {
	for _, cfg := range []struct {
		width int
		scale float32
	}{{980, 1}, {740, 1}, {980, 1.5}} {
		t.Run(fmt.Sprintf("%d-%.1f", cfg.width, cfg.scale), func(t *testing.T) {
			a := fixtureApp(t)
			activity := activityFixture()
			tt := ui.NewTester(func(c *ui.Context) {
				a.skin(c)
				ui.Column(c).Padding(20).Gap(14).Children(func() {
					a.activityDetails(c, activity)
					ui.Text(c, "footer")
				})
			}, cfg.width, 1200)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: cfg.scale})
			card, ok := tt.Find("最近活动日卡片")
			if !ok {
				t.Fatal("missing recent activity card")
			}
			lastY := float32(-1)
			for i := 16; i >= 9; i-- {
				row, ok := tt.Find(fmt.Sprintf("2026-10-%02d", i))
				if !ok || row.X < card.X+19 || row.X+row.W > card.X+card.W-19 || row.Y < lastY || row.Y+row.H > card.Y+card.H-19 {
					t.Fatalf("day %d outside padded card or overlapping: row=%+v card=%+v", i, row, card)
				}
				lastY = row.Y + row.H
			}
			if _, ok := tt.Find("2026-10-08"); ok {
				t.Fatal("more than eight recent days")
			}
			footer, _ := tt.Find("footer")
			if footer.Y < card.Y+card.H {
				t.Fatal("recent activity overlaps footer")
			}
			captureActivity(t, tt, fmt.Sprintf("activity-details-%d-%.1f", cfg.width, cfg.scale))
			if err := tt.Click("2026-10-09"); err != nil || a.page != 2 || a.filter["start"] != "2026-10-09" || a.filter["end"] != "2026-10-09" {
				t.Fatal("last row cannot open its records", err)
			}
		})
	}
}

func TestActivityHoursFollowPointerAndKeyboard(t *testing.T) {
	a := fixtureApp(t)
	activity := activityFixture()
	tt := ui.NewTester(func(c *ui.Context) {
		a.skin(c)
		ui.Column(c).Padding(20).Children(func() { a.activityHours(c, activity) })
	}, 640, 320)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	r, _ := tt.Find("活跃时段柱状图")
	before := bytes.Clone(tt.Image().Pix)
	tt.Move(r.X+r.W*3.5/24, r.Y+40)
	if !tt.HasText("03:00–04:00 · 4K tokens") || !tt.HasText("4 用量记录") || bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("hour 3 hover feedback missing", tt.Texts())
	}
	captureActivity(t, tt, "activity-hours-hover")
	tt.Move(r.X+r.W*18.5/24, r.Y+40)
	if !tt.HasText("18:00–19:00 · 19K tokens") {
		t.Fatal("hour selection did not follow pointer", tt.Texts())
	}
	tt.ClickAt(r.X+r.W*18.5/24, r.Y+40)
	tt.Move(2, 2)
	tt.Key(0, ui.KeyRight)
	if !tt.HasText("19:00–20:00 · 20K tokens") {
		t.Fatal("keyboard hour selection", tt.Texts())
	}
	tt.Key(0, ui.KeyEnd)
	if !tt.HasText("23:00–24:00") {
		t.Fatal("last hour")
	}
	tt.Key(0, ui.KeyHome)
	if !tt.HasText("00:00–01:00") {
		t.Fatal("first hour")
	}
	activity["hours"] = []Object{}
	tt.Frame()
	if !tt.HasText("00:00–01:00 · 0 tokens") {
		t.Fatal("empty hours retained stale values")
	}
}

func TestCalendarPointerKeepsHitTargetsAndFutureDatesDisabled(t *testing.T) {
	a := fixtureApp(t)
	a.data["activity"] = activityFixture()
	tt := ui.NewTester(func(c *ui.Context) { a.skin(c); a.activity(c, true) }, 1000, 400)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	label := "2026-10-15 · 150,000 tokens"
	r, ok := tt.Find(label)
	if !ok {
		t.Fatal("calendar day missing", tt.Texts())
	}
	before := bytes.Clone(tt.Image().Pix)
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	if bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("calendar has no pointer response")
	}
	if next, _ := tt.Find(label); next != r {
		t.Fatal("calendar hover changed hit target")
	}
	captureActivity(t, tt, "activity-calendar-hover")
	if err := tt.Click(label); err != nil || a.selectedDay != "2026-10-15" {
		t.Fatal("calendar selection", err)
	}
	if err := tt.Click("2026-10-17 · 0 tokens"); err != nil || a.selectedDay != "2026-10-15" {
		t.Fatal("future day selectable", err)
	}
	tt.Move(0, 390)
	tt.SetDark(true)
	if !tt.HasText("2026-10-15") {
		t.Fatal("dark calendar lost selection")
	}
}

func TestActivityPaintMotionRetargetsAndReducesMotion(t *testing.T) {
	m := widgetMotion{target: 0}
	var shown float32
	target := float32(1)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(30, 30).Draw(func(p *ui.Painter, r ui.Rect) {
			shown = paintMotion(c, p, &m, target, motionFill)
		})
	}, 50, 50)
	if shown >= 1 || !m.active(time.Now()) {
		t.Fatal("entrance skipped its intermediate state")
	}
	previous := m.value(time.Now())
	target = 0
	tt.Frame()
	if m.from < previous || m.from >= 1 || m.target != 0 {
		t.Fatal("reversal did not preserve current value", m)
	}
	target = 1
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if shown != 1 || m.active(time.Now()) {
		t.Fatal("reduced motion did not settle")
	}
}
