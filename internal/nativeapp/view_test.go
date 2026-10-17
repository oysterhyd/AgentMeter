package nativeapp

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"local.codex.monitor/internal/monitor"
	"local.codex.monitor/internal/testfixture"

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
	if !a.commandOpen || !tt.HasText("页面") {
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

func TestSidebarTransparencyPreference(t *testing.T) {
	a := fixtureApp(t)
	tt := ui.NewTester(a.View, 980, 700)
	tt.SetVibrancy(true)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		for _, reduced := range []bool{false, true} {
			a.reduceTransparency = reduced
			tt.Frame()
			_, _, _, alpha := tt.Image().At(4, 400).RGBA()
			if (alpha == 65535) != reduced {
				t.Fatalf("dark=%v reduceTransparency=%v: sidebar alpha=%d", dark, reduced, alpha)
			}
		}
	}
}

func TestRedesignDPIThemesAndSettings(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1, 1.5, 2} {
			for page := range pageIDs {
				t.Run(fmt.Sprintf("%s/dark=%v/scale=%.1f", pageIDs[page], dark, scale), func(t *testing.T) {
					a := fixtureApp(t)
					a.page = page
					tt := ui.NewTester(a.View, 980, 700)
					tt.SetPreferences(ui.Preferences{ReduceMotion: true})
					tt.SetTitleBar(ui.TitleBar{Height: 32, Right: 138})
					tt.SetScale(scale)
					tt.SetDark(dark)
					if a.pal.dark != dark || !a.reduceMotion || !tt.HasText(pageNames[page]) {
						t.Fatal("appearance, reduced motion or page missing")
					}
					if got := tt.Image().Bounds(); got.Dx() != int(980*scale) || got.Dy() != int(700*scale) {
						t.Fatalf("DPI raster: %v", got)
					}
					for _, label := range []string{"刷新数据与额度", "导出 CSV"} {
						if page == 4 && label == "导出 CSV" {
							continue
						}
						r, ok := tt.Find(label)
						if !ok || r.X < sidebarWidth || r.X+r.W > 980-contentInset || r.Y < 32 {
							t.Fatalf("toolbar overlaps window controls: %s %+v", label, r)
						}
					}
					if page == 2 {
						if err := tt.Click("逐次调用"); err != nil || a.historyMode != 1 || !tt.HasText("首字") {
							t.Fatal("call view", err)
						}
					}
					if page == 4 {
						for i, section := range []string{"通用", "账号", "模型价格", "数据与存储"} {
							if err := tt.Click(section); err != nil || a.settingsSection != i {
								t.Fatal("settings navigation", err)
							}
							if !tt.HasText([]string{"软件更新", "账号管理", "新增价格版本", "采集 Claude Code 用量"}[i]) {
								t.Fatal(tt.Texts())
							}
						}
					}
					if out := os.Getenv("MONITOR_NATIVE_CAPTURE"); out != "" {
						if err := os.MkdirAll(out, 0700); err != nil {
							t.Fatal(err)
						}
						f, err := os.Create(filepath.Join(out, fmt.Sprintf("redesign-%s-%v-%.1f.png", pageIDs[page], dark, scale)))
						if err != nil {
							t.Fatal(err)
						}
						err = png.Encode(f, tt.Image())
						_ = f.Close()
						if err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
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

// BenchmarkViewFrame measures one settled frame — build, layout and paint —
// of each page at 1280×860 once entrance animations have finished.
func BenchmarkViewFrame(b *testing.B) {
	for i, id := range pageIDs {
		b.Run(id, func(b *testing.B) {
			a := fixtureApp(b)
			a.navigate(i)
			tt := ui.NewTester(a.View, 1280, 860)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			for range 30 {
				tt.Frame()
			}
			time.Sleep(1500 * time.Millisecond)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tt.Frame()
			}
		})
	}
}

func TestOpenAIModelMark(t *testing.T) {
	for model, want := range map[string]bool{
		"gpt-5.5": true, "GPT-4o": true, "gpt-5-codex": true, "codex-mini-latest": true, "o3": true, "o4-mini": true,
		"openai/gpt-5": true, "chatgpt-4o-latest": true, "claude-sonnet-4": false, "unpriced-native": false, "omni": false, "": false,
	} {
		if openAIModel(model) != want {
			t.Errorf("openAIModel(%q) = %v", model, !want)
		}
	}
}
