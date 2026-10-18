package monitor

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (s *Store) ingestRequestEvents(events []Object) error {
	return s.transaction(func() error {
		for _, event := range events {
			if err := s.recordRequestEvent(event); err != nil {
				return err
			}
		}
		return s.reconcileRequestEvents()
	})
}

func (s *Store) recordRequestEvent(e Object) error {
	stamp := parse(e["ts"])
	if stamp.IsZero() || !safeIdentifier(text(e["session"])) {
		return nil
	}
	if strings.HasPrefix(text(e["session"]), "pi:") && e["pi_provider"] == "openai" && !s.piOAuth {
		return nil
	}
	ts := iso(stamp)
	if cleared := s.clearedAt(); cleared != "" && ts <= cleared {
		return nil
	}
	kind := text(e["kind"])
	if kind != "completed" && kind != "failed" && kind != "retry" {
		return nil
	}
	model := text(e["model"])
	if !modelName.MatchString(model) {
		model = "unknown"
	}
	var turn, response any
	if safeIdentifier(text(e["turn"])) {
		turn = e["turn"]
	}
	if safeIdentifier(text(e["response_id"])) {
		response = e["response_id"]
	}
	code := text(e["error_code"])
	switch code {
	case "", "rate_limit", "timeout", "connection", "authentication", "server", "request_error":
	default:
		code = "request_error"
	}
	var requestKey any
	if safeIdentifier(text(e["request_key"])) {
		requestKey = e["request_key"]
	}
	accountSource, agent := "codex", AgentCodex
	if strings.HasPrefix(text(e["session"]), "claude:") {
		accountSource, agent = "claude", AgentClaude
	} else if strings.HasPrefix(text(e["session"]), "pi:") {
		accountSource = "pi"
		if e["pi_provider"] == "openai" {
			accountSource = "pi-openai"
		}
	}
	return s.exec(`INSERT OR IGNORE INTO request_events(id,ts,session,turn,model,kind,response_id,input,cached,output,reasoning,ttft,attempt,http_status,error_code,source,request_key,account,agent)
        VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e["id"], ts, e["session"], turn, model, kind, response,
		telemetryNumber(e["input"]), telemetryNumber(e["cached"]), telemetryNumber(e["output"]), telemetryNumber(e["reasoning"]), telemetryNumber(e["ttft"]), telemetryNumber(e["attempt"]), telemetryNumber(e["http_status"]), code, e["source"], requestKey, s.accountAt(ts, accountSource), agent)
}

// Current Codex OTel logs have a conversation ID and token signature but no
// response ID. Match only a unique nearby usage record, never the nearest of
// several ambiguous candidates. Exact response-ID association takes precedence.
func (s *Store) reconcileRequestEvents() error {
	if err := s.exec(`UPDATE request_events AS e SET account=(SELECT u.account FROM usage u WHERE u.id=e.usage_id),turn=(SELECT u.turn FROM usage u WHERE u.id=e.usage_id)
        WHERE e.usage_id IS NOT NULL AND EXISTS(SELECT 1 FROM usage u WHERE u.id=e.usage_id AND (u.account IS NOT e.account OR u.turn IS NOT e.turn))`); err != nil {
		return err
	}
	for _, event := range s.mustQuery("SELECT id,ts,session,model FROM request_events WHERE kind!='completed' AND turn IS NULL AND substr(session,1,3)!='pi:' ORDER BY ts DESC LIMIT 2000") {
		rows := s.mustQuery("SELECT id,account,model FROM turns WHERE session=? AND started<=? AND (ended>=? OR (ended IS NULL AND status='running')) LIMIT 2", event["session"], event["ts"], event["ts"])
		if len(rows) == 1 {
			if err := s.exec("UPDATE request_events SET turn=?,account=?,association='turn_time',model=CASE WHEN model='unknown' THEN ? ELSE model END WHERE id=?", rows[0]["id"], rows[0]["account"], rows[0]["model"], event["id"]); err != nil {
				return err
			}
		}
	}
	for _, event := range s.mustQuery("SELECT * FROM request_events WHERE kind='completed' AND usage_id IS NULL ORDER BY ts DESC LIMIT 2000") {
		var rows []Object
		association := "response_id"
		if strings.HasPrefix(text(event["session"]), "claude:") {
			// Claude Code's transcript records the same API request ID.
			association = "request_id"
			if truth(event["request_key"]) {
				rows = s.mustQuery("SELECT id,turn,account FROM usage WHERE session=? AND request_id=? LIMIT 2", event["session"], event["request_key"])
			}
		} else if truth(event["response_id"]) {
			id := text(event["response_id"])
			if strings.HasPrefix(text(event["session"]), "pi:") {
				id = "pi:" + hash("response:"+id)
			}
			rows = s.mustQuery("SELECT id,turn,account FROM usage WHERE id=? AND session=?", id, event["session"])
		} else if event["input"] != nil && event["output"] != nil && event["cached"] != nil {
			association = "time_tokens"
			stamp := parse(event["ts"])
			conditions := "session=? AND ts>=? AND ts<=? AND input=? AND output=? AND cached=?"
			args := []any{event["session"], iso(stamp.Add(-2 * time.Second)), iso(stamp.Add(2 * time.Second)), event["input"], event["output"], event["cached"]}
			if truth(event["turn"]) {
				conditions += " AND turn=?"
				args = append(args, event["turn"])
			}
			if event["model"] != "unknown" {
				// Missing rollout metadata must not prevent an otherwise unique
				// token/time match. Known conflicting models remain excluded.
				conditions += " AND (model=? OR model IN ('','unknown') OR model IS NULL)"
				args = append(args, event["model"])
			}
			rows = s.mustQuery("SELECT id,turn,account FROM usage WHERE "+conditions+" LIMIT 2", args...)
		}
		if len(rows) == 0 && strings.HasPrefix(text(event["session"]), "pi:") && event["input"] != nil && event["output"] != nil && event["cached"] != nil {
			association = "time_tokens"
			stamp := parse(event["ts"])
			rows = s.mustQuery("SELECT id,turn,account FROM usage WHERE session=? AND ts>=? AND ts<=? AND input=? AND output=? AND cached=? LIMIT 2", event["session"], iso(stamp.Add(-2*time.Second)), iso(stamp.Add(2*time.Second)), event["input"], event["output"], event["cached"])
		}
		if len(rows) != 1 {
			continue
		}
		row := rows[0]
		if err := s.exec("UPDATE OR IGNORE request_events SET usage_id=?,turn=?,account=?,association=? WHERE id=?", row["id"], row["turn"], row["account"], association, event["id"]); err != nil {
			return err
		}
	}
	// Only an explicitly associated completion can repair a missing model.
	// Never propagate a session's latest model across unrelated requests.
	if err := s.exec(`UPDATE usage AS u SET model=(SELECT e.model FROM request_events e WHERE e.usage_id=u.id AND e.model NOT IN ('','unknown'))
        WHERE (u.model IS NULL OR u.model IN ('','unknown')) AND EXISTS(SELECT 1 FROM request_events e WHERE e.usage_id=u.id AND e.model NOT IN ('','unknown'))`); err != nil {
		return err
	}
	// Pi can emit a failed provider attempt before the successful response has an ID.
	// Its explicit request key joins those events to the eventual message/turn.
	return s.exec(`UPDATE request_events AS e SET turn=(SELECT c.turn FROM request_events c WHERE c.kind='completed' AND c.usage_id IS NOT NULL AND c.session=e.session AND c.request_key=e.request_key LIMIT 1),
        account=(SELECT c.account FROM request_events c WHERE c.kind='completed' AND c.usage_id IS NOT NULL AND c.session=e.session AND c.request_key=e.request_key LIMIT 1)
        WHERE e.kind!='completed' AND e.request_key IS NOT NULL AND EXISTS(SELECT 1 FROM request_events c WHERE c.kind='completed' AND c.usage_id IS NOT NULL AND c.session=e.session AND c.request_key=e.request_key)`)
}

var diagnosticField = regexp.MustCompile(`(?:^|[\s{])([\w.]+)=(?:"([^"\r\n]*)"|([^\s}:]+))`)
var diagnosticDatabase = regexp.MustCompile(`^logs_(\d+)\.sqlite$`)

func decodeDiagnostic(row Object, fileID string) Object {
	target, body := text(row["target"]), text(row["feedback_log_body"])
	if target != "codex_core::responses_retry" && target != "codex_core::codex" {
		return nil
	}
	if !strings.Contains(body, "retrying sampling request") && !strings.Contains(body, "stream disconnected - retrying") {
		return nil
	}
	fields := Object{}
	for _, match := range diagnosticField.FindAllStringSubmatch(body, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		switch match[1] {
		case "thread_id", "conversation.id", "turn_id", "turn.id", "model", "retries":
			fields[match[1]] = value
		}
	}
	session := firstText(row, "thread_id")
	if session == "" {
		session = firstText(fields, "thread_id", "conversation.id")
	}
	if !safeIdentifier(session) {
		return nil
	}
	stamp := time.Unix(int64(num(row["ts"])), int64(num(row["ts_nanos"])))
	event := Object{"ts": iso(stamp), "session": session, "turn": firstText(fields, "turn_id", "turn.id"), "model": fields["model"], "kind": "retry", "attempt": telemetryNumber(fields["retries"]), "error_code": classifyRequestError(body, nil), "source": "diagnostic"}
	event["id"] = "diagnostic:" + hash(fileID+"|"+text(row["id"])+"|"+text(row["ts_nanos"])+"|"+jsonText(event))
	return event
}

// Read only Codex's diagnostic SQLite files; the cursor and sanitized events
// live in the monitor profile. A missing/unsupported database does not stop usage scans.
func (s *Store) scanRequestDiagnostics(ctx context.Context, home string) (int, Object) {
	status := Object{"available": false, "events": 0}
	entries, err := os.ReadDir(home)
	if err != nil {
		return 0, status
	}
	file := ""
	version := -1
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := diagnosticDatabase.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		n, _ := strconv.Atoi(match[1])
		if n > version {
			version = n
			file = filepath.Join(home, entry.Name())
		}
	}
	if file == "" {
		return 0, status
	}
	absolute, err := filepath.Abs(file)
	if err != nil {
		return 0, status
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		status["error"] = "diagnostics_unavailable"
		return 0, status
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var maximum int64
	if err = db.QueryRowContext(readCtx, "SELECT COALESCE(MAX(id),0) FROM logs").Scan(&maximum); err != nil {
		status["error"] = "diagnostics_unavailable"
		return 0, status
	}
	key := "diagnosticCursor:" + hash(absolute)
	previous := obj(s.get(key))
	cursor := int64(num(previous["id"]))
	if maximum < cursor {
		cursor = 0
	}
	if cursor > 0 && previous["ts"] != nil {
		var stamp, nanos int64
		if err := db.QueryRowContext(readCtx, "SELECT ts,ts_nanos FROM logs WHERE id=?", cursor).Scan(&stamp, &nanos); err == nil && (stamp != int64(num(previous["ts"])) || nanos != int64(num(previous["nanos"]))) {
			cursor = 0
		}
	}
	rows, err := db.QueryContext(readCtx, `SELECT id,ts,ts_nanos,target,feedback_log_body,thread_id FROM logs
        WHERE id>? AND id<=? AND target IN ('codex_core::responses_retry','codex_core::codex') ORDER BY id LIMIT 2000`, cursor, maximum)
	if err != nil {
		status["error"] = "diagnostics_schema"
		return 0, status
	}
	events := []Object{}
	read := 0
	last := cursor
	for rows.Next() {
		var id, ts, nanos int64
		var target string
		var body, thread sql.NullString
		if err = rows.Scan(&id, &ts, &nanos, &target, &body, &thread); err != nil {
			break
		}
		last = id
		read++
		if event := decodeDiagnostic(Object{"id": id, "ts": ts, "ts_nanos": nanos, "target": target, "feedback_log_body": body.String, "thread_id": thread.String}, hash(absolute)[:16]); event != nil {
			events = append(events, event)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		status["error"] = "diagnostics_read"
		return 0, status
	}
	if read < 2000 {
		last = maximum
	}
	count := 0
	err = s.transaction(func() error {
		for _, event := range events {
			existing := len(s.mustOne("SELECT 1 FROM request_events WHERE id=?", event["id"])) > 0
			if err := s.recordRequestEvent(event); err != nil {
				return err
			}
			if !existing && len(s.mustOne("SELECT 1 FROM request_events WHERE id=?", event["id"])) > 0 {
				count++
			}
		}
		var stamp, nanos int64
		if last > 0 {
			if err := db.QueryRowContext(readCtx, "SELECT ts,ts_nanos FROM logs WHERE id=?", last).Scan(&stamp, &nanos); err != nil {
				return err
			}
		}
		return s.set(key, Object{"id": last, "ts": stamp, "nanos": nanos})
	})
	if err != nil {
		status["error"] = "diagnostics_store"
		return 0, status
	}
	status["available"], status["events"], status["pending"] = true, count, read == 2000
	return count, status
}

func requestErrorLabel(code string) string {
	return map[string]string{"rate_limit": "限流", "timeout": "超时", "connection": "连接中断", "authentication": "认证失败", "server": "服务错误", "request_error": "请求错误"}[code]
}

func requestTimingNote(association string) string {
	if association == "response_id" {
		return "按响应 ID 关联"
	}
	if association == "request_id" {
		return "按请求 ID 关联"
	}
	if association == "time_tokens" {
		return "按时间与 Token 唯一匹配"
	}
	return "未采集首字时间"
}
