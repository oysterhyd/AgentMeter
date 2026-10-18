package monitor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClaudeTelemetryTimesCallsByRequestID(t *testing.T) {
	s := testStore(t)
	f := newClaudeFixture(t)
	at := f.at.Add(3 * time.Second)
	common := Object{"session.id": "s1", "model": "claude-opus-5-5", "user.email": "SECRET EMAIL", "prompt": "SECRET PROMPT", "user.account_uuid": "SECRET ACCOUNT"}
	payload := func(fields Object) []Object {
		all := clone(common)
		for k, v := range fields {
			all[k] = v
		}
		events, err := decodeTelemetry(telemetryPayload(at, all))
		if err != nil {
			t.Fatal(err)
		}
		return events
	}
	completed := payload(Object{"event.name": "api_request", "request_id": "req-msg-1", "ttft_ms": "850", "duration_ms": "4000",
		"input_tokens": "3", "output_tokens": "40", "cache_read_tokens": "1000", "cache_creation_tokens": "200"})
	// Requests that do not stream (titles, classifiers) report no TTFT.
	untimed := payload(Object{"event.name": "api_request", "request_id": "req-title", "duration_ms": "300", "input_tokens": "9", "output_tokens": "4", "cache_read_tokens": "0", "cache_creation_tokens": "0"})
	failed := payload(Object{"event.name": "api_error", "request_id": "req-failed", "status_code": "529", "error": "Overloaded", "attempt": "3"})
	if len(completed) != 1 || len(untimed) != 0 || len(failed) != 1 {
		t.Fatal(completed, untimed, failed)
	}
	if strings.Contains(jsonText(append(completed, failed...)), "SECRET") {
		t.Fatal("telemetry retained private attributes")
	}
	if e := completed[0]; e["session"] != "claude:s1" || e["request_key"] != "req-msg-1" || num(e["input"]) != 1203 || num(e["cached"]) != 1000 || num(e["output"]) != 40 {
		t.Fatal(e)
	}
	// The exporter can deliver before the transcript is scanned.
	if err := s.ingestRequestEvents(append(completed, failed...)); err != nil {
		t.Fatal(err)
	}
	f.write("s1.jsonl", f.prompt("time this"), f.response("msg-1", "claude-opus-5-5", 3, 40, 1000, 200, 100))
	f.scan(s)
	r := s.mustOne("SELECT * FROM request_events WHERE kind='completed'")
	if r["usage_id"] != "claude:msg-1" || r["association"] != "request_id" || num(r["ttft"]) != 850 || r["agent"] != AgentClaude || r["turn"] == nil {
		t.Fatal(r)
	}
	if e := s.mustOne("SELECT * FROM request_events WHERE kind='failed'"); e["agent"] != AgentClaude || e["error_code"] != "server" || num(e["http_status"]) != 529 {
		t.Fatal(e)
	}
	rows := s.callRows(Object{"agent": AgentClaude}, "0000", "9999", indexPrices(s.prices()))
	if len(rows) != 2 || num(rows[0]["ttft"]) != 850 || rows[0]["timingNote"] != "按请求 ID 关联" || rows[1]["status"] != "failed" {
		t.Fatal(rows)
	}
	if rows := s.callRows(Object{"agent": AgentCodex}, "0000", "9999", indexPrices(s.prices())); len(rows) != 0 {
		t.Fatal("Claude timing leaked into Codex calls", rows)
	}
	// Rescanning or a resent batch neither duplicates nor unlinks timing.
	f.scan(s)
	if err := s.ingestRequestEvents(completed); err != nil {
		t.Fatal(err)
	}
	if num(s.mustOne("SELECT COUNT(*) n FROM request_events")["n"]) != 2 || s.mustOne("SELECT usage_id FROM request_events WHERE kind='completed'")["usage_id"] != "claude:msg-1" {
		t.Fatal("timing duplicated or unlinked")
	}
}

func topKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	members, err := jsonMembers(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, m := range members {
		keys = append(keys, m.key)
	}
	return keys
}

func TestClaudeSettingsSetupPreservesAndRestores(t *testing.T) {
	for name, original := range map[string]string{
		"no env":    "{\n  \"hooks\": {\n    \"SessionStart\": []\n  },\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"node \\\"C:/x/statusline.js\\\"\"\n  },\n  \"theme\": \"dark\"\n}\n",
		"user env":  "{\n  \"env\": {\n    \"FOO\": \"bar\",\n    \"CLAUDE_CODE_ENABLE_TELEMETRY\": \"true\"\n  },\n  \"model\": \"opus\"\n}\n",
		"crlf+bom":  "\ufeff{\r\n\t\"model\": \"opus\",\r\n\t\"env\": {}\r\n}\r\n",
		"compact":   `{"model":"opus","permissions":{"allow":["Bash(go test:*)"]}}`,
		"no file":   "",
		"html-safe": "{\n  \"note\": \"<a&b>\"\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := &Service{store: testStore(t), config: Config{ClaudeHome: t.TempDir()}}
			path := filepath.Join(s.config.ClaudeHome, "settings.json")
			if original != "" {
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, port := range []int{4319, 4320, 4320} {
				if state := s.setupClaude(true, port); state["state"] != "ready" {
					t.Fatal(state)
				}
				raw, _ := os.ReadFile(path)
				var config Object
				if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &config); err != nil {
					t.Fatal(err)
				}
				env := obj(config["env"])
				for k, v := range claudeTelemetryEnv(port) {
					if env[k] != v {
						t.Fatal("env not synchronized", env)
					}
				}
				if original != "" {
					want := append(topKeys(t, []byte(original)), "env")
					if !strings.Contains(original, `"env"`) {
						if got := topKeys(t, raw); !reflect.DeepEqual(got, want) {
							t.Fatal("setting order changed", got)
						}
					}
				}
				if strings.Contains(original, "FOO") && env["FOO"] != "bar" {
					t.Fatal("user env lost")
				}
				if strings.Contains(original, "<a&b>") && !bytes.Contains(raw, []byte("<a&b>")) {
					t.Fatal("unrelated string re-escaped")
				}
			}
			if state := s.setupClaude(false, 4320); state["state"] != "disabled" {
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

func TestClaudeSettingsSetupKeepsConflictsAndManualEdits(t *testing.T) {
	s := &Service{store: testStore(t), config: Config{ClaudeHome: t.TempDir()}}
	path := filepath.Join(s.config.ClaudeHome, "settings.json")
	for _, original := range []string{"{invalid", `{"env":"x"}`, `{"env":{"OTEL_LOGS_EXPORTER":"console"}}`,
		`{"env":{"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT":"https://collector.example.test/v1/logs"}}`, `{"env":{"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL":"grpc"}}`} {
		_ = os.WriteFile(path, []byte(original), 0600)
		if state := s.setupClaude(true, 4319); state["state"] != "error" {
			t.Fatal("conflicting config overwritten", original, state)
		}
		if after, _ := os.ReadFile(path); string(after) != original {
			t.Fatal("conflicting config changed")
		}
	}
	_ = os.WriteFile(path, []byte(`{"model":"opus"}`), 0600)
	if state := s.setupClaude(true, 4319); state["state"] != "ready" || !truth(state["changed"]) {
		t.Fatal(state)
	}
	if backup, err := os.ReadFile(path + ".codex-monitor.bak"); err != nil || string(backup) != `{"model":"opus"}` {
		t.Fatal("original backup missing", err)
	}
	raw, _ := os.ReadFile(path)
	modified := bytes.ReplaceAll(raw, []byte("127.0.0.1:4319"), []byte("collector.example.test:4319"))
	_ = os.WriteFile(path, modified, 0600)
	if state := s.setupClaude(false, 4319); state["state"] != "error" {
		t.Fatal("manual exporter edit ignored")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, modified) {
		t.Fatal("manually modified exporter overwritten")
	}
	missing := &Service{store: testStore(t), config: Config{ClaudeHome: filepath.Join(t.TempDir(), "absent")}}
	if state := missing.setupClaude(true, 4319); state["state"] != "missing" {
		t.Fatal(state)
	}
	if _, err := os.Stat(missing.config.ClaudeHome); !os.IsNotExist(err) {
		t.Fatal("created a Claude Code directory")
	}
}
