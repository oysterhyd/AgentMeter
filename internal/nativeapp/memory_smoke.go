package nativeapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/egoist/mygo"
)

// Memory acceptance keeps real windows alive long enough for idle sampling.
// Heap profiles are opt-in and taken in separate diagnostic runs: forcing GC
// would otherwise distort the Windows process-memory measurements.
func (a *App) smokeMemory() {
	_ = os.MkdirAll(a.options.Capture, 0700)
	started := time.Now()
	checkpoints := []Object{}
	transitions := []Object{}
	widget := func(stage string) {
		a.updateWait(func() {
			start := time.Now()
			a.showWidget()
			transitions = append(transitions, Object{"stage": stage, "prepareMs": float64(time.Since(start)) / float64(time.Millisecond)})
		})
	}
	checkpoint := func(stage string) {
		_ = os.WriteFile(filepath.Join(a.options.Capture, "memory-stage.txt"), []byte(stage), 0600)
		time.Sleep(4 * time.Second)
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		checkpoint := Object{"stage": stage, "elapsedMs": time.Since(started).Milliseconds(), "heapAlloc": stats.HeapAlloc, "heapInuse": stats.HeapInuse, "heapSys": stats.HeapSys, "heapReleased": stats.HeapReleased, "totalAlloc": stats.TotalAlloc, "numGC": stats.NumGC}
		if os.Getenv("MONITOR_MEMORY_PROFILE") == "1" {
			runtime.GC()
			runtime.ReadMemStats(&stats)
			checkpoint["liveHeap"] = stats.HeapAlloc
			if f, err := os.Create(filepath.Join(a.options.Capture, stage+".pprof")); err == nil {
				_ = pprof.WriteHeapProfile(f)
				_ = f.Close()
			}
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	wait := func(page int) bool {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			ready := false
			a.updateWait(func() {
				ready = !a.loading && a.snapshotPage == page && a.lastRenderedPage == page && a.errorText == ""
			})
			if ready {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	a.updateWait(func() { a.win.Show(); a.win.Focus() })
	verified := wait(0)
	checkpoint("overview")
	for page := 1; page < len(pageIDs); page++ {
		a.updateWait(func() { a.navigate(page) })
		verified = wait(page) && verified
	}
	checkpoint("pages")
	a.updateWait(func() { a.filter["range"], a.historyMode = "all", 1; a.navigate(2) })
	verified = wait(2) && verified
	a.updateWait(func() {
		calls := obj(a.data["calls"])
		verified = verified && len(objects(calls["rows"])) > 0
		data, _ := json.Marshal(Object{"rows": len(objects(calls["rows"])), "pagination": calls["pagination"]})
		_ = os.WriteFile(filepath.Join(a.options.Capture, "memory-calls.json"), data, 0600)
	})
	checkpoint("calls")
	widget("card")
	checkpoint("card")
	a.updateWait(func() {
		verified = verified && a.widget != nil && a.widgetVerified
		if a.widget != nil {
			a.widget.activate("collapse")
		}
	})
	checkpoint("orb")
	a.updateWait(func() {
		if a.widget != nil {
			a.widget.activate("hide")
		}
	})
	checkpoint("tray")
	a.updateWait(func() { a.restore() })
	verified = wait(2) && verified
	checkpoint("restored")
	widget("cold-card")
	checkpoint("cold-card")
	a.updateWait(func() { a.restore() })
	a.updateWait(func() {
		data, _ := json.MarshalIndent(Object{"verified": verified && a.errorText == "", "error": a.errorText, "checkpoints": checkpoints, "transitions": transitions}, "", "  ")
		_ = os.WriteFile(filepath.Join(a.options.Capture, "memory-result.json"), data, 0600)
		a.quitting = true
		mygo.App.Quit()
	})
}
