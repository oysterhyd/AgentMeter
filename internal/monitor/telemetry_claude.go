package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
)

// claudeTelemetryKeys are the only settings.json env entries Monitor owns.
var claudeTelemetryKeys = []string{"CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_LOGS_EXPORTER", "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"}

var jsonIndent = regexp.MustCompile(`\n([ \t]+)"`)

func claudeTelemetryEnv(port int) Object {
	return Object{"CLAUDE_CODE_ENABLE_TELEMETRY": "1", "OTEL_LOGS_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL": "http/json",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": fmt.Sprintf("http://127.0.0.1:%d/v1/logs", port)}
}

type jsonMember struct {
	key   string
	value json.RawMessage
}

// jsonMembers decodes one object's members in file order.
func jsonMembers(data []byte) ([]jsonMember, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("not an object")
	}
	var members []jsonMember
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		members = append(members, jsonMember{t.(string), value})
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data")
	}
	return members, nil
}

func encodeMembers(members []jsonMember) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// replaceClaudeEnv sets (string) or removes (nil) the managed env entries,
// dropping an env object left empty when dropEmpty. Other settings keep their
// order and values; the file keeps its indentation, line endings, BOM and
// final newline.
func replaceClaudeEnv(raw []byte, env Object, dropEmpty bool) ([]byte, error) {
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
	var envMembers []jsonMember
	at := -1
	for i, m := range members {
		if m.key == "env" {
			if envMembers, err = jsonMembers(m.value); err != nil {
				return nil, invalid
			}
			at = i
		}
	}
	for _, key := range claudeTelemetryKeys {
		i := slices.IndexFunc(envMembers, func(m jsonMember) bool { return m.key == key })
		if env[key] == nil {
			if i >= 0 {
				envMembers = slices.Delete(envMembers, i, i+1)
			}
			continue
		}
		value, _ := json.Marshal(env[key])
		if i >= 0 {
			envMembers[i].value = value
		} else {
			envMembers = append(envMembers, jsonMember{key, value})
		}
	}
	if at >= 0 && len(envMembers) == 0 && dropEmpty {
		members = slices.Delete(members, at, at+1)
	} else if at >= 0 {
		members[at].value = encodeMembers(envMembers)
	} else if len(envMembers) > 0 {
		members = append(members, jsonMember{"env", encodeMembers(envMembers)})
	}
	result, err := formatClaudeSettings(raw, data, empty, members)
	if err != nil {
		return nil, invalid
	}
	var before, after Object
	if json.Unmarshal(data, &before) != nil || json.Unmarshal(bytes.TrimPrefix(result, []byte{0xef, 0xbb, 0xbf}), &after) != nil {
		return nil, invalid
	}
	beforeEnv, afterEnv := obj(before["env"]), obj(after["env"])
	delete(before, "env")
	delete(after, "env")
	for _, key := range claudeTelemetryKeys {
		delete(beforeEnv, key)
		delete(afterEnv, key)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeEnv, afterEnv) {
		return nil, fmt.Errorf("采集配置校验失败，已保留原文件")
	}
	return result, nil
}

// formatClaudeSettings encodes members with the original file's indentation,
// line endings, BOM and final newline.
func formatClaudeSettings(raw, data []byte, empty bool, members []jsonMember) ([]byte, error) {
	var out bytes.Buffer
	var err error
	if indent := jsonIndent.FindSubmatch(data); indent != nil {
		err = json.Indent(&out, encodeMembers(members), "", string(indent[1]))
	} else if bytes.ContainsRune(bytes.TrimSpace(data), '\n') {
		err = json.Indent(&out, encodeMembers(members), "", "  ")
	} else {
		err = json.Compact(&out, encodeMembers(members))
	}
	if err != nil {
		return nil, err
	}
	result := out.Bytes()
	if bytes.HasSuffix(data, []byte("\n")) {
		result = append(result, '\n')
	}
	if bytes.Contains(data, []byte("\r\n")) {
		result = bytes.ReplaceAll(result, []byte("\n"), []byte("\r\n"))
	}
	if len(data) != len(raw) && !empty {
		result = append([]byte{0xef, 0xbb, 0xbf}, result...)
	}
	return result, nil
}

// setupClaude points Claude Code's OTel log export at the local collector
// through settings.json env, remembering the values it replaced. Prompts and
// responses stay redacted by Claude Code's defaults and are dropped on receipt.
func (s *Service) setupClaude(enabled bool, port int) Object {
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
	var config Object
	if data := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})); len(data) != 0 {
		if json.Unmarshal(data, &config) != nil || config == nil {
			return fail("Claude Code 配置格式有误，已保留原文件")
		}
	}
	if _, ok := config["env"].(map[string]any); config["env"] != nil && !ok {
		return fail("Claude Code 配置格式有误，已保留原文件")
	}
	current := obj(config["env"])
	binding := obj(s.store.get("telemetryClaudeBinding"))
	desired := Object{}
	if !enabled {
		result["state"] = "disabled"
		if binding["path"] != path {
			return result
		}
		for _, key := range claudeTelemetryKeys {
			if current[key] != obj(binding["expected"])[key] {
				return fail("Claude Code 遥测配置已手动修改，已保留当前配置")
			}
			desired[key] = obj(binding["previous"])[key]
		}
	} else {
		desired = claudeTelemetryEnv(port)
		previousPort := int(num(obj(s.store.get("telemetryStatus"))["port"]))
		for _, key := range claudeTelemetryKeys {
			v, exists := current[key]
			known := !exists || v == desired[key] || v == obj(binding["expected"])[key]
			switch key {
			case "CLAUDE_CODE_ENABLE_TELEMETRY":
				known = known || v == "true"
			case "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT":
				known = known || v == claudeTelemetryEnv(4319)[key] || (previousPort > 0 && v == claudeTelemetryEnv(previousPort)[key])
			}
			if !known {
				return fail("Claude Code 已配置其他遥测导出器，已保留原配置")
			}
		}
		if binding["path"] != path {
			previous := Object{}
			for _, key := range claudeTelemetryKeys {
				if v, exists := current[key]; exists {
					previous[key] = v
				}
			}
			binding = Object{"path": path, "previous": previous, "hadEnv": config["env"] != nil}
			// Save ownership before modifying the file, so a crash remains recoverable.
			if err = s.store.set("telemetryClaudeBinding", binding); err != nil {
				return fail("无法保存采集配置状态")
			}
		}
	}
	for _, key := range claudeTelemetryKeys {
		if v, exists := current[key]; v != desired[key] || exists != (desired[key] != nil) {
			after, err := replaceClaudeEnv(raw, desired, !enabled && !truth(binding["hadEnv"]))
			if err != nil {
				return fail(err.Error())
			}
			if err = writeClientFile(path, raw, after, true); err != nil {
				return fail(err.Error())
			}
			result["changed"] = true
			break
		}
	}
	if enabled {
		binding["expected"] = desired
	} else {
		binding = Object{}
	}
	if err = s.store.set("telemetryClaudeBinding", binding); err != nil {
		return fail("无法保存采集配置状态")
	}
	return result
}
