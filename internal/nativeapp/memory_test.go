package nativeapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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

func TestCompactBrandMatchesOriginalMipPixels(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "assets", "monitor-glass.png"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := ui.DecodeBitmap(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, scale := range []float32{.8, 1, 1.125, 1.25, 1.5, 1.98, 2, 3, 5, 8, 16} {
		compact, err := decodeBrand(data, scale)
		if err != nil {
			t.Fatal(err)
		}
		for _, size := range []int{35, 40} {
			view := func(brand *ui.Bitmap) func(*ui.Context) {
				return func(c *ui.Context) {
					c.Root().Draw(func(p *ui.Painter, r ui.Rect) { p.Image(brand, r, ui.FillBox) })
				}
			}
			before, after := ui.Render(view(original), size, size, scale), ui.Render(view(compact), size, size, scale)
			if !bytes.Equal(before.Pix, after.Pix) {
				t.Fatalf("logo pixels differ at %d DIP, scale %v", size, scale)
			}
		}
		if scale == 1.5 {
			w, h := compact.Size()
			if w != 627 || h != 627 {
				t.Fatalf("kept unnecessary logo pixels: %dx%d", w, h)
			}
		}
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

func TestBackgroundMatchesSVGAtEveryPixel(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][3]float32{{1, 1, 1}, {31, 29, 1.25}, {980, 700, 1}, {1380, 960, 1}, {980, 700, 1.5}, {1380, 960, 2}, {1541, 1041, 1.25}} {
			t.Run(fmt.Sprint(dark, size), func(t *testing.T) {
				original := ui.Render(func(c *ui.Context) {
					c.Root().Draw(func(p *ui.Painter, r ui.Rect) { p.Image(backgrounds[dark], r, ui.FillBox) })
				}, int(size[0]), int(size[1]), size[2])
				var background backgroundRenderer
				actual := ui.Render(func(c *ui.Context) {
					c.Root().Draw(func(p *ui.Painter, r ui.Rect) { background.draw(p, r, dark) })
				}, int(size[0]), int(size[1]), size[2])
				if !bytes.Equal(original.Pix, actual.Pix) {
					count, first := 0, -1
					for i, v := range original.Pix {
						if actual.Pix[i] != v {
							count++
							if first == -1 {
								first = i
							}
						}
					}
					t.Fatalf("%d channels differ; first at (%d,%d) channel %d: %d != %d", count, first/4%original.Bounds().Dx(), first/4/original.Bounds().Dx(), first%4, actual.Pix[first], original.Pix[first])
				}
			})
		}
	}
}

func TestBackgroundCachesOneImmutableSize(t *testing.T) {
	var background backgroundRenderer
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Draw(func(p *ui.Painter, r ui.Rect) { background.draw(p, r, c.Theme().Dark) })
	}, 100, 80)
	first := background.bitmap
	tt.Frame()
	if background.bitmap != first {
		t.Fatal("unchanged background was rebuilt")
	}
	tt.SetSize(120, 90)
	if background.bitmap == first || background.width != 120 || background.height != 90 {
		t.Fatal("resize did not replace background")
	}
	second := background.bitmap
	tt.SetDark(true)
	if background.bitmap == second || !background.dark {
		t.Fatal("theme did not replace background")
	}
}

func BenchmarkGUIBackground(b *testing.B) {
	for _, specialized := range []bool{false, true} {
		b.Run(fmt.Sprint(specialized), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var background backgroundRenderer
				ui.Render(func(c *ui.Context) {
					c.Root().Draw(func(p *ui.Painter, r ui.Rect) {
						if specialized {
							background.draw(p, r, false)
						} else {
							p.Image(backgrounds[false], r, ui.FillBox)
						}
					})
				}, 1380, 960, 1.5)
			}
		})
	}
}
