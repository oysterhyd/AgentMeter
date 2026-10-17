package nativeapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Run only in the isolated native acceptance profile. Exercise the same load
// and generation handling used by the time control, including superseded work.
func (a *App) smokeRanges() {
	var savedFilter Object
	a.updateWait(func() { savedFilter = clone(a.filter); a.navigate(0) })
	wait := func() bool {
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			ready := false
			a.updateWait(func() {
				key, _ := json.Marshal(a.requestFilter())
				ready = !a.loading && a.snapshotPage == a.page && a.snapshotKey == string(key)
			})
			if ready {
				return true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return false
	}
	verified := wait()
	timings := []Object{}
	for _, r := range []string{"today", "7d", "30d", "all", "today", "7d", "30d", "all", "custom"} {
		started := time.Now()
		a.updateWait(func() {
			if r == "custom" {
				now := time.Now()
				a.filter["start"], a.filter["end"] = now.AddDate(0, 0, -6).Format("2006-01-02"), now.Format("2006-01-02")
			}
			a.change("range", r)
		})
		ready := wait()
		verified = verified && ready
		timings = append(timings, Object{"range": r, "ready": ready, "snapshotMs": float64(time.Since(started)) / float64(time.Millisecond)})
	}
	a.updateWait(func() {
		for _, r := range []string{"today", "30d", "7d", "all"} {
			a.change("range", r)
		}
	})
	latestWins := wait()
	var f Object
	a.updateWait(func() { f = a.requestFilter() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	csv, err := a.client.Call(ctx, "export", f)
	cancel()
	ideExport := err == nil && strings.Contains(str(csv), `"vscode"`)
	// Capture both the restored range reveal and its settled frame. Snapshot
	// timing above remains independent of animation duration.
	if png, err := a.win.CapturePage(); err == nil {
		_ = os.WriteFile(filepath.Join(a.options.Capture, "ranges-enter.png"), png, 0600)
	}
	time.Sleep(motionReveal + 60*time.Millisecond)
	if png, err := a.win.CapturePage(); err == nil {
		_ = os.WriteFile(filepath.Join(a.options.Capture, "ranges.png"), png, 0600)
	}
	a.updateWait(func() {
		verified = verified && latestWins && err == nil && a.errorText == ""
		data, _ := json.MarshalIndent(Object{"verified": verified, "latestWins": latestWins, "vscodeExport": ideExport, "timings": timings}, "", "  ")
		_ = os.WriteFile(filepath.Join(a.options.Capture, "ranges-result.json"), data, 0600)
		if !verified {
			a.errorText = "时间范围切换验收失败"
		}
		a.filter = savedFilter
		a.navigate(4)
	})
}
