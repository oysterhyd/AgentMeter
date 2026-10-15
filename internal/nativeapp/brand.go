package nativeapp

import (
	"io/fs"

	"github.com/egoist/mygo/ui"
)

// prepareBrand decodes the app mark once. The embedded image is 160 px —
// four times its largest 40 DIP use — so it stays sharp up to 400% scale
// while costing only 100 KB of pixels.
func (a *App) prepareBrand() {
	if a.resources == nil || a.brand != nil {
		return
	}
	data, err := fs.ReadFile(a.resources, "assets/monitor-glass.png")
	if err != nil {
		return
	}
	if a.brand, err = ui.DecodeBitmap(data); err != nil {
		a.brand = nil
		return
	}
	if a.widget != nil {
		a.widget.painter.brand = a.brand
		a.widget.painter.invalidate()
	}
}
