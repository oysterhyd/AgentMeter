package monitor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// desktopSample is one synthetic plan-usage-history.json entry; nil windows
// are omitted like Desktop's samples before any usage.
func desktopSample(at time.Time, org string, five, week any) Object {
	u := Object{}
	if five != nil {
		u["fh"] = five
	}
	if week != nil {
		u["sd"] = week
	}
	return Object{"t": float64(at.UnixMilli()), "org": org, "u": u}
}

func writeDesktopHistory(t *testing.T, path string, version float64, samples ...Object) {
	t.Helper()
	list := []any{}
	for _, s := range samples {
		list = append(list, s)
	}
	if err := os.WriteFile(path, []byte(jsonText(Object{"version": version, "samples": list})), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeDesktopHistoryRecordsQuotaReadOnly(t *testing.T) {
	s, f := testStore(t), newClaudeFixture(t)
	history := filepath.Join(t.TempDir(), ClaudeDesktopUsageFile)
	src := Sources{Home: t.TempDir(), ClaudeHome: f.home, ClaudeConfig: f.config, ClaudeDesktopUsage: history}
	scan := func() {
		t.Helper()
		if _, err := s.ScanSources(context.Background(), src, true, nil); err != nil {
			t.Fatal(err)
		}
		s.claudeDesktop.size = -1 // rapid rewrites can keep size and coarse mtime
	}
	latest := func(slot string) Object {
		for _, q := range s.latestQuotas(s.get("currentClaudeAccount")) {
			if q["bucket"] == "claude" && q["slot"] == slot {
				return q
			}
		}
		return nil
	}
	rows := func() float64 {
		return num(s.mustOne("SELECT COUNT(*) n FROM quotas WHERE source=?", ClaudeDesktopSource)["n"])
	}
	// The cached snapshot at f.at resets its 5-hour window at +3h and its
	// 7-day window at +72h.
	early, late := f.at.Add(10*time.Minute), f.at.Add(5*time.Hour)
	writeDesktopHistory(t, history, 2,
		desktopSample(f.at.Add(-time.Hour), "org-acct-1", nil, nil),
		desktopSample(early, "org-acct-1", 20, 30),
		desktopSample(f.at.Add(20*time.Minute), "org-other", 99, 99),
		desktopSample(f.at.Add(30*time.Minute), "org-acct-1", -1, "high"),
		desktopSample(late, "org-acct-1", 4, 31))
	before, _ := os.ReadFile(history)
	scan()
	if after, _ := os.ReadFile(history); !bytes.Equal(after, before) {
		t.Fatal("Desktop history modified")
	}
	if rows() != 4 {
		t.Fatal("expected two samples of two windows", s.mustQuery("SELECT ts,slot,used FROM quotas WHERE source=?", ClaudeDesktopSource))
	}
	if q := latest("primary"); num(q["used"]) != 4 || q["resets"] != nil || q["source"] != ClaudeDesktopSource || q["plan"] != "Max" || num(q["minutes"]) != 300 {
		t.Fatal("reset window must not borrow the expired reset", q)
	}
	if q := latest("secondary"); num(q["used"]) != 31 || num(q["resets"]) != float64(f.at.Add(72*time.Hour).Unix()) || num(q["minutes"]) != 10080 {
		t.Fatal("weekly reset not inferred", q)
	}
	if q := s.mustOne("SELECT resets FROM quotas WHERE source=? AND slot='primary' AND ts=?", ClaudeDesktopSource, iso(early)); num(q["resets"]) != float64(f.at.Add(3*time.Hour).Unix()) {
		t.Fatal("five-hour reset not inferred", q)
	}
	if status := obj(s.get("claudeQuotaStatus")); status["attempted"] != iso(late) {
		t.Fatal(status)
	}
	// Rereads neither replay nor duplicate samples; unknown formats are ignored.
	scan()
	writeDesktopHistory(t, history, 3, desktopSample(late.Add(time.Minute), "org-acct-1", 50, 50))
	scan()
	if rows() != 4 || num(latest("primary")["used"]) != 4 {
		t.Fatal("unknown history version recorded", rows())
	}
	writeDesktopHistory(t, history, 2, desktopSample(late, "org-acct-1", 4, 31), desktopSample(late.Add(30*time.Minute), "org-acct-1", 9, 32))
	written, _ := os.ReadFile(history)
	scan()
	if rows() != 6 || num(latest("primary")["used"]) != 9 {
		t.Fatal("new sample not recorded", rows())
	}
	if after, _ := os.ReadFile(history); !bytes.Equal(after, written) {
		t.Fatal("Desktop history modified")
	}
	// Another signed-in organization's samples never cross accounts.
	f.account("acct-2", "other@example.test", 1, 1)
	scan()
	if n := s.mustOne("SELECT COUNT(*) n FROM quotas WHERE source=? AND account=?", ClaudeDesktopSource, s.get("currentClaudeAccount"))["n"]; num(n) != 0 {
		t.Fatal("samples attributed to another organization", n)
	}
}

func TestNewestClaudeDesktopUsage(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "claude_a", ClaudeDesktopUsageFile)
	newer := filepath.Join(root, "claude_b", ClaudeDesktopUsageFile)
	for i, path := range []string{older, newer} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		writeDesktopHistory(t, path, 2)
		at := time.Now().Add(time.Duration(i-2) * time.Hour)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if got := NewestClaudeDesktopUsage([]string{filepath.Join(root, "missing", "*"), filepath.Join(root, "claude_*", ClaudeDesktopUsageFile)}); got != newer {
		t.Fatal(got)
	}
	if got := NewestClaudeDesktopUsage(nil); got != "" {
		t.Fatal(got)
	}
}
