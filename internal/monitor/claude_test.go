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

// claudeFixture writes synthetic Claude Code transcripts; no personal data.
type claudeFixture struct {
	t        *testing.T
	home     string
	config   string
	at       time.Time
	sequence int
}

func newClaudeFixture(t *testing.T) *claudeFixture {
	home := t.TempDir()
	f := &claudeFixture{t: t, home: home, config: filepath.Join(home, ".claude.json"), at: time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)}
	f.account("acct-1", "dev@example.test", 2, 3)
	return f
}

func (f *claudeFixture) account(uuid, email string, five, week float64) {
	f.t.Helper()
	cfg := Object{"numStartups": 3, "projects": Object{"D:/x": Object{"history": []any{"ignored"}}},
		"oauthAccount": Object{"accountUuid": uuid, "organizationUuid": "org-" + uuid, "emailAddress": email, "organizationType": "claude_max"},
		"cachedUsageUtilization": Object{"fetchedAtMs": float64(f.at.UnixMilli()), "accountUuid": uuid, "utilization": Object{
			"five_hour":      Object{"utilization": five, "resets_at": f.at.Add(3 * time.Hour).Format(time.RFC3339Nano)},
			"seven_day":      Object{"utilization": week, "resets_at": f.at.Add(72 * time.Hour).Format(time.RFC3339Nano)},
			"seven_day_opus": nil, "limits": []any{Object{"kind": "session"}}, "member_dashboard_available": false, "extra_usage": Object{"is_enabled": false},
		}}}
	if err := os.WriteFile(f.config, []byte(jsonText(cfg)), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *claudeFixture) line(kind string, fields Object) string {
	f.sequence++
	f.at = f.at.Add(time.Second)
	o := Object{"type": kind, "timestamp": f.at.Format(time.RFC3339Nano), "uuid": "u" + text(float64(f.sequence)),
		"sessionId": "s1", "cwd": `D:\work\demo`, "entrypoint": "cli", "isSidechain": false, "version": "2.1.0"}
	for k, v := range fields {
		o[k] = v
	}
	return jsonText(o)
}

func (f *claudeFixture) prompt(text string) string {
	return f.line("user", Object{"message": Object{"role": "user", "content": text}})
}

func (f *claudeFixture) response(id, model string, input, output, read, write, write1h float64) string {
	usage := Object{"input_tokens": input, "output_tokens": output, "cache_read_input_tokens": read, "cache_creation_input_tokens": write,
		"cache_creation": Object{"ephemeral_5m_input_tokens": write - write1h, "ephemeral_1h_input_tokens": write1h}, "output_tokens_details": Object{"thinking_tokens": output / 2}}
	return f.line("assistant", Object{"requestId": "req-" + id, "message": Object{"id": id, "model": model, "role": "assistant",
		"content": []any{Object{"type": "text", "text": "secret body that must not be stored"}}, "usage": usage}})
}

func (f *claudeFixture) write(rel string, lines ...string) {
	f.t.Helper()
	file := filepath.Join(f.home, "projects", "D--work-demo", rel)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		f.t.Fatal(err)
	}
	out, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer out.Close()
	if _, err = out.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		f.t.Fatal(err)
	}
}

func (f *claudeFixture) scan(s *Store) Object {
	f.t.Helper()
	status, err := s.ScanSources(context.Background(), Sources{Home: f.t.TempDir(), ClaudeHome: f.home, ClaudeConfig: f.config}, true, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return status
}

func TestClaudeTranscriptsBecomeUsageTasksAndQuota(t *testing.T) {
	s := testStore(t)
	f := newClaudeFixture(t)
	f.write("s1.jsonl",
		f.line("file-history-snapshot", Object{"snapshot": Object{}}),
		f.prompt("build the parser"),
		// One response is written per content block, first with partial output.
		f.response("msg-1", "claude-opus-5-5", 3, 5, 1000, 200, 100),
		f.response("msg-1", "claude-opus-5-5", 3, 40, 1000, 200, 100),
		f.response("msg-1", "claude-opus-5-5", 3, 40, 1000, 200, 100),
		f.line("user", Object{"message": Object{"role": "user", "content": []any{Object{"type": "tool_result", "content": "big output"}}}}),
		f.response("msg-2", "claude-opus-5-5-20260901", 2, 60, 1200, 0, 0),
		f.line("system", Object{"subtype": "turn_duration", "durationMs": 4200.}),
		f.prompt("<command-name>/clear</command-name>"),
		f.line("user", Object{"isMeta": true, "message": Object{"role": "user", "content": "Caveat: meta"}}),
		f.prompt("second task"),
		f.line("assistant", Object{"isApiErrorMessage": true, "message": Object{"id": "e", "model": "<synthetic>", "usage": Object{"input_tokens": 0., "output_tokens": 0.}}}),
		f.line("user", Object{"message": Object{"role": "user", "content": []any{Object{"type": "text", "text": "[Request interrupted by user]"}}}}),
		f.line("assistant", Object{"message": Object{"id": "n", "model": "<synthetic>", "usage": Object{"input_tokens": 0., "output_tokens": 0.}}}),
	)
	// A subagent transcript has the parent's session ID and only sidechain lines.
	f.write("s1/subagents/agent-a.jsonl", f.line("assistant", Object{"isSidechain": true, "requestId": "req-sub", "message": Object{"id": "msg-sub", "model": "claude-haiku-5-5",
		"usage": Object{"input_tokens": 10., "output_tokens": 7., "cache_read_input_tokens": 0., "cache_creation_input_tokens": 0.}}}))
	status := f.scan(s)
	if num(status["errors"]) != 0 || num(status["claudeFiles"]) != 2 || !truth(status["claudeSourceExists"]) {
		t.Fatal(status)
	}
	rows := s.mustQuery("SELECT * FROM usage ORDER BY ts")
	if len(rows) != 3 {
		t.Fatalf("expected 3 deduplicated responses, got %v", rows)
	}
	first := rows[0]
	// Input includes cache reads and writes; reasoning is within output.
	if first["id"] != "claude:msg-1" || num(first["input"]) != 1203 || num(first["cached"]) != 1000 || num(first["output"]) != 40 ||
		num(first["cache_write"]) != 200 || num(first["cache_write_1h"]) != 100 || num(first["reasoning"]) != 20 || first["agent"] != AgentClaude {
		t.Fatal(first)
	}
	for _, r := range rows {
		if strings.Contains(jsonText(r), "secret") {
			t.Fatal("message body persisted")
		}
	}
	turns := s.mustQuery("SELECT * FROM turns ORDER BY started")
	if len(turns) != 2 || turns[0]["status"] != "completed" || num(turns[0]["duration"]) != 4200 || turns[1]["status"] != "aborted" || turns[0]["agent"] != AgentClaude {
		t.Fatal(turns)
	}
	if sub := s.mustOne("SELECT turn FROM usage WHERE id='claude:msg-sub'"); sub["turn"] != turns[1]["id"] {
		t.Fatalf("subagent usage not attributed to the task in progress: %v", sub)
	}
	session := s.mustOne("SELECT * FROM sessions WHERE id='claude:s1'")
	if session["agent"] != AgentClaude || session["source"] != "cli" || session["project"] != `D:\work\demo` {
		t.Fatal(session)
	}
	account := text(s.get("currentClaudeAccount"))
	if account == "" || account == Unknown || s.mustOne("SELECT label FROM accounts WHERE id=?", account)["label"] != "dev@example.test" {
		t.Fatal("Claude account not detected")
	}
	// History before the first observation stays unattributed, as for Codex;
	// records written while the account is observed belong to it.
	if n := num(s.mustOne("SELECT COUNT(*) n FROM usage WHERE account=?", Unknown)["n"]); n != 3 {
		t.Fatalf("history attributed without an observation: %v", n)
	}
	if err := s.exec("UPDATE account_observations SET started=? WHERE source='claude'", iso(f.at)); err != nil {
		t.Fatal(err)
	}
	f.write("s1.jsonl", f.prompt("live"), f.response("msg-live", "claude-opus-5-5", 1, 1, 0, 0, 0))
	f.scan(s)
	if row := s.mustOne("SELECT account FROM usage WHERE id='claude:msg-live'"); row["account"] != account {
		t.Fatalf("live usage not attributed to the Claude account: %v", row)
	}
	quotas := s.mustQuery("SELECT * FROM quotas ORDER BY slot,ts LIMIT 2")
	if len(quotas) != 2 || quotas[0]["bucket"] != "claude" || num(quotas[0]["minutes"]) != 300 || num(quotas[0]["used"]) != 2 || quotas[0]["plan"] != "Max" || quotas[1]["agent"] != AgentClaude {
		t.Fatal(quotas)
	}
	// Opus 5.5: $4 input, $0.20 read, $5 5m write, $8 1h write, $20 output.
	cost := indexPrices(s.prices()).calculate(first)
	want := (3*4 + 1000*.2 + 100*5 + 100*8 + 40*20) / 1e6
	if diff := cost.total - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("cost %v, want %v", cost.total, want)
	}
	if cost := indexPrices(s.prices()).calculate(rows[1]); cost.price == nil {
		t.Fatal("dated model ID was not priced")
	}
}

func TestClaudeIncrementalResumeAndDisable(t *testing.T) {
	s := testStore(t)
	f := newClaudeFixture(t)
	f.write("s1.jsonl", f.prompt("one"), f.response("msg-1", "claude-sonnet-5-5", 1, 10, 0, 0, 0))
	f.scan(s)
	f.write("s1.jsonl", f.response("msg-2", "claude-sonnet-5-5", 1, 20, 0, 0, 0))
	// A resumed session copies earlier responses into a new transcript.
	f.write("s2.jsonl", f.line("assistant", Object{"sessionId": "s2", "message": Object{"id": "msg-1", "model": "claude-sonnet-5-5", "usage": Object{"input_tokens": 1., "output_tokens": 10.}}}))
	f.scan(s)
	if row := s.mustOne("SELECT COUNT(*) n,SUM(output) output FROM usage"); num(row["n"]) != 2 || num(row["output"]) != 30 {
		t.Fatal(row)
	}
	// Unchanged files are skipped without re-reading.
	f.scan(s)
	if row := s.mustOne("SELECT COUNT(*) n FROM usage"); num(row["n"]) != 2 {
		t.Fatal(row)
	}
	if _, err := s.saveSettings(Object{"claudeEnabled": false}); err != nil {
		t.Fatal(err)
	}
	f.write("s1.jsonl", f.response("msg-3", "claude-sonnet-5-5", 1, 20, 0, 0, 0))
	if status := f.scan(s); truth(status["claudeSourceExists"]) {
		t.Fatal("disabled collector still reported a source")
	}
	if row := s.mustOne("SELECT COUNT(*) n FROM usage"); num(row["n"]) != 2 {
		t.Fatal("disabled Claude Code collection still read transcripts")
	}
}

func TestClaudeClearBoundaryAndPartialConfig(t *testing.T) {
	s := testStore(t)
	f := newClaudeFixture(t)
	f.write("s1.jsonl", f.prompt("old"), f.response("old", "claude-opus-5-5", 1, 1, 0, 0, 0))
	if err := s.clear(f.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	f.at = f.at.Add(time.Minute)
	f.account("acct-1", "dev@example.test", 9, 9)
	f.write("s1.jsonl", f.prompt("new"), f.response("new", "claude-opus-5-5", 1, 1, 0, 0, 0))
	f.scan(s)
	if rows := s.mustQuery("SELECT id FROM usage"); len(rows) != 1 || rows[0]["id"] != "claude:new" {
		t.Fatal(rows)
	}
	account := s.get("currentClaudeAccount")
	// A half-written config keeps the previous identity rather than flapping.
	if err := os.WriteFile(f.config, []byte(`{"oauthAccount":{"accountUuid":"acct-1"`), 0600); err != nil {
		t.Fatal(err)
	}
	f.scan(s)
	if s.get("currentClaudeAccount") != account {
		t.Fatal("partial config changed the account")
	}
}

func TestClaudePromptClassification(t *testing.T) {
	for _, c := range []struct {
		content             string
		prompt, interrupted bool
	}{
		{`"fix it"`, true, false},
		{`"<command-name>/model</command-name>"`, false, false},
		{`[{"type":"tool_result","content":"x"}]`, false, false},
		{`[{"type":"text","text":"[Request interrupted by user for tool use]"}]`, false, true},
		{`[{"type":"image"},{"type":"text","text":"look"}]`, true, false},
		{`""`, false, false},
	} {
		prompt, interrupted := claudePrompt(json.RawMessage(c.content))
		if prompt != c.prompt || interrupted != c.interrupted {
			t.Errorf("%s: got %v %v", c.content, prompt, interrupted)
		}
	}
}

func TestCanonicalModel(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5": "claude-opus-5-5", "claude-sonnet-4-5-20250929": "claude-sonnet-4-5", "claude-opus-5-5[1m]": "claude-opus-5-5",
		"anthropic/claude-haiku-5-5": "claude-haiku-5-5", "claude-opus-4-5@20251101": "claude-opus-4-5", "gpt-5.6-sol": "gpt-5.6-sol", "GPT-5.5": "gpt-5.5",
	} {
		if got := canonicalModel(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestAgentFilterBreakdownAndQuotas(t *testing.T) {
	s := testStore(t)
	f := newClaudeFixture(t)
	f.write("s1.jsonl", f.prompt("task"), f.response("c1", "claude-sonnet-5-5", 100, 50, 0, 0, 0))
	f.scan(s)
	at := f.at.Add(-time.Hour)
	if err := s.exec(`INSERT INTO sessions(id,project,origin,created,source) VALUES('cx','p','Codex Desktop',?,'desktop');
		INSERT INTO usage(id,session,ts,model,input,cached,output,reasoning,cache_write,kind,account) VALUES('x1','cx',?,'gpt-5.5',1000,0,10,0,0,'逐次记录','codex-a');
		INSERT INTO quotas(id,ts,bucket,slot,used,minutes,resets,source,account) VALUES('q','2026-10-07T00:00:00.000Z','codex','primary',10,300,NULL,'在线查询','codex-a')`, iso(at), iso(at)); err != nil {
		t.Fatal(err)
	}
	if err := s.set("currentAccount", "codex-a"); err != nil {
		t.Fatal(err)
	}
	all, err := s.Snapshot(Object{"range": "all"}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	agents := all["agents"].([]Object)
	if len(agents) != 2 || agents[0]["name"] != AgentCodex || num(agents[1]["total"]) != 150 {
		t.Fatal(agents)
	}
	if present := all["agentsPresent"].([]string); len(present) != 2 {
		t.Fatal(present)
	}
	if quotas := all["quotas"].([]Object); len(quotas) != 3 {
		t.Fatalf("expected Codex and Claude quotas: %v", quotas)
	}
	claude, err := s.Snapshot(Object{"range": "all", "agent": AgentClaude}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	if sums := obj(claude["sums"]); num(sums["total"]) != 150 || num(sums["requests"]) != 1 {
		t.Fatal(sums)
	}
	if models := obj(claude["options"])["models"].([]string); len(models) != 1 || models[0] != "claude-sonnet-5-5" {
		t.Fatal(models)
	}
	for _, q := range claude["quotas"].([]Object) {
		if q["agent"] != AgentClaude {
			t.Fatal("Codex quota in the Claude Code view")
		}
	}
	if days := obj(claude["activity"])["days"].([]Object); len(days) != 1 || num(days[0]["total"]) != 150 {
		t.Fatal(days)
	}
	if csv, err := s.Export(Object{"range": "all", "agent": AgentCodex}, f.at); err != nil || strings.Contains(csv, "claude-sonnet") || !strings.Contains(csv, "gpt-5.5") {
		t.Fatal(csv, err)
	}
	// The widget follows the agent used most recently, unless one is chosen.
	if w := s.Widget(f.at); w["quotaAgent"] != AgentClaude || len(w["quotas"].([]Object)) == 0 {
		t.Fatal(w["quotaAgent"], w["quotas"])
	}
	if _, err := s.saveSettings(Object{"widgetQuotaAgent": AgentCodex}); err != nil {
		t.Fatal(err)
	}
	if w := s.Widget(f.at); w["quotaAgent"] != AgentCodex || w["bucket"] != "codex" {
		t.Fatal(w["quotaAgent"], w["bucket"])
	}
}
