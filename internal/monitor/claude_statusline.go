package monitor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// Claude Code refreshes cachedUsageUtilization only when it calls its usage
// endpoint, but hands rate_limits to the statusLine command after every
// response. Monitor owns statusLine with a bridge that keeps those two windows
// and then runs the user's own command, so its output stays unchanged.

const (
	ClaudeStatuslineSource = "Claude Code 状态栏"
	claudeStatuslineFile   = "claude-statusline.json"
	claudeStatuslineFlag   = "--claude-statusline"
	// claudeStatuslineEvery limits unchanged samples to one row set a minute,
	// matching the Codex quota query interval.
	claudeStatuslineEvery = time.Minute
)

// findGitBash locates the Git Bash that Claude Code runs statusLine commands
// with. Without it Claude Code uses PowerShell, whose quoting the bridge does
// not support, so the user's statusLine is then left alone.
var findGitBash = func() string {
	exists := func(p string) bool { info, e := os.Stat(p); return e == nil && !info.IsDir() }
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" && exists(p) {
		return p
	}
	if git, e := exec.LookPath("git"); e == nil {
		dir := filepath.Dir(git) // Git\cmd, Git\bin or Git\mingw64\bin
		for _, p := range []string{filepath.Join(dir, "..", "bin", "bash.exe"), filepath.Join(dir, "..", "..", "bin", "bash.exe")} {
			if exists(p) {
				return filepath.Clean(p)
			}
		}
	}
	for _, key := range []string{"ProgramFiles", "ProgramW6432", "LOCALAPPDATA"} {
		if base := os.Getenv(key); base != "" {
			for _, p := range []string{filepath.Join(base, "Git", "bin", "bash.exe"), filepath.Join(base, "Programs", "Git", "bin", "bash.exe")} {
				if exists(p) {
					return p
				}
			}
		}
	}
	return ""
}

type statuslineWindow struct {
	Used     *float64 `json:"used_percentage"`
	ResetsAt *float64 `json:"resets_at"`
}

// claudeStatuslineSample is all the bridge keeps from the statusLine input;
// session, workspace, model and cost fields are never written.
type claudeStatuslineSample struct {
	At       int64             `json:"at"`
	FiveHour *statuslineWindow `json:"five_hour,omitempty"`
	SevenDay *statuslineWindow `json:"seven_day,omitempty"`
}

func (w *statuslineWindow) valid() bool {
	return w != nil && w.Used != nil && validNumber(*w.Used) && *w.Used >= 0 && (w.ResetsAt == nil || validNumber(*w.ResetsAt))
}

// saveClaudeStatusline writes the rate-limit windows from one statusLine input.
func saveClaudeStatusline(data string, input []byte, now time.Time) error {
	var in struct {
		RateLimits *struct {
			FiveHour *statuslineWindow `json:"five_hour"`
			SevenDay *statuslineWindow `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if json.Unmarshal(input, &in) != nil || in.RateLimits == nil {
		return nil
	}
	sample := claudeStatuslineSample{At: now.UnixMilli()}
	if in.RateLimits.FiveHour.valid() {
		sample.FiveHour = in.RateLimits.FiveHour
	}
	if in.RateLimits.SevenDay.valid() {
		sample.SevenDay = in.RateLimits.SevenDay
	}
	if sample.FiveHour == nil && sample.SevenDay == nil {
		return nil
	}
	b, _ := json.Marshal(sample)
	if err := os.MkdirAll(data, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(data, ".claude-statusline-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		// Concurrent sessions race here; any of their samples is current.
		err = os.Rename(f.Name(), filepath.Join(data, claudeStatuslineFile))
	}
	return err
}

// RunClaudeStatusline is the statusLine command Monitor installs. Recording is
// best effort; the wrapped command always receives the original input.
func RunClaudeStatusline(data, wrapped string, stdin io.Reader, stdout, stderr io.Writer) int {
	input, _ := io.ReadAll(io.LimitReader(stdin, 4<<20))
	_ = saveClaudeStatusline(data, input, time.Now())
	command, err := decodeStatuslineCommand(wrapped)
	if err != nil || command == "" {
		return 0
	}
	bash := findGitBash()
	if bash == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(input), stdout, stderr
	hideProcess(cmd)
	if err = cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return 1
	}
	return 0
}

func decodeStatuslineCommand(v string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(v)
	return string(b), err
}

// bashQuote double-quotes a Windows path for Git Bash with forward slashes.
func bashQuote(path string) (string, bool) {
	path = filepath.ToSlash(path)
	return `"` + path + `"`, !strings.ContainsAny(path, "\"$`\\\r\n")
}

// claudeStatuslineCommand builds the bridge command that wraps original.
func claudeStatuslineCommand(executable, data, original string) (string, bool) {
	exe, ok := bashQuote(executable)
	if !ok {
		return "", false
	}
	command := exe + " " + claudeStatuslineFlag
	if original != "" {
		command += " " + claudeStatuslineFlag + "-command=" + base64.RawURLEncoding.EncodeToString([]byte(original))
	}
	if data != "" {
		dir, ok := bashQuote(data)
		if !ok {
			return "", false
		}
		command += " --data " + dir
	}
	return command, true
}

// bridgedStatusline reports whether command is a Monitor bridge, returning the
// command it wraps.
func bridgedStatusline(command string) (original string, ok bool) {
	if !strings.Contains(command, " "+claudeStatuslineFlag) {
		return "", false
	}
	for _, field := range strings.Fields(command) {
		if v, found := strings.CutPrefix(field, claudeStatuslineFlag+"-command="); found {
			original, err := decodeStatuslineCommand(v)
			return original, err == nil
		}
	}
	return "", true
}

// setJSONMember sets one member of a raw object in place, keeping the order
// of the others; a new member goes last.
func setJSONMember(raw json.RawMessage, key string, value any) json.RawMessage {
	members, _ := jsonMembers(raw)
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false) // keep shell operators such as && readable
	_ = e.Encode(value)
	encoded := json.RawMessage(bytes.TrimSpace(b.Bytes()))
	for i := range members {
		if members[i].key == key {
			members[i].value = encoded
			return encodeMembers(members)
		}
	}
	return encodeMembers(append(members, jsonMember{key, encoded}))
}

func compactJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if raw == nil || json.Compact(&out, raw) != nil {
		return string(raw)
	}
	return out.String()
}

// replaceClaudeSetting sets (non-nil) or removes (nil) one top-level member of
// settings.json, preserving everything else like replaceClaudeEnv.
func replaceClaudeSetting(raw []byte, key string, value json.RawMessage) ([]byte, error) {
	invalid := fmt.Errorf("Claude Code 配置格式有误，已保留原文件")
	data := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	empty := len(bytes.TrimSpace(data)) == 0
	if empty {
		data = []byte("{\n}\n")
	}
	members, err := jsonMembers(data)
	if err != nil {
		return nil, invalid
	}
	i := -1
	for j, m := range members {
		if m.key == key {
			i = j
		}
	}
	switch {
	case value == nil && i >= 0:
		members = append(members[:i], members[i+1:]...)
	case value != nil:
		if i >= 0 {
			members[i].value = value
		} else {
			members = append(members, jsonMember{key, value})
		}
	}
	result, err := formatClaudeSettings(raw, data, empty, members)
	if err != nil {
		return nil, invalid
	}
	var before, after Object
	if json.Unmarshal(data, &before) != nil || json.Unmarshal(bytes.TrimPrefix(result, []byte{0xef, 0xbb, 0xbf}), &after) != nil {
		return nil, invalid
	}
	delete(before, key)
	delete(after, key)
	if !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("状态栏配置校验失败，已保留原文件")
	}
	return result, nil
}

// setupClaudeStatusline installs (enabled) or restores (disabled) the bridge.
func (s *Service) setupClaudeStatusline(enabled bool) Object {
	if s.config.ClaudeHome == "" {
		return Object{"state": "missing"}
	}
	if info, err := os.Stat(s.config.ClaudeHome); err != nil || !info.IsDir() {
		return Object{"state": "missing"}
	}
	path := filepath.Join(s.config.ClaudeHome, "settings.json")
	result := Object{"state": "ready", "path": path, "changed": false}
	fail := func(message string) Object { result["state"], result["error"] = "error", message; return result }
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fail("无法读取 Claude Code 配置，请检查文件权限")
	}
	// statusLine is kept as raw members so a restore keeps its key order.
	var current json.RawMessage
	var line Object
	if data := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})); len(data) != 0 {
		members, err := jsonMembers(data)
		if err != nil {
			return fail("Claude Code 配置格式有误，已保留原文件")
		}
		for _, m := range members {
			if m.key == "statusLine" {
				current = m.value
			}
		}
		if current != nil && (json.Unmarshal(current, &line) != nil || line == nil) {
			return fail("Claude Code 配置格式有误，已保留原文件")
		}
	}
	exists := current != nil
	command, _ := line["command"].(string)
	binding := obj(s.store.get("claudeStatuslineBinding"))
	var desired json.RawMessage
	if !enabled {
		result["state"] = "disabled"
		if binding["path"] != path {
			return result
		}
		if command != binding["expected"] {
			return fail("Claude Code 状态栏已手动修改，已保留当前配置")
		}
		if previous, ok := binding["previous"].(string); ok {
			desired = json.RawMessage(previous)
		}
	} else {
		bash, executable := findGitBash(), s.config.Executable
		if bash == "" || executable == "" || strings.HasPrefix(strings.ToLower(executable), strings.ToLower(filepath.Clean(os.TempDir())+string(filepath.Separator))) {
			// Without Git Bash, or from a temporary `go run` build, the
			// command would not survive; the cached snapshot still works.
			result["state"] = "unavailable"
			return result
		}
		if exists && line["type"] != "command" {
			return fail("Claude Code 状态栏不是命令类型，已保留原配置")
		}
		original, ours := bridgedStatusline(command)
		if binding["path"] != path || (!ours && command != binding["expected"]) {
			// First takeover, a lost binding, or a statusLine the user replaced.
			var previous any
			if exists {
				previous = string(current)
				if ours && original == "" {
					previous = nil
				} else if ours {
					previous = string(setJSONMember(current, "command", original))
				}
			}
			binding = Object{"path": path, "previous": previous}
			// Save ownership before modifying the file, so a crash remains recoverable.
			if err = s.store.set("claudeStatuslineBinding", binding); err != nil {
				return fail("无法保存状态栏配置状态")
			}
		}
		next := json.RawMessage(`{"type":"command"}`)
		original = ""
		if previous, ok := binding["previous"].(string); ok {
			next = setJSONMember(json.RawMessage(previous), "type", "command")
			var p Object
			_ = json.Unmarshal([]byte(previous), &p)
			original, _ = p["command"].(string)
		}
		data := s.config.Data
		if filepath.Clean(data) == filepath.Join(os.Getenv("APPDATA"), "codex-monitor") {
			data = ""
		}
		bridge, ok := claudeStatuslineCommand(executable, data, original)
		if !ok {
			result["state"] = "unavailable"
			return result
		}
		desired, binding["expected"] = setJSONMember(next, "command", bridge), bridge
	}
	if compactJSON(current) != compactJSON(desired) {
		after, err := replaceClaudeSetting(raw, "statusLine", desired)
		if err != nil {
			return fail(err.Error())
		}
		if err = writeClientFile(path, raw, after, true); err != nil {
			return fail(err.Error())
		}
		result["changed"] = true
	}
	if !enabled {
		binding = Object{}
	}
	if err = s.store.set("claudeStatuslineBinding", binding); err != nil {
		return fail("无法保存状态栏配置状态")
	}
	return result
}

// claudeStatuslineCache remembers the last sample file read and recorded.
type claudeStatuslineCache struct {
	size     int64
	mtime    time.Time
	sample   *claudeStatuslineSample
	recorded time.Time
	key      string
}

// readClaudeStatusline returns the newest bridge sample, rereading the file
// only when it changed. A half-replaced file keeps the previous sample.
func (s *Store) readClaudeStatusline(path string) *claudeStatuslineSample {
	c := &s.claudeStatusline
	st, err := os.Stat(path)
	if path == "" || err != nil {
		return c.sample
	}
	if c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.sample
	}
	b, err := os.ReadFile(path)
	var sample claudeStatuslineSample
	if err != nil || json.Unmarshal(b, &sample) != nil || sample.At <= 0 {
		return c.sample
	}
	c.size, c.mtime = st.Size(), st.ModTime()
	if c.sample == nil || sample.At > c.sample.At {
		c.sample = &sample
	}
	return c.sample
}

// observeClaudeStatusline records the latest bridge sample for account,
// returning the timestamp of the newest recorded sample.
func (s *Store) observeClaudeStatusline(path, account, plan string) (string, error) {
	c := &s.claudeStatusline
	sample := s.readClaudeStatusline(path)
	if sample == nil {
		return "", nil
	}
	at := time.UnixMilli(sample.At)
	key := account + "|" + jsonText(sample.FiveHour) + "|" + jsonText(sample.SevenDay)
	if key == c.key && at.Sub(c.recorded) < claudeStatuslineEvery {
		return iso(c.recorded), nil
	}
	ts := iso(at)
	if cleared := s.clearedAt(); cleared != "" && ts <= cleared {
		return "", nil
	}
	for _, w := range []struct {
		slot    string
		minutes float64
		window  *statuslineWindow
	}{{"primary", 300, sample.FiveHour}, {"secondary", 10080, sample.SevenDay}} {
		if !w.window.valid() {
			continue
		}
		var resets any
		if w.window.ResetsAt != nil && *w.window.ResetsAt > 0 {
			resets = *w.window.ResetsAt
		}
		used := min(100., *w.window.Used)
		id := hash(account + "|" + ts + "|claude|" + w.slot + "|" + text(used) + "|" + text(resets))
		var p any
		if plan != "" {
			p = plan
		}
		if e := s.exec("INSERT OR IGNORE INTO quotas(id,ts,bucket,slot,used,minutes,resets,plan,source,account,agent) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, ts, "claude", w.slot, used, w.minutes, resets, p, ClaudeStatuslineSource, account, AgentClaude); e != nil {
			return "", e
		}
	}
	c.key, c.recorded = key, at
	return ts, nil
}
