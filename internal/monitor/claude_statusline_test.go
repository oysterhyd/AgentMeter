package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func statuslineInput(five, week float64, resets time.Time) []byte {
	return []byte(jsonText(Object{"session_id": "synthetic-session", "transcript_path": "C:/synthetic/s.jsonl", "cwd": "D:/work/demo",
		"model": Object{"id": "claude-opus-5-5", "display_name": "Opus"}, "cost": Object{"total_cost_usd": 1.5},
		"rate_limits": Object{"five_hour": Object{"used_percentage": five, "resets_at": float64(resets.Unix())},
			"seven_day": Object{"used_percentage": week, "resets_at": float64(resets.Add(96 * time.Hour).Unix())}}}))
}

func TestClaudeStatuslineSampleKeepsOnlyRateLimits(t *testing.T) {
	data := t.TempDir()
	file := filepath.Join(data, claudeStatuslineFile)
	for _, input := range []string{"", "{invalid", `{"session_id":"x"}`, `{"rate_limits":{"five_hour":{"used_percentage":"high"}}}`} {
		if err := saveClaudeStatusline(data, []byte(input), time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatal("sample written without rate limits", input)
		}
	}
	now := time.Date(2026, 10, 7, 1, 10, 0, 0, time.UTC)
	if err := saveClaudeStatusline(data, statuslineInput(41.5, 9, now.Add(time.Hour)), now); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	var sample Object
	if err := json.Unmarshal(raw, &sample); err != nil {
		t.Fatal(err)
	}
	if len(sample) != 3 || num(sample["at"]) != float64(now.UnixMilli()) || num(obj(sample["five_hour"])["used_percentage"]) != 41.5 {
		t.Fatal(string(raw))
	}
	for _, private := range []string{"synthetic-session", "D:/work", "Opus", "cost"} {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatal("statusLine session data persisted", private)
		}
	}
	if entries, _ := os.ReadDir(data); len(entries) != 1 {
		t.Fatal("temporary sample left behind", entries)
	}
}

func TestClaudeStatuslineRefreshesQuotaBetweenUsageFetches(t *testing.T) {
	s, f := testStore(t), newClaudeFixture(t)
	data := t.TempDir()
	src := Sources{Home: t.TempDir(), ClaudeHome: f.home, ClaudeConfig: f.config, ClaudeStatusline: filepath.Join(data, claudeStatuslineFile)}
	scan := func() {
		t.Helper()
		if _, err := s.ScanSources(context.Background(), src, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	primary := func() Object {
		for _, q := range s.latestQuotas(s.get("currentClaudeAccount")) {
			if q["bucket"] == "claude" && q["slot"] == "primary" {
				return q
			}
		}
		return nil
	}
	rows := func() float64 {
		return num(s.mustOne("SELECT COUNT(*) n FROM quotas WHERE source=?", ClaudeStatuslineSource)["n"])
	}
	scan()
	if q := primary(); num(q["used"]) != 2 || q["source"] != ClaudeQuotaSource {
		t.Fatal("cached snapshot not recorded", q)
	}
	at := f.at.Add(10 * time.Minute)
	reset := at.Add(2 * time.Hour)
	sample := func(five float64, at time.Time) {
		t.Helper()
		if err := saveClaudeStatusline(data, statuslineInput(five, 7, reset), at); err != nil {
			t.Fatal(err)
		}
		// Rapid rewrites can keep the size and coarse mtime; force a reread.
		s.claudeStatusline.size = -1
	}
	sample(35, at)
	scan()
	q := primary()
	if num(q["used"]) != 35 || q["source"] != ClaudeStatuslineSource || num(q["resets"]) != float64(at.Add(2*time.Hour).Unix()) || q["plan"] != "Max" {
		t.Fatal("statusLine sample not preferred", q)
	}
	if status := obj(s.get("claudeQuotaStatus")); status["attempted"] != iso(at) || status["account"] != s.get("currentClaudeAccount") {
		t.Fatal(status)
	}
	sample(35, at.Add(30*time.Second))
	scan()
	if rows() != 2 {
		t.Fatal("unchanged sample recorded within a minute", rows())
	}
	sample(36, at.Add(40*time.Second))
	scan()
	if rows() != 4 || num(primary()["used"]) != 36 {
		t.Fatal("changed sample not recorded", rows())
	}
	sample(36, at.Add(2*time.Minute))
	scan()
	if rows() != 6 || obj(s.get("claudeQuotaStatus"))["attempted"] != iso(at.Add(2*time.Minute)) {
		t.Fatal("minute sample not recorded", rows())
	}
	// A newer /usage fetch wins again, and an older sample never regresses it.
	f.at = at.Add(5 * time.Minute)
	f.account("acct-1", "dev@example.test", 50, 8)
	scan()
	if q := primary(); num(q["used"]) != 50 || obj(s.get("claudeQuotaStatus"))["attempted"] != iso(f.at) {
		t.Fatal("newer cached snapshot ignored", q)
	}
}

func bridgeService(t *testing.T) (*Service, string) {
	t.Helper()
	previous := findGitBash
	t.Cleanup(func() { findGitBash = previous })
	findGitBash = func() string { return `C:\Program Files\Git\bin\bash.exe` }
	s := &Service{store: testStore(t), config: Config{ClaudeHome: t.TempDir(), Data: `C:\Users\Synthetic\AppData\Roaming\codex-monitor-test`,
		Executable: `C:\Users\Synthetic\AppData\Local\Programs\AgentMeter\Codex Monitor Native.exe`}}
	return s, filepath.Join(s.config.ClaudeHome, "settings.json")
}

func statusLineOf(t *testing.T, path string) Object {
	t.Helper()
	raw, _ := os.ReadFile(path)
	var config Object
	if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &config); err != nil {
		t.Fatal(err)
	}
	return obj(config["statusLine"])
}

func TestClaudeStatuslineSetupWrapsAndRestores(t *testing.T) {
	for name, original := range map[string]string{
		"wrapped":  "{\n  \"model\": \"opus\",\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"ccstatusline\",\n    \"padding\": 0,\n    \"refreshInterval\": 5\n  },\n  \"theme\": \"dark\"\n}\n",
		"none":     "{\n  \"model\": \"opus\"\n}\n",
		"crlf+bom": "\ufeff{\r\n\t\"statusLine\": {\r\n\t\t\"command\": \"bash ~/.claude/line.sh && echo <ok>\",\r\n\t\t\"type\": \"command\"\r\n\t}\r\n}\r\n",
		"no file":  "",
	} {
		t.Run(name, func(t *testing.T) {
			s, path := bridgeService(t)
			if original != "" {
				_ = os.WriteFile(path, []byte(original), 0600)
			}
			var before Object
			_ = json.Unmarshal(bytes.TrimPrefix([]byte(original), []byte{0xef, 0xbb, 0xbf}), &before)
			for i := range 2 {
				state := s.setupClaudeStatusline(true)
				if state["state"] != "ready" || truth(state["changed"]) != (i == 0) {
					t.Fatal(i, state)
				}
				line := statusLineOf(t, path)
				command := text(line["command"])
				if !strings.HasPrefix(command, `"C:/Users/Synthetic/AppData/Local/Programs/AgentMeter/Codex Monitor Native.exe" --claude-statusline`) ||
					!strings.HasSuffix(command, ` --data "C:/Users/Synthetic/AppData/Roaming/codex-monitor-test"`) {
					t.Fatal(command)
				}
				wrapped, ours := bridgedStatusline(command)
				if !ours || wrapped != text(obj(before["statusLine"])["command"]) {
					t.Fatalf("wrapped %q", wrapped)
				}
				for k, v := range obj(before["statusLine"]) {
					if k != "command" && line[k] != v {
						t.Fatal("statusLine option lost", k)
					}
				}
			}
			if state := s.setupClaudeStatusline(false); state["state"] != "disabled" || !truth(state["changed"]) {
				t.Fatal(state)
			}
			after, _ := os.ReadFile(path)
			if original != "" && string(after) != original {
				t.Fatalf("disable did not restore the file:\n%q\n%q", after, original)
			}
			if original == "" && string(after) != "{}\n" {
				t.Fatalf("%q", after)
			}
		})
	}
}

func TestClaudeStatuslineSetupKeepsManualEditsAndUnsupportedShells(t *testing.T) {
	s, path := bridgeService(t)
	original := `{"statusLine":{"type":"command","command":"ccstatusline"}}`
	_ = os.WriteFile(path, []byte(original), 0600)
	if state := s.setupClaudeStatusline(true); state["state"] != "ready" {
		t.Fatal(state)
	}
	if backup, err := os.ReadFile(path + ".codex-monitor.bak"); err != nil || string(backup) != original {
		t.Fatal("original backup missing", err)
	}
	// The user picks another statusLine: wrap it and restore it later.
	_ = os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"npx -y ccusage statusline"}}`), 0600)
	if state := s.setupClaudeStatusline(true); state["state"] != "ready" || !truth(state["changed"]) {
		t.Fatal(state)
	}
	if wrapped, _ := bridgedStatusline(text(statusLineOf(t, path)["command"])); wrapped != "npx -y ccusage statusline" {
		t.Fatal(wrapped)
	}
	// A lost binding recovers the wrapped command from the bridge itself.
	_ = s.store.set("claudeStatuslineBinding", Object{})
	if state := s.setupClaudeStatusline(true); state["state"] != "ready" || truth(state["changed"]) {
		t.Fatal(state)
	}
	if state := s.setupClaudeStatusline(false); state["state"] != "disabled" {
		t.Fatal(state)
	}
	if after, _ := os.ReadFile(path); string(after) != `{"statusLine":{"type":"command","command":"npx -y ccusage statusline"}}` {
		t.Fatal(string(after))
	}
	// A hand-edited bridge is left alone on disable.
	_ = s.setupClaudeStatusline(true)
	raw, _ := os.ReadFile(path)
	modified := bytes.Replace(raw, []byte("--claude-statusline "), []byte("--claude-statusline --verbose "), 1)
	_ = os.WriteFile(path, modified, 0600)
	if state := s.setupClaudeStatusline(false); state["state"] != "error" {
		t.Fatal("manual bridge edit ignored", state)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, modified) {
		t.Fatal("manually modified statusLine overwritten")
	}
	for name, change := range map[string]func(*Service){
		"no git bash": func(*Service) { findGitBash = func() string { return "" } },
		"go run build": func(s *Service) {
			s.config.Executable = filepath.Join(os.TempDir(), "go-build1", "b001", "exe", "monitor.exe")
		},
		"no binary": func(s *Service) { s.config.Executable = "" },
	} {
		s, path := bridgeService(t)
		change(s)
		_ = os.WriteFile(path, []byte(original), 0600)
		if state := s.setupClaudeStatusline(true); state["state"] != "unavailable" {
			t.Fatal(name, state)
		}
		if after, _ := os.ReadFile(path); string(after) != original {
			t.Fatal(name, "statusLine changed")
		}
	}
	for _, conflict := range []string{"{invalid", `{"statusLine":"x"}`, `{"statusLine":{"type":"static","text":"hi"}}`} {
		s, path := bridgeService(t)
		_ = os.WriteFile(path, []byte(conflict), 0600)
		if state := s.setupClaudeStatusline(true); state["state"] != "error" {
			t.Fatal("conflicting statusLine overwritten", conflict, state)
		}
		if after, _ := os.ReadFile(path); string(after) != conflict {
			t.Fatal("conflicting statusLine changed")
		}
	}
}

func TestRunClaudeStatuslinePassesInputThrough(t *testing.T) {
	if findGitBash() == "" {
		t.Skip("Git Bash not installed")
	}
	data := t.TempDir()
	input := statuslineInput(12, 3, time.Now().Add(time.Hour))
	var out, errs bytes.Buffer
	command, _ := claudeStatuslineCommand(`C:\x.exe`, "", "cat; echo; echo done")
	encoded := strings.TrimPrefix(strings.Fields(command)[2], claudeStatuslineFlag+"-command=")
	if code := RunClaudeStatusline(data, encoded, bytes.NewReader(input), &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	if out.String() != string(input)+"\ndone\n" {
		t.Fatalf("%q", out.String())
	}
	if _, err := os.Stat(filepath.Join(data, claudeStatuslineFile)); err != nil {
		t.Fatal("sample not saved", err)
	}
	command, _ = claudeStatuslineCommand(`C:\x.exe`, "", "exit 3")
	encoded = strings.TrimPrefix(strings.Fields(command)[2], claudeStatuslineFlag+"-command=")
	if code := RunClaudeStatusline(data, encoded, strings.NewReader("{}"), &out, &errs); code != 3 {
		t.Fatal("exit status not passed through", code)
	}
}
