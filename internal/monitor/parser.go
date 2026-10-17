package monitor

import (
	"bytes"
	"encoding/json"
)

// Prefer client identities: CLI/third-party app-server sessions can also carry
// source=vscode. Use the source only when no known originator identifies them.
func codexSource(meta Object) string {
	switch text(meta["originator"]) {
	case "Codex Desktop", "codex_work_desktop", "codex_desktop":
		return "desktop"
	case "Codex CLI", "codex_cli_rs", "codex-tui", "codex-cli", "codex_exec", "monocode", "monocode-text":
		return "cli"
	case "codex_vscode":
		return "vscode"
	}
	switch text(meta["source"]) {
	case "cli", "exec":
		return "cli"
	case "vscode":
		return "vscode"
	}
	return ""
}

func rawAt(raw []byte, keys ...string) string {
	for _, k := range keys {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return ""
		}
		raw = m[k]
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return ""
	}
	return b.String()
}
func (s *Store) addQuota(raw Object, ts, source string, account any) error {
	if len(raw) == 0 || parse(ts).IsZero() {
		return nil
	}
	if cleared := s.clearedAt(); cleared != "" && ts <= cleared {
		return nil
	}
	if account == nil {
		account = s.accountAt(ts, "codex")
	}
	bucket := text(raw["limitId"])
	if bucket == "" {
		bucket = text(raw["limit_id"])
	}
	if bucket == "" {
		bucket = "codex"
	}
	for _, slot := range []string{"primary", "secondary"} {
		w := obj(raw[slot])
		if len(w) == 0 {
			continue
		}
		pick := func(a, b string) any {
			if w[a] != nil {
				return w[a]
			}
			return w[b]
		}
		used, minutes, resets := pick("usedPercent", "used_percent"), pick("windowDurationMins", "window_minutes"), pick("resetsAt", "resets_at")
		if !validNumber(used) || !validNumber(minutes) {
			continue
		}
		if !validNumber(resets) {
			resets = nil
		}
		if source == "日志" && len(s.mustOne("SELECT 1 FROM quotas WHERE ts=? AND bucket=? AND slot=? AND used=? AND resets IS ? AND source=? LIMIT 1", ts, bucket, slot, min(100., num(used)), resets, source)) > 0 {
			continue
		}
		reset := text(resets)
		id := hash(text(account) + "|" + ts + "|" + bucket + "|" + slot + "|" + text(used) + "|" + reset)
		plan := raw["planType"]
		if !truth(plan) {
			plan = raw["plan_type"]
		}
		if !truth(plan) {
			plan = nil
		}
		if e := s.exec("INSERT OR IGNORE INTO quotas(id,ts,bucket,slot,used,minutes,resets,plan,source,account) VALUES(?,?,?,?,?,?,?,?,?,?)", id, ts, bucket, slot, min(100., num(used)), minutes, resets, plan, source, account); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) addUsage(id string, u Object, ts string, state Object, kind string) error {
	input, output := u["input_tokens"], u["output_tokens"]
	cached := u["cached_input_tokens"]
	if !validNumber(cached) {
		cached = 0.
	}
	if !validNumber(input) || !validNumber(output) || num(cached) > num(input) {
		return nil
	}
	account := state["account"]
	if account == nil {
		account = s.mustOne("SELECT account FROM usage WHERE id=?", id)["account"]
		if !truth(account) {
			account = s.accountAt(ts, "codex")
		}
	}
	reason, write := u["reasoning_output_tokens"], u["cache_write_input_tokens"]
	if !validNumber(reason) {
		reason = 0.
	}
	if !validNumber(write) {
		write = 0.
	}
	model := state["model"]
	if !truth(model) {
		model = "unknown"
	}
	turn := state["turn"]
	if !truth(turn) {
		turn = nil
	}
	mode := "IGNORE"
	if s.replay {
		mode = "REPLACE"
	}
	if e := s.exec("INSERT OR "+mode+" INTO usage(id,session,turn,ts,model,input,cached,output,reasoning,cache_write,kind,account) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, state["session"], turn, ts, model, input, cached, output, reason, write, kind, account); e != nil {
		return e
	}
	if truth(turn) {
		return s.exec("UPDATE turns SET model=?,last_seen=? WHERE id=?", model, ts, turn)
	}
	return nil
}
func (s *Store) process(o, state Object, raw []byte) error {
	p := obj(o["payload"])
	t := parse(o["timestamp"])
	if t.IsZero() {
		return nil
	}
	ts := iso(t)
	model := func() any {
		if truth(state["model"]) {
			return state["model"]
		}
		return "unknown"
	}
	if o["type"] == "session_meta" {
		for k := range state {
			delete(state, k)
		}
		state["session"], state["source"], state["project"], state["created"] = p["id"], codexSource(p), p["cwd"], ts
		if !truth(state["project"]) {
			state["project"] = UnassignedProject
		}
		if truth(state["source"]) && truth(state["session"]) {
			origin := text(p["originator"])
			if origin == "" {
				origin = "Codex CLI"
				if state["source"] == "vscode" {
					origin = "codex_vscode"
				}
			}
			return s.exec("INSERT INTO sessions(id,project,origin,created,source) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET source=excluded.source", state["session"], state["project"], origin, ts, state["source"])
		}
		return nil
	}
	if !truth(state["source"]) || !truth(state["session"]) {
		return nil
	}
	cleared := s.clearedAt()
	allowed := (cleared == "" || ts > cleared) && ts >= text(state["created"])
	switch o["type"] {
	case "turn_context":
		state["model"] = p["model"]
		if !truth(state["model"]) {
			state["model"] = "unknown"
		}
		if truth(p["turn_id"]) {
			state["turn"] = p["turn_id"]
		}
	case "event_msg":
		switch p["type"] {
		case "task_started":
			turn := p["turn_id"]
			if !truth(turn) {
				turn = hash(text(state["session"]) + ts)
			}
			state["turn"], state["modern"] = turn, false
			if allowed {
				return s.exec("INSERT OR IGNORE INTO turns(id,session,model,started,ended,status,duration,ttft,last_seen,account) VALUES(?,?,?,?,?,?,?,?,?,?)", turn, state["session"], model(), ts, nil, "running", nil, nil, ts, s.accountAt(ts, "codex"))
			}
		case "task_complete", "turn_aborted":
			id := p["turn_id"]
			if !truth(id) {
				id = state["turn"]
			}
			if allowed && truth(id) {
				status := "completed"
				if p["type"] == "turn_aborted" {
					status = "aborted"
				} else if truth(p["error"]) {
					status = "failed"
				}
				duration, ttft := p["duration_ms"], p["time_to_first_token_ms"]
				if !validNumber(duration) {
					duration = nil
				}
				if !validNumber(ttft) {
					ttft = nil
				}
				return s.exec("UPDATE turns SET ended=?,status=?,duration=?,ttft=?,model=?,last_seen=? WHERE id=?", ts, status, duration, ttft, model(), ts, id)
			}
		case "token_count":
			if allowed {
				if e := s.addQuota(obj(p["rate_limits"]), ts, "日志", nil); e != nil {
					return e
				}
			}
			info := obj(p["info"])
			if total := obj(info["total_token_usage"]); len(total) > 0 {
				if !truth(state["modern"]) && allowed {
					prev := obj(state["total"])
					usage := Object{}
					for _, k := range []string{"input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens", "cache_write_input_tokens"} {
						usage[k] = max(0., num(total[k])-num(prev[k]))
					}
					if len(prev) > 0 && num(total["total_tokens"]) < num(prev["total_tokens"]) {
						for k, v := range obj(info["last_token_usage"]) {
							usage[k] = v
						}
					}
					if num(usage["input_tokens"])+num(usage["output_tokens"]) > 0 {
						key := ts + rawAt(raw, "payload", "info", "total_token_usage")
						if state["source"] == "cli" || state["source"] == "vscode" {
							key = text(state["session"]) + "|" + key
						}
						if e := s.addUsage("legacy:"+hash(key), usage, ts, state, "累计差分"); e != nil {
							return e
						}
					}
				}
				state["total"] = total
			}
		}
	case "token_usage_record":
		state["modern"] = true
		if truth(p["thread_token_usage"]) {
			state["total"] = p["thread_token_usage"]
		}
		if truth(p["thread_id"]) && p["thread_id"] != state["session"] {
			return nil
		}
		if allowed && truth(p["usage"]) {
			id := text(p["response_id"])
			if id == "" {
				id = hash(ts + rawAt(raw, "payload", "usage"))
			}
			r := clone(state)
			if truth(p["turn_id"]) {
				r["turn"] = p["turn_id"]
			}
			return s.addUsage(id, obj(p["usage"]), ts, r, "逐次记录")
		}
	}
	return nil
}
func (s *Store) processPi(o, state Object) error {
	t := parse(o["timestamp"])
	if t.IsZero() {
		return nil
	}
	ts := iso(t)
	if o["type"] == "session" {
		id, ok := o["id"].(string)
		if !ok || id == "" {
			return nil
		}
		project := text(o["cwd"])
		if project == "" {
			project = UnassignedProject
		}
		state["session"], state["project"], state["created"] = "pi:"+id, project, ts
		return nil
	}
	if !truth(state["session"]) {
		return nil
	}
	message := Object{}
	if o["type"] == "message" {
		message = obj(o["message"])
		if message["role"] != "assistant" {
			return nil
		}
	} else if o["type"] == "usage" {
		message = o
	}
	openai := message["provider"] == "openai" && s.piOAuth
	legacy := message["provider"] == "openai-codex"
	api := "openai-codex-responses"
	if openai {
		api = "openai-responses"
	}
	if (!openai && !legacy) || (o["type"] == "message" && message["api"] != api) || message["stopReason"] == "pending" {
		return nil
	}
	cleared := s.clearedAt()
	if cleared != "" && ts <= cleared {
		return nil
	}
	model, ok := message["model"].(string)
	if !ok || model == "" {
		return nil
	}
	u := obj(message["usage"])
	input, output := u["input"], u["output"]
	cached, write, reason := u["cacheRead"], u["cacheWrite"], u["reasoning"]
	if cached == nil {
		cached = 0.
	}
	if write == nil {
		write = 0.
	}
	if reason == nil {
		reason = 0.
	}
	for _, v := range []any{input, output, cached, write, reason} {
		if !validNumber(v) {
			return nil
		}
	}
	if num(reason) > num(output) {
		return nil
	}
	total := num(input) + num(cached) + num(write)
	if total+num(output) <= 0 {
		return nil
	}
	response, _ := message["responseId"].(string)
	if response == "" && text(o["id"]) == "" {
		return nil
	}
	key := "response:" + response
	if response == "" {
		key = jsonText([]any{o["id"], ts, message["provider"], message["model"]})
	}
	id := "pi:" + hash(key)
	existing := s.mustOne("SELECT session,account FROM usage WHERE id=?", id)
	if len(existing) > 0 && !s.replay {
		return nil
	}
	if e := s.exec("INSERT OR IGNORE INTO sessions(id,project,origin,created,source) VALUES(?,?,?,?,'pi')", state["session"], state["project"], PiOrigin, state["created"]); e != nil {
		return e
	}
	session := state["session"]
	if truth(existing["session"]) {
		session = existing["session"]
	}
	source, kind := "pi", PiKind
	if openai {
		source, kind = "pi-openai", PiOpenAIKind
	}
	account := existing["account"]
	if !truth(account) {
		account = s.accountAt(ts, source)
	}
	status := "completed"
	if message["stopReason"] == "error" {
		status = "failed"
	} else if message["stopReason"] == "aborted" {
		status = "aborted"
	}
	if e := s.exec("INSERT OR IGNORE INTO turns(id,session,model,started,ended,status,duration,ttft,last_seen,account) VALUES(?,?,?,?,?,?,?,?,?,?)", id, session, model, ts, ts, status, nil, nil, ts, account); e != nil {
		return e
	}
	return s.addUsage(id, Object{"input_tokens": total, "cached_input_tokens": cached, "output_tokens": output, "reasoning_output_tokens": reason, "cache_write_input_tokens": write}, ts, Object{"session": session, "turn": id, "model": model, "account": account}, kind)
}
