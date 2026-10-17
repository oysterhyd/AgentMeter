package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVSCodeResumeReplayRepairsModelsAndRecordedTTFT(t *testing.T) {
	s := testStore(t)
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	lines := []string{}
	appendRecord := func(kind string, p Object) time.Time {
		stamp := at.Add(time.Duration(len(lines)) * time.Second)
		// Match the actual rollout's timestamp/type header before its payload.
		lines = append(lines, fmt.Sprintf(`{"timestamp":%q,"type":%q,"payload":%s}`, iso(stamp), kind, jsonText(p)))
		return stamp
	}
	meta := Object{"id": "ide", "originator": "codex_vscode", "source": "vscode", "cwd": "project"}
	appendRecord("session_meta", meta)
	appendRecord("event_msg", Object{"type": "task_started", "turn_id": "turn"})
	appendRecord("world_state", Object{"full": true, "state": Object{"model": "gpt-5.5"}})
	appendRecord("turn_context", Object{"model": "gpt-5.5", "turn_id": "turn"})
	models := []string{"gpt-5.5", "gpt-5.5", "gpt-5.4", "gpt-6-astra"}
	for i, model := range models {
		if i == 1 {
			appendRecord("session_meta", meta)
		}
		if i == 2 {
			appendRecord("event_msg", Object{"type": "thread_settings_applied", "thread_settings": Object{"model": model}})
		}
		if i == 3 {
			appendRecord("world_state", Object{"full": true, "state": Object{"model": model}})
		}
		id := []string{"one", "two", "three", "four"}[i]
		u := Object{"input_tokens": float64((i + 1) * 1000), "cached_input_tokens": float64((i + 1) * 400), "output_tokens": float64((i + 1) * 100)}
		stamp := appendRecord("token_usage_record", Object{"thread_id": "ide", "turn_id": "turn", "response_id": id, "usage": u})
		// Simulate rows already imported by parser v3 with the missing model.
		if i < 2 {
			if err := s.exec("INSERT INTO usage(id,session,turn,ts,model,input,cached,output,kind) VALUES(?,'ide','turn',?,'unknown',?,?,?,'逐次记录')", id, iso(stamp), u["input_tokens"], u["cached_input_tokens"], u["output_tokens"]); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.recordRequestEvent(Object{"id": "ttft-" + id, "session": "ide", "turn": "turn", "ts": iso(stamp), "kind": "completed", "model": model, "input": u["input_tokens"], "cached": u["cached_input_tokens"], "output": u["output_tokens"], "ttft": float64(1000 + i*100), "source": "otel"}); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			// Resume metadata must not re-enable cumulative double counting.
			appendRecord("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": Object{"input_tokens": 3000., "cached_input_tokens": 1200., "output_tokens": 300., "total_tokens": 3300.}}})
		}
	}
	file := filepath.Join(dir, "resume.jsonl")
	data := []byte(strings.Join(lines, "\n") + "\n")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.exec("INSERT INTO files(path,offset,state,mtime,parser_version) VALUES(?,?,?,?,3)", file, len(data), jsonText(Object{"session": "ide", "source": "vscode", "model": "unknown"}), float64(info.ModTime().UnixNano())/1e6); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		if _, err = s.Scan(context.Background(), home, "", "", true, nil); err != nil {
			t.Fatal(err)
		}
		if r := s.mustOne("SELECT COUNT(*) n,SUM(input) input FROM usage"); num(r["n"]) != 4 || num(r["input"]) != 10000 {
			t.Fatal("replay duplicated or changed token usage", r)
		}
		rows := s.callRows(Object{}, "0000", "9999", indexPrices(s.prices()))
		for i, r := range rows {
			if r["model"] != models[i] || num(r["ttft"]) != float64(1000+i*100) || r["cost"] == nil {
				t.Fatal("model, timing or pricing not restored", r)
			}
		}
	}
}

func TestRequestModelRepairRequiresUniqueEvidence(t *testing.T) {
	for _, scene := range []string{"unique-unknown", "known-conflict", "ambiguous-unknown", "ambiguous-known", "exact-conflict"} {
		t.Run(scene, func(t *testing.T) {
			s := testStore(t)
			at := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
			model := "unknown"
			if scene == "known-conflict" || scene == "exact-conflict" {
				model = "gpt-5.4"
			}
			seedCall(t, s, "response", "ide", "turn", model, Unknown, at, 1000, 400, 100)
			if strings.HasPrefix(scene, "ambiguous-") {
				other := "unknown"
				if scene == "ambiguous-known" {
					other = "gpt-5.5"
				}
				seedCall(t, s, "other", "ide", "turn", other, Unknown, at, 1000, 400, 100)
			}
			e := Object{"id": "timing", "ts": iso(at), "session": "ide", "kind": "completed", "model": "gpt-5.5", "input": 1000., "cached": 400., "output": 100., "ttft": 450., "source": "otel"}
			if scene == "exact-conflict" {
				e["response_id"] = "response"
			}
			if err := s.ingestRequestEvents([]Object{e}); err != nil {
				t.Fatal(err)
			}
			u := s.mustOne("SELECT model,input FROM usage WHERE id='response'")
			linked := s.mustOne("SELECT usage_id FROM request_events WHERE id='timing'")["usage_id"]
			if scene == "unique-unknown" {
				if u["model"] != "gpt-5.5" || linked != "response" {
					t.Fatal("unique evidence was not used", u, linked)
				}
			} else if u["model"] != model || (scene != "exact-conflict" && linked != nil) {
				t.Fatal("ambiguous evidence changed a record", u, linked)
			}
			if num(u["input"]) != 1000 {
				t.Fatal("metadata repair changed token counts")
			}
		})
	}
}

func TestUsageReplayOnlyRepairsMissingModels(t *testing.T) {
	s := testStore(t)
	at := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	seedCall(t, s, "response", "ide", "turn", "gpt-5.5", Unknown, at, 1000, 400, 100)
	state := Object{"session": "ide", "model": "gpt-5.4"}
	u := Object{"input_tokens": 2000., "cached_input_tokens": 800., "output_tokens": 200.}
	for _, missing := range []bool{false, true} {
		if missing {
			if err := s.exec("UPDATE usage SET model='unknown' WHERE id='response'"); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.addUsage("response", u, iso(at), state, "逐次记录"); err != nil {
			t.Fatal(err)
		}
		row := s.mustOne("SELECT model,input,output FROM usage WHERE id='response'")
		want := "gpt-5.5"
		if missing {
			want = "gpt-5.4"
		}
		if row["model"] != want || num(row["input"]) != 1000 || num(row["output"]) != 100 {
			t.Fatal(row)
		}
	}
}

func TestCodexResumeKeepsStateOnlyForTheSameSession(t *testing.T) {
	s := testStore(t)
	at := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	state := Object{"session": "ide", "source": "vscode", "model": "gpt-5.5", "modern": true, "turn": "turn", "total": Object{"input_tokens": 1000.}, "created": iso(at)}
	process := func(id string) {
		t.Helper()
		o := Object{"type": "session_meta", "timestamp": iso(at.Add(time.Second)), "payload": Object{"id": id, "originator": "codex_vscode", "source": "vscode", "cwd": "project"}}
		if err := s.process(o, state, []byte(jsonText(o))); err != nil {
			t.Fatal(err)
		}
	}
	process("ide")
	if state["model"] != "gpt-5.5" || !truth(state["modern"]) || state["turn"] != "turn" || num(obj(state["total"])["input_tokens"]) != 1000 || state["created"] != iso(at) {
		t.Fatal("resume lost its state", state)
	}
	process("other")
	if state["model"] != nil || truth(state["modern"]) || state["turn"] != nil || state["total"] != nil {
		t.Fatal("state leaked across sessions", state)
	}
}
