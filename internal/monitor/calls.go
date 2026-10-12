package monitor

import (
	"container/heap"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
)

func (s *Store) callRows(f Object, lo, hi string, pricing priceIndex) []Object {
	rows := []Object{}
	usageCount := 0
	s.eachCall(f, lo, hi, pricing, func(r Object) {
		rows = append(rows, clone(r))
		if r["status"] == "usage" {
			usageCount++
		}
	})
	if usageCount == 0 || usageCount == len(rows) {
		return rows
	}
	// The two SQL streams already have timestamp/ID order.
	merged := make([]Object, 0, len(rows))
	left, right := rows[:usageCount], rows[usageCount:]
	less := callBefore("oldest")
	for len(left) > 0 && len(right) > 0 {
		if less(right[0], left[0]) {
			merged = append(merged, right[0])
			right = right[1:]
		} else {
			merged = append(merged, left[0])
			left = left[1:]
		}
	}
	merged = append(merged, left...)
	return append(merged, right...)
}

func (s *Store) eachCall(f Object, lo, hi string, pricing priceIndex, visit func(Object)) {
	conditions := []string{"u.ts>=?", "u.ts<?"}
	args := []any{lo, hi}
	for _, pair := range [][2]string{{"account", "u.account"}, {"model", "u.model"}, {"session", "u.session"}, {"turn", "u.turn"}, {"project", "s.project"}} {
		if truth(f[pair[0]]) {
			conditions = append(conditions, pair[1]+"=?")
			args = append(args, f[pair[0]])
		}
	}
	sequence := map[string]int{}
	if !truth(f["callStatus"]) || f["callStatus"] == "usage" {
		s.each(`SELECT u.*,s.project,e.ttft,e.association,e.source timing_source,e.response_id
        FROM usage u LEFT JOIN sessions s ON s.id=u.session LEFT JOIN request_events e ON e.usage_id=u.id
        WHERE `+strings.Join(conditions, " AND ")+" ORDER BY u.ts,u.id", args, func(r Object) {
			r["usage_id"], r["id"], r["status"] = r["id"], "usage:"+text(r["id"]), "usage"
			r["total"] = num(r["input"]) + num(r["output"])
			r["cacheRate"] = nil
			if num(r["input"]) > 0 {
				r["cacheRate"] = num(r["cached"]) / num(r["input"])
			}
			p := pricing.calculate(r)
			p.write(r)
			delete(r, "priceInput")
			delete(r, "priceCached")
			delete(r, "priceOutput")
			if p := p.price; p != nil {
				r["priceInput"], r["priceCached"], r["priceOutput"] = p["input"], p["cached"], p["output"]
			}
			key := text(r["session"]) + ":" + text(r["turn"])
			sequence[key]++
			r["sequence"] = sequence[key]
			r["timingNote"] = requestTimingNote(text(r["association"]))
			if text(r["project"]) == "" {
				r["project"] = UnassignedProject
			}
			visit(r)
		})
	}
	if truth(f["callStatus"]) && f["callStatus"] != "failed" && f["callStatus"] != "retry" {
		return
	}
	conditions = []string{"e.ts>=?", "e.ts<?", "e.kind IN ('failed','retry')"}
	args = []any{lo, hi}
	if truth(f["callStatus"]) {
		conditions = append(conditions, "e.kind=?")
		args = append(args, f["callStatus"])
	}
	for _, pair := range [][2]string{{"account", "e.account"}, {"model", "e.model"}, {"session", "e.session"}, {"turn", "e.turn"}, {"project", "s.project"}} {
		if truth(f[pair[0]]) {
			conditions = append(conditions, pair[1]+"=?")
			args = append(args, f[pair[0]])
		}
	}
	s.each("SELECT e.*,s.project FROM request_events e LEFT JOIN sessions s ON s.id=e.session WHERE "+strings.Join(conditions, " AND ")+" ORDER BY e.ts,e.id", args, func(r Object) {
		r["status"], r["cost"], r["total"], r["cacheRate"] = r["kind"], nil, nil, nil
		r["errorLabel"] = requestErrorLabel(text(r["error_code"]))
		if text(r["project"]) == "" {
			r["project"] = UnassignedProject
		}
		visit(r)
	})
}

func callMatches(f Object) func(Object) bool {
	query := strings.ToLower(strings.TrimSpace(text(f["callSearch"])))
	return func(r Object) bool {
		if truth(f["callStatus"]) && r["status"] != f["callStatus"] {
			return false
		}
		if query != "" {
			for _, key := range []string{"id", "usage_id", "response_id", "session", "turn", "model", "project"} {
				if strings.Contains(strings.ToLower(text(r[key])), query) {
					return true
				}
			}
			return false
		}
		return true
	}
}

func callBefore(order string) func(Object, Object) bool {
	key := ""
	switch order {
	case "tokens":
		key = "total"
	case "cost", "ttft":
		key = order
	}
	return func(left, right Object) bool {
		if key != "" {
			if (left[key] == nil) != (right[key] == nil) {
				return left[key] != nil
			}
			if num(left[key]) != num(right[key]) {
				return num(left[key]) > num(right[key])
			}
		}
		if left["ts"] == right["ts"] {
			if order == "oldest" {
				return text(left["id"]) < text(right["id"])
			}
			return text(left["id"]) > text(right["id"])
		}
		if order == "oldest" {
			return text(left["ts"]) < text(right["ts"])
		}
		return text(left["ts"]) > text(right["ts"])
	}
}

func filterCalls(rows []Object, f Object) []Object {
	result := make([]Object, 0, len(rows))
	match := callMatches(f)
	for _, r := range rows {
		if match(r) {
			result = append(result, r)
		}
	}
	order := text(f["callOrder"])
	if order != "oldest" {
		if order == "tokens" || order == "cost" || order == "ttft" {
			less := callBefore(order)
			sort.SliceStable(result, func(i, j int) bool { return less(result[i], result[j]) })
		} else {
			slices.Reverse(result)
			// Preserve the original stable order of exact timestamp/ID ties.
			for start := 0; start < len(result); {
				end := start + 1
				for end < len(result) && result[end]["ts"] == result[start]["ts"] && result[end]["id"] == result[start]["id"] {
					end++
				}
				slices.Reverse(result[start:end])
				start = end
			}
		}
	}
	return result
}

func paginateCalls(rows []Object, page, size int) ([]Object, Object) {
	size = max(1, min(200, size))
	pages := max(1, (len(rows)+size-1)/size)
	page = max(1, min(pages, page))
	start := min(len(rows), (page-1)*size)
	return slices.Clone(rows[start:min(len(rows), start+size)]), Object{"page": page, "pageSize": size, "total": len(rows), "pages": pages}
}

type callAccumulator struct {
	summary             Object
	timings             []float64
	input, cached, cost float64
	priced              int
	largest             Object
}

func newCallAccumulator() *callAccumulator {
	return &callAccumulator{summary: Object{"usageRecords": 0, "failedEvents": 0, "retryEvents": 0, "ttftSamples": 0, "avgTtft": nil, "p50Ttft": nil, "p95Ttft": nil, "total": 0., "cost": nil, "unpriced": 0, "cacheRate": nil, "largestId": nil}}
}

func (a *callAccumulator) add(r Object, before func(Object, Object) bool) {
	summary := a.summary
	switch r["status"] {
	case "failed":
		summary["failedEvents"] = num(summary["failedEvents"]) + 1
	case "retry":
		summary["retryEvents"] = num(summary["retryEvents"]) + 1
	case "usage":
		summary["usageRecords"] = num(summary["usageRecords"]) + 1
		summary["total"] = num(summary["total"]) + num(r["total"])
		a.input += num(r["input"])
		a.cached += num(r["cached"])
		if r["cost"] == nil {
			summary["unpriced"] = num(summary["unpriced"]) + 1
		} else {
			a.cost += num(r["cost"])
			a.priced++
		}
		if r["ttft"] != nil {
			a.timings = append(a.timings, num(r["ttft"]))
		}
		if tokens := num(r["total"]); tokens > 0 && (a.largest == nil || tokens > num(a.largest["total"]) || tokens == num(a.largest["total"]) && before != nil && before(r, a.largest)) {
			if a.largest == nil {
				a.largest = Object{}
			}
			for _, key := range []string{"id", "ts", "total", "cost", "ttft"} {
				a.largest[key] = r[key]
			}
			summary["largestId"] = r["id"]
		}
	}
}

func (a *callAccumulator) result() Object {
	summary, timings := a.summary, a.timings
	if a.priced > 0 {
		summary["cost"] = a.cost
	}
	if a.input > 0 {
		summary["cacheRate"] = a.cached / a.input
	}
	if len(timings) > 0 {
		sort.Float64s(timings)
		sum := 0.
		for _, v := range timings {
			sum += v
		}
		summary["ttftSamples"], summary["avgTtft"] = len(timings), sum/float64(len(timings))
		summary["p50Ttft"] = timings[int(math.Ceil(.5*float64(len(timings))))-1]
		summary["p95Ttft"] = timings[int(math.Ceil(.95*float64(len(timings))))-1]
	}
	return summary
}

func callShares(rows []Object, summary Object) {
	for _, r := range rows {
		r["tokenShare"], r["costShare"] = nil, nil
		if r["status"] == "usage" && num(summary["total"]) > 0 {
			r["tokenShare"] = num(r["total"]) / num(summary["total"])
		}
		if r["status"] == "usage" && r["cost"] != nil && num(summary["cost"]) > 0 {
			r["costShare"] = num(r["cost"]) / num(summary["cost"])
		}
	}
}

func summarizeCalls(rows []Object) Object {
	a := newCallAccumulator()
	for _, r := range rows {
		a.add(r, nil)
	}
	summary := a.result()
	callShares(rows, summary)
	return summary
}

// A max heap keeps the worst retained row at the root. Only the requested
// page prefix is retained, rather than all decorated call maps.
type callHeap struct {
	rows   []Object
	before func(Object, Object) bool
}

func (h callHeap) Len() int           { return len(h.rows) }
func (h callHeap) Less(i, j int) bool { return h.before(h.rows[j], h.rows[i]) }
func (h callHeap) Swap(i, j int)      { h.rows[i], h.rows[j] = h.rows[j], h.rows[i] }
func (h *callHeap) Push(v any)        { h.rows = append(h.rows, v.(Object)) }
func (h *callHeap) Pop() any {
	last := len(h.rows) - 1
	r := h.rows[last]
	h.rows[last] = nil
	h.rows = h.rows[:last]
	return r
}

func (s *Store) callSnapshot(f Object, lo, hi string, pricing priceIndex) Object {
	count := int(num(s.mustOne(`SELECT (SELECT COUNT(*) FROM usage)+(SELECT COUNT(*) FROM request_events WHERE kind IN ('failed','retry')) n`)["n"]))
	page := max(1, min(max(1, (count+49)/50), int(num(f["callPage"]))))
	limit := min(count, page*50)
	before, match := callBefore(text(f["callOrder"])), callMatches(f)
	h := &callHeap{rows: make([]Object, 0, min(limit, 200)), before: before}
	a := newCallAccumulator()
	total := 0
	s.eachCall(f, lo, hi, pricing, func(r Object) {
		if !match(r) {
			return
		}
		total++
		a.add(r, before)
		if len(h.rows) < limit {
			heap.Push(h, clone(r))
		} else if limit > 0 && before(r, h.rows[0]) {
			clear(h.rows[0])
			maps.Copy(h.rows[0], r)
			heap.Fix(h, 0)
		}
	})
	sort.Slice(h.rows, func(i, j int) bool { return before(h.rows[i], h.rows[j]) })
	pages := max(1, (total+49)/50)
	page = min(page, pages)
	start := min(len(h.rows), (page-1)*50)
	selected := slices.Clone(h.rows[start:min(len(h.rows), start+50)])
	summary := a.result()
	callShares(selected, summary)
	pagination := Object{"page": page, "pageSize": 50, "total": total, "pages": pages}
	return Object{"rows": selected, "pagination": pagination, "summary": summary}
}

func (s *Store) taskCallSnapshot(f Object, pricing priceIndex) Object {
	if !truth(f["detailSession"]) || !truth(f["detailTurn"]) {
		return Object{}
	}
	filter := Object{"session": f["detailSession"], "turn": f["detailTurn"], "account": f["account"]}
	rows := s.callRows(filter, "0000", "9999", pricing)
	summary := summarizeCalls(rows)
	selected, pagination := paginateCalls(rows, int(num(f["detailPage"])), 25)
	return Object{"session": f["detailSession"], "turn": f["detailTurn"], "rows": selected, "pagination": pagination, "summary": summary}
}

type csvBuffer struct {
	strings.Builder
	cols []string
}

func newCSV(cols []string) *csvBuffer {
	b := &csvBuffer{cols: cols}
	b.WriteString("\uFEFF")
	b.WriteString(strings.Join(cols, ","))
	return b
}

func (b *csvBuffer) row(r Object) {
	b.WriteString("\r\n")
	for i, key := range b.cols {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		v := text(r[key])
		if v != "" && strings.ContainsRune("=+@-\t\r", rune(v[0])) {
			b.WriteByte('\'')
		}
		b.WriteString(strings.ReplaceAll(v, "\"", "\"\""))
		b.WriteByte('"')
	}
}

func csvRows(cols []string, rows []Object) string {
	b := newCSV(cols)
	for _, r := range rows {
		b.row(r)
	}
	return b.String()
}

func (s *Store) exportCalls(f Object, lo, hi string) string {
	rows := filterCalls(s.callRows(f, lo, hi, indexPrices(s.prices())), f)
	cols := []string{"ts", "id", "usage_id", "response_id", "session", "turn", "model", "status", "input", "cached", "output", "reasoning", "cache_write", "cost", "inputCost", "outputCost", "priceId", "ttft", "association", "attempt", "http_status", "error_code", "kind"}
	return csvRows(cols, rows)
}

func callCoverage(s *Store) Object {
	return s.mustOne(`SELECT COUNT(*) received,COALESCE(SUM(usage_id IS NOT NULL),0) associated FROM request_events WHERE kind='completed'`)
}
