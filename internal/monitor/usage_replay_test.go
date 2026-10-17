package monitor

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type resumedUsageFixture struct {
	home, file string
	duplicates []string
	lines      []string
	at         time.Time
}

func seedResumedUsage(t *testing.T, s *Store, origin string) resumedUsageFixture {
	t.Helper()
	f := resumedUsageFixture{home: t.TempDir(), at: parse("2026-10-17T01:00:00Z")}
	f.file = filepath.Join(f.home, "sessions", "resume.jsonl")
	if err := os.MkdirAll(filepath.Dir(f.file), 0700); err != nil {
		t.Fatal(err)
	}
	state := Object{}
	appendRecord := func(kind string, payload Object) string {
		t.Helper()
		o := Object{"timestamp": iso(f.at.Add(time.Duration(len(f.lines)) * time.Second)), "type": kind, "payload": payload}
		payloadJSON := jsonText(payload)
		if kind == "event_msg" {
			rest := clone(payload)
			delete(rest, "type")
			// Real rollouts put the event discriminator before large usage data.
			payloadJSON = fmt.Sprintf(`{"type":%q,%s`, payload["type"], jsonText(rest)[1:])
		}
		line := fmt.Sprintf(`{"timestamp":%q,"type":%q,"payload":%s}`, o["timestamp"], kind, payloadJSON)
		f.lines = append(f.lines, line)
		if err := s.process(o, state, []byte(line)); err != nil {
			t.Fatal(err)
		}
		return line
	}
	meta := Object{"id": "session", "originator": origin, "source": "vscode", "cwd": "project"}
	appendRecord("session_meta", meta)
	appendRecord("turn_context", Object{"model": "gpt-5.5"})
	// A real legacy delta before modern records must survive the repair.
	appendRecord("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": Object{"input_tokens": 100., "cached_input_tokens": 40., "output_tokens": 10., "reasoning_output_tokens": 2., "total_tokens": 110.}}})
	for i, model := range []string{"gpt-5.5", "gpt-5.4"} {
		appendRecord("event_msg", Object{"type": "task_started", "turn_id": fmt.Sprint("turn-", i)})
		appendRecord("turn_context", Object{"model": model})
		u := Object{"input_tokens": float64(200 * (i + 1)), "cached_input_tokens": float64(80 * (i + 1)), "output_tokens": float64(20 * (i + 1)), "reasoning_output_tokens": float64(4 * (i + 1))}
		input := float64(300 + i*400)
		total := Object{"input_tokens": input, "cached_input_tokens": input * .4, "output_tokens": input * .1, "reasoning_output_tokens": input * .02, "total_tokens": input * 1.1}
		response := fmt.Sprint("response-", i)
		appendRecord("token_usage_record", Object{"thread_id": "session", "response_id": response, "usage": u, "thread_token_usage": total})
		appendRecord("session_meta", meta)
		line := appendRecord("event_msg", Object{"type": "token_count", "info": Object{"total_token_usage": total}})
		var o Object
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			t.Fatal(err)
		}
		id := legacyUsageID(state, text(o["timestamp"]), []byte(line))
		f.duplicates = append(f.duplicates, id)
		// Simulate the obsolete full cumulative rows left by the old parser.
		if err := s.addUsage(id, total, text(o["timestamp"]), Object{"session": "session", "model": "unknown", "account": "original-account"}, "累计差分"); err != nil {
			t.Fatal(err)
		}
		if err := s.recordRequestEvent(Object{"id": fmt.Sprint("timing-", i), "ts": text(o["timestamp"]), "session": "session", "kind": "completed", "model": model, "response_id": response, "ttft": float64(450 + i*100), "source": "otel"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.file, []byte(strings.Join(f.lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(f.file)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.exec("INSERT INTO files(path,offset,state,mtime,parser_version) VALUES(?,?,?,?,4)", f.file, info.Size(), jsonText(state), float64(info.ModTime().UnixNano())/1e6); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParserUpgradeRemovesOnlyProvenCumulativeDuplicates(t *testing.T) {
	for _, origin := range []string{"codex_vscode", "codex_cli_rs", "Codex Desktop"} {
		t.Run(origin, func(t *testing.T) {
			s := testStore(t)
			f := seedResumedUsage(t, s, origin)
			// Preserve a real unknown record without corresponding source proof.
			seedCall(t, s, "unrelated", "other", "other-turn", "unknown", "keep-account", f.at, 50, 20, 5)
			if err := s.exec("UPDATE request_events SET usage_id=?,association='response_id' WHERE id='timing-0'", f.duplicates[0]); err != nil {
				t.Fatal(err)
			}
			before := s.mustQuery("SELECT * FROM usage WHERE id NOT IN (?,?) ORDER BY id", f.duplicates[0], f.duplicates[1])
			filter := Object{"range": "all", "page": "history", "session": "session"}
			warm, err := s.Snapshot(filter, f.at)
			if err != nil || num(obj(warm["sums"])["input"]) != 1700 {
				t.Fatal("fixture did not reproduce inflated analysis", warm, err)
			}
			for repeat := 0; repeat < 2; repeat++ {
				status, err := s.Scan(context.Background(), f.home, "", "", true, nil)
				if err != nil || num(status["errors"]) != 0 {
					t.Fatal(status, err)
				}
				if after := s.mustQuery("SELECT * FROM usage ORDER BY id"); jsonText(after) != jsonText(before) {
					t.Fatal("repair changed genuine usage, models or accounts", after, before)
				}
				got, err := s.Snapshot(filter, f.at)
				if err != nil || num(obj(got["sums"])["input"]) != 700 || num(obj(got["sums"])["output"]) != 70 {
					t.Fatal("analysis or cached totals still include duplicate usage", got, err)
				}
				calls := s.callRows(filter, "0000", "9999", indexPrices(s.prices()))
				input, output := 0., 0.
				for _, call := range calls {
					if call["model"] == "unknown" {
						t.Fatal("duplicate unknown model remains in calls", call)
					}
					input += num(call["input"])
					output += num(call["output"])
				}
				if len(calls) != 3 || input != 700 || output != 70 {
					t.Fatal("call details disagree with task analysis", calls)
				}
				for _, model := range got["models"].([]Object) {
					if model["name"] == "unknown" {
						t.Fatal("task analysis still has duplicate unknown usage", model)
					}
				}
				exported, err := s.Export(filter, f.at)
				if err != nil {
					t.Fatal(err)
				}
				csvRows, err := csv.NewReader(strings.NewReader(exported)).ReadAll()
				if err != nil || len(csvRows) != 4 {
					t.Fatal("export includes obsolete usage", csvRows, err)
				}
				exportedInput := 0
				for _, row := range csvRows[1:] {
					value, err := strconv.Atoi(row[4])
					if err != nil {
						t.Fatal(err)
					}
					exportedInput += value
				}
				if exportedInput != 700 {
					t.Fatal("export disagrees with analysis", exportedInput)
				}
				for i := 0; i < 2; i++ {
					e := s.mustOne("SELECT usage_id,ttft FROM request_events WHERE id=?", fmt.Sprint("timing-", i))
					if e["usage_id"] != fmt.Sprint("response-", i) || num(e["ttft"]) != float64(450+i*100) {
						t.Fatal("repair lost a recorded timing", e)
					}
				}
			}
		})
	}
}

func TestForeignThreadCannotInvalidateGenuineLegacyUsage(t *testing.T) {
	s := testStore(t)
	f := seedResumedUsage(t, s, "codex_vscode")
	foreign := fmt.Sprintf(`{"timestamp":%q,"type":"token_usage_record","payload":{"thread_id":"other","response_id":"foreign","usage":{"input_tokens":9999,"output_tokens":999},"thread_token_usage":{"input_tokens":9999,"output_tokens":999}}}`, iso(f.at.Add(time.Second)))
	lines := append(append(append([]string{}, f.lines[:2]...), foreign), f.lines[2:]...)
	if err := os.WriteFile(f.file, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := s.Scan(context.Background(), f.home, "", "", true, nil)
	if err != nil || num(status["errors"]) != 0 {
		t.Fatal(status, err)
	}
	got := s.mustOne("SELECT COUNT(*) n,SUM(input) input FROM usage")
	if num(got["n"]) != 3 || num(got["input"]) != 700 {
		t.Fatal("foreign thread lost genuine legacy usage or changed its baseline", got)
	}
}

func TestLegacyUsageRepairRequiresCompleteEvidenceAndAtomicCommit(t *testing.T) {
	for _, scene := range []string{"different-counts", "retained-event", "malformed-source", "commit-failure"} {
		t.Run(scene, func(t *testing.T) {
			s := testStore(t)
			f := seedResumedUsage(t, s, "codex_vscode")
			switch scene {
			case "different-counts":
				if err := s.exec("UPDATE usage SET input=input-1 WHERE id=?", f.duplicates[0]); err != nil {
					t.Fatal(err)
				}
			case "retained-event":
				// The identical timestamp/total was first a genuine legacy event.
				// A later modern replay of that event must not delete it.
				f.lines = append(append(append([]string{}, f.lines[:2]...), f.lines[7]), f.lines[2:]...)
			case "malformed-source":
				f.lines = append(f.lines, `{"type":"token_usage_record","payload":INVALID}`)
			case "commit-failure":
				if err := s.exec(`CREATE TRIGGER fail_replay BEFORE INSERT ON files BEGIN SELECT RAISE(ABORT,'test commit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(f.file, []byte(strings.Join(f.lines, "\n")+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			status, err := s.Scan(context.Background(), f.home, "", "", true, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(s.mustOne("SELECT id FROM usage WHERE id=?", f.duplicates[0])) == 0 {
				t.Fatal("deleted usage without conclusive source evidence", scene, status)
			}
			if scene == "commit-failure" {
				if num(status["errors"]) != 1 || num(s.mustOne("SELECT parser_version FROM files WHERE path=?", f.file)["parser_version"]) != 4 || s.legacyReplay != nil {
					t.Fatal("replay did not roll back or release temporary state", status)
				}
				if err := s.exec("DROP TRIGGER fail_replay"); err != nil {
					t.Fatal(err)
				}
				status, err = s.Scan(context.Background(), f.home, "", "", true, nil)
				if err != nil || num(status["errors"]) != 0 || len(s.mustOne("SELECT id FROM usage WHERE id=?", f.duplicates[0])) != 0 {
					t.Fatal("failed replay could not be retried", status, err)
				}
			}
		})
	}
}
