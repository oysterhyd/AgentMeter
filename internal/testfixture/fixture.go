// Package testfixture seeds synthetic statistics for native UI acceptance.
// It never reads personal Codex/Pi logs or opens an existing database.
package testfixture

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"local.codex.monitor/internal/monitor"
)

func Seed(dir string) error {
	file := filepath.Join(dir, "monitor.sqlite")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		return fmt.Errorf("fixture requires a new database: %s", file)
	}
	store, err := monitor.Open(file)
	if err != nil {
		return err
	}
	if err = store.Close(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exec := func(query string, args ...any) error { _, err := tx.Exec(query, args...); return err }
	stamp := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	now := time.Now()
	date := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
	accounts := []string{"native-account-a", "native-account-b", monitor.Unknown}
	for i, id := range accounts[:2] {
		if err = exec("INSERT INTO accounts VALUES(?,?,?)", id, fmt.Sprintf("验收账号 %c", 'A'+i), "manual"); err != nil {
			return err
		}
	}
	for key, value := range map[string]any{
		"currentAccount": accounts[0],
		"scan":           monitor.Object{"sourceExists": true, "errors": 0, "lastScan": stamp(now), "files": 60},
		"quotaStatus":    monitor.Object{"ok": true, "account": accounts[0], "attempted": stamp(now)},
	} {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err = exec("INSERT OR REPLACE INTO kv VALUES(?,?)", key, string(b)); err != nil {
			return err
		}
	}
	for i := 0; i < 9; i++ {
		origin, source := "Codex Desktop", "desktop"
		if i%3 == 1 {
			origin, source = "codex-tui", "cli"
		}
		if i%3 == 2 {
			origin, source = "codex_vscode", "vscode"
		}
		if err = exec("INSERT INTO sessions(id,project,origin,created,source) VALUES(?,?,?,?,?)", fmt.Sprintf("native-session-%d", i), fmt.Sprintf(`D:\验收项目\项目%d`, i%3), origin, stamp(date), source); err != nil {
			return err
		}
	}
	for i := 0; i < 155; i++ {
		started := date.AddDate(0, 0, -i%40).Add(time.Duration(i%7) * 15 * time.Minute)
		ended := started.Add(time.Minute)
		id, session := fmt.Sprintf("native-turn-%03d", i), fmt.Sprintf("native-session-%d", i%9)
		model := []string{"gpt-6-astra", "gpt-5.6-sol", "unpriced-native"}[i%3]
		var ttft any = 220
		if i%4 == 0 {
			ttft = nil
		}
		if err = exec("INSERT INTO turns VALUES(?,?,?,?,?,?,?,?,?,?)", id, session, model, stamp(started), stamp(ended), []string{"completed", "failed", "aborted"}[i%3], 60000, ttft, stamp(ended), accounts[i%3]); err != nil {
			return err
		}
		if err = exec("INSERT INTO usage VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", fmt.Sprintf("native-usage-%d", i), session, id, stamp(started), model, 10000+i, 5000+i, 1000+i, 200, 0, []string{"pi · openai", "逐次记录"}[i%2], accounts[i%3]); err != nil {
			return err
		}
		if i%9 != 0 {
			if err = exec("INSERT INTO request_events(id,ts,session,turn,model,kind,usage_id,ttft,source,association,account) VALUES(?,?,?,?,?,'completed',?,?,'otel','response_id',?)", fmt.Sprintf("native-timing-%d", i), stamp(started), session, id, model, fmt.Sprintf("native-usage-%d", i), 500+i*37, accounts[i%3]); err != nil {
				return err
			}
		}
		if i%40 == 0 {
			for n := 1; n <= 3; n++ {
				at := stamp(started.Add(time.Duration(n) * 10 * time.Second))
				usageID := fmt.Sprintf("native-response-%d-%d", i, n)
				if err = exec("INSERT INTO usage VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", usageID, session, id, at, model, 12000+n*8000, 9000+n*5000, 800+n*500, 200+n*100, 0, "逐次记录", accounts[i%3]); err != nil {
					return err
				}
				if err = exec("INSERT INTO request_events(id,ts,session,turn,model,kind,usage_id,ttft,source,association,account) VALUES(?,?,?,?,?,'completed',?,?,'otel','time_tokens',?)", "timing-"+usageID, at, session, id, model, usageID, 700+n*1400, accounts[i%3]); err != nil {
					return err
				}
			}
			for n, kind := range []string{"failed", "retry"} {
				if err = exec("INSERT INTO request_events(id,ts,session,turn,model,kind,attempt,http_status,error_code,source,account) VALUES(?,?,?,?,?,?,1,429,'rate_limit','otel',?)", fmt.Sprintf("native-event-%d-%d", i, n), stamp(started.Add(time.Duration(5+n)*time.Second)), session, id, model, kind, accounts[i%3]); err != nil {
					return err
				}
			}
		}
	}
	if err = exec("INSERT INTO prices(model,effective,input,cached,output,cache_write,source) VALUES(?,?,?,?,?,?,?)", "gpt-6-astra", stamp(date.AddDate(0, 0, -10)), 11, 1.1, 51, 13, "手动设置"); err != nil {
		return err
	}
	for a, account := range accounts[:2] {
		for i := 0; i < 15; i++ {
			reset := date.Unix() + 4800
			if i >= 8 {
				reset = date.Unix() + 24000
			}
			for slot, minutes := range map[string]int{"primary": 300, "secondary": 10080} {
				used := 20 + (i%8)*5 + a*7
				r := reset
				if slot == "secondary" {
					used = 32 + i + a*7
					r = date.Unix() + 604800
				}
				if err = exec("INSERT INTO quotas VALUES(?,?,?,?,?,?,?,?,?,?)", fmt.Sprintf("fixture-%d-%d-%s", a, i, slot), stamp(date.Add(time.Duration(i)*10*time.Minute)), "codex", slot, used, minutes, r, "plus", "在线查询", account); err != nil {
					return err
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	service, err := monitor.Start(monitor.Config{Data: dir, Home: filepath.Join(dir, "source"), Offline: true, Version: "2.6.2"})
	if err != nil {
		return err
	}
	defer service.Close()
	for name, method := range map[string]string{"snapshot.json": "snapshot", "widget.json": "widget"} {
		b, err := service.Call(context.Background(), method, monitor.Object{"page": "all", "range": "all", "pageSize": 50})
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			return err
		}
	}
	return nil
}
