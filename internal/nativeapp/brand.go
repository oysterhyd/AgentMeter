package nativeapp

import (
	"bytes"
	"image"
	"image/draw"
	"io/fs"
	"math"

	"github.com/egoist/mygo/ui"
)

// Discard oversized mip levels only when exact halving keeps the bitmap's
// sampling thresholds unchanged at every fractional DPI. MyGO rounds odd
// levels up, so going beyond those would change which level is selected.
func decodeBrand(data []byte, scale float32) (*ui.Bitmap, error) {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	rgba, ok := source.(*image.RGBA)
	if !ok || rgba.Stride != 4*bounds.Dx() || bounds.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(rgba, rgba.Bounds(), source, bounds.Min, draw.Src)
	}
	levels := int(math.Log2(float64(float32(min(bounds.Dx(), bounds.Dy())) / (40 * max(scale, .01)))))
	for {
		if levels <= 0 || rgba.Bounds().Dx()%2 != 0 || rgba.Bounds().Dy()%2 != 0 {
			break
		}
		w, h := (rgba.Bounds().Dx()+1)/2, (rgba.Bounds().Dy()+1)/2
		next := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			y0, y1 := 2*y, min(2*y+1, rgba.Bounds().Dy()-1)
			for x := 0; x < w; x++ {
				x0, x1 := 2*x, min(2*x+1, rgba.Bounds().Dx()-1)
				out := next.Pix[y*next.Stride+4*x:]
				for c := 0; c < 4; c++ {
					out[c] = byte((uint32(rgba.Pix[y0*rgba.Stride+4*x0+c]) + uint32(rgba.Pix[y0*rgba.Stride+4*x1+c]) + uint32(rgba.Pix[y1*rgba.Stride+4*x0+c]) + uint32(rgba.Pix[y1*rgba.Stride+4*x1+c]) + 2) / 4)
				}
			}
		}
		rgba = next
		levels--
	}
	return ui.NewBitmap(rgba), nil
}

func (a *App) prepareBrand(scale float32) bool {
	if a.resources == nil || a.brand != nil && scale <= a.brandScale {
		return false
	}
	data, err := fs.ReadFile(a.resources, "assets/monitor-glass.png")
	if err != nil {
		return false
	}
	brand, err := decodeBrand(data, scale)
	if err != nil {
		return false
	}
	a.brand, a.brandScale = brand, scale
	if a.widget != nil {
		a.widget.painter.brand = brand
		a.widget.painter.invalidate()
	}
	return true
}
