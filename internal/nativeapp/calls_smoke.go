package nativeapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Exercise the real call view, including the full-history backend workload.
// This is used only by isolated acceptance and process-memory runs.
func (a *App) smokeCalls() {
	var savedFilter Object
	var savedMode int
	a.updateWait(func() {
		savedFilter, savedMode = clone(a.filter), a.historyMode
		a.filter["range"], a.historyMode = "all", 1
		a.navigate(2)
	})
	ready := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		a.updateWait(func() {
			ready = !a.loading && a.snapshotPage == 2 && a.lastRenderedPage == 2 && len(obj(a.data["calls"])) > 0
		})
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(motionReveal + 60*time.Millisecond)
	if ready {
		if png, err := a.win.CapturePage(); err == nil {
			_ = os.WriteFile(filepath.Join(a.options.Capture, "calls.png"), png, 0600)
		}
	}
	a.updateWait(func() {
		calls := obj(a.data["calls"])
		data, _ := json.Marshal(Object{"verified": ready && a.errorText == "", "rows": len(objects(calls["rows"])), "pagination": calls["pagination"], "summary": calls["summary"], "error": a.errorText})
		_ = os.WriteFile(filepath.Join(a.options.Capture, "calls-result.json"), data, 0600)
		a.filter, a.historyMode = savedFilter, savedMode
		a.navigate(4)
	})
}
