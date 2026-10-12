package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func (s *Service) findCodex(override string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) || strings.ToLower(filepath.Ext(override)) != ".exe" {
			return "", fmt.Errorf("请选择有效的 codex.exe 绝对路径")
		}
		if _, e := os.Stat(override); e != nil {
			return "", fmt.Errorf("请选择有效的 codex.exe 绝对路径")
		}
		return override, nil
	}
	if s.quotaBinary != "" {
		if _, e := os.Stat(s.quotaBinary); e == nil {
			return s.quotaBinary, nil
		}
	}
	probe := func(file string) bool {
		if _, e := os.Stat(file); e != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, file, "--version")
		hideProcess(cmd)
		if cmd.Run() == nil {
			s.quotaBinary = file
			return true
		}
		return false
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		file := filepath.Join(dir, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe")
		if probe(file) {
			return file, nil
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", "Get-AppxPackage *OpenAI.Codex* | Select-Object -ExpandProperty InstallLocation")
	hideProcess(cmd)
	out, e := cmd.Output()
	if e == nil {
		dirs := strings.Split(strings.TrimSpace(string(out)), "\n")
		for i := len(dirs) - 1; i >= 0; i-- {
			file := filepath.Join(strings.TrimSpace(dirs[i]), "app", "resources", "codex.exe")
			if probe(file) {
				return file, nil
			}
		}
	}
	return "", fmt.Errorf("未找到 Codex App Server，请在设置中选择桌面端随附的 codex.exe")
}
func (s *Service) readQuota(executable string) (Object, error) {
	binary, e := s.findCodex(executable)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(s.ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server")
	hideProcess(cmd)
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(v), "CODEX_HOME=") {
			env = append(env, v)
		}
	}
	cmd.Env = append(env, "CODEX_HOME="+s.config.Home)
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return nil, fmt.Errorf("无法启动 Codex App Server")
	}
	defer func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	send := func(v Object) error { _, e := in.Write([]byte(jsonText(v) + "\n")); return e }
	if e = send(Object{"id": 1, "method": "initialize", "params": Object{"clientInfo": Object{"name": "codex_monitor", "title": "Codex Monitor", "version": "0.1.0"}}}); e != nil {
		return nil, e
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	for scanner.Scan() {
		var message Object
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		switch num(message["id"]) {
		case 1:
			if message["error"] != nil {
				return nil, fmt.Errorf("App Server 初始化失败")
			}
			if e = send(Object{"method": "initialized"}); e != nil {
				return nil, e
			}
			if e = send(Object{"id": 2, "method": "account/rateLimits/read"}); e != nil {
				return nil, e
			}
		case 2:
			if message["error"] != nil {
				return nil, fmt.Errorf("%s", quotaError(jsonText(message["error"])))
			}
			return Object{"result": message["result"], "timestamp": iso(time.Now())}, nil
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("额度查询超时；保留上次快照，稍后重试")
	}
	return nil, fmt.Errorf("Codex App Server 提前退出，请检查登录状态")
}

var httpStatus = regexp.MustCompile(`\b(401|403|429|500|502|503|504)\b`)

func quotaError(message string) string {
	status := httpStatus.FindString(message)
	m := strings.ToLower(message)
	reason := "额度服务暂时不可用"
	switch {
	case status == "401" || strings.Contains(m, "unauthenticated") || strings.Contains(m, "not logged in") || strings.Contains(m, "authentication required"):
		reason = "登录状态已失效，请在 Codex 中重新登录"
	case status == "403":
		reason = "额度服务拒绝访问"
	case status == "429":
		reason = "额度查询受到限流，请稍后重试"
	case strings.Contains(m, "timeout") || strings.Contains(m, "timed out"):
		reason = "额度查询超时"
	case strings.Contains(m, "connect") || strings.Contains(m, "network") || strings.Contains(m, "dns") || strings.Contains(m, "request") || strings.Contains(m, "fetch"):
		reason = "无法连接额度服务，请检查网络"
	}
	if status != "" {
		reason += "（HTTP " + status + "）"
	}
	return reason + "；保留最近快照"
}
func (s *Service) beginQuota() {
	if s.quotaBusy || s.ctx.Err() != nil {
		return
	}
	s.quotaBusy = true
	before := ReadAccount(s.config.Home)
	account := Unknown
	if before != nil {
		account = text(before["id"])
	}
	executable := text(s.store.settings()["codexExecutable"])
	go func() {
		r, e := s.readQuota(executable)
		args := Object{"account": account, "value": r}
		if e != nil {
			args["error"] = e.Error()
		}
		select {
		case s.jobs <- job{method: "_quota", args: json.RawMessage(jsonText(args))}:
		case <-s.ctx.Done():
		}
	}()
}
func (s *Service) applyQuota(p Object) {
	defer func() {
		for _, ch := range s.refreshPending {
			ch <- reply{data: true}
		}
		s.refreshPending = nil
	}()
	s.quotaBusy = false
	account := Unknown
	if a := ReadAccount(s.config.Home); a != nil {
		account = text(a["id"])
	}
	if p["account"] != account {
		s.emit("updated", nil)
		return
	}
	if truth(p["error"]) {
		_ = s.store.set("quotaStatus", Object{"account": account, "ok": false, "attempted": iso(time.Now()), "reason": p["error"]})
		s.emit("updated", nil)
		return
	}
	v := obj(p["value"])
	result := obj(v["result"])
	groups := obj(result["rateLimitsByLimitId"])
	if len(groups) == 0 {
		groups = Object{"codex": result["rateLimits"]}
	}
	list := []Object{}
	for _, r := range groups {
		q := obj(r)
		_ = s.store.addQuota(q, text(v["timestamp"]), "在线查询", account)
		list = append(list, q)
	}
	_ = s.store.set("quotaStatus", Object{"ok": true, "attempted": v["timestamp"], "account": account})
	s.emit("quota", Object{"account": account, "groups": list, "muted": s.store.settings()["muted"]})
	s.emit("updated", nil)
}
