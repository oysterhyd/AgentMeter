package nativeapp

import (
	"os"
	"runtime/debug"
	"sync/atomic"
)

// GUI images outlive individual requests. Collect before those short-lived
// frames double a large live heap; the soft budget also asks Go to return idle
// pages sooner. It excludes Direct3D/Win32 memory and is not a process cap.
// Explicit user settings always win. CLI/stdio never call this function.
func configureGUIMemory() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(80 << 20)
	}
}

func guiMemoryBudget(width, height int) int64 {
	// Leave room for simultaneous old/new bitmaps and software fallback on
	// very large/high-DPI windows. A small fixed budget there would thrash GC.
	return max(80<<20, (32<<20)+int64(width)*int64(height)*16)
}

func adjustGUIMemory(width, height int) {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(guiMemoryBudget(width, height))
	}
}

var reclaimingGUIMemory atomic.Bool

// An unused widget can be the last allocation for minutes. Release its dead
// image pages after the hide grace period instead of waiting for more work to
// trigger GC. This runs off the GUI thread and coalesces concurrent requests.
func reclaimGUIMemory() {
	if !reclaimingGUIMemory.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer reclaimingGUIMemory.Store(false)
		debug.FreeOSMemory()
	}()
}
