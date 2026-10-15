package nativeapp

import (
	"bytes"
	"runtime/debug"
	"testing"
	"time"
)

func TestGUIMemoryDefaultsRespectUserSettings(t *testing.T) {
	oldGC, oldLimit := debug.SetGCPercent(123), debug.SetMemoryLimit(256<<20)
	defer debug.SetGCPercent(oldGC)
	defer debug.SetMemoryLimit(oldLimit)
	t.Setenv("GOGC", "123")
	t.Setenv("GOMEMLIMIT", "256MiB")
	configureGUIMemory()
	if debug.SetGCPercent(123) != 123 || debug.SetMemoryLimit(-1) != 256<<20 {
		t.Fatal("explicit GC settings changed")
	}
	t.Setenv("GOGC", "")
	t.Setenv("GOMEMLIMIT", "")
	configureGUIMemory()
	if debug.SetGCPercent(50) != 50 || debug.SetMemoryLimit(-1) != 80<<20 {
		t.Fatal("GUI defaults were not applied")
	}
}

func TestGUIMemoryBudgetAllowsLargeViewports(t *testing.T) {
	if guiMemoryBudget(1920, 1080) != 80<<20 {
		t.Fatal("normal viewport changed base budget")
	}
	if guiMemoryBudget(3840, 2160) < 150<<20 {
		t.Fatal("4K viewport has insufficient drawing headroom")
	}
}

func TestWidgetReleasesUnusedCardAndRebuildsIdenticalPixels(t *testing.T) {
	m, p := fixtureWidget(t)
	p.model = &m
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	m.ReduceMotion = true
	before := bytes.Clone(p.render(now, 1.5).Pix)
	m.toggle(now)
	p.render(now, 1.5)
	if p.card != nil || p.cardImage != nil || p.filteredCard != nil || p.metricLayers[0] != nil || p.blur.a != nil {
		t.Fatal("collapsed orb retains unused card buffers")
	}
	m.toggle(now)
	if !bytes.Equal(before, p.render(now, 1.5).Pix) {
		t.Fatal("expanded card changed after rebuilding")
	}
	p.release()
	if p.frame != nil || p.orb != nil || p.card != nil || p.frameImage != nil {
		t.Fatal("hidden widget retains renderers")
	}
	if !bytes.Equal(before, p.render(now, 1.5).Pix) {
		t.Fatal("card changed after complete release")
	}
}

func TestWidgetDropsBlurWorkspaceAfterExpandedMorph(t *testing.T) {
	m, p := fixtureWidget(t)
	p.model = &m
	m.ReduceMotion = false
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	p.render(now, 1.5)
	m.toggle(now)
	p.render(now.Add(time.Second), 1.5)
	m.toggle(now.Add(time.Second))
	p.render(now.Add(1200*time.Millisecond), 1.5)
	if p.blur.a == nil {
		t.Fatal("test did not exercise morph blur")
	}
	p.render(now.Add(2*time.Second), 1.5)
	if p.blur.a != nil || p.blur.down != nil || p.blur.source != nil {
		t.Fatal("finished morph retains blur workspace")
	}
}
