package monitor

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func Bounds(f Object, now time.Time) (time.Time, time.Time, error) {
	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)
	switch text(f["range"]) {
	case "7d":
		start = start.AddDate(0, 0, -6)
	case "30d":
		start = start.AddDate(0, 0, -29)
	case "all":
		start = time.Unix(0, 0)
	case "custom":
		var e error
		start, e = time.ParseInLocation("2006-01-02", text(f["start"]), time.Local)
		if e != nil {
			return start, end, fmt.Errorf("请选择完整日期范围")
		}
		end, e = time.ParseInLocation("2006-01-02", text(f["end"]), time.Local)
		if e != nil {
			return start, end, fmt.Errorf("日期范围无效")
		}
		end = end.AddDate(0, 0, 1)
	}
	if !end.After(start) {
		return start, end, fmt.Errorf("日期范围无效")
	}
	return start, end, nil
}

// Store.prices is ordered by effective date, manual priority, then ID.
// Filtering into per-model slices preserves that tie order for Search.
type priceIndex map[string][]Object

func indexPrices(prices []Object) priceIndex {
	index := make(priceIndex)
	for _, p := range prices {
		if !truth(p["retired"]) {
			model := text(p["model"])
			index[model] = append(index[model], p)
		}
	}
	return index
}
func (index priceIndex) calculate(r Object) usageCost {
	prices, ts := index[text(r["model"])], text(r["ts"])
	i := sort.Search(len(prices), func(i int) bool { return text(prices[i]["effective"]) <= ts })
	if i < len(prices) {
		return calculateCost(r, prices[i])
	}
	return usageCost{}
}

type usageCost struct {
	input, output, total, saved float64
	price                       Object
}

func calculateCost(r, p Object) usageCost {
	if p == nil {
		return usageCost{}
	}
	plain := math.Max(0, num(r["input"])-num(r["cached"])-num(r["cache_write"]))
	in := plain*num(p["input"]) + num(r["cached"])*num(p["cached"]) + num(r["cache_write"])*num(p["cache_write"])
	out := num(r["output"]) * num(p["output"])
	return usageCost{in / 1e6, out / 1e6, (in + out) / 1e6, num(r["cached"]) * (num(p["input"]) - num(p["cached"])) / 1e6, p}
}

func (p usageCost) write(r Object) {
	if p.price == nil {
		r["inputCost"], r["outputCost"], r["cost"], r["saved"], r["priceId"] = nil, nil, nil, nil, nil
		return
	}
	r["inputCost"], r["outputCost"], r["cost"], r["saved"], r["priceId"] = p.input, p.output, p.total, p.saved, p.price["id"]
}
func addNumbers(to, from Object, keys ...string) {
	for _, k := range keys {
		to[k] = num(to[k]) + num(from[k])
	}
}
func emptyTotals() Object {
	return Object{"input": 0., "cached": 0., "output": 0., "inputCost": 0., "outputCost": 0., "cost": 0., "unpriced": 0., "requests": 0.}
}
func finishTotals(t Object) Object {
	if num(t["requests"]) == 0 || num(t["unpriced"]) == num(t["requests"]) {
		t["inputCost"], t["outputCost"], t["cost"] = nil, nil, nil
	}
	return t
}
func addTotal(t, r Object, p usageCost) {
	t["requests"] = num(t["requests"]) + 1
	addNumbers(t, r, "input", "cached", "output")
	if p.price == nil {
		t["unpriced"] = num(t["unpriced"]) + 1
	} else {
		t["inputCost"] = num(t["inputCost"]) + p.input
		t["outputCost"] = num(t["outputCost"]) + p.output
		t["cost"] = num(t["cost"]) + p.total
	}
}
func matches(t Object, project any, search string) bool {
	if search == "" {
		return true
	}
	for _, v := range []any{t["id"], t["session"], t["model"], project} {
		if strings.Contains(strings.ToLower(text(v)), search) {
			return true
		}
	}
	return false
}

// tally is one ranking row while it accumulates; plain fields keep the
// per-usage hot path free of map writes and float boxing.
type tally struct {
	name                                                   string
	total, input, output, cached, cost, unpriced, requests float64
}

type ranking struct {
	byKey map[string]*tally
	order []*tally
}

func newRanking() *ranking { return &ranking{byKey: map[string]*tally{}} }
func (r *ranking) add(key string, u Object, p usageCost) {
	v := r.byKey[key]
	if v == nil {
		v = &tally{name: key}
		r.byKey[key] = v
		r.order = append(r.order, v)
	}
	input, output := num(u["input"]), num(u["output"])
	v.total += input + output
	v.input += input
	v.output += output
	v.cached += num(u["cached"])
	v.cost += p.total
	v.requests++
	if p.price == nil {
		v.unpriced++
	}
}

// list returns the rows in first-seen order.
func (r *ranking) list() []Object {
	out := make([]Object, len(r.order))
	for i, v := range r.order {
		out[i] = Object{"name": v.name, "total": v.total, "input": v.input, "output": v.output, "cached": v.cached, "cost": v.cost, "unpriced": v.unpriced, "requests": v.requests}
	}
	return out
}

// rank returns the rows by total, largest first.
func (r *ranking) rank() []Object {
	sort.SliceStable(r.order, func(i, j int) bool { return r.order[i].total > r.order[j].total })
	return r.list()
}
func (s *Store) resolve(f Object) Object {
	f = clone(f)
	if f["account"] == "current" {
		a := s.get("currentAccount")
		if !truth(a) {
			a = Unknown
		}
		f["account"] = a
	}
	return f
}
func projectOf(sessions map[string]Object, id any) string {
	p := text(sessions[text(id)]["project"])
	if p == "" {
		p = UnassignedProject
	}
	return p
}

type turnTotals struct {
	totals Object
	models map[string]Object
	order  []Object
}

func newTurnTotals() *turnTotals {
	return &turnTotals{totals: emptyTotals(), models: map[string]Object{}, order: []Object{}}
}

func (t *turnTotals) add(u Object, p usageCost) {
	addTotal(t.totals, u, p)
	key := text(u["model"])
	if t.models[key] == nil {
		v := emptyTotals()
		v["model"] = u["model"]
		t.models[key] = v
		t.order = append(t.order, v)
	}
	addTotal(t.models[key], u, p)
}

func usageFrom(f Object) string {
	from := "FROM usage u"
	if truth(f["project"]) {
		from += " LEFT JOIN sessions s ON s.id=u.session"
	}
	return from
}
func (s *Store) Snapshot(input Object, now time.Time) (out Object, err error) {
	f := s.resolve(input)
	start, end, e := Bounds(f, now)
	if e != nil {
		return nil, e
	}
	lo, hi := iso(start), iso(end)
	page := text(f["page"])
	if page == "" {
		page = "all"
	}
	has := func(names ...string) bool {
		for _, n := range names {
			if n == page {
				return true
			}
		}
		return false
	}
	analytics := has("all", "overview", "history")
	optionsWanted := has("all", "overview", "history", "settings", "activity")
	recordsWanted := has("all", "history")
	historyWanted := has("all", "quota")
	views := []string{}
	switch page {
	case "all":
		views = []string{"models", "projects", "tasks"}
	case "overview":
		views = []string{"models"}
	case "history":
		v := text(f["view"])
		if v != "projects" && v != "tasks" {
			v = "models"
		}
		views = []string{v}
	}
	pageSize := int(num(f["pageSize"]))
	if pageSize == 0 {
		pageSize = 200
	}
	pageSize = max(1, min(200, pageSize))
	requested := max(1, int(num(f["recordPage"])))
	prices := s.prices()
	pricing := indexPrices(prices)
	defer func() {
		if out == nil {
			return
		}
		if has("all", "history") {
			out["calls"] = Object{}
			if has("all") || f["historyMode"] == "calls" {
				out["calls"] = s.callSnapshot(f, lo, hi, pricing)
			}
			out["callDetails"] = s.taskCallSnapshot(f, pricing)
			out["timingCoverage"] = callCoverage(s)
		}
		if has("all", "history", "settings") {
			out["telemetry"] = s.get("telemetryStatus")
			out["requestDiagnostics"] = s.get("requestDiagnostics")
			if out["telemetry"] == nil {
				port := int(num(s.settings()["telemetryPort"]))
				out["telemetry"] = Object{"listening": false, "port": port, "config": telemetryConfig(port)}
			}
		}
	}()
	sessions := map[string]Object{}
	available := []Object{}
	if optionsWanted || truth(f["project"]) {
		for _, r := range s.mustQuery("SELECT * FROM sessions") {
			sessions[text(r["id"])] = r
			available = append(available, r)
		}
	}
	if truth(f["account"]) && optionsWanted {
		members := map[string]bool{}
		for _, r := range s.mustQuery("SELECT DISTINCT session FROM usage WHERE account=?", f["account"]) {
			members[text(r["session"])] = true
		}
		kept := []Object{}
		for _, r := range available {
			if members[text(r["id"])] {
				kept = append(kept, r)
			}
		}
		available = kept
	}
	conditions := []string{"u.ts>=?", "u.ts<?"}
	params := []any{lo, hi}
	for _, pair := range [][2]string{{"account", "u.account"}, {"model", "u.model"}, {"session", "u.session"}, {"project", "s.project"}} {
		if truth(f[pair[0]]) {
			conditions = append(conditions, pair[1]+"=?")
			params = append(params, f[pair[0]])
		}
	}
	turns := []Object{}
	if analytics {
		tc := []string{"t.started>=?", "t.started<?"}
		tp := []any{lo, hi}
		for _, k := range []string{"account", "model"} {
			if truth(f[k]) {
				tc = append(tc, "(t."+k+"=? OR EXISTS (SELECT 1 FROM usage u WHERE u.turn=t.id AND u.session=t.session AND u."+k+"=?))")
				tp = append(tp, f[k], f[k])
			}
		}
		if truth(f["session"]) {
			tc = append(tc, "t.session=?")
			tp = append(tp, f["session"])
		}
		if truth(f["project"]) {
			tc = append(tc, "s.project=?")
			tp = append(tp, f["project"])
		}
		turns = s.mustQuery("SELECT t.* FROM turns t LEFT JOIN sessions s ON s.id=t.session WHERE "+strings.Join(tc, " AND ")+" ORDER BY t.started DESC,t.id DESC", tp...)
	}
	sums := Object{"input": 0., "cached": 0., "output": 0., "reasoning": 0., "cache_write": 0., "cost": 0., "saved": 0., "unpriced": 0., "requests": 0., "legacyRequests": 0.}
	maps := map[string]*ranking{"models": newRanking(), "projects": newRanking(), "tasks": newRanking(), "timeline": newRanking()}
	hourly := !truth(f["range"]) || f["range"] == "today"
	var calendar *activityAccumulator
	reuseActivity := false
	if has("all", "overview", "activity") {
		calendar = s.newActivity(f, now)
		activityStart, activityEnd := activityBounds(f, now)
		local := now.In(time.Local)
		tomorrow := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, time.Local)
		if activityEnd.After(tomorrow) {
			activityEnd = tomorrow
		}
		reuseActivity = analytics && !start.After(activityStart) && !end.Before(activityEnd)
	}
	if analytics {
		var sum struct{ requests, legacy, unpriced, cost, saved, input, cached, output, reasoning, cacheWrite float64 }
		bucketMinute, bucket := "", ""
		s.each("SELECT u.* "+usageFrom(f)+" WHERE "+strings.Join(conditions, " AND ")+" ORDER BY u.ts", params, func(u Object) {
			p := pricing.calculate(u)
			if reuseActivity {
				calendar.add(u, p)
			}
			sum.requests++
			if u["kind"] != "逐次记录" && u["kind"] != PiKind && u["kind"] != PiOpenAIKind {
				sum.legacy++
			}
			sum.input += num(u["input"])
			sum.cached += num(u["cached"])
			sum.output += num(u["output"])
			sum.reasoning += num(u["reasoning"])
			sum.cacheWrite += num(u["cache_write"])
			if p.price == nil {
				sum.unpriced++
			} else {
				sum.cost += p.total
				sum.saved += p.saved
			}
			for _, v := range views {
				key := text(u["model"])
				if v == "projects" {
					key = projectOf(sessions, u["session"])
				}
				if v == "tasks" {
					key = text(u["session"])
				}
				maps[v].add(key, u, p)
			}
			if has("all", "overview") {
				format := "2006-01-02"
				if hourly {
					format += " 15:00"
				}
				// Rows arrive in time order, so consecutive rows in the same
				// UTC minute share a bucket; skip re-parsing them.
				ts := text(u["ts"])
				if len(ts) < 16 || ts[:16] != bucketMinute {
					bucketMinute, bucket = "", parse(ts).In(time.Local).Format(format)
					if len(ts) == len("2006-01-02T15:04:05.000Z") && ts[len(ts)-1] == 'Z' {
						bucketMinute = ts[:16]
					}
				}
				maps["timeline"].add(bucket, u, p)
			}
		})
		sums = Object{"input": sum.input, "cached": sum.cached, "output": sum.output, "reasoning": sum.reasoning, "cache_write": sum.cacheWrite, "cost": sum.cost, "saved": sum.saved, "unpriced": sum.unpriced, "requests": sum.requests, "legacyRequests": sum.legacy}
	}
	completed, failed, aborted := 0, 0, 0
	timed, latency := []Object{}, []Object{}
	recordTurns := []Object{}
	search := strings.ToLower(strings.TrimSpace(text(f["recordSearch"])))
	for _, t := range turns {
		switch t["status"] {
		case "completed":
			completed++
			if num(t["duration"]) > 0 {
				timed = append(timed, t)
			}
		case "failed":
			failed++
		case "aborted":
			aborted++
		}
		if t["ttft"] != nil {
			latency = append(latency, t)
		}
		if recordsWanted && (!truth(f["recordStatus"]) || t["status"] == f["recordStatus"]) && matches(t, sessions[text(t["session"])]["project"], search) {
			recordTurns = append(recordTurns, t)
		}
	}
	if f["recordOrder"] == "oldest" {
		for i, j := 0, len(recordTurns)-1; i < j; i, j = i+1, j-1 {
			recordTurns[i], recordTurns[j] = recordTurns[j], recordTurns[i]
		}
	}
	pages := max(1, (len(recordTurns)+pageSize-1)/pageSize)
	recordPage := min(requested, pages)
	begin := min(len(recordTurns), (recordPage-1)*pageSize)
	chosen := recordTurns[begin:min(len(recordTurns), begin+pageSize)]
	byTurn := map[string]*turnTotals{}
	outputByTurn := map[string]float64{}
	if analytics {
		q := "SELECT t.id, COALESCE(SUM(u.output),0) output FROM turns t LEFT JOIN usage u ON u.turn=t.id AND u.session=t.session "
		p := []any{}
		if truth(f["account"]) {
			q += "AND u.account=? "
			p = append(p, f["account"])
		}
		p = append(p, lo, hi)
		for _, r := range s.mustQuery(q+"WHERE t.started>=? AND t.started<? GROUP BY t.id", p...) {
			outputByTurn[text(r["id"])] = num(r["output"])
		}
	}
	if len(chosen) > 0 {
		ids := []any{}
		marks := []string{}
		for _, t := range chosen {
			ids = append(ids, t["id"])
			marks = append(marks, "?")
		}
		s.each("SELECT u.* FROM usage u JOIN turns t ON t.id=u.turn AND t.session=u.session WHERE t.id IN ("+strings.Join(marks, ",")+") ORDER BY u.ts", ids, func(r Object) {
			if truth(f["account"]) && r["account"] != f["account"] {
				return
			}
			key := text(r["turn"])
			if byTurn[key] == nil {
				byTurn[key] = newTurnTotals()
			}
			byTurn[key].add(r, pricing.calculate(r))
		})
	}
	records := []Object{}
	for _, t := range chosen {
		r := clone(t)
		totals := byTurn[text(t["id"])]
		if totals == nil {
			totals = newTurnTotals()
		}
		for k, v := range finishTotals(totals.totals) {
			r[k] = v
		}
		for _, m := range totals.order {
			finishTotals(m)
		}
		r["models"] = totals.order
		r["project"] = projectOf(sessions, t["session"])
		r["source"] = sessions[text(t["session"])]["source"]
		r["origin"] = sessions[text(t["session"])]["origin"]
		records = append(records, r)
	}
	duration, output := 0., 0.
	var latest Object
	for _, t := range timed {
		tokens := outputByTurn[text(t["id"])]
		if tokens <= 0 {
			continue
		}
		duration += num(t["duration"])
		output += tokens
		if latest == nil {
			latest = clone(t)
			latest["output"] = tokens
		}
	}
	var account any = f["account"]
	if !truth(account) {
		account = s.get("currentAccount")
	}
	if !truth(account) {
		account = Unknown
	}
	aq := any(nil)
	if truth(f["account"]) {
		aq = f["account"]
	}
	active := 0
	for _, t := range s.mustQuery("SELECT t.* FROM turns t WHERE t.status='running' AND t.last_seen>=? AND (? IS NULL OR t.account=? OR EXISTS (SELECT 1 FROM usage u WHERE u.turn=t.id AND u.session=t.session AND u.account=?)) ORDER BY t.started DESC,t.id DESC", iso(now.Add(-2*time.Minute)), aq, aq, aq) {
		if (!truth(f["model"]) || t["model"] == f["model"]) && (!truth(f["session"]) || t["session"] == f["session"]) && (!truth(f["project"]) || sessions[text(t["session"])]["project"] == f["project"]) {
			active++
		}
	}
	quotas := s.latestQuotas(account)
	history := []Object{}
	samples := 0
	if historyWanted {
		history, samples = s.quotaHistory(account, lo, hi)
	}
	sums["total"] = num(sums["input"]) + num(sums["output"])
	sums["cacheRate"] = nil
	if num(sums["input"]) > 0 {
		sums["cacheRate"] = num(sums["cached"]) / num(sums["input"])
	}
	performance := Object{"completed": completed, "failed": failed, "aborted": aborted, "successRate": nil, "active": active, "activeReason": "最近 2 分钟有统计事件且未结束的任务；等待或静默任务可能不计入", "avgDuration": nil, "ttft": nil, "ttftSamples": len(latency), "taskTps": nil, "latestTps": nil, "latestAt": nil}
	if completed+failed > 0 {
		performance["successRate"] = float64(completed) / float64(completed+failed)
	}
	if len(timed) > 0 {
		v := 0.
		for _, t := range timed {
			v += num(t["duration"])
		}
		performance["avgDuration"] = v / float64(len(timed))
	}
	if len(latency) > 0 {
		v := 0.
		for _, t := range latency {
			v += num(t["ttft"])
		}
		performance["ttft"] = v / float64(len(latency))
	}
	if duration > 0 {
		performance["taskTps"] = output / (duration / 1000)
	}
	if latest != nil {
		performance["latestTps"] = num(latest["output"]) / (num(latest["duration"]) / 1000)
		performance["latestAt"] = latest["ended"]
	}
	tasks := maps["tasks"].rank()
	for _, t := range tasks {
		t["project"] = projectOf(sessions, t["name"])
	}
	timeline := maps["timeline"].list()
	sort.SliceStable(timeline, func(i, j int) bool { return text(timeline[i]["name"]) < text(timeline[j]["name"]) })
	for _, r := range timeline {
		v := text(r["name"])
		format := "2006-01-02"
		if strings.Contains(v, " ") {
			format += " 15:04"
		}
		t, _ := time.ParseInLocation(format, v, time.Local)
		r["time"] = t.UnixMilli()
	}
	options := Object{"models": []string{}, "projects": []string{}, "sessions": []Object{}}
	accountArgs := []any{}
	accountClause := ""
	if truth(f["account"]) {
		accountArgs = append(accountArgs, f["account"])
		accountClause = "WHERE account=?"
	}
	if optionsWanted {
		models := []string{}
		// A loose index scan: one index seek per distinct model, not one row
		// visit per usage record.
		scope, scopeArgs := "", []any{}
		if truth(f["account"]) {
			scope, scopeArgs = " AND account=?", []any{f["account"], f["account"]}
		}
		for _, r := range s.mustQuery("WITH RECURSIVE m(model) AS (SELECT MIN(model) FROM usage WHERE model IS NOT NULL"+scope+" UNION ALL SELECT (SELECT MIN(model) FROM usage WHERE model>m.model"+scope+") FROM m WHERE m.model IS NOT NULL) SELECT model FROM m WHERE model IS NOT NULL", scopeArgs...) {
			models = append(models, text(r["model"]))
		}
		projects := []string{}
		seen := map[string]bool{}
		for _, r := range available {
			p := text(r["project"])
			if !seen[p] {
				projects = append(projects, p)
				seen[p] = true
			}
		}
		sort.Strings(projects)
		options["models"], options["projects"] = models, projects
	}
	if has("all", "settings") {
		v := []Object{}
		for _, r := range available {
			v = append(v, Object{"id": r["id"], "project": r["project"]})
		}
		options["sessions"] = v
	}
	var activity any
	if calendar != nil {
		if !reuseActivity {
			s.each(calendar.query, calendar.args, func(u Object) { calendar.add(u, pricing.calculate(u)) })
		}
		activity = calendar.result()
	}
	var status any
	if q := obj(s.get("quotaStatus")); q["account"] == account {
		status = q
	}
	priceOutput := []Object{}
	if has("all", "settings") {
		priceOutput = prices
	}
	return Object{"activity": activity, "range": Object{"start": lo, "end": hi}, "sums": sums, "performance": performance, "models": maps["models"].rank(), "projects": maps["projects"].rank(), "tasks": tasks, "timeline": timeline, "turns": records, "quotas": quotas, "records": Object{"page": recordPage, "pageSize": pageSize, "total": len(recordTurns), "pages": pages}, "quotaHistorySamples": samples, "quotaHistory": history, "quotaAccount": account, "scan": s.get("scan"), "quotaStatus": status, "currentAccount": s.get("currentAccount"), "accounts": s.mustQuery("SELECT * FROM accounts ORDER BY label,id"), "coverage": s.mustOne("SELECT (SELECT MIN(ts) FROM usage "+accountClause+") first,(SELECT MAX(ts) FROM usage "+accountClause+") last,(SELECT COUNT(*) FROM usage "+accountClause+") records", append(append(append([]any{}, accountArgs...), accountArgs...), accountArgs...)...), "options": options, "settings": s.settings(), "prices": priceOutput, "recovery": s.get("recovery")}, nil
}

func (s *Store) quotaHistory(account any, lo, hi string) ([]Object, int) {
	qs := s.mustQuery("SELECT * FROM quotas WHERE account=? AND ts>=? AND ts<? ORDER BY ts", account, lo, hi)
	if len(qs) == 0 {
		return []Object{}, 0
	}
	first := parse(qs[0]["ts"]).UnixMilli()
	width := math.Max(1, float64(parse(qs[len(qs)-1]["ts"]).UnixMilli()-first)/150)
	bins := map[string][]Object{}
	binOrder := []string{}
	previous := map[string]Object{}
	points := map[string]Object{}
	pointOrder := []string{}
	put := func(q Object) {
		id := text(q["id"])
		if points[id] == nil {
			pointOrder = append(pointOrder, id)
		}
		points[id] = q
	}
	for _, q := range qs {
		k := text(q["account"]) + ":" + text(q["bucket"]) + ":" + text(q["slot"])
		p := previous[k]
		minutes := num(q["minutes"])
		if minutes == 0 {
			minutes = 300
		}
		maxGap := math.Max(60, math.Min(minutes, 360)) * 60000
		if p != nil && float64(parse(q["ts"]).UnixMilli()-parse(p["ts"]).UnixMilli()) > maxGap {
			q["gapBefore"] = true
			put(p)
			put(q)
		}
		previous[k] = q
		reset := "null"
		if q["resets"] != nil {
			reset = text(q["resets"])
		}
		key := k + ":" + reset + ":" + fmt.Sprint(math.Floor(float64(parse(q["ts"]).UnixMilli()-first)/width))
		b := bins[key]
		if b == nil {
			b = []Object{q, q, q, q}
			bins[key] = b
			binOrder = append(binOrder, key)
		}
		b[1] = q
		if num(q["used"]) < num(b[2]["used"]) {
			b[2] = q
		}
		if num(q["used"]) > num(b[3]["used"]) {
			b[3] = q
		}
	}
	for _, k := range binOrder {
		for _, q := range bins[k] {
			put(q)
		}
	}
	out := []Object{}
	for _, k := range pointOrder {
		out = append(out, points[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return text(out[i]["ts"]) < text(out[j]["ts"]) })
	return out, len(qs)
}

func (s *Store) Export(f Object, now time.Time) (string, error) {
	f = s.resolve(f)
	start, end, e := Bounds(f, now)
	if e != nil {
		return "", e
	}
	if f["exportMode"] == "calls" {
		return s.exportCalls(f, iso(start), iso(end)), nil
	}
	conditions := []string{"u.ts>=?", "u.ts<?"}
	params := []any{iso(start), iso(end)}
	for _, p := range [][2]string{{"account", "u.account"}, {"model", "u.model"}, {"session", "u.session"}, {"project", "s.project"}, {"recordStatus", "t.status"}} {
		if truth(f[p[0]]) {
			conditions = append(conditions, p[1]+"=?")
			params = append(params, f[p[0]])
		}
	}
	search := strings.ToLower(strings.TrimSpace(text(f["recordSearch"])))
	if search != "" || truth(f["recordStatus"]) {
		conditions = append(conditions, "t.started>=?", "t.started<?")
		params = append(params, iso(start), iso(end))
	}
	join := ""
	if truth(f["project"]) {
		join = " AND s.project=?"
		params = append([]any{f["project"]}, params...)
	}
	pricing := indexPrices(s.prices())
	cols := []string{"ts", "model", "session", "turn", "input", "cached", "output", "reasoning", "cache_write", "cost", "saved", "priceId", "kind", "account", "source", "origin"}
	b := newCSV(cols)
	s.each("SELECT u.*,s.project,s.source,s.origin,t.id record_id,t.session record_session,t.model record_model FROM usage u LEFT JOIN sessions s ON s.id=u.session"+join+" LEFT JOIN turns t ON t.id=u.turn AND t.session=u.session WHERE "+strings.Join(conditions, " AND ")+" ORDER BY u.ts", params, func(r Object) {
		if !matches(Object{"id": r["record_id"], "session": r["record_session"], "model": r["record_model"]}, r["project"], search) {
			return
		}
		pricing.calculate(r).write(r)
		b.row(r)
	})
	return b.String(), nil
}
