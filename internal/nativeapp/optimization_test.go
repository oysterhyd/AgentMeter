package nativeapp

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestNormalizedListsPreserveNestedDataAndJSON(t *testing.T) {
	var data Object
	raw := []byte(`{"rows":[{"name":"a","models":[{"name":"b","cost":null}]}],"names":["x","y"],"mixed":["x",4],"empty":[]}`)
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(data)
	normalizeLists(data)
	after, _ := json.Marshal(data)
	if !bytes.Equal(before, after) {
		t.Fatalf("%s != %s", before, after)
	}
	rows := objects(data["rows"])
	if len(rows) != 1 || objects(rows[0]["models"])[0]["cost"] != nil || len(stringsOf(data["names"])) != 2 {
		t.Fatal(data)
	}
}

func TestBreakdownCacheInvalidatesOnSnapshotSearchAndSort(t *testing.T) {
	a := NewApp()
	a.data["models"] = []Object{{"name": "b", "total": 20., "cost": 1.}, {"name": "a", "total": 10., "cost": 2.}}
	if got := a.breakdownRows("models"); got[0]["name"] != "b" {
		t.Fatal(got)
	}
	a.breakdownSort = ui.SortOrder{Column: "name"}
	if got := a.breakdownRows("models"); got[0]["name"] != "a" {
		t.Fatal(got)
	}
	a.breakdownSearch = "B"
	if got := a.breakdownRows("models"); len(got) != 1 || got[0]["name"] != "b" {
		t.Fatal(got)
	}
	a.data["models"] = []Object{{"name": "replacement", "total": 30.}}
	a.snapshotRevision++
	if got := a.breakdownRows("models"); len(got) != 0 {
		t.Fatal(got)
	}
	a.breakdownSearch = ""
	if got := a.breakdownRows("models"); got[0]["name"] != "replacement" {
		t.Fatal(got)
	}
}

func TestWidgetAnimatedMetricSettlesToFreshRender(t *testing.T) {
	m, p := fixtureWidget(t)
	p.model = &m
	now := time.Now()
	p.render(now, 1)
	m.ReduceMotion = false
	m.hoverMotion[0] = widgetMotion{from: 1, target: 0, start: now, duration: 350 * time.Millisecond}
	p.render(now.Add(300*time.Millisecond), 1)
	settled := bytes.Clone(p.render(now.Add(time.Second), 1).Pix)
	p.invalidate()
	if fresh := p.render(now.Add(time.Second), 1); !bytes.Equal(settled, fresh.Pix) {
		t.Fatal("final metric frame was skipped")
	}
}

func TestSettingsShowsVersionAndUpdateControl(t *testing.T) {
	a := fixtureApp(t)
	a.page = 4
	tt := ui.NewTester(a.View, 1380, 960)
	if !tt.HasText("软件更新") || !tt.HasText("检查更新") || !tt.HasText("自动检查更新") || !tt.HasText("自动下载更新") || !tt.HasText("当前版本 "+appVersion) {
		t.Fatal(tt.Texts())
	}
}
