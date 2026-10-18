package monitor

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

const piManagedHeader = "// Managed by Codex Monitor: local first-token collection.\n"

var piLocalEndpoint = regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+/v1/logs`)

// Parse TOML before editing. Remove only root OTel expressions using AST ranges,
// so comments, multiline strings and all other client settings keep their bytes.
func replaceOTel(raw []byte, otel Object) ([]byte, error) {
	data := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	var before Object
	if err := toml.Unmarshal(data, &before); err != nil {
		return nil, fmt.Errorf("Codex 配置格式有误，已保留原文件")
	}
	var parser unstable.Parser
	parser.Reset(data)
	var table []string
	var ranges [][2]int
	for parser.NextExpression() {
		n := parser.Expression()
		if n.Kind != unstable.KeyValue && n.Kind != unstable.Table && n.Kind != unstable.ArrayTable {
			continue
		}
		var key []string
		it := n.Key()
		for it.Next() {
			key = append(key, string(it.Node().Data))
		}
		start, end := int(n.Raw.Offset), int(n.Raw.Offset+n.Raw.Length)
		if n.Kind != unstable.KeyValue {
			table = key
			start = int(n.Child().Raw.Offset)
			start -= len(data[:start]) - (bytes.LastIndexByte(data[:start], '\n') + 1)
			end = start
		} else if len(table) != 0 {
			key = append(append([]string{}, table...), key...)
		}
		if len(key) == 0 || key[0] != "otel" {
			continue
		}
		// Include the rest of the expression's last line, but never following data.
		if i := bytes.IndexByte(data[end:], '\n'); i >= 0 {
			end += i + 1
		} else {
			end = len(data)
		}
		ranges = append(ranges, [2]int{start, end})
	}
	if parser.Error() != nil {
		return nil, fmt.Errorf("Codex 配置格式有误，已保留原文件")
	}
	result := append([]byte{}, data...)
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		result = append(result[:r[0]], result[r[1]:]...)
	}
	if otel != nil {
		encoded, err := toml.Marshal(Object{"otel": otel})
		if err != nil {
			return nil, fmt.Errorf("无法生成本机采集配置")
		}
		if bytes.Contains(data, []byte("\r\n")) {
			encoded = bytes.ReplaceAll(encoded, []byte("\n"), []byte("\r\n"))
		}
		result = append(result, '\n')
		result = append(result, encoded...)
	}
	var after Object
	if err := toml.Unmarshal(result, &after); err != nil {
		return nil, fmt.Errorf("采集配置校验失败，已保留原文件")
	}
	delete(before, "otel")
	delete(after, "otel")
	if !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("采集配置校验失败，已保留原文件")
	}
	if len(data) != len(raw) {
		result = append([]byte{0xef, 0xbb, 0xbf}, result...)
	}
	return result, nil
}

func localExporter(port int) Object {
	return Object{"otlp-http": Object{"endpoint": fmt.Sprintf("http://127.0.0.1:%d/v1/logs", port), "protocol": "json"}}
}

func compatibleExporter(v any) bool {
	if v == nil || v == "none" {
		return true
	}
	exporter := obj(v)
	http := obj(exporter["otlp-http"])
	u, err := url.Parse(text(http["endpoint"]))
	return len(exporter) == 1 && len(http) == 2 && http["protocol"] == "json" && err == nil &&
		u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.Port() != "" && u.Path == "/v1/logs" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

// Backups stay beside the client configuration. Only the two managed, non-secret
// fields are remembered in Monitor; never store the whole config in its database.
func writeClientFile(path string, before, after []byte, backup bool) error {
	if bytes.Equal(before, after) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("无法写入客户端配置，请检查目录权限")
	}
	if backup && len(before) != 0 {
		file, err := os.OpenFile(path+".codex-monitor.bak", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = file.Write(before)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				return fmt.Errorf("备份客户端配置失败，已保留原文件")
			}
		} else if !os.IsExist(err) {
			return fmt.Errorf("备份客户端配置失败，已保留原文件")
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".codex-monitor-*")
	if err != nil {
		return fmt.Errorf("无法写入客户端配置，请检查目录权限")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(after)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("写入客户端配置失败，已保留原文件")
	}
	current, err := os.ReadFile(path)
	if (err != nil && !os.IsNotExist(err)) || !bytes.Equal(current, before) {
		return fmt.Errorf("客户端配置刚被修改，请点击重新检查")
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("替换客户端配置失败，已保留原文件")
	}
	return nil
}

func (s *Service) setupCodex(enabled bool, port int) Object {
	if s.config.Home == "" {
		return Object{"state": "missing"}
	}
	path := filepath.Join(s.config.Home, "config.toml")
	result := Object{"state": "ready", "path": path, "changed": false}
	fail := func(message string) Object { result["state"], result["error"] = "error", message; return result }
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fail("无法读取 Codex 配置，请检查文件权限")
	}
	var config Object
	if err = toml.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &config); err != nil {
		return fail("Codex 配置格式有误，已保留原文件")
	}
	otel := clone(obj(config["otel"]))
	if config["otel"] != nil && len(obj(config["otel"])) == 0 {
		if _, ok := config["otel"].(map[string]any); !ok {
			return fail("Codex 配置格式有误，已保留原文件")
		}
	}
	binding := obj(s.store.get("telemetryCodexBinding"))
	if !enabled {
		result["state"] = "disabled"
		if binding["path"] != path {
			return result
		}
		if !reflect.DeepEqual(otel["exporter"], binding["expectedExporter"]) || otel["log_user_prompt"] != false {
			return fail("Codex 遥测配置已手动修改，已保留当前配置")
		}
		for _, field := range []string{"exporter", "log_user_prompt"} {
			delete(otel, field)
			if truth(binding["had_"+field]) {
				otel[field] = binding[field]
			}
		}
	} else {
		if !compatibleExporter(otel["exporter"]) {
			return fail("Codex 已配置其他遥测导出器，已保留原配置")
		}
		if len(obj(otel["exporter"])) != 0 {
			previousPort := int(num(obj(s.store.get("telemetryStatus"))["port"]))
			known := reflect.DeepEqual(otel["exporter"], localExporter(port)) || reflect.DeepEqual(otel["exporter"], localExporter(4319)) ||
				reflect.DeepEqual(otel["exporter"], binding["expectedExporter"]) || (previousPort > 0 && reflect.DeepEqual(otel["exporter"], localExporter(previousPort)))
			if !known {
				return fail("Codex 已配置其他遥测导出器，已保留原配置")
			}
		}
		if binding["path"] != path {
			binding = Object{"path": path, "hadOtel": config["otel"] != nil}
			for _, field := range []string{"exporter", "log_user_prompt"} {
				v, exists := otel[field]
				binding["had_"+field] = exists
				binding[field] = v
			}
			// Save ownership before modifying the file, so a crash remains recoverable.
			if err = s.store.set("telemetryCodexBinding", binding); err != nil {
				return fail("无法保存采集配置状态")
			}
		}
		otel["exporter"], otel["log_user_prompt"] = localExporter(port), false
	}
	if len(otel) == 0 && !truth(binding["hadOtel"]) {
		otel = nil
	}
	if reflect.DeepEqual(obj(config["otel"]), obj(otel)) {
		if enabled {
			binding["expectedExporter"] = localExporter(port)
			_ = s.store.set("telemetryCodexBinding", binding)
		} else {
			_ = s.store.set("telemetryCodexBinding", Object{})
		}
		return result
	}
	after, err := replaceOTel(raw, otel)
	if err != nil {
		return fail(err.Error())
	}
	if err = writeClientFile(path, raw, after, true); err != nil {
		return fail(err.Error())
	}
	result["changed"] = true
	if enabled {
		binding["expectedExporter"] = localExporter(port)
	} else {
		binding = Object{}
	}
	if err = s.store.set("telemetryCodexBinding", binding); err != nil {
		return fail("无法保存采集配置状态")
	}
	return result
}

func (s *Service) setupPi(enabled bool, port int) Object {
	if s.config.PiHome == "" || len(s.config.PiExtension) == 0 {
		return Object{"state": "missing"}
	}
	path := filepath.Join(s.config.PiHome, "extensions", "codex-monitor.ts")
	result := Object{"state": "ready", "path": path, "changed": false}
	fail := func(message string) Object { result["state"], result["error"] = "error", message; return result }
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fail("无法读取 Pi 扩展，请检查文件权限")
	}
	binding := obj(s.store.get("telemetryPiBinding"))
	normalize := func(b []byte) []byte {
		return piLocalEndpoint.ReplaceAll(bytes.TrimPrefix(b, []byte(piManagedHeader)), []byte("http://127.0.0.1:4319/v1/logs"))
	}
	owned := len(raw) != 0 && ((binding["path"] == path && binding["hash"] == hash(string(raw))) || bytes.Equal(normalize(raw), normalize(s.config.PiExtension)))
	if !enabled {
		result["state"] = "disabled"
		if len(raw) == 0 {
			return result
		}
		if !owned {
			return fail("Pi 同名扩展已修改，已保留当前文件")
		}
		if err = os.Remove(path); err != nil {
			return fail("无法关闭 Pi 扩展，请检查文件权限")
		}
		_ = s.store.set("telemetryPiBinding", Object{})
		result["changed"] = true
		return result
	}
	if len(raw) != 0 && !owned {
		return fail("Pi 已有不同的同名扩展，已保留当前文件")
	}
	after := []byte(piManagedHeader + strings.ReplaceAll(string(s.config.PiExtension), "http://127.0.0.1:4319/v1/logs", fmt.Sprintf("http://127.0.0.1:%d/v1/logs", port)))
	if err = writeClientFile(path, raw, after, false); err != nil {
		return fail(err.Error())
	}
	result["changed"] = !bytes.Equal(raw, after)
	if err = s.store.set("telemetryPiBinding", Object{"path": path, "hash": hash(string(after))}); err != nil {
		return fail("无法保存 Pi 扩展状态")
	}
	return result
}

func (s *Service) setupTelemetryClients(status Object, enabled bool, port int) {
	status["codex"] = s.setupCodex(enabled, port)
	status["pi"] = s.setupPi(enabled, port)
	status["claude"] = s.setupClaude(enabled && truth(s.store.settings()["claudeEnabled"]), port)
}
