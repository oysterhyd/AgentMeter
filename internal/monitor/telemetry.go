package monitor

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Only the OTLP attributes needed for request analysis cross into the service.
// Bodies, prompts, tools, headers, account identities and raw errors are discarded.
type otlpValue struct {
	String string      `json:"stringValue"`
	Int    json.Number `json:"intValue"`
	Double *float64    `json:"doubleValue"`
	Bool   *bool       `json:"boolValue"`
}
type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}
type otlpRecord struct {
	Time       string          `json:"timeUnixNano"`
	Attributes []otlpAttribute `json:"attributes"`
}
type otlpLogs struct {
	Resources []struct {
		Scopes []struct {
			Records []otlpRecord `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
}

func telemetryConfig(port int) string {
	return fmt.Sprintf("[otel]\nlog_user_prompt = false\nexporter = { otlp-http = { endpoint = \"http://127.0.0.1:%d/v1/logs\", protocol = \"json\" } }\n", port)
}

func (s *Service) stopTelemetry() {
	if s.telemetry != nil {
		_ = s.telemetry.Close()
		s.telemetry = nil
	}
}

func (s *Service) configureTelemetry(autoPort bool) error {
	s.stopTelemetry()
	settings := s.store.settings()
	port := int(num(settings["telemetryPort"]))
	status := Object{"enabled": truth(settings["telemetryEnabled"]), "listening": false, "port": port, "config": telemetryConfig(port)}
	if s.config.Offline {
		return s.store.set("telemetryStatus", status)
	}
	// The quota bridge follows the Claude Code collector, not the TTFT switch.
	status["claudeStatusline"] = s.setupClaudeStatusline(truth(settings["claudeEnabled"]))
	if !truth(settings["telemetryEnabled"]) {
		s.setupTelemetryClients(status, false, port)
		return s.store.set("telemetryStatus", status)
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil && autoPort {
		for candidate := port + 1; candidate <= port+16 && candidate <= 65535; candidate++ {
			listener, err = net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", candidate))
			if err == nil {
				port = candidate
				if _, err = s.store.saveSettings(Object{"telemetryPort": port}); err != nil {
					listener.Close()
					return err
				}
				status["port"], status["config"] = port, telemetryConfig(port)
				break
			}
		}
	}
	if err != nil {
		status["error"] = "采集端口不可用，请选择其他端口"
		_ = s.store.set("telemetryStatus", status)
		return fmt.Errorf("采集端口不可用，请选择其他端口")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", s.receiveTelemetry)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	s.telemetry = server
	status["listening"] = true
	s.setupTelemetryClients(status, true, port)
	previous := obj(s.store.get("telemetryStatus"))
	for _, field := range []string{"receivedAt", "receivedEvents", "totalEvents", "ttftSamples"} {
		if previous[field] != nil {
			status[field] = previous[field]
		}
	}
	for _, client := range []string{"codex", "pi", "claude"} {
		state := obj(status[client])
		if truth(obj(previous[client])["restartRequired"]) || truth(state["changed"]) {
			state["restartRequired"] = true
		}
	}
	if err = s.store.set("telemetryStatus", status); err != nil {
		listener.Close()
		s.telemetry = nil
		return err
	}
	go func() { _ = server.Serve(listener) }()
	return nil
}

func (s *Service) receiveTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != "" { // This endpoint is for the local exporter, not browser scripts.
		http.Error(w, "browser origin rejected", http.StatusForbidden)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "configure OTLP HTTP protocol=json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var input io.Reader = r.Body
	if encoding := r.Header.Get("Content-Encoding"); encoding == "gzip" {
		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "invalid gzip", http.StatusBadRequest)
			return
		}
		defer reader.Close()
		input = io.LimitReader(reader, (8<<20)+1)
	} else if encoding != "" {
		http.Error(w, "unsupported encoding", http.StatusUnsupportedMediaType)
		return
	}
	data, err := io.ReadAll(input)
	if err != nil || len(data) > 8<<20 {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	records, err := decodeTelemetry(data)
	if err != nil {
		http.Error(w, "invalid OTLP JSON", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err = s.CallValue(ctx, "_telemetry", records); err != nil {
		http.Error(w, "collector unavailable; retry batch", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

func decodeTelemetry(data []byte) ([]Object, error) {
	var logs otlpLogs
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, err
	}
	result := []Object{}
	count := 0
	for _, resource := range logs.Resources {
		for _, scope := range resource.Scopes {
			for _, record := range scope.Records {
				count++
				if count > 10000 {
					return nil, fmt.Errorf("too many records")
				}
				attrs := Object{}
				for _, attr := range record.Attributes {
					switch attr.Key {
					case "event.name", "event.kind", "event.timestamp", "conversation.id", "turn.id", "model", "response.id", "response_id", "attempt", "http.response.status_code", "success", "ttft_ms", "input_token_count", "cached_token_count", "output_token_count", "reasoning_token_count", "error.message", "codex.monitor.request_id", "codex.monitor.provider",
						"session.id", "request_id", "status_code", "error", "input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens":
						v := attr.Value
						var value any = v.String
						if v.Int != "" {
							value = v.Int.String()
						}
						if v.Double != nil {
							value = *v.Double
						}
						if v.Bool != nil {
							value = strconv.FormatBool(*v.Bool)
						}
						attrs[attr.Key] = value
					}
				}
				stamp := parse(attrs["event.timestamp"])
				if stamp.IsZero() {
					if nanos, err := strconv.ParseInt(record.Time, 10, 64); err == nil && nanos > 0 {
						stamp = time.Unix(0, nanos)
					}
				}
				session := text(attrs["conversation.id"])
				switch attrs["event.name"] {
				case "api_request", "api_error", "claude_code.api_request", "claude_code.api_error":
					// Claude Code names its session and API request instead.
					session = ""
					if safeIdentifier(text(attrs["session.id"])) {
						session = "claude:" + text(attrs["session.id"])
					}
				}
				if stamp.IsZero() || !safeIdentifier(session) {
					continue
				}
				row := Object{"ts": iso(stamp), "session": session, "turn": nil, "model": "unknown", "source": "otel"}
				if strings.HasPrefix(text(row["session"]), "pi:") && attrs["codex.monitor.provider"] != "openai" && attrs["codex.monitor.provider"] != "openai-codex" {
					continue
				}
				if value := telemetryNumber(attrs["ttft_ms"]); value != nil {
					row["ttft"] = value
				}
				if attrs["codex.monitor.provider"] == "openai" || attrs["codex.monitor.provider"] == "openai-codex" {
					row["pi_provider"] = attrs["codex.monitor.provider"]
				}
				if strings.HasPrefix(text(row["session"]), "pi:") && safeIdentifier(text(attrs["codex.monitor.request_id"])) {
					row["request_key"] = attrs["codex.monitor.request_id"]
				}
				if safeIdentifier(text(attrs["turn.id"])) {
					row["turn"] = attrs["turn.id"]
				}
				if modelName.MatchString(text(attrs["model"])) {
					row["model"] = attrs["model"]
				}
				if id := firstText(attrs, "response.id", "response_id"); safeIdentifier(id) {
					row["response_id"] = id
				}
				if attempt := telemetryNumber(attrs["attempt"]); attempt != nil && num(attempt) <= 1000 && num(attempt) == float64(int(num(attempt))) {
					row["attempt"] = attempt
				}
				status := telemetryNumber(attrs["http.response.status_code"])
				if status != nil && num(status) >= 100 && num(status) <= 599 {
					row["http_status"] = status
				}
				errorMessage := text(attrs["error.message"])
				failed := errorMessage != "" || text(attrs["success"]) == "false" || num(status) >= 400 || attrs["event.kind"] == "response.failed"
				switch attrs["event.name"] {
				case "codex.monitor.retry":
					if !strings.HasPrefix(text(row["session"]), "pi:") {
						continue
					}
					row["kind"] = "retry"
				case "codex.sse_event", "codex.websocket_event":
					if failed {
						row["kind"], row["error_code"] = "failed", classifyRequestError(errorMessage, status)
					} else if attrs["event.kind"] == "response.completed" && telemetryNumber(attrs["ttft_ms"]) != nil {
						row["kind"], row["ttft"] = "completed", telemetryNumber(attrs["ttft_ms"])
						for to, from := range map[string]string{"input": "input_token_count", "cached": "cached_token_count", "output": "output_token_count", "reasoning": "reasoning_token_count"} {
							row[to] = telemetryNumber(attrs[from])
						}
					} else {
						continue
					}
				case "codex.api_request", "codex.websocket_request":
					if failed {
						row["kind"], row["error_code"] = "failed", classifyRequestError(errorMessage, status)
					} else if num(row["attempt"]) > 0 {
						row["kind"] = "retry"
					} else {
						continue
					}
				case "api_request", "claude_code.api_request":
					// Only streamed responses report TTFT; the rest are side requests
					// such as titles that have no transcript usage to time.
					if row["ttft"] == nil {
						continue
					}
					row["kind"] = "completed"
					input, output, read, write := telemetryNumber(attrs["input_tokens"]), telemetryNumber(attrs["output_tokens"]), telemetryNumber(attrs["cache_read_tokens"]), telemetryNumber(attrs["cache_creation_tokens"])
					if input != nil && output != nil && read != nil && write != nil {
						// Transcript usage counts cache reads and writes as input.
						row["input"], row["cached"], row["output"] = num(input)+num(read)+num(write), read, output
					}
				case "api_error", "claude_code.api_error":
					status = telemetryNumber(attrs["status_code"])
					if status != nil && num(status) >= 100 && num(status) <= 599 {
						row["http_status"] = status
					}
					row["kind"], row["error_code"] = "failed", classifyRequestError(text(attrs["error"]), status)
				default:
					continue
				}
				if strings.HasPrefix(session, "claude:") && safeIdentifier(text(attrs["request_id"])) {
					row["request_key"] = attrs["request_id"]
				}
				// Millisecond event timestamps can collide; retain the OTLP nanoseconds in the fingerprint.
				row["id"] = "otel:" + hash(record.Time+"|"+text(attrs["event.name"])+"|"+jsonText(row))
				result = append(result, row)
			}
		}
	}
	return result, nil
}

func telemetryNumber(v any) any {
	if validNumber(v) {
		return v
	}
	if n, err := strconv.ParseFloat(text(v), 64); err == nil && validNumber(n) {
		return n
	}
	return nil
}

func firstText(o Object, keys ...string) string {
	for _, k := range keys {
		if v := text(o[k]); v != "" {
			return v
		}
	}
	return ""
}

func safeIdentifier(s string) bool {
	if s == "" || len(s) > 160 {
		return false
	}
	for _, ch := range s {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("_:-.", ch)) {
			return false
		}
	}
	return true
}

func classifyRequestError(message string, status any) string {
	s := strings.ToLower(message)
	for _, item := range []struct {
		code     string
		patterns []string
	}{
		{"rate_limit", []string{"rate limit", "rate_limit", "too many requests"}},
		{"timeout", []string{"timed out", "timeout"}},
		{"connection", []string{"disconnected", "connection", "tls", "websocket", "stream closed"}},
		{"authentication", []string{"unauthorized", "authentication"}},
	} {
		for _, p := range item.patterns {
			if strings.Contains(s, p) {
				return item.code
			}
		}
	}
	switch int(num(status)) {
	case 401, 403:
		return "authentication"
	case 429:
		return "rate_limit"
	}
	if num(status) >= 500 {
		return "server"
	}
	return "request_error"
}
