package monitor

import (
	"math"
	"time"
)

func activityBounds(f Object, now time.Time) (time.Time, time.Time) {
	now = now.In(time.Local)
	year := int(num(f["activityYear"]))
	if year < 1970 || year > now.Year() || num(f["activityYear"]) != float64(year) {
		year = now.Year()
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(1, 0, 0)
	return start, end
}

type activityAccumulator struct {
	now           time.Time
	year          int
	lo, hi, today string
	first         any
	days          map[string]Object
	order, hours  []Object
	members       map[string]map[string]bool
	sessions      map[string]bool
	query         string
	args          []any
}

func (s *Store) newActivity(f Object, now time.Time) *activityAccumulator {
	now = now.In(time.Local)
	start, end := activityBounds(f, now)
	year := start.Year()
	where := ""
	values := []any{}
	for _, p := range [][2]string{{"account", "u.account"}, {"model", "u.model"}, {"session", "u.session"}, {"project", "s.project"}} {
		if truth(f[p[0]]) {
			where += " AND " + p[1] + "=?"
			values = append(values, f[p[0]])
		}
	}
	join := usageFrom(f)
	first := s.mustOne("SELECT MIN(u.ts) first "+join+" WHERE 1=1"+where, values...)["first"]
	a := &activityAccumulator{now: now, year: year, lo: iso(start), hi: iso(end), today: now.Format("2006-01-02"), first: first,
		days: map[string]Object{}, order: []Object{}, members: map[string]map[string]bool{}, sessions: map[string]bool{}, hours: make([]Object, 24)}
	for h := 0; h < 24; h++ {
		a.hours[h] = Object{"hour": h, "total": 0., "requests": 0.}
	}
	a.args = append([]any{a.lo, a.hi}, values...)
	a.query = "SELECT u.* " + join + " WHERE u.ts>=? AND u.ts<?" + where + " ORDER BY u.ts"
	return a
}

func (s *Store) activity(f Object, pricing priceIndex, now time.Time, rows []Object) Object {
	a := s.newActivity(f, now)
	if rows == nil {
		s.each(a.query, a.args, func(u Object) { a.add(u, pricing.calculate(u)) })
	} else {
		for _, u := range rows {
			a.add(u, pricing.calculate(u))
		}
	}
	return a.result()
}

func (a *activityAccumulator) add(u Object, p usageCost) {
	if ts := text(u["ts"]); ts < a.lo || ts >= a.hi {
		return
	}
	t := parse(u["ts"]).In(time.Local)
	key := t.Format("2006-01-02")
	if key > a.today {
		return
	}
	day := a.days[key]
	if day == nil {
		day = Object{"date": key, "total": 0., "output": 0., "requests": 0., "cost": 0., "unpriced": 0.}
		a.days[key] = day
		a.order = append(a.order, day)
		a.members[key] = map[string]bool{}
	}
	tokens := num(u["input"]) + num(u["output"])
	day["total"] = num(day["total"]) + tokens
	day["output"] = num(day["output"]) + num(u["output"])
	day["requests"] = num(day["requests"]) + 1
	if p.price == nil {
		day["unpriced"] = num(day["unpriced"]) + 1
	} else {
		day["cost"] = num(day["cost"]) + p.total
	}
	a.members[key][text(u["session"])] = true
	a.sessions[text(u["session"])] = true
	h := a.hours[t.Hour()]
	h["total"] = num(h["total"]) + tokens
	h["requests"] = num(h["requests"]) + 1
}

func (a *activityAccumulator) result() Object {
	active := map[int64]bool{}
	longest, streak := 0, 0
	previous := int64(math.MinInt64)
	total, requests := 0., 0.
	var peak any
	for _, day := range a.order {
		day["sessions"] = len(a.members[text(day["date"])])
		if day["unpriced"] == day["requests"] {
			day["cost"] = nil
		}
		total += num(day["total"])
		requests += num(day["requests"])
		if num(day["total"]) <= 0 {
			continue
		}
		d, _ := time.Parse("2006-01-02", text(day["date"]))
		index := d.Unix() / 86400
		active[index] = true
		if index == previous+1 {
			streak++
		} else {
			streak = 1
		}
		longest = max(longest, streak)
		previous = index
		if peak == nil || num(day["total"]) > num(obj(peak)["total"]) {
			peak = day
		}
	}
	current := 0
	if a.year == a.now.Year() {
		d, _ := time.Parse("2006-01-02", a.today)
		i := d.Unix() / 86400
		if !active[i] {
			i--
		}
		for active[i] {
			current++
			i--
		}
	}
	firstYear := a.now.Year()
	if truth(a.first) {
		firstYear = parse(a.first).In(time.Local).Year()
	}
	return Object{"year": a.year, "today": a.today, "firstYear": firstYear, "days": a.order, "hours": a.hours, "stats": Object{"activeDays": len(active), "longestStreak": longest, "currentStreak": current, "sessions": len(a.sessions), "total": total, "requests": requests, "peak": peak}}
}

func (s *Store) Widget(now time.Time) Object {
	account := s.get("currentAccount")
	if !truth(account) {
		account = Unknown
	}
	local := now.In(time.Local)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	lo := yesterday
	if now.Add(-24 * time.Hour).Before(lo) {
		lo = now.Add(-24 * time.Hour)
	}
	timeline, speed := []Object{}, []Object{}
	for i := 0; i < 96; i++ {
		timeline = append(timeline, Object{"time": now.UnixMilli() - int64(95-i)*900000, "total": 0.})
	}
	for i := 0; i < 20; i++ {
		speed = append(speed, Object{"time": now.UnixMilli() - int64(19-i)*3000, "total": 0.})
	}
	total, previous, input, cached, output := 0., 0., 0., 0., 0.
	s.each("SELECT ts,input,cached,output FROM usage WHERE ts>=? AND ts<=? ORDER BY ts", []any{iso(lo), iso(now)}, func(r Object) {
		t := parse(r["ts"])
		tokens := num(r["input"]) + num(r["output"])
		if !t.Before(today) {
			total += tokens
			input += num(r["input"])
			cached += num(r["cached"])
		} else if !t.Before(yesterday) {
			previous += tokens
		}
		bin := int(math.Ceil(float64(t.UnixMilli()-(now.UnixMilli()-86400000))/900000)) - 1
		if bin >= 0 && bin < 96 {
			timeline[bin]["total"] = num(timeline[bin]["total"]) + tokens
		}
		bin = int(math.Ceil(float64(t.UnixMilli()-(now.UnixMilli()-60000))/3000)) - 1
		if bin >= 0 && bin < 20 {
			output += num(r["output"])
			speed[bin]["total"] = num(speed[bin]["total"]) + num(r["output"])/3
		}
	})
	quotas := s.latestQuotas(account)
	bucket := widgetBucket(quotas)
	chosen := []Object{}
	for _, q := range quotas {
		if q["bucket"] == bucket {
			chosen = append(chosen, q)
		}
	}
	change, cacheRate := any(nil), any(nil)
	if previous > 0 {
		change = (total - previous) / previous
	}
	if input > 0 {
		cacheRate = cached / input
	}
	var status any
	if q := obj(s.get("quotaStatus")); q["account"] == account {
		status = q
	}
	r := Object{"account": account, "usageScope": "all", "total": total, "previous": previous, "change": change, "cacheRate": cacheRate, "tps": output / 60, "speed": speed, "timeline": timeline, "quotas": chosen, "scan": s.get("scan"), "quotaStatus": status, "language": s.settings()["language"]}
	if bucket != nil {
		r["bucket"] = bucket
	}
	return r
}

func widgetBucket(quotas []Object) any {
	var first, primary any
	prefer := func(candidate string, current any) bool {
		if current == nil {
			return true
		}
		previous := text(current)
		return candidate != previous && (candidate == "codex" || previous != "codex" && candidate < previous)
	}
	for _, q := range quotas {
		bucket := text(q["bucket"])
		if prefer(bucket, first) {
			first = bucket
		}
		if num(q["minutes"]) == 300 && prefer(bucket, primary) {
			primary = bucket
		}
	}
	if primary != nil {
		return primary
	}
	return first
}

func (s *Store) latestQuotas(account any) []Object {
	return s.mustQuery("SELECT q.* FROM (SELECT DISTINCT account,bucket,slot FROM quotas WHERE account=?) b JOIN quotas q ON q.rowid=(SELECT rowid FROM quotas WHERE account=b.account AND bucket=b.bucket AND slot=b.slot ORDER BY ts DESC,rowid DESC LIMIT 1)", account)
}
