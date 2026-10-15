package nativeapp

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Object = map[string]any

func shortNumber(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
func (a *App) duration(v any) string {
	if v == nil {
		return "—"
	}
	if number(v) < 60000 {
		return fmt.Sprintf("%.1f", number(v)/1000) + a.tr(" 秒")
	}
	return fmt.Sprintf("%.1f", number(v)/60000) + a.tr(" 分钟")
}
func (a *App) recordCost(row Object) string {
	if number(row["requests"]) == 0 {
		return "—"
	}
	if row["cost"] == nil {
		return a.tr("未定价")
	}
	value := fmt.Sprintf("%.6f", number(row["cost"]))
	for strings.HasSuffix(value, "0") && len(strings.Split(value, ".")[1]) > 4 {
		value = strings.TrimSuffix(value, "0")
	}
	if number(row["unpriced"]) > 0 {
		value += a.tr(" + 未定价")
	}
	return "$" + value
}

func obj(v any) Object {
	if o, ok := v.(map[string]any); ok {
		return o
	}
	return Object{}
}
func objects(v any) []Object {
	if rows, ok := v.([]Object); ok {
		return rows
	}
	a, _ := v.([]any)
	r := make([]Object, 0, len(a))
	for _, v := range a {
		r = append(r, obj(v))
	}
	return r
}
func stringsOf(v any) []string {
	if values, ok := v.([]string); ok {
		return values
	}
	a, _ := v.([]any)
	r := make([]string, 0, len(a))
	for _, v := range a {
		r = append(r, str(v))
	}
	return r
}

// Convert homogeneous JSON lists once, as the snapshot enters the UI.
// Subsequent frames borrow typed slices instead of copying every element.
func normalizeLists(o Object) {
	for key, value := range o {
		switch value := value.(type) {
		case map[string]any:
			normalizeLists(Object(value))
		case []Object:
			for _, row := range value {
				normalizeLists(row)
			}
		case []any:
			if len(value) == 0 {
				continue
			}
			switch value[0].(type) {
			case map[string]any:
				rows := make([]Object, 0, len(value))
				for _, item := range value {
					row, ok := item.(map[string]any)
					if !ok {
						rows = nil
						break
					}
					normalizeLists(Object(row))
					rows = append(rows, Object(row))
				}
				if rows != nil {
					o[key] = rows
				}
			case string:
				values := make([]string, 0, len(value))
				for _, item := range value {
					s, ok := item.(string)
					if !ok {
						values = nil
						break
					}
					values = append(values, s)
				}
				if values != nil {
					o[key] = values
				}
			}
		}
	}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	f, _ := strconv.ParseFloat(str(v), 64)
	return f
}
func truth(v any) bool { b, _ := v.(bool); return b }
func valid(v any) bool { return v != nil && !math.IsNaN(number(v)) && !math.IsInf(number(v), 0) }
func compact(v any) string {
	if v == nil {
		return "—"
	}
	n := number(v)
	if math.Abs(n) >= 1e9 {
		return shortNumber(n/1e9) + "B"
	}
	if math.Abs(n) >= 1e6 {
		return shortNumber(n/1e6) + "M"
	}
	if math.Abs(n) >= 1e3 {
		return shortNumber(n/1e3) + "K"
	}
	return fmt.Sprintf("%.0f", n)
}
func full(v any) string {
	if v == nil {
		return "—"
	}
	s := fmt.Sprintf("%.0f", number(v))
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func money(v any) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", number(v))
}
func ratio(v any) string {
	if !valid(v) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", 100*number(v))
}
func stamp(v any) string {
	if v == nil || str(v) == "" {
		return "—"
	}
	var t time.Time
	switch v.(type) {
	case float64, int, int64:
		t = time.UnixMilli(int64(number(v)))
	default:
		t, _ = time.Parse(time.RFC3339, str(v))
	}
	if t.IsZero() {
		return str(v)
	}
	return t.Local().Format("2006-01-02 15:04")
}
func shortPath(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	if s == "" {
		return "—"
	}
	return filepath.Base(s)
}
func clone(o Object) Object {
	r := Object{}
	for k, v := range o {
		r[k] = v
	}
	return r
}
func bounded(n, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, n)) }

func (a *App) english() bool { return str(obj(a.data["settings"])["language"]) == "en" }

// tr translates s and fills its {0}, {1}… placeholders in one pass.
func (a *App) tr(s string, args ...any) string {
	if a.english() {
		if next := a.translations[s]; next != "" {
			s = next
		}
	}
	if len(args) == 0 || strings.IndexByte(s, '{') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8*len(args))
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			j, n := i+1, 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				n = n*10 + int(s[j]-'0')
				j++
			}
			if j > i+1 && j < len(s) && s[j] == '}' && n < len(args) {
				b.WriteString(str(args[n]))
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
func (a *App) system(s string) string {
	if a.english() {
		if v := a.systemTranslations[s]; v != "" {
			return v
		}
		if v := a.translations[s]; v != "" {
			return v
		}
	}
	return s
}
func (a *App) accountLabel(id string) string {
	for _, v := range objects(a.data["accounts"]) {
		if str(v["id"]) == id {
			return str(v["label"])
		}
	}
	return a.tr("未归属")
}
