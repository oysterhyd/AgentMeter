package monitor

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotCacheKeepsEveryRangeAndFilterEquivalent(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	for i, days := range []int{0, 3, 15, 40} {
		if err := s.exec("INSERT INTO sessions(id,project,origin,source) VALUES(?,?,?,'vscode')", fmt.Sprint(i), fmt.Sprint(i%2), "codex_vscode"); err != nil {
			t.Fatal(err)
		}
		if err := s.exec("INSERT INTO usage(id,session,ts,model,input,cached,output,account) VALUES(?,?,?,?,1000,400,200,?)", fmt.Sprint(i), fmt.Sprint(i), iso(now.AddDate(0, 0, -days)), "gpt-5.5", fmt.Sprint(i%2)); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range []string{"overview", "activity", "history", "quota", "settings", "all"} {
		for _, r := range []string{"today", "7d", "30d", "all", "custom"} {
			for _, scope := range []Object{{}, {"model": "gpt-5.5"}, {"project": "1"}, {"account": "1"}, {"session": "2"}, {"historyMode": "calls"}} {
				f := clone(scope)
				f["range"], f["page"], f["start"], f["end"] = r, page, "2026-09-01", "2026-10-07"
				start, end, _ := Bounds(f, now)
				cold, err := s.Snapshot(f, now)
				if err != nil {
					t.Fatal(err)
				}
				warm, err := s.Snapshot(f, now)
				if err != nil {
					t.Fatal(err)
				}
				// Recompute all queries and the calendar independently of caches.
				s.snapshotCache.calendars = nil
				fresh, err := s.snapshot(s.resolve(f), start, end, now)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(cold, fresh) || !reflect.DeepEqual(warm, fresh) {
					t.Fatalf("cached result differs: %s/%s/%v", page, r, scope)
				}
			}
		}
	}
}

func TestSnapshotCacheDetachedInvalidatedAndTimeSensitive(t *testing.T) {
	s := testStore(t)
	now := time.Date(2026, 10, 7, 23, 59, 0, 0, time.Local)
	if err := s.exec("INSERT INTO sessions(id,project) VALUES('s','project'); INSERT INTO turns(id,session,status,last_seen,started) VALUES('t','s','running',?,?); INSERT INTO usage(id,session,ts,model,input,output) VALUES('u','s',?,'gpt-5.5',1000,200)", iso(now.Add(-110*time.Second)), iso(now), iso(now)); err != nil {
		t.Fatal(err)
	}
	f := Object{"page": "overview", "range": "today"}
	read := func(at time.Time) Object {
		t.Helper()
		v, err := s.Snapshot(f, at)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := read(now)
	obj(first["sums"])["total"] = -1
	first["models"].([]Object)[0]["total"] = -1
	obj(first["activity"])["days"].([]Object)[0]["total"] = -1
	if next := read(now); num(obj(next["sums"])["total"]) != 1200 || num(next["models"].([]Object)[0]["total"]) != 1200 || num(obj(next["activity"])["days"].([]Object)[0]["total"]) != 1200 {
		t.Fatal("caller mutated cached data")
	}
	if num(obj(read(now)["performance"])["active"]) != 1 || num(obj(read(now.Add(30 * time.Second))["performance"])["active"]) != 0 {
		t.Fatal("active task did not age out")
	}
	if num(obj(read(now.Add(3 * time.Minute))["sums"])["total"]) != 0 {
		t.Fatal("today cache survived midnight")
	}
	// Direct connection writes must invalidate, including prepared statements.
	stmt, err := s.db.Prepare("UPDATE usage SET input=2000 WHERE id='u'")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stmt.Exec(); err != nil {
		t.Fatal(err)
	}
	stmt.Close()
	if num(obj(read(now)["sums"])["total"]) != 2200 {
		t.Fatal("prepared write did not invalidate")
	}
	file := text(s.mustOne("PRAGMA database_list")["file"])
	external, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = external.Exec("UPDATE usage SET input=3000 WHERE id='u'"); err != nil {
		t.Fatal(err)
	}
	external.Close()
	if num(obj(read(now)["sums"])["total"]) != 3200 {
		t.Fatal("external commit did not invalidate")
	}
	for i := 0; i < snapshotCacheLimit+4; i++ {
		next := clone(f)
		next["model"] = fmt.Sprint(i)
		if _, err := s.Snapshot(next, now); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.snapshotCache.pages) > snapshotCacheLimit || len(s.snapshotCache.calendars) > snapshotCacheLimit {
		t.Fatal("unbounded cache")
	}
}

func BenchmarkRangeSwitch(b *testing.B) {
	s, now := reviewStore(b)
	for _, page := range []string{"overview", "today", "history", "calls"} {
		for _, mode := range []string{"cold", "shared-calendar", "warm"} {
			b.Run(page+"/"+mode, func(b *testing.B) {
				f := Object{"page": page, "range": "all", "pageSize": 50}
				if page == "today" {
					f["page"], f["range"] = "overview", "today"
				}
				if page == "calls" {
					f["page"], f["historyMode"] = "history", "calls"
				}
				if _, err := s.Snapshot(f, now); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if mode != "warm" {
						s.snapshotCache.pages = nil
					}
					if mode == "cold" {
						s.snapshotCache.calendars = nil
					}
					if _, err := s.Snapshot(f, now); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
