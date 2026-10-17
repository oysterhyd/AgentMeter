package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVSCodeUsageSourcesDeduplicationAndExport(t *testing.T) {
	for _, meta := range []Object{
		{"originator": "codex_vscode", "source": "vscode"},
		{"originator": "codex_vscode"},
		{"source": "vscode"},
	} {
		t.Run(jsonText(meta), func(t *testing.T) {
			s := testStore(t)
			now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
			process := func(kind string, payload Object, state Object) {
				t.Helper()
				o := Object{"type": kind, "timestamp": iso(now), "payload": payload}
				if err := s.process(o, state, []byte(jsonText(o))); err != nil {
					t.Fatal(err)
				}
			}
			// Independent IDE sessions can report identical cumulative usage in
			// the same millisecond; each must count once, even after a replay.
			for replay := 0; replay < 2; replay++ {
				for _, id := range []string{"ide-a", "ide-b"} {
					state := Object{}
					m := clone(meta)
					m["id"], m["cwd"] = id, "project"
					process("session_meta", m, state)
					process("turn_context", Object{"model": "gpt-5.5"}, state)
					for i := 0; i < 2; i++ {
						process("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": Object{"input_tokens": 100., "cached_input_tokens": 40., "output_tokens": 20., "total_tokens": 120.}}}, state)
					}
					process("token_usage_record", Object{"response_id": "response-" + id, "usage": Object{"input_tokens": 200., "cached_input_tokens": 80., "output_tokens": 50.}}, state)
					process("token_usage_record", Object{"response_id": "response-" + id, "usage": Object{"input_tokens": 200., "cached_input_tokens": 80., "output_tokens": 50.}}, state)
				}
			}
			if row := s.mustOne("SELECT COUNT(*) n,SUM(input) input,SUM(output) output FROM usage"); num(row["n"]) != 4 || num(row["input"]) != 600 || num(row["output"]) != 140 {
				t.Fatal(row)
			}
			if row := s.mustOne("SELECT source,origin FROM sessions WHERE id='ide-a'"); row["source"] != "vscode" || row["origin"] != "codex_vscode" {
				t.Fatal(row)
			}
			got, err := s.Snapshot(Object{"page": "overview", "range": "all"}, now)
			if err != nil || num(obj(got["sums"])["total"]) != 740 {
				t.Fatal(got, err)
			}
			if num(s.Widget(now)["total"]) != 740 {
				t.Fatal("widget missed IDE usage")
			}
			csv, err := s.Export(Object{"range": "all"}, now)
			if err != nil || !strings.Contains(csv, `"vscode"`) || !strings.Contains(csv, `"codex_vscode"`) {
				t.Fatal(csv, err)
			}
			if _, err = json.Marshal(got); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKnownClientsKeepPriorityOverVSCodeSessionSource(t *testing.T) {
	for _, origin := range []string{"Codex Desktop", "codex_cli_rs", "monocode", "monocode-text"} {
		want := "cli"
		if origin == "Codex Desktop" {
			want = "desktop"
		}
		if got := codexSource(Object{"originator": origin, "source": "vscode"}); got != want {
			t.Fatalf("%s: %s", origin, got)
		}
	}
	if codexSource(Object{"originator": "unrelated", "source": "unknown"}) != "" {
		t.Fatal("unknown clients were admitted")
	}
}
