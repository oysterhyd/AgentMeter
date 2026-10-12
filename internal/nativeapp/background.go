package nativeapp

import (
	"image"
	"math"

	"github.com/egoist/mygo/ui"
)

// The background consists of an opaque base and two radial gradients. Draw
// these directly into one immutable bitmap, instead of using the general SVG
// rasterizer's full-window coverage, layer and geometry workspaces.
type backgroundRenderer struct {
	bitmap        *ui.Bitmap
	width, height int
	dark          bool
	adaptMemory   bool
}

func (b *backgroundRenderer) draw(p *ui.Painter, r ui.Rect, dark bool) {
	scale := p.Scale()
	w := int(math.Round(float64((r.X+r.W)*scale)) - math.Round(float64(r.X*scale)))
	h := int(math.Round(float64((r.Y+r.H)*scale)) - math.Round(float64(r.Y*scale)))
	if w <= 0 || h <= 0 {
		return
	}
	if b.bitmap == nil || b.width != w || b.height != h || b.dark != dark {
		if b.adaptMemory {
			adjustGUIMemory(w, h)
		}
		b.bitmap = ui.NewBitmap(backgroundPixels(w, h, dark))
		b.width, b.height, b.dark = w, h, dark
	}
	p.Image(b.bitmap, r, ui.FillBox)
}

func backgroundPixels(w, h int, dark bool) *image.RGBA {
	base, green := [3]byte{234, 243, 248}, [3]byte{213, 245, 235}
	if dark {
		base, green = [3]byte{16, 29, 41}, [3]byte{35, 77, 73}
	}
	lut := func(color [3]byte, opacity, end float64) (values [256][4]uint32) {
		for i := range values {
			t := float64(i) / 255
			alpha := 0.
			if t < end {
				alpha = opacity + (0-opacity)*(t/end)
			}
			for c := 0; c < 3; c++ {
				values[i][c] = uint32(float64(color[c])/255*alpha*255 + .5)
			}
			values[i][3] = uint32(alpha*255 + .5)
		}
		return
	}
	blueLUT, greenLUT := lut([3]byte{128, 186, 255}, .1, .56), lut(green, .82, .48)
	// Match the SVG's viewBox and objectBoundingBox transform, lookup-table
	// quantization and integer source-over rounding, including at high DPI.
	sx, sy := float64(w)/1380*1380, float64(h)/960*960
	det := sx * sy
	ix, iy := sy/det, sx/det
	position := func(x, y, cx, cy, radius float64) int {
		x, y = x-cx, y-cy
		a := -radius * radius
		s := math.Sqrt(-a * (x*x + y*y))
		t := math.Max(s/a, -s/a)
		return int(min(1., t)*255 + .5)
	}
	divide := func(v uint32) uint32 { return (v + 128 + (v+128)>>8) >> 8 }
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		py := iy * (float64(y) + .5)
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			px := ix * (float64(x) + .5)
			blue := blueLUT[position(px, py, .04, .65, 1)]
			green := greenLUT[position(px, py, .86, 0, .9)]
			out := row[4*x : 4*x+4]
			for c := 0; c < 3; c++ {
				v := blue[c] + divide(uint32(base[c])*(255-blue[3]))
				out[c] = byte(green[c] + divide(v*(255-green[3])))
			}
			out[3] = 255
		}
	}
	return img
}
