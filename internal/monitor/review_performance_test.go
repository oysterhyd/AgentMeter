package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reviewStore(b *testing.B) (*Store, time.Time) {
	b.Helper()
	s, now := benchmarkStore(b)
	if err := s.exec("UPDATE usage SET model='gpt-5.5',kind='逐次记录',turn='turn-'||session"); err != nil {
		b.Fatal(err)
	}
	if err := s.exec(`INSERT INTO turns(id,session,model,started,ended,status,duration,account)
		SELECT 'turn-'||id,id,'gpt-5.5',?,?,'completed',1000,'unassigned' FROM sessions`, iso(now.Add(-6*time.Hour)), iso(now)); err != nil {
		b.Fatal(err)
	}
	if err := s.exec(`INSERT INTO request_events(id,ts,session,turn,model,kind,usage_id,ttft,source,association)
		SELECT 'timing-'||id,ts,session,turn,model,'completed',id,(rowid%997)*11,'otel','response_id' FROM usage`); err != nil {
		b.Fatal(err)
	}
	return s, now
}

// All scenes use the same 20,000 priced calls and 100 completed tasks.
func BenchmarkReview(b *testing.B) {
	s, now := reviewStore(b)
	for _, page := range []string{"overview", "activity", "history", "calls", "quota", "settings", "widget", "export"} {
		b.Run(page, func(b *testing.B) {
			f := Object{"page": page, "range": "all", "pageSize": 50}
			if page == "calls" {
				f["page"], f["historyMode"] = "history", "calls"
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				switch page {
				case "widget":
					s.Widget(now)
				case "export":
					if _, err := s.Export(f, now); err != nil {
						b.Fatal(err)
					}
				default:
					if _, err := s.Snapshot(f, now); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
	if target := os.Getenv("MONITOR_BENCH_PROFILE"); target != "" {
		file := text(s.mustOne("PRAGMA database_list")["file"])
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		if err := os.MkdirAll(target, 0700); err != nil {
			b.Fatal(err)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "monitor.sqlite"), data, 0600); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReviewCallOrdering(b *testing.B) {
	rows := make([]Object, 20000)
	for i := range rows {
		rows[i] = Object{"id": fmt.Sprintf("usage:%05d", i), "ts": fmt.Sprintf("%08d", i/4), "status": "usage", "total": float64(i % 1000), "cost": float64(i%23) / 10, "ttft": float64(i % 19)}
	}
	for _, order := range []string{"newest", "oldest", "tokens", "cost", "ttft"} {
		b.Run(order, func(b *testing.B) {
			f := Object{"callOrder": order}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				filterCalls(rows, f)
			}
		})
	}
}

func BenchmarkReviewScan(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "monitor.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	profile := b.TempDir()
	dir := filepath.Join(profile, "sessions")
	if err = os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, "fixture.jsonl"))
	if err != nil {
		b.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	encoder.Encode(Object{"type": "session_meta", "timestamp": iso(now.Add(-time.Hour)), "payload": Object{"id": "session", "originator": "Codex Desktop", "cwd": "fixture"}})
	encoder.Encode(Object{"type": "turn_context", "timestamp": iso(now), "payload": Object{"model": "gpt-5.5"}})
	for i := 0; i < 1000; i++ {
		encoder.Encode(Object{"type": "token_usage_record", "timestamp": iso(now.Add(time.Duration(i) * time.Second)), "payload": Object{"response_id": fmt.Sprint(i), "usage": Object{"input_tokens": 1000, "cached_input_tokens": 400, "output_tokens": 200}}})
	}
	file.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err = s.exec("DELETE FROM files; DELETE FROM usage"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err = s.Scan(context.Background(), profile, "", "", true, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReviewRecovery(b *testing.B) {
	s, _ := reviewStore(b)
	file := text(s.mustOne("PRAGMA database_list")["file"])
	if err := s.Close(); err != nil {
		b.Fatal(err)
	}
	target := filepath.Join(b.TempDir(), "recovered.sqlite")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := RecoverDatabase(file, target); err != nil {
			b.Fatal(err)
		}
	}
}
