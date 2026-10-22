package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Claude Desktop runs Claude Code headless (stream-json), which neither runs
// statusLine nor refreshes cachedUsageUtilization. Desktop itself samples the
// signed-in organization's 5-hour and 7-day utilization into
// plan-usage-history.json; Monitor only reads that file, never writes it.

const (
	ClaudeDesktopSource    = "Claude Desktop 用量记录"
	ClaudeDesktopUsageFile = "plan-usage-history.json"
	// claudeDesktopVersion is the only history format understood; another
	// version is ignored rather than guessed at.
	claudeDesktopVersion = 2
)

// claudeDesktopHistory decodes only sample times, organizations and the two
// utilization percentages.
type claudeDesktopHistory struct {
	Version int `json:"version"`
	Samples []struct {
		T   float64 `json:"t"`
		Org string  `json:"org"`
		// Windows stay untyped so a mistyped value is skipped, not read as 0.
		U struct {
			FiveHour any `json:"fh"`
			SevenDay any `json:"sd"`
		} `json:"u"`
	} `json:"samples"`
}

// claudeDesktopCache remembers the last history read and the newest sample
// already recorded, so an unchanged file is neither reread nor replayed.
type claudeDesktopCache struct {
	path     string
	size     int64
	mtime    time.Time
	history  *claudeDesktopHistory
	org      string
	recorded float64
}

// NewestClaudeDesktopUsage returns the most recently written history matching
// patterns: the Store build keeps it under Claude-Data\claude_<publisher>.
func NewestClaudeDesktopUsage(patterns []string) string {
	newest, at := "", time.Time{}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			if st, err := os.Stat(path); err == nil && !st.IsDir() && st.ModTime().After(at) {
				newest, at = path, st.ModTime()
			}
		}
	}
	return newest
}

// readClaudeDesktop returns the history, rereading the file only when it
// changed. A half-written or unknown-format file keeps the previous history.
func (s *Store) readClaudeDesktop(path string) *claudeDesktopHistory {
	c := &s.claudeDesktop
	if c.path != path {
		*c = claudeDesktopCache{path: path}
	}
	st, err := os.Stat(path)
	if path == "" || err != nil {
		return c.history
	}
	if c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.history
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c.history
	}
	var history claudeDesktopHistory
	if json.Unmarshal(b, &history) != nil || history.Version != claudeDesktopVersion {
		return c.history
	}
	c.size, c.mtime, c.history = st.Size(), st.ModTime(), &history
	return c.history
}

// observeClaudeDesktop records the organization's new Desktop samples for
// account, returning the timestamp of the newest one.
func (s *Store) observeClaudeDesktop(path, account, org, plan string) (string, error) {
	c := &s.claudeDesktop
	history := s.readClaudeDesktop(path)
	if history == nil || org == "" {
		return "", nil
	}
	if c.org != org {
		c.org, c.recorded = org, 0
	}
	latest, newest := "", c.recorded
	cleared := s.clearedAt()
	for _, sample := range history.Samples {
		if sample.Org != org || !validNumber(sample.T) || sample.T <= 0 {
			continue
		}
		at := time.UnixMilli(int64(sample.T))
		ts := iso(at)
		if cleared != "" && ts <= cleared {
			continue
		}
		if sample.T <= c.recorded {
			latest = max(latest, ts)
			continue
		}
		recorded := false
		for _, w := range []struct {
			slot    string
			minutes float64
			used    any
		}{{"primary", 300, sample.U.FiveHour}, {"secondary", 10080, sample.U.SevenDay}} {
			value, ok := w.used.(float64)
			if !ok || !validNumber(value) || value < 0 {
				continue
			}
			used := min(100., value)
			// Desktop keeps no reset time; reuse one another source reported
			// for the window this sample falls in. The ID omits it, so a later
			// inference never duplicates the sample.
			var resets any
			if r := s.mustOne("SELECT resets FROM quotas WHERE account=? AND bucket='claude' AND slot=? AND resets>? AND resets-?<=? ORDER BY ts DESC LIMIT 1",
				account, w.slot, float64(at.Unix()), w.minutes*60, float64(at.Unix()))["resets"]; r != nil {
				resets = r
			}
			var p any
			if plan != "" {
				p = plan
			}
			id := hash(account + "|" + ts + "|claude-desktop|" + w.slot + "|" + text(used))
			if e := s.exec("INSERT OR IGNORE INTO quotas(id,ts,bucket,slot,used,minutes,resets,plan,source,account,agent) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, ts, "claude", w.slot, used, w.minutes, resets, p, ClaudeDesktopSource, account, AgentClaude); e != nil {
				return "", e
			}
			recorded = true
		}
		if recorded {
			latest = max(latest, ts)
			newest = max(newest, sample.T)
		}
	}
	c.recorded = newest
	return latest, nil
}
