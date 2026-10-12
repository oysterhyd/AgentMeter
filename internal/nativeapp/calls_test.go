package nativeapp

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"local.codex.monitor/internal/monitor"
)

func captureCallView(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	out := os.Getenv("MONITOR_NATIVE_CAPTURE")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(out, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCallsCompactRowsAndKeyboardDisclosure(t *testing.T) {
	a := fixtureApp(t)
	a.page = 2
	a.historyMode = 1
	tt := ui.NewTester(a.View, 1380, 960)
	for _, label := range []string{"逐次调用明细", "首字时间", "输入 / 输出", "等值估算"} {
		if !tt.HasText(label) {
			t.Fatal(label, tt.Texts())
		}
	}
	rows := objects(obj(a.data["calls"])["rows"])
	if len(rows) == 0 {
		t.Fatal("fixture has no calls")
	}
	id := str(rows[0]["id"])
	if err := tt.Click(id); err != nil {
		t.Fatal(err)
	}
	if a.expandedCall != id || !tt.HasText("调用详情") {
		t.Fatal("call disclosure")
	}
	captureCallView(t, tt, "calls-expanded-headless")
	tt.Key(0, ui.KeyEnter)
	if a.expandedCall != "" {
		t.Fatal("Enter did not collapse focused call")
	}
	captureCallView(t, tt, "calls-headless")
	a.callState = ui.ListState{} // A new headless engine owns its own ListState frame bookkeeping.
	captureCallView(t, ui.NewTester(a.View, 980, 820), "calls-compact-headless")
	if a.ttft(nil) != "未知" || a.ttft(0) != "0.00 秒" {
		t.Fatal("unknown timing was rendered as zero")
	}
	obj(a.data["settings"])["language"] = "en"
	a.callState = ui.ListState{}
	english := ui.NewTester(a.View, 1380, 960)
	if !english.HasText("Individual call details") || !english.HasText("TTFT") {
		t.Fatal("call analysis localization")
	}
	captureCallView(t, english, "calls-english-headless")
}

func TestNativeTaskCallsShowWholeTaskSharesAndTiming(t *testing.T) {
	a := fixtureApp(t)
	client, err := startNativeClient(monitor.Config{Data: a.options.Data, Home: a.options.Data, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	a.data, err = client.Object(context.Background(), "snapshot", Object{"page": "history", "range": "all", "detailSession": "native-session-4", "detailTurn": "native-turn-040"})
	if err != nil {
		t.Fatal(err)
	}
	d := obj(a.data["callDetails"])
	if len(objects(d["rows"])) != 6 {
		t.Fatal(d)
	}
	a.expandedCall = str(objects(d["rows"])[0]["id"])
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Padding(22).Gap(16).Children(func() { a.taskCalls(c, Object{"id": "native-turn-040", "session": "native-session-4"}) })
	}, 1380, 960)
	for _, label := range []string{"任务内调用分析", "任务 Token 占比", "调用详情"} {
		if !tt.HasText(label) {
			t.Fatal(label, tt.Texts())
		}
	}
	captureCallView(t, tt, "task-calls-headless")
}
