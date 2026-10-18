package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Data, Home, PiHome, PiSessions, Version string
	// ClaudeHome is Claude Code's config directory (with projects/);
	// ClaudeConfig is its .claude.json. Empty disables Claude Code.
	ClaudeHome, ClaudeConfig string
	// Executable is the installed Monitor binary that Claude Code's statusLine
	// bridge invokes; empty leaves statusLine alone.
	Executable  string
	PiExtension []byte
	Offline                  bool
}
type Event struct {
	Name string
	Data any
}
type reply struct {
	data any
	err  error
}
type job struct {
	method string
	args   json.RawMessage
	reply  chan reply
}
type Service struct {
	config         Config
	store          *Store
	jobs           chan job
	Events         chan Event
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	release        func()
	quotaBusy      bool
	quotaBinary    string
	refreshPending []chan reply
	claudeQuota    any // the Claude Code sample last announced
	telemetry      *http.Server
}

func Start(config Config) (*Service, error) {
	release, e := AcquireProfileLock(config.Data)
	if e != nil {
		return nil, e
	}
	s, e := Open(filepath.Join(config.Data, "monitor.sqlite"))
	if e != nil {
		release()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := &Service{config: config, store: s, jobs: make(chan job, 32), Events: make(chan Event, 64), ctx: ctx, cancel: cancel, done: make(chan struct{}), release: release}
	go v.loop()
	return v, nil
}
func (s *Service) emit(name string, data any) {
	select {
	case s.Events <- Event{name, data}:
	default:
	}
}
func (s *Service) loop() {
	defer close(s.done)
	defer close(s.Events)
	defer s.release()
	defer s.store.Close()
	defer s.stopTelemetry()
	scan := time.NewTicker(3 * time.Second)
	quota := time.NewTicker(5 * time.Second)
	defer scan.Stop()
	defer quota.Stop()
	_ = s.configureTelemetry(true)
	if !s.config.Offline {
		s.scan(false)
		s.beginQuota()
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-s.jobs:
			if j.method == "refresh" && !s.config.Offline {
				s.scan(true)
				if j.reply != nil {
					s.refreshPending = append(s.refreshPending, j.reply)
				}
				s.beginQuota()
				continue
			}
			r, e := s.handle(j.method, j.args)
			if j.reply != nil {
				j.reply <- reply{r, e}
			}
		case <-scan.C:
			if !s.config.Offline {
				s.scan(false)
			}
		case <-quota.C:
			if !s.config.Offline {
				q := obj(s.store.get("quotaStatus"))
				if parse(q["attempted"]).IsZero() || time.Since(parse(q["attempted"])) >= time.Duration(num(s.store.settings()["quotaInterval"]))*time.Second {
					s.beginQuota()
				}
			}
		}
	}
}
func (s *Service) Call(ctx context.Context, method string, args any) (json.RawMessage, error) {
	value, err := s.CallValue(ctx, method, args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// CallValue transfers detached results to the in-process UI. Arguments are
// still serialized before queuing, so caller mutations cannot reach the store.
func (s *Service) CallValue(ctx context.Context, method string, args any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, e := json.Marshal(args)
	if e != nil {
		return nil, e
	}
	j := job{method: method, args: raw, reply: make(chan reply, 1)}
	select {
	case s.jobs <- j:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, errors.New("采集服务已退出")
	}
	select {
	case r := <-j.reply:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, errors.New("采集服务已退出")
	}
}
func (s *Service) Close() { s.cancel(); <-s.done }
func (s *Service) handle(method string, raw json.RawMessage) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	var value any
	var args any
	_ = json.Unmarshal(raw, &args)
	p := obj(args)
	now := time.Now()
	switch method {
	case "snapshot":
		r, e := s.store.Snapshot(p, now)
		if e != nil {
			return nil, e
		}
		var pi any
		if s.config.PiSessions != "" {
			pi = s.config.PiSessions
		}
		var claude any
		if s.config.ClaudeHome != "" {
			claude = s.config.ClaudeHome
		}
		r["paths"] = Object{"data": s.config.Data, "home": s.config.Home, "piSessions": pi, "claudeHome": claude}
		r["version"] = s.config.Version
		value = r
	case "widget":
		if n := num(p["now"]); n > 0 {
			now = time.UnixMilli(int64(n))
		}
		value = s.store.Widget(now)
	case "settings":
		previous := s.store.settings()
		value, err = s.store.saveSettings(p)
		if err == nil && (p["telemetryEnabled"] != nil || p["telemetryPort"] != nil || p["claudeEnabled"] != nil) {
			if err = s.configureTelemetry(false); err != nil {
				_, _ = s.store.saveSettings(previous)
				_ = s.configureTelemetry(false)
			}
		}
	case "telemetrySetup":
		err = s.configureTelemetry(false)
		value = s.store.get("telemetryStatus")
	case "price":
		value, err = s.store.savePrice(p)
	case "deletePrice":
		value, err = s.store.deletePrice(args)
	case "account":
		value, err = s.store.saveAccount(p)
	case "export":
		value, err = s.store.Export(p, now)
	case "clear":
		err = s.store.clear(now)
		value = true
	case "refresh":
		if !s.config.Offline {
			s.scan(true)
			s.beginQuota()
		}
		value = true
	case "notice":
		before := s.store.mustOne("SELECT id FROM notices WHERE id=?", args)
		err = s.store.exec("INSERT OR IGNORE INTO notices VALUES(?,?)", args, iso(now))
		value = len(before) == 0
	case "_quota":
		s.applyQuota(p)
		return nil, nil
	case "_scan":
		piHome, piSessions := s.config.PiHome, s.config.PiSessions
		if v, ok := p["piHome"].(string); ok {
			piHome = v
		}
		if v, ok := p["piSessions"].(string); ok {
			piSessions = v
		}
		src := s.sources()
		src.PiHome, src.PiSessions = piHome, piSessions
		_, err = s.store.ScanSources(s.ctx, src, true, func(v Object) { s.emit("progress", v) })
		value = true
	case "_telemetry":
		var events []Object
		if err = json.Unmarshal(raw, &events); err != nil {
			return nil, err
		}
		if err = s.store.ingestRequestEvents(events); err != nil {
			return nil, err
		}
		status := obj(s.store.get("telemetryStatus"))
		status["receivedAt"], status["receivedEvents"] = iso(now), len(events)
		status["totalEvents"] = num(status["totalEvents"]) + float64(len(events))
		status["ttftSamples"] = num(s.store.mustOne("SELECT COUNT(*) n FROM request_events WHERE source='otel' AND kind='completed' AND ttft IS NOT NULL")["n"])
		for _, event := range events {
			client := "codex"
			if strings.HasPrefix(text(event["session"]), "pi:") {
				client = "pi"
			}
			obj(status[client])["restartRequired"] = false
		}
		err = s.store.set("telemetryStatus", status)
		value = true
	default:
		return nil, fmt.Errorf("未知操作")
	}
	if err != nil {
		return nil, err
	}
	if method != "snapshot" && method != "widget" && method != "export" && method != "notice" {
		s.emit("updated", nil)
	}
	return value, nil
}
func (s *Service) sources() Sources {
	c := s.config
	src := Sources{Home: c.Home, PiHome: c.PiHome, PiSessions: c.PiSessions, ClaudeHome: c.ClaudeHome, ClaudeConfig: c.ClaudeConfig}
	if c.ClaudeHome != "" && c.Data != "" {
		src.ClaudeStatusline = filepath.Join(c.Data, claudeStatuslineFile)
	}
	return src
}
func (s *Service) scan(full bool) {
	defer func() {
		if recover() != nil {
			s.emit("error", "读取本机统计记录失败，将自动重试")
		}
	}()
	status, e := s.store.ScanSources(s.ctx, s.sources(), full, func(p Object) { s.emit("progress", p) })
	if e != nil {
		if s.ctx.Err() == nil {
			s.emit("error", "读取本机统计记录失败，将自动重试")
		}
		return
	}
	if truth(status["changed"]) || truth(status["full"]) || num(status["errors"]) > 0 {
		s.emit("updated", nil)
	}
	if q := obj(s.store.get("claudeQuotaStatus")); truth(q["attempted"]) && q["attempted"] != s.claudeQuota {
		s.claudeQuota = q["attempted"]
		s.emit("quota", Object{"agent": AgentClaude, "account": q["account"], "groups": s.store.claudeQuotaGroups(q["account"]), "muted": s.store.settings()["muted"]})
		s.emit("updated", nil)
	}
	if num(status["errors"]) > 0 {
		entry := Object{"at": iso(time.Now()), "code": "scan_records", "count": status["errors"], "items": status["diagnostics"]}
		file := filepath.Join(s.config.Data, "diagnostics.jsonl")
		if f, e := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
			_, _ = f.WriteString(jsonText(entry) + "\n")
			_ = f.Close()
		}
	}
}
