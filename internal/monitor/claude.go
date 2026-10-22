package monitor

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strings"
	"time"
)

// Claude Code keeps one JSONL transcript per session under
// <config>/projects/<encoded cwd>/<session>.jsonl, and subagents write
// <session>/subagents/agent-*.jsonl with the parent's session ID. Only token
// counts, models, timings and identifiers are decoded: prompts, responses and
// tool calls are skipped by the decoder and never stored.

const ClaudeQuotaSource = "Claude Code 本地缓存"

type claudeUsage struct {
	Input         *float64 `json:"input_tokens"`
	Output        *float64 `json:"output_tokens"`
	CacheRead     float64  `json:"cache_read_input_tokens"`
	CacheCreation float64  `json:"cache_creation_input_tokens"`
	Creation      struct {
		OneHour float64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	Details struct {
		Thinking float64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

type claudeLine struct {
	Type        string   `json:"type"`
	Subtype     string   `json:"subtype"`
	Timestamp   string   `json:"timestamp"`
	UUID        string   `json:"uuid"`
	SessionID   string   `json:"sessionId"`
	RequestID   string   `json:"requestId"`
	Cwd         string   `json:"cwd"`
	Entrypoint  string   `json:"entrypoint"`
	IsSidechain bool     `json:"isSidechain"`
	IsMeta      bool     `json:"isMeta"`
	IsAPIError  bool     `json:"isApiErrorMessage"`
	DurationMs  *float64 `json:"durationMs"`
	Message     struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *claudeUsage    `json:"usage"`
	} `json:"message"`
}

// claudeSource maps Claude Code's entrypoint onto the shared session sources.
func claudeSource(entrypoint string) string {
	e := strings.ToLower(entrypoint)
	switch {
	case strings.Contains(e, "vscode") || strings.Contains(e, "jetbrains") || strings.Contains(e, "ide"):
		return "vscode"
	case strings.Contains(e, "desktop"):
		return "desktop"
	}
	return "cli"
}

// claudePrompt reports whether a user record starts a task: typed text from
// the person, not a tool result, slash-command echo or interruption notice.
func claudePrompt(content json.RawMessage) (prompt, interrupted bool) {
	content = bytes.TrimSpace(content)
	if len(content) == 0 {
		return false, false
	}
	texts := []string{}
	if content[0] == '"' {
		var s string
		if json.Unmarshal(content, &s) != nil {
			return false, false
		}
		texts = append(texts, s)
	} else if content[0] == '[' {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(content, &blocks) != nil {
			return false, false
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_result":
				return false, false
			case "text":
				texts = append(texts, b.Text)
			}
		}
	}
	for _, t := range texts {
		t = strings.TrimSpace(t)
		switch {
		case strings.HasPrefix(t, "[Request interrupted by user"):
			return false, true
		case t == "", strings.HasPrefix(t, "<command-"), strings.HasPrefix(t, "<local-command-"), strings.HasPrefix(t, "Caveat:"):
			continue
		}
		return true, false
	}
	return false, false
}

func (s *Store) processClaude(line []byte, state Object) error {
	var r claudeLine
	if json.Unmarshal(line, &r) != nil {
		return errRecord
	}
	t := parse(r.Timestamp)
	if t.IsZero() || r.SessionID == "" {
		return nil
	}
	ts := iso(t)
	session := "claude:" + r.SessionID
	if state["session"] != session {
		// A new file, or a transcript continuing another session.
		state["session"], state["turn"] = session, nil
		project := r.Cwd
		if project == "" {
			project = UnassignedProject
		}
		origin := r.Entrypoint
		if origin == "" {
			origin = "cli"
		}
		if e := s.exec("INSERT INTO sessions(id,project,origin,created,source,agent) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", session, project, origin, ts, claudeSource(r.Entrypoint), AgentClaude); e != nil {
			return e
		}
	}
	cleared := s.clearedAt()
	allowed := cleared == "" || ts > cleared
	turn := text(state["turn"])
	switch r.Type {
	case "user":
		if r.IsSidechain || r.IsMeta {
			return nil
		}
		prompt, interrupted := claudePrompt(r.Message.Content)
		if interrupted && turn != "" && allowed {
			return s.exec("UPDATE turns SET status='aborted',ended=?,last_seen=? WHERE id=? AND status IN ('running','failed')", ts, ts, turn)
		}
		if !prompt {
			return nil
		}
		// Tasks without a recorded duration end where their last response did.
		if turn != "" {
			if e := s.exec("UPDATE turns SET status='completed',ended=last_seen WHERE id=? AND status='running'", turn); e != nil {
				return e
			}
		}
		turn = "claude:" + r.UUID
		if r.UUID == "" {
			turn = "claude:" + hash(session+ts)
		}
		state["turn"] = turn
		if !allowed {
			return nil
		}
		model := text(state["model"])
		if model == "" {
			model = "unknown"
		}
		return s.exec("INSERT OR IGNORE INTO turns(id,session,model,started,ended,status,duration,ttft,last_seen,account,agent) VALUES(?,?,?,?,?,?,?,?,?,?,?)", turn, session, model, ts, nil, "running", nil, nil, ts, s.accountAt(ts, "claude"), AgentClaude)
	case "system":
		if r.Subtype != "turn_duration" || turn == "" || !allowed || r.DurationMs == nil || !validNumber(*r.DurationMs) {
			return nil
		}
		return s.exec("UPDATE turns SET status=CASE WHEN status IN ('failed','aborted') THEN status ELSE 'completed' END,ended=COALESCE(ended,?),duration=?,last_seen=? WHERE id=?", ts, *r.DurationMs, ts, turn)
	case "assistant":
		model := r.Message.Model
		if r.IsAPIError {
			if turn != "" && allowed {
				return s.exec("UPDATE turns SET status='failed',last_seen=? WHERE id=? AND status='running'", ts, turn)
			}
			return nil
		}
		if model == "<synthetic>" {
			return nil // Local notices such as "No response requested", not API calls.
		}
		u := r.Message.Usage
		if u == nil || u.Input == nil || u.Output == nil {
			return nil
		}
		input := *u.Input + u.CacheRead + u.CacheCreation
		for _, v := range []float64{*u.Input, *u.Output, u.CacheRead, u.CacheCreation, u.Creation.OneHour, u.Details.Thinking} {
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil
			}
		}
		if input+*u.Output <= 0 || !allowed {
			return nil
		}
		if !modelName.MatchString(model) {
			model = "unknown"
		}
		state["model"] = model
		// One API response is written once per content block, all with the
		// same message ID; resumed sessions copy earlier responses verbatim.
		key := r.Message.ID
		if key == "" {
			key = r.RequestID
		}
		if key == "" {
			key = hash(session + "|" + ts + "|" + r.UUID)
		}
		id := "claude:" + key
		if turn == "" {
			// Subagent transcripts carry no prompts: attribute their usage to
			// the parent session's task in progress at that time.
			turn = text(s.mustOne("SELECT id FROM turns WHERE session=? AND started<=? ORDER BY started DESC LIMIT 1", session, ts)["id"])
		}
		var turnValue any
		if turn != "" {
			turnValue = turn
		}
		var request any
		if safeIdentifier(r.RequestID) {
			request = r.RequestID
		}
		account := s.mustOne("SELECT account FROM usage WHERE id=?", id)["account"]
		if !truth(account) {
			account = s.accountAt(ts, "claude")
		}
		// Streaming writes partial counts first; keep each field's maximum.
		if e := s.exec(`INSERT INTO usage(id,session,turn,ts,model,input,cached,output,reasoning,cache_write,cache_write_1h,kind,account,agent,request_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
            ON CONFLICT(id) DO UPDATE SET input=max(usage.input,excluded.input),cached=max(usage.cached,excluded.cached),output=max(usage.output,excluded.output),
            reasoning=max(usage.reasoning,excluded.reasoning),cache_write=max(usage.cache_write,excluded.cache_write),cache_write_1h=max(usage.cache_write_1h,excluded.cache_write_1h),
            turn=COALESCE(usage.turn,excluded.turn),request_id=COALESCE(usage.request_id,excluded.request_id),model=CASE WHEN usage.model IN ('','unknown') THEN excluded.model ELSE usage.model END
            WHERE usage.agent='claude'`,
			id, session, turnValue, ts, model, input, u.CacheRead, *u.Output, min(u.Details.Thinking, *u.Output), u.CacheCreation, min(u.Creation.OneHour, u.CacheCreation), "逐次记录", account, AgentClaude, request); e != nil {
			return e
		}
		if turn != "" {
			// A later response clears an earlier API error in the same task.
			return s.exec("UPDATE turns SET model=COALESCE(NULLIF(?,'unknown'),model),last_seen=max(last_seen,?),status=CASE WHEN status='failed' AND ended IS NULL THEN 'running' ELSE status END WHERE id=?", model, ts, turn)
		}
	}
	return nil
}

// claudeConfigCache avoids re-reading .claude.json, which Claude Code
// rewrites often and can be several megabytes, unless it changed.
type claudeConfigCache struct {
	path    string
	size    int64
	mtime   time.Time
	account Object
	plan    string
	org     string
	quota   *claudeQuota
}

type claudeWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// Utilization mixes window objects with lists and flags, so each window is
// decoded on its own; unknown and malformed entries are ignored.
type claudeQuota struct {
	FetchedAtMs float64                    `json:"fetchedAtMs"`
	AccountUUID string                     `json:"accountUuid"`
	Utilization map[string]json.RawMessage `json:"utilization"`
}

// Only the account identity and the utilization Claude Code itself cached are
// decoded. Credentials live in a different file that is never opened.
type claudeConfigFile struct {
	OAuthAccount *struct {
		AccountUUID      string `json:"accountUuid"`
		OrganizationUUID string `json:"organizationUuid"`
		Email            string `json:"emailAddress"`
		DisplayName      string `json:"displayName"`
		OrganizationType string `json:"organizationType"`
	} `json:"oauthAccount"`
	Cached *claudeQuota `json:"cachedUsageUtilization"`
}

// claudeWindows maps Claude's utilization keys onto quota buckets and slots.
var claudeWindows = []struct{ key, bucket, slot string }{
	{"five_hour", "claude", "primary"}, {"seven_day", "claude", "secondary"},
	{"seven_day_opus", "claude-opus", "secondary"}, {"seven_day_sonnet", "claude-sonnet", "secondary"},
}

func (s *Store) readClaudeConfig(path string) *claudeConfigCache {
	c := &s.claudeConfig
	st, err := os.Stat(path)
	if path == "" || err != nil {
		*c = claudeConfigCache{path: path}
		return c
	}
	if c.path == path && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c
	}
	previous := *c
	*c = claudeConfigCache{path: path, size: st.Size(), mtime: st.ModTime()}
	b, err := os.ReadFile(path)
	var cfg claudeConfigFile
	if err != nil || json.Unmarshal(b, &cfg) != nil {
		// Claude Code may be mid-write: keep the last identity and retry.
		previous.size = -1
		*c = previous
		return c
	}
	if cfg.OAuthAccount == nil || strings.TrimSpace(cfg.OAuthAccount.AccountUUID) == "" {
		return c // Signed out, or an API-key setup without a Claude account.
	}
	a := cfg.OAuthAccount
	id := hash("claude:" + a.AccountUUID)
	label := a.Email
	if label == "" {
		label = a.DisplayName
	}
	if label == "" || len(label) >= 200 {
		label = "Claude " + id[:8]
	}
	c.account = Object{"id": id, "label": label}
	c.org = a.OrganizationUUID
	if plan := strings.TrimPrefix(a.OrganizationType, "claude_"); plan != "" && plan != a.OrganizationType {
		c.plan = strings.ToUpper(plan[:1]) + plan[1:]
	}
	if cfg.Cached != nil && cfg.Cached.AccountUUID == a.AccountUUID && cfg.Cached.FetchedAtMs > 0 {
		c.quota = cfg.Cached
	}
	return c
}

// observeClaude records the signed-in Claude account and samples the quota
// utilization Claude Code last fetched, the newer per-response windows the
// statusLine bridge saved and the samples Claude Desktop keeps. It reads; it
// never writes Claude Code or Claude Desktop files.
func (s *Store) observeClaude(path, statusline, desktop, now string) error {
	c := s.readClaudeConfig(path)
	if e := s.recordAccount(c.account, now, "claude"); e != nil {
		return e
	}
	if c.account == nil {
		return nil
	}
	account := text(c.account["id"])
	latest, err := s.observeClaudeCache(c, account)
	if err != nil {
		return err
	}
	ts, err := s.observeClaudeStatusline(statusline, account, c.plan)
	if err != nil {
		return err
	}
	latest = max(latest, ts)
	if ts, err = s.observeClaudeDesktop(desktop, account, c.org, c.plan); err != nil {
		return err
	}
	latest = max(latest, ts)
	if latest == "" {
		return nil
	}
	return s.set("claudeQuotaStatus", Object{"ok": true, "account": account, "attempted": latest})
}

// observeClaudeCache records cachedUsageUtilization, returning its timestamp.
func (s *Store) observeClaudeCache(c *claudeConfigCache, account string) (string, error) {
	if c.quota == nil {
		return "", nil
	}
	ts := iso(time.UnixMilli(int64(c.quota.FetchedAtMs)))
	if cleared := s.clearedAt(); cleared != "" && ts <= cleared {
		return "", nil
	}
	for _, w := range claudeWindows {
		var v *claudeWindow
		if json.Unmarshal(c.quota.Utilization[w.key], &v) != nil || v == nil || v.Utilization == nil || !validNumber(*v.Utilization) {
			continue
		}
		minutes := 300.
		if w.slot == "secondary" {
			minutes = 10080
		}
		var resets any
		if reset := parse(v.ResetsAt); !reset.IsZero() {
			resets = float64(reset.Unix())
		}
		used := min(100., *v.Utilization)
		id := hash(account + "|" + ts + "|" + w.bucket + "|" + w.slot + "|" + text(used) + "|" + text(resets))
		var plan any
		if c.plan != "" {
			plan = c.plan
		}
		if e := s.exec("INSERT OR IGNORE INTO quotas(id,ts,bucket,slot,used,minutes,resets,plan,source,account,agent) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, ts, w.bucket, w.slot, used, minutes, resets, plan, ClaudeQuotaSource, account, AgentClaude); e != nil {
			return "", e
		}
	}
	return ts, nil
}

// claudeQuotaGroups shapes the latest Claude samples like App Server's rate
// limits, so one reminder path serves both agents.
func (s *Store) claudeQuotaGroups(account any) []Object {
	groups := map[string]Object{}
	order := []string{}
	for _, q := range s.latestQuotas(account) {
		bucket := text(q["bucket"])
		if groups[bucket] == nil {
			name := "Claude Code"
			switch bucket {
			case "claude-opus":
				name += " · Opus"
			case "claude-sonnet":
				name += " · Sonnet"
			}
			groups[bucket] = Object{"limitId": bucket, "limitName": name}
			order = append(order, bucket)
		}
		groups[bucket][text(q["slot"])] = Object{"usedPercent": q["used"], "windowDurationMins": q["minutes"], "resetsAt": q["resets"]}
	}
	out := []Object{}
	for _, bucket := range order {
		out = append(out, groups[bucket])
	}
	return out
}
