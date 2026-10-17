package monitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIIdentitiesModernAndLegacyUsage(t *testing.T) {
	for _, meta := range []Object{
		{"originator": "Codex CLI"}, {"originator": "codex_cli_rs"},
		{"originator": "codex-tui"}, {"originator": "codex_exec"},
		{"source": "cli"}, {"source": "exec"},
		{"originator": "monocode", "source": "vscode"},
		{"originator": "monocode-text", "source": "vscode"},
		{"originator": "codex_cli_rs", "source": "vscode"},
	} {
		t.Run(jsonText(meta), func(t *testing.T) {
			s := testStore(t)
			at := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
			state := Object{}
			process := func(kind string, p Object) {
				t.Helper()
				o := Object{"type": kind, "timestamp": iso(at), "payload": p}
				if err := s.process(o, state, []byte(jsonText(o))); err != nil {
					t.Fatal(err)
				}
				at = at.Add(time.Second)
			}
			meta = clone(meta)
			meta["id"], meta["cwd"] = "cli", "project"
			process("session_meta", meta)
			process("event_msg", Object{"type": "task_started", "turn_id": "turn"})
			process("turn_context", Object{"model": "gpt-5.5", "turn_id": "turn"})
			usage := func(input, output float64) Object {
				return Object{"input_tokens": input, "cached_input_tokens": input / 2, "output_tokens": output, "total_tokens": input + output}
			}
			for _, u := range []Object{usage(100, 20), usage(100, 20), usage(180, 40)} {
				process("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": u}})
			}
			// A compaction reset contributes the last response, not a negative delta.
			process("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": usage(40, 10), "last_token_usage": usage(40, 10)}})
			for i := 0; i < 2; i++ {
				process("token_usage_record", Object{"response_id": "response", "thread_id": "cli", "usage": usage(200, 50)})
			}
			process("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": usage(900, 900)}})
			process("event_msg", Object{"type": "task_complete", "turn_id": "turn", "duration_ms": 1000.})
			row := s.mustOne("SELECT COUNT(*) n,SUM(input) input,SUM(output) output FROM usage")
			if num(row["n"]) != 4 || num(row["input"]) != 420 || num(row["output"]) != 100 {
				t.Fatal(row)
			}
			if s.mustOne("SELECT source FROM sessions WHERE id='cli'")["source"] != "cli" {
				t.Fatal("missing CLI source")
			}
			snapshot, err := s.Snapshot(Object{"range": "all"}, at)
			if err != nil {
				t.Fatal(err)
			}
			if num(obj(snapshot["sums"])["total"]) != 520 || snapshot["turns"].([]Object)[0]["source"] != "cli" {
				t.Fatal(snapshot["sums"], snapshot["turns"])
			}
			csv, err := s.Export(Object{"range": "all"}, at)
			if err != nil || !strings.Contains(csv, "source,origin") || !strings.Contains(csv, `"cli"`) {
				t.Fatal(csv, err)
			}
		})
	}
}

func cliLog(id, origin, created string, stamps ...string) []byte {
	meta := Object{"id": id, "originator": origin, "cwd": "project"}
	if origin == "monocode" || origin == "monocode-text" || origin == "codex_vscode" {
		meta["source"] = "vscode"
	}
	lines := []string{jsonText(Object{"type": "session_meta", "timestamp": created, "payload": meta})}
	for i, ts := range stamps {
		lines = append(lines, jsonText(Object{"type": "event_msg", "timestamp": ts, "payload": Object{"type": "token_count", "info": Object{"total_token_usage": Object{"input_tokens": (i + 1) * 100, "cached_input_tokens": (i + 1) * 50, "output_tokens": (i + 1) * 20, "total_tokens": (i + 1) * 120}}}}))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestUpgradeReplaysIgnoredCLIAndPreservesDesktopAndClearBoundary(t *testing.T) {
	s := testStore(t)
	home := t.TempDir()
	dir := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	created := "2026-10-07T00:00:00.000Z"
	stamps := []string{"2026-10-07T00:00:01.000Z", "2026-10-07T00:00:02.000Z", "2026-10-07T00:00:03.000Z"}
	if err := s.set("settings", Object{"clearedAt": stamps[1]}); err != nil {
		t.Fatal(err)
	}
	origins := []string{"Codex Desktop", "codex_cli_rs", "codex_exec", "monocode", "monocode-text", "codex_vscode"}
	for _, origin := range origins {
		file := filepath.Join(dir, origin+".jsonl")
		data := cliLog(origin, origin, created, stamps...)
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		if origin == "Codex Desktop" {
			state := Object{}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var o Object
				if err := json.Unmarshal([]byte(line), &o); err != nil {
					t.Fatal(err)
				}
				if err := s.process(o, state, []byte(line)); err != nil {
					t.Fatal(err)
				}
			}
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		// v2.6.2 and v2.7.0 consumed these complete, unchanged logs.
		version := 0
		if strings.HasPrefix(origin, "monocode") {
			version = 1
		}
		if origin == "codex_vscode" {
			version = 2
		}
		if err := s.exec("INSERT INTO files(path,offset,state,mtime,parser_version) VALUES(?,?,?,?,?)", file, len(data), jsonText(Object{"session": origin, "desktop": origin == "Codex Desktop", "source": ""}), float64(info.ModTime().UnixNano())/1e6, version); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Scan(context.Background(), home, "", "", true, nil); err != nil {
			t.Fatal(err)
		}
		row := s.mustOne("SELECT COUNT(*) n,SUM(input) input,SUM(output) output,MIN(ts) first FROM usage")
		if num(row["n"]) != float64(len(origins)) || num(row["input"]) != float64(len(origins)*100) || num(row["output"]) != float64(len(origins)*20) || row["first"] != stamps[2] {
			t.Fatal("replay lost or duplicated usage", row)
		}
	}
	if num(s.mustOne("SELECT COUNT(*) n FROM files WHERE parser_version=?", codexParserVersion)["n"]) != float64(len(origins)) {
		t.Fatal("parser version not committed")
	}
	// Incremental appends work after replay, including a formerly ignored file.
	file := filepath.Join(dir, "codex_exec.jsonl")
	appendData := strings.Split(string(cliLog("unused", "", created, append(stamps, "2026-10-07T00:00:04.000Z")...)), "\n")[4] + "\n"
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(appendData); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err = s.Scan(context.Background(), home, "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	if num(s.mustOne("SELECT SUM(input) n FROM usage")["n"]) != float64((len(origins)+1)*100) {
		t.Fatal("append missed")
	}
	if num(s.Widget(parse("2026-10-07T00:00:05Z"))["total"]) != float64((len(origins)+1)*120) {
		t.Fatal("widget missed CLI")
	}
}

func TestCLIForkHistoryOnlySetsCumulativeBaseline(t *testing.T) {
	s := testStore(t)
	state := Object{}
	data := cliLog("fork", "codex_cli_rs", "2026-10-07T00:00:02Z", "2026-10-07T00:00:01Z", "2026-10-07T00:00:03Z")
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var o Object
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			t.Fatal(err)
		}
		if err := s.process(o, state, []byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if row := s.mustOne("SELECT COUNT(*) n,SUM(input) input FROM usage"); num(row["n"]) != 1 || num(row["input"]) != 100 {
		t.Fatal(row)
	}
}

func TestCLIScanFailureRollsBackAndRetriesAfterReopen(t *testing.T) {
	home, profile := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "cli.jsonl")
	data := cliLog("cli", "codex-tui", "2026-10-07T00:00:00Z", "2026-10-07T00:00:01Z", "2026-10-07T00:00:02Z")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(profile, "monitor.sqlite")
	s, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.exec(`CREATE TRIGGER fail_usage BEFORE INSERT ON usage WHEN NEW.ts='2026-10-07T00:00:02.000Z' BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	status, err := s.Scan(context.Background(), home, "", "", true, nil)
	if err != nil || num(status["errors"]) != 1 {
		t.Fatal(status, err)
	}
	if num(s.mustOne("SELECT COUNT(*) n FROM usage")["n"]) != 0 || num(s.mustOne("SELECT COUNT(*) n FROM files")["n"]) != 0 {
		t.Fatal("failed scan committed partial data")
	}
	if err := s.exec("DROP TRIGGER fail_usage"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background(), home, "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Commit only the complete lines, then read the completed append on restart.
	next := strings.Split(string(cliLog("unused", "", "2026-10-07T00:00:00Z", "2026-10-07T00:00:01Z", "2026-10-07T00:00:02Z", "2026-10-07T00:00:03Z")), "\n")[3]
	appendLine := func(value string) {
		t.Helper()
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(value); err != nil {
			t.Fatal(err)
		}
	}
	appendLine(next)
	if _, err := s.Scan(context.Background(), home, "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	if num(s.mustOne("SELECT offset FROM files WHERE path=?", file)["offset"]) != float64(len(data)) {
		t.Fatal("partial line consumed")
	}
	appendLine("\n")
	if _, err := s.Scan(context.Background(), home, "", "", true, nil); err != nil {
		t.Fatal(err)
	}
	if row := s.mustOne("SELECT COUNT(*) n,SUM(input) input FROM usage"); num(row["n"]) != 3 || num(row["input"]) != 300 {
		t.Fatal(row)
	}
}
