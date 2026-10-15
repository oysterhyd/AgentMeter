package nativeapp

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/vector"
)

type chartPixel [2]float32

// Rasterize independently of MyGO's shared text/shape atlas. The worker owns
// every buffer, and the GUI receives only a completed immutable bitmap.
func (r *chartRenderer) raster(key chartImageKey) *image.RGBA {
	scale := key.scale
	w := int(math.Round(math.Ceil(float64(key.width+12)) * float64(scale)))
	h := int(math.Round(math.Ceil(float64(key.height+12)) * float64(scale)))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	z := vector.NewRasterizer(w, h)
	x := func(t float64) float32 {
		if key.last <= key.first {
			return 6 + key.width/2
		}
		return 6 + float32((t-key.first)/(key.last-key.first))*key.width
	}
	y := func(v float64) float32 { return 12 + float32(1-v/r.maximum)*(key.height-12) }
	polys := make([][]chartPixel, len(r.segments))
	for i, segment := range r.segments {
		display := segment
		if r.data.quota {
			display = chartDisplayPoints(segment, r.maximum*(chartStrokeWidth/2)/float64(max(1, key.height-12)))
		}
		polys[i] = chartRasterCurve(display, x, y, scale)
	}
	paint := func(ink ui.Color) {
		z.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{R: ink.R, G: ink.G, B: ink.B, A: ink.A}), image.Point{})
		z.Reset(w, h)
	}
	first, last := r.points[0].Time, r.points[len(r.points)-1].Time
	if key.last > key.first && (last-first)/(key.last-key.first) >= .1 {
		for i, segment := range r.segments {
			poly := polys[i]
			if len(segment) < 2 || segment[len(segment)-1].Time <= segment[0].Time {
				continue
			}
			z.MoveTo(poly[0][0], poly[0][1])
			for _, point := range poly[1:] {
				z.LineTo(point[0], point[1])
			}
			baseline := chartQuarter(y(0) * scale)
			z.LineTo(poly[len(poly)-1][0], baseline)
			z.LineTo(poly[0][0], baseline)
			z.ClosePath()
		}
		top := float32(h)
		for i, poly := range polys {
			if len(r.segments[i]) > 1 {
				for _, point := range poly {
					top = min(top, point[1])
				}
			}
		}
		g := chartFill(key.accent)
		z.Draw(img, img.Bounds(), chartGradient{top, chartQuarter(y(0) * scale), g.From, g.To}, image.Point{})
		z.Reset(w, h)
	}
	// Match the existing reset hint, whose Painter.Line uses axis-aligned
	// rectangles; a quota reset remains a dotted discontinuity.
	resetInk := key.accent.Alpha(.45)
	resetColor := image.NewUniform(color.NRGBA{R: resetInk.R, G: resetInk.G, B: resetInk.B, A: resetInk.A})
	for i, segment := range r.segments {
		if i == 0 || !r.data.quota || segment[0].Gap {
			continue
		}
		previous := r.segments[i-1]
		point := previous[len(previous)-1]
		a, b := chartPixel{x(point.Time), y(point.Value)}, chartPixel{x(segment[0].Time), y(segment[0].Value)}
		dx, dy := b[0]-a[0], b[1]-a[1]
		length := float32(math.Hypot(float64(dx), float64(dy)))
		for start := float32(0); start < length; start += 8 {
			end := min(length, start+3)
			left, top := a[0]+dx*start/length, a[1]+dy*start/length
			right, bottom := a[0]+dx*end/length, a[1]+dy*end/length
			left, right = min(left, right), max(left, right)
			top, bottom = min(top, bottom), max(top, bottom)
			if right-left < bottom-top {
				left -= .6
				right = left + 1.2
			} else {
				top -= .6
				bottom = top + 1.2
			}
			rect := image.Rect(int(math.Round(float64(left*scale))), int(math.Round(float64(top*scale))), int(math.Round(float64(right*scale))), int(math.Round(float64(bottom*scale))))
			draw.Draw(img, rect, resetColor, image.Point{}, draw.Over)
		}
	}
	for _, poly := range polys {
		if len(poly) < 2 {
			continue
		}
		for i, point := range poly {
			if i > 0 {
				a := poly[i-1]
				dx, dy := point[0]-a[0], point[1]-a[1]
				length := float32(math.Hypot(float64(dx), float64(dy)))
				if length > 0 {
					nx, ny := -dy/length*chartStrokeWidth*scale/2, dx/length*chartStrokeWidth*scale/2
					z.MoveTo(a[0]-nx, a[1]-ny)
					z.LineTo(point[0]-nx, point[1]-ny)
					z.LineTo(point[0]+nx, point[1]+ny)
					z.LineTo(a[0]+nx, a[1]+ny)
					z.ClosePath()
				}
			}
			chartRasterDisc(z, point, chartStrokeWidth*scale/2)
		}
	}
	paint(key.accent)
	markers := false
	for _, segment := range r.segments {
		radius := float32(0)
		if r.data.count < 15 {
			radius = 2.5
			if r.data.count < 4 {
				radius = 4.5
			}
		}
		if len(segment) == 1 {
			radius = max(radius, 3)
		}
		if radius > 0 {
			markers = true
			for _, point := range segment {
				chartRasterDisc(z, chartPixel{x(point.Time) * scale, y(point.Value) * scale}, radius*scale)
			}
		}
	}
	if markers {
		paint(key.accent)
	}
	return img
}

// chartFill fades the area under a curve from its peak down to nothing.
func chartFill(accent ui.Color) ui.LinearGradient {
	return ui.LinearGradient{From: accent.Alpha(.24), To: accent.Alpha(0), Angle: 180}
}

// chartGradient is the vertical gradient source for the rasterized area.
type chartGradient struct {
	top, bottom float32
	from, to    ui.Color
}

func (g chartGradient) ColorModel() color.Model { return color.NRGBAModel }
func (g chartGradient) Bounds() image.Rectangle { return image.Rect(-1e9, -1e9, 1e9, 1e9) }
func (g chartGradient) At(_, y int) color.Color {
	t := float32(0)
	if g.bottom > g.top {
		t = max(0, min(1, (float32(y)+.5-g.top)/(g.bottom-g.top)))
	}
	mix := func(a, b uint8) uint8 { return uint8(float32(a) + (float32(b)-float32(a))*t + .5) }
	return color.NRGBA{R: g.from.R, G: g.from.G, B: g.from.B, A: mix(g.from.A, g.to.A)}
}

func chartQuarter(v float32) float32 { return float32(math.Round(float64(v*4))) / 4 }

func chartRasterDisc(z *vector.Rasterizer, center chartPixel, radius float32) {
	steps := max(8, min(32, int(radius*4)))
	z.MoveTo(center[0]+radius, center[1])
	for i := 1; i < steps; i++ {
		angle := 2 * math.Pi * float64(i) / float64(steps)
		z.LineTo(center[0]+radius*float32(math.Cos(angle)), center[1]+radius*float32(math.Sin(angle)))
	}
	z.ClosePath()
}

// Use the same monotone controls, device-pixel subdivision and quarter-pixel
// snapping as the visible vector path. Fill and stroke share these samples.
func chartRasterCurve(points []chartPoint, x, y func(float64) float32, scale float32) []chartPixel {
	if len(points) == 0 {
		return nil
	}
	result := make([]chartPixel, 0, min(256, len(points)*4))
	result = append(result, chartPixel{x(points[0].Time) * scale, y(points[0].Value) * scale})
	tangent := chartTangent(points, 0)
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		next := chartTangent(points, i)
		span := b.Time - a.Time
		end := chartPixel{x(b.Time) * scale, y(b.Value) * scale}
		if span <= 0 || tangent == next && b.Value-a.Value == tangent*span {
			result = append(result, end)
		} else {
			dx := (x(b.Time) - x(a.Time)) / 3
			start := result[len(result)-1]
			left := chartPixel{(x(a.Time) + dx) * scale, y(a.Value+span*tangent/3) * scale}
			right := chartPixel{(x(b.Time) - dx) * scale, y(b.Value-span*next/3) * scale}
			length := float64(0)
			previous := start
			for _, point := range [3]chartPixel{left, right, end} {
				length += float64(float32(math.Hypot(float64(point[0]-previous[0]), float64(point[1]-previous[1]))))
				previous = point
			}
			steps := max(2, min(64, int(math.Sqrt(length)*2)))
			for step := 1; step <= steps; step++ {
				t := float32(step) / float32(steps)
				u := 1 - t
				c0, c1, c2, c3 := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				result = append(result, chartPixel{c0*start[0] + c1*left[0] + c2*right[0] + c3*end[0], c0*start[1] + c1*left[1] + c2*right[1] + c3*end[1]})
			}
		}
		tangent = next
	}
	for i := range result {
		result[i][0], result[i][1] = chartQuarter(result[i][0]), chartQuarter(result[i][1])
	}
	return result
}
