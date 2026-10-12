package nativeapp

import (
	"github.com/egoist/mygo/ui"
	"image"
	"math"
)

// CSS perspective(700px) rotateX/rotateY, sampled in premultiplied RGBA.
// The projection origin is the centre of the metrics row, as in the old UI.
func widgetTilt(src *image.RGBA, m *widgetModel, r ui.Rect, amount, scale float32) *image.RGBA {
	index := max(0, min(3, int(math.Round(float64((r.X-30)/127.25)))))
	ax := float64((.5-m.light[index][1])*5*amount) * math.Pi / 180
	ay := float64((m.light[index][0]-.5)*5*amount) * math.Pi / 180
	ax = math.Max(-.05, math.Min(.05, ax))
	ay = math.Max(-.05, math.Min(.05, ay))
	if math.Abs(ax)+math.Abs(ay) < .00001 {
		return src
	}
	cx, cy := float64((20+r.W/2)*scale), float64((20+r.H/2)*scale)
	parentX := float64(r.X+r.W/2-280) * float64(scale)
	perspective := 700 * float64(scale)
	g, h := math.Cos(ax)*math.Sin(ay)/perspective, -math.Sin(ax)/perspective
	a, b, c, d := math.Cos(ay)-parentX*g, -parentX*h, math.Sin(ax)*math.Sin(ay), math.Cos(ax)
	dst := image.NewRGBA(src.Bounds())
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			u, v := float64(x)-cx, float64(y)-cy
			aa, bb, cc, dd := a-u*g, b-u*h, c-v*g, d-v*h
			det := aa*dd - bb*cc
			if math.Abs(det) < 1e-9 {
				continue
			}
			sx, sy := (u*dd-v*bb)/det+cx, (v*aa-u*cc)/det+cy
			widgetSample(dst.Pix[y*dst.Stride+x*4:], src, sx, sy)
		}
	}
	return dst
}
func widgetSample(out []byte, src *image.RGBA, x, y float64) {
	ix, iy := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(ix), y-float64(iy)
	var rgba [4]float64
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			px, py := ix+dx, iy+dy
			if px < 0 || py < 0 || px >= src.Bounds().Dx() || py >= src.Bounds().Dy() {
				continue
			}
			wx, wy := 1-fx, 1-fy
			if dx == 1 {
				wx = fx
			}
			if dy == 1 {
				wy = fy
			}
			offset := py*src.Stride + px*4
			for channel := range rgba {
				rgba[channel] += float64(src.Pix[offset+channel]) * wx * wy
			}
		}
	}
	for channel, value := range rgba {
		out[channel] = uint8(math.Round(value))
	}
}

// Three linear-time box passes approximate CSS's Gaussian blur without a
// browser filter or a copy of the desktop behind the transparent surface.
type widgetBlurWorkspace struct {
	a, b, down                    *image.RGBA
	source                        *image.RGBA
	filteredSource                *image.RGBA
	filteredWidth, filteredPasses int
}

func (w *widgetBlurWorkspace) apply(src *image.RGBA, sigma float64) *image.RGBA {
	// Half-resolution filtering cuts the pixel work by four. Small radii
	// retain full resolution so nearly-sharp text keeps its detail.
	if sigma >= 1 {
		bounds := image.Rect(0, 0, (src.Bounds().Dx()+1)/2, (src.Bounds().Dy()+1)/2)
		if w.down == nil || w.down.Bounds() != bounds {
			w.down = image.NewRGBA(bounds)
			w.source = nil
		}
		if w.source != src {
			for y := 0; y < bounds.Dy(); y++ {
				for x := 0; x < bounds.Dx(); x++ {
					x0, x1 := 2*x, min(2*x+1, src.Bounds().Dx()-1)
					y0, y1 := 2*y, min(2*y+1, src.Bounds().Dy()-1)
					for c := 0; c < 4; c++ {
						w.down.Pix[y*w.down.Stride+x*4+c] = byte((int(src.Pix[y0*src.Stride+x0*4+c]) + int(src.Pix[y0*src.Stride+x1*4+c]) + int(src.Pix[y1*src.Stride+x0*4+c]) + int(src.Pix[y1*src.Stride+x1*4+c]) + 2) / 4)
					}
				}
			}
			w.source = src
			w.filteredSource = nil
		}
		src, sigma = w.down, sigma/2
	}
	return w.full(src, sigma)
}

func (w *widgetBlurWorkspace) full(src *image.RGBA, sigma float64) *image.RGBA {
	if sigma < .35 {
		return src
	}
	width := int(math.Floor(math.Sqrt(4*sigma*sigma + 1)))
	if width%2 == 0 {
		width--
	}
	width = max(1, width)
	upper := width + 2
	lowerPasses := int(math.Round((12*sigma*sigma - 3*float64(width*width) - 12*float64(width) - 9) / (-4*float64(width) - 4)))
	if w.filteredSource == src && w.filteredWidth == width && w.filteredPasses == lowerPasses {
		return w.b
	}
	input := src
	if w.a == nil || w.a.Bounds() != src.Bounds() {
		w.a, w.b = image.NewRGBA(src.Bounds()), image.NewRGBA(src.Bounds())
	}
	for pass := 0; pass < 3; pass++ {
		size := upper
		if pass < lowerPasses {
			size = width
		}
		radius := (size - 1) / 2
		if radius == 0 {
			continue
		}
		widgetBoxBlur(input, w.a, radius, true)
		widgetBoxBlur(w.a, w.b, radius, false)
		input = w.b
	}
	if input != src {
		w.filteredSource, w.filteredWidth, w.filteredPasses = src, width, lowerPasses
	}
	return input
}
func widgetBoxBlur(src, dst *image.RGBA, radius int, horizontal bool) {
	width, height := src.Bounds().Dx(), src.Bounds().Dy()
	lines, length := height, width
	if !horizontal {
		lines, length = width, height
	}
	sourceStep, destinationStep := 4, 4
	sourceLine, destinationLine := src.Stride, dst.Stride
	if !horizontal {
		sourceStep, destinationStep = src.Stride, dst.Stride
		sourceLine, destinationLine = 4, 4
	}
	divisor := 2*radius + 1
	for line := 0; line < lines; line++ {
		sourceBase, destinationBase := line*sourceLine, line*destinationLine
		var sum [4]int
		for at := 0; at <= min(radius, length-1); at++ {
			i := sourceBase + at*sourceStep
			for c := range sum {
				sum[c] += int(src.Pix[i+c])
			}
		}
		for at := 0; at < length; at++ {
			i := destinationBase + at*destinationStep
			for c := range sum {
				dst.Pix[i+c] = uint8((sum[c] + divisor/2) / divisor)
			}
			remove, add := at-radius, at+radius+1
			if remove >= 0 {
				i := sourceBase + remove*sourceStep
				for c := range sum {
					sum[c] -= int(src.Pix[i+c])
				}
			}
			if add < length {
				i := sourceBase + add*sourceStep
				for c := range sum {
					sum[c] += int(src.Pix[i+c])
				}
			}
		}
	}
}
