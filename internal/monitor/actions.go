package monitor

import (
	"fmt"
	"github.com/google/uuid"
	"regexp"
	"strings"
	"time"
)

func (s *Store) saveSettings(input Object) (Object, error) {
	r := s.settings()
	if v := text(input["language"]); v == "zh-CN" || v == "en" {
		r["language"] = v
	}
	if v := text(input["theme"]); v == "system" || v == "light" || v == "dark" {
		r["theme"] = v
	}
	if v := text(input["widgetQuotaAgent"]); v == "auto" || v == AgentCodex || v == AgentClaude {
		r["widgetQuotaAgent"] = v
	}
	for _, k := range []string{"muted", "autoStart", "telemetryEnabled", "claudeEnabled"} {
		if v, ok := input[k].(bool); ok {
			r[k] = v
		}
	}
	if v, ok := input["telemetryPort"]; ok {
		if !validNumber(v) || num(v) < 1024 || num(v) > 65535 || num(v) != float64(int(num(v))) {
			return nil, fmt.Errorf("采集端口须为 1024–65535 的整数")
		}
		r["telemetryPort"] = v
	}
	if v, ok := input["quotaInterval"]; ok {
		if num(v) != 60 && num(v) != 120 && num(v) != 300 {
			return nil, fmt.Errorf("刷新间隔无效")
		}
		r["quotaInterval"] = v
	}
	if v, ok := input["codexExecutable"].(string); ok && len(v) < 1024 {
		r["codexExecutable"] = v
	}
	return r, s.set("settings", r)
}

var modelName = regexp.MustCompile(`^[a-zA-Z0-9._:/-]{1,100}$`)

func (s *Store) savePrice(p Object) ([]Object, error) {
	if !modelName.MatchString(text(p["model"])) {
		return nil, fmt.Errorf("模型名称无效")
	}
	for _, k := range []string{"input", "cached", "output", "cache_write"} {
		if !validNumber(p[k]) || num(p[k]) > 100000 {
			return nil, fmt.Errorf("价格须为有限的非负数")
		}
	}
	var long any
	if v, ok := p["cache_write_1h"]; ok && v != nil {
		if !validNumber(v) || num(v) > 100000 {
			return nil, fmt.Errorf("价格须为有限的非负数")
		}
		long = v
	}
	t := parse(p["effective"])
	if t.IsZero() {
		return nil, fmt.Errorf("生效时间无效")
	}
	if p["id"] != nil {
		if num(p["id"]) < 1 || float64(int64(num(p["id"]))) != num(p["id"]) {
			return nil, fmt.Errorf("价格版本无效")
		}
		r := s.mustOne("SELECT source,retired FROM prices WHERE id=?", p["id"])
		if r["source"] != "手动设置" || truth(r["retired"]) {
			return nil, fmt.Errorf("仅可修改有效的手动价格版本")
		}
		if e := s.exec("UPDATE prices SET model=?,effective=?,input=?,cached=?,output=?,cache_write=?,cache_write_1h=? WHERE id=?", p["model"], iso(t), p["input"], p["cached"], p["output"], p["cache_write"], long, p["id"]); e != nil {
			return nil, e
		}
	} else {
		if e := s.exec("INSERT INTO prices(model,effective,input,cached,output,cache_write,cache_write_1h,source) VALUES(?,?,?,?,?,?,?,?)", p["model"], iso(t), p["input"], p["cached"], p["output"], p["cache_write"], long, "手动设置"); e != nil {
			return nil, e
		}
	}
	return s.prices(), nil
}
func (s *Store) deletePrice(id any) ([]Object, error) {
	if num(id) < 1 || float64(int64(num(id))) != num(id) {
		return nil, fmt.Errorf("价格版本无效")
	}
	r := s.mustOne("SELECT source,retired FROM prices WHERE id=?", id)
	if len(r) == 0 {
		return nil, fmt.Errorf("价格版本不存在")
	}
	if r["source"] != "手动设置" || truth(r["retired"]) {
		return nil, fmt.Errorf("仅可删除手动价格版本")
	}
	if e := s.exec("DELETE FROM prices WHERE id=?", id); e != nil {
		return nil, e
	}
	return s.prices(), nil
}
func (s *Store) saveAccount(p Object) (Object, error) {
	label := strings.TrimSpace(text(p["label"]))
	if label == "" || len([]rune(label)) > 100 {
		return nil, fmt.Errorf("账号名称须为 1–100 个字符")
	}
	id := text(p["id"])
	if id == "" {
		id = uuid.NewString()
	} else if len(s.mustOne("SELECT id FROM accounts WHERE id=?", id)) == 0 {
		return nil, fmt.Errorf("账号不存在")
	}
	e := s.exec("INSERT INTO accounts VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label", id, label, "manual")
	return Object{"id": id, "label": label}, e
}
func (s *Store) clear(now time.Time) error {
	e := s.transaction(func() error {
		if e := s.exec("DELETE FROM usage;DELETE FROM turns;DELETE FROM quotas;DELETE FROM notices;DELETE FROM request_events;"); e != nil {
			return e
		}
		settings := s.settings()
		settings["clearedAt"] = iso(now)
		return s.set("settings", settings)
	})
	if e != nil {
		return e
	}
	if e = s.exec("PRAGMA wal_checkpoint(TRUNCATE)"); e != nil {
		return e
	}
	return s.exec("VACUUM")
}
