package nativeapp

import (
	"context"
	"encoding/json"
	"image/png"
	"local.codex.monitor/internal/monitor"
	"local.codex.monitor/internal/testfixture"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func fixtureApp(t testing.TB) *App {
	t.Helper()
	file := os.Getenv("MONITOR_NATIVE_FIXTURE")
	if file == "" {
		file = t.TempDir()
		if err := testfixture.Seed(file); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(file, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.options.Data = file
	a.filter["range"] = "all"
	if logo, err := os.ReadFile(filepath.Join("..", "..", "assets", "monitor-glass.png")); err == nil {
		a.brand, _ = ui.DecodeBitmap(logo)
	}
	if err = json.Unmarshal(b, &a.data); err != nil {
		t.Fatal(err)
	}
	normalizeLists(a.data)
	for p, d := range map[string]*map[string]string{"translations.json": &a.translations, "system-translations.json": &a.systemTranslations} {
		b, err := os.ReadFile(filepath.Join("..", "..", "assets", "locales", p))
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(b, d)
	}
	return a
}
func TestNativePagesAndKeyboard(t *testing.T) {
	a := fixtureApp(t)
	tt := ui.NewTester(a.View, 1380, 960)
	if !tt.HasText("Token 消耗") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("历史分析"); err != nil {
		t.Fatal(err)
	}
	if a.page != 2 || !tt.HasText("任务运行记录") {
		t.Fatal("history navigation")
	}
	tt.Key(ui.Alt, ui.Key2)
	if a.page != 1 || !tt.HasText("活动日历") {
		t.Fatal("activity shortcut")
	}
	tt.Key(ui.Cmd, ui.KeyK)
	if !a.commandOpen || !tt.HasText("命令面板") {
		t.Fatal("command palette")
	}
	tt.Key(0, ui.KeyEscape)
	if a.commandOpen {
		t.Fatal("modal Escape")
	}
	tt.SetSize(980, 700)
	tt.SetScale(1.5)
	if tt.Image().Bounds().Dx() != 1470 {
		t.Fatal("DPI")
	}
	tt.SetDark(true)
	if !tt.HasText("活动日历") {
		t.Fatal("dark rendering")
	}
}
func TestNativePageSnapshots(t *testing.T) {
	a := fixtureApp(t)
	out := os.Getenv("MONITOR_NATIVE_CAPTURE")
	for i, id := range pageIDs {
		a.page = i
		tt := ui.NewTester(a.View, 1380, 960)
		if !tt.HasText(pageNames[i]) {
			t.Fatalf("missing %s", id)
		}
		if out != "" {
			if err := os.MkdirAll(out, 0700); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(out, id+"-headless.png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	a.page = 4
	for i := 0; i < 4; i++ {
		a.settingsSection = i
		tt := ui.NewTester(a.View, 1380, 960)
		if len(tt.Texts()) == 0 {
			t.Fatal("settings section")
		}
	}
}
func TestGoTransportRetainsUnknownAndNumbers(t *testing.T) {
	a := fixtureApp(t)
	profile := a.options.Data
	client, err := startNativeClient(monitor.Config{Data: profile, Home: profile, Offline: true, Version: "2.4.2"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := client.CallJSON(ctx, "snapshot", Object{"page": "all", "range": "all", "pageSize": 50})
	if err != nil {
		t.Fatal(err)
	}
	var actual Object
	if err = json.Unmarshal(b, &actual); err != nil {
		t.Fatal(err)
	}
	expected := obj(a.data["sums"])
	for _, key := range []string{"total", "input", "cached", "output", "requests", "unpriced", "cost", "saved", "cacheRate"} {
		got := obj(actual["sums"])[key]
		if got != expected[key] {
			t.Fatalf("%s: %v != %v", key, got, expected[key])
		}
	}
}
func TestQuotaSegmentsBreakAtResetAndGap(t *testing.T) {
	points := []chartPoint{{Time: 1000, Value: 80, Reset: 3}, {Time: 2000, Value: 60, Reset: 3}, {Time: 4000, Value: 100, Reset: 9}, {Time: 5000, Value: 90, Reset: 9}, {Time: 6000, Value: 85, Reset: 9, Gap: true}}
	segments := chartSegments(points, true)
	if len(segments) != 3 || len(segments[0]) != 2 || segments[1][0].Value != 100 {
		t.Fatal(segments)
	}
}
