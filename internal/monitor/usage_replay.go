package monitor

// A parser upgrade replays a complete source file in its existing transaction.
// Retain genuine legacy deltas, even when the same event appears again after
// modern records. Only exact, source-proven cumulative duplicates are removed.
type legacyUsageReplay struct {
	existing   map[string]map[string]bool
	retained   map[string]bool
	suppressed map[string]Object
}

func legacyUsageID(state Object, ts string, raw []byte) string {
	key := ts + rawAt(raw, "payload", "info", "total_token_usage")
	if state["source"] == "cli" || state["source"] == "vscode" {
		key = text(state["session"]) + "|" + key
	}
	return "legacy:" + hash(key)
}

func (s *Store) suppressLegacyUsage(state Object, ts string, raw []byte, total Object) error {
	r := s.legacyReplay
	session := text(state["session"])
	candidates, loaded := r.existing[session]
	if !loaded {
		// Query once per replayed session, using usage_session_time. Most
		// sessions have no obsolete rows, so avoid hashing and DELETEs per call.
		rows, err := s.query("SELECT id FROM usage WHERE session=? AND kind='累计差分' AND substr(id,1,7)='legacy:'", session)
		if err != nil {
			return err
		}
		candidates = map[string]bool{}
		for _, row := range rows {
			candidates[text(row["id"])] = true
		}
		r.existing[session] = candidates
	}
	if len(candidates) > 0 {
		id := legacyUsageID(state, ts, raw)
		if candidates[id] {
			r.suppressed[id] = Object{"session": session, "ts": ts, "total": total}
		}
	}
	return nil
}

func (s *Store) reconcileLegacyUsage() error {
	for id, candidate := range s.legacyReplay.suppressed {
		if s.legacyReplay.retained[id] {
			continue
		}
		total := obj(candidate["total"])
		// The faulty resume reset inserted the entire cumulative total, rather
		// than a delta. Never remove unrelated or genuine legacy usage.
		where := `id=? AND session=? AND ts=? AND kind='累计差分'
            AND input=? AND cached=? AND output=?
            AND COALESCE(reasoning,0)=? AND COALESCE(cache_write,0)=?`
		args := []any{id, candidate["session"], candidate["ts"], num(total["input_tokens"]), num(total["cached_input_tokens"]), num(total["output_tokens"]), num(total["reasoning_output_tokens"]), num(total["cache_write_input_tokens"])}
		// Keep diagnostic events so normal reconciliation can associate their
		// response IDs with real calls; deleting usage must not lose timings.
		if err := s.exec("UPDATE request_events SET usage_id=NULL,association=NULL WHERE usage_id IN (SELECT id FROM usage WHERE "+where+")", args...); err != nil {
			return err
		}
		if err := s.exec("DELETE FROM usage WHERE "+where, args...); err != nil {
			return err
		}
	}
	return nil
}
