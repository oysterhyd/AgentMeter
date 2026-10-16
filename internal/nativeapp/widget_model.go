package nativeapp

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const widgetWidth, widgetHeight = 560, 380

func parseTime(v any) time.Time {
	switch v.(type) {
	case float64, int64, int:
		return time.UnixMilli(int64(number(v)))
	}
	t, _ := time.Parse(time.RFC3339Nano, str(v))
	return t
}

type widgetMotion struct {
	from, target float32
	start        time.Time
	duration     time.Duration
}

func (m widgetMotion) value(now time.Time) float32 {
	if m.start.IsZero() || m.duration == 0 {
		return m.target
	}
	p := float32(now.Sub(m.start)) / float32(m.duration)
	return m.from + (m.target-m.from)*motionEase(max(0, min(1, p)))
}
func (m *widgetMotion) to(value float32, now time.Time, duration time.Duration) {
	m.from, m.target, m.start, m.duration = m.value(now), value, now, duration
}
func (m widgetMotion) active(now time.Time) bool {
	return m.from != m.target && now.Sub(m.start) < m.duration
}

type widgetModel struct {
	Data                                                       Object
	Error                                                      string
	Busy, Collapsed, Visible, ReduceMotion, ReduceTransparency bool
	Dark                                                       bool
	hover, focus                                               int
	pointerX, pointerY                                         float32
	dirty                                                      bool
	shape, visibility                                          widgetMotion
	bars                                                       [4]widgetMotion
	hoverMotion                                                [5]widgetMotion
	light                                                      [5][2]float32
	pressed                                                    bool
	keyboard                                                   bool
	clock                                                      time.Time
}

func newWidgetModel() widgetModel {
	m := widgetModel{Data: Object{}, hover: -1, focus: -1, dirty: true}
	for i := range m.light {
		m.light[i] = [2]float32{.5, 0}
	}
	return m
}
func (m *widgetModel) en() bool { return m.Data["language"] == "en" }
func (m *widgetModel) t(zh, en string) string {
	if m.en() {
		return en
	}
	return zh
}
func (m *widgetModel) setVisible(visible bool, now time.Time) {
	m.Visible, m.dirty = visible, true
	d := 240 * time.Millisecond
	if m.ReduceMotion {
		d = 0
	}
	target := float32(0)
	if visible {
		target = 1
	}
	m.visibility.to(target, now, d)
}
func (m *widgetModel) toggle(now time.Time) {
	m.Collapsed, m.dirty = !m.Collapsed, true
	d := 680 * time.Millisecond
	if m.ReduceMotion {
		d = 0
	}
	target := float32(0)
	if m.Collapsed {
		target = 1
	}
	m.shape.to(target, now, d)
	if m.Collapsed {
		m.focus = 7
	} else {
		m.focus = 1
	}
}
func (m *widgetModel) morphing(now time.Time) bool { return m.shape.active(now) }
func (m *widgetModel) toggleBusy(now time.Time) bool {
	if m.shape.start.IsZero() {
		return false
	}
	duration := 760 * time.Millisecond
	if m.ReduceMotion {
		duration = 40 * time.Millisecond
	}
	return now.Sub(m.shape.start) < duration
}
func (m *widgetModel) animating(now time.Time) bool {
	if m.shape.active(now) || m.visibility.active(now) || (m.Busy && !m.ReduceMotion) {
		return true
	}
	for _, a := range m.bars {
		if a.active(now) {
			return true
		}
	}
	for _, a := range m.hoverMotion {
		if a.active(now) {
			return true
		}
	}
	return false
}
func (m *widgetModel) glass(now time.Time) ui.Rect {
	p := m.shape.value(now)
	w, h := 536-416*p, 356-236*p
	return ui.Rect{X: 548 - w, Y: 12, W: w, H: h}
}
func (m *widgetModel) collecting(now time.Time) bool {
	scan := obj(m.Data["scan"])
	last := parseTime(scan["lastScan"])
	return !last.IsZero() && now.Sub(last) < 15*time.Second && truth(scan["sourceExists"]) && number(scan["errors"]) == 0 && m.Error == ""
}

type widgetMetric struct {
	Label, Icon, Tip string
	Value            any
	Speed            bool
}

func (m *widgetModel) quota(minutes float64, now time.Time) (any, string) {
	for _, q := range objects(m.Data["quotas"]) {
		if number(q["minutes"]) != minutes {
			continue
		}
		expired := number(q["resets"]) > 0 && number(q["resets"])*1000 <= float64(now.UnixMilli())
		stale := now.Sub(parseTime(q["ts"])) > 5*time.Minute || !truth(obj(m.Data["quotaStatus"])["ok"])
		prefix := ""
		if expired {
			prefix = m.t("等待重置更新 · ", "Awaiting reset · ")
		} else if stale {
			prefix = m.t("旧快照 · 待刷新 · ", "Saved · retrying · ")
		}
		tip := prefix + str(q["bucket"]) + " · " + m.t("采样 ", "Sampled ") + parseTime(q["ts"]).Local().Format("2006/01/02 15:04:05")
		if number(q["resets"]) > 0 {
			tip += " · " + m.t("重置 ", "Resets ") + time.Unix(int64(number(q["resets"])), 0).Local().Format("2006/01/02 15:04:05")
		}
		tip += " · " + m.t("当前 Codex 账号", "Current Codex account")
		if expired {
			return nil, tip
		}
		return max(0., 100-number(q["used"])), tip
	}
	return nil, m.t("当前账号暂无额度快照", "No quota snapshot for this account")
}
func (m *widgetModel) metrics(now time.Time) []widgetMetric {
	five, fiveTip := m.quota(300, now)
	week, weekTip := m.quota(10080, now)
	var tps, cache any
	if len(m.Data) > 0 && m.collecting(now) {
		tps = m.Data["tps"]
	}
	if m.Data["cacheRate"] != nil {
		cache = number(m.Data["cacheRate"]) * 100
	}
	return []widgetMetric{
		{m.t("当前 5h 额度", "5h quota"), "gauge-fill", fiveTip, five, false},
		{m.t("本周额度", "Weekly quota"), "calendar-dots-fill", weekTip, week, false},
		{m.t("实时 TPS", "Live TPS"), "broadcast-fill", m.t("本机全部账号最近 60 秒日志中的输出 token / 60；包含等待，并非精确生成速度。", "All local accounts: output tokens recorded in the last 60 seconds / 60; includes idle time, not exact generation speed."), tps, true},
		{m.t("缓存命中率", "Cache hit rate"), "lightning-fill", m.t("本机全部账号今日缓存输入 / 输入 Token（含未归属 pi）", "All local accounts: cached input / input tokens today, including unassigned Pi"), cache, false},
	}
}
func widgetCompact(v any) string {
	if v == nil {
		return "—"
	}
	n := number(v)
	unit := ""
	divisor := 1.
	for i, u := range []string{"K", "M", "B", "T"} {
		d := math.Pow(1000, float64(i+1))
		if math.Abs(n) >= d {
			unit, divisor = u, d
		}
	}
	value := math.Round(n/divisor*100) / 100
	if math.Abs(value) >= 1000 && divisor < 1e12 {
		divisor *= 1000
		unit = map[float64]string{1e3: "K", 1e6: "M", 1e9: "B", 1e12: "T"}[divisor]
		value = math.Round(n/divisor*100) / 100
	}
	return strconv.FormatFloat(value, 'f', -1, 64) + unit
}
func widgetPercent(v any) string {
	if v == nil {
		return "—"
	}
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", number(v)), "0"), ".") + "%"
}
func (m *widgetModel) scope() string {
	return m.t("本机全部账号（含未归属 pi）；额度仅显示当前 Codex 账号。", "All local accounts, including unassigned Pi; quotas show only the current Codex account.")
}
func widgetMetricRect(i int) ui.Rect {
	return ui.Rect{X: 30 + 127.25*float32(i), Y: 91, W: 118.25, H: 120}
}
func widgetClamp(x, y int, area ui.Rect) (int, int) {
	return max(int(area.X)-548+48, min(x, int(area.X+area.W)-428-48)), max(int(area.Y)-132+48, min(y, int(area.Y+area.H)-12-48))
}
