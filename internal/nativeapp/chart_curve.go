package nativeapp

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Steffen's monotone cubic tangents keep peaks and flat runs at their observed
// values. Unlike an unconstrained spline, they cannot invent an overshoot.
// https://d3js.org/d3-shape/curve#curveMonotoneX
func chartInnerTangent(a, b, c chartPoint) float64 {
	h0, h1 := b.Time-a.Time, c.Time-b.Time
	if h0 <= 0 || h1 <= 0 {
		return 0
	}
	s0, s1 := (b.Value-a.Value)/h0, (c.Value-b.Value)/h1
	if s0 == 0 || s1 == 0 || math.Signbit(s0) != math.Signbit(s1) {
		return 0
	}
	weighted := (s0*h1 + s1*h0) / (h0 + h1)
	return math.Copysign(2*min(math.Abs(s0), math.Abs(s1), math.Abs(weighted)/2), s0)
}

func chartTangent(points []chartPoint, i int) float64 {
	if len(points) < 2 {
		return 0
	}
	if i > 0 && i < len(points)-1 {
		return chartInnerTangent(points[i-1], points[i], points[i+1])
	}
	left := 0
	if i > 0 {
		left = len(points) - 2
	}
	h := points[left+1].Time - points[left].Time
	if h <= 0 {
		return 0
	}
	slope := (points[left+1].Value - points[left].Value) / h
	if len(points) == 2 {
		return slope
	}
	inner := 1
	if i > 0 {
		inner = len(points) - 2
	}
	return (3*slope - chartInnerTangent(points[inner-1], points[inner], points[inner+1])) / 2
}

func chartCurve(points []chartPoint, x, y func(float64) float32) ui.Path {
	var path ui.Path
	if len(points) == 0 {
		return path
	}
	path.MoveTo(x(points[0].Time), y(points[0].Value))
	tangent := chartTangent(points, 0)
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		next := chartTangent(points, i)
		h := b.Time - a.Time
		if h <= 0 || (tangent == next && b.Value-a.Value == tangent*h) {
			path.LineTo(x(b.Time), y(b.Value))
		} else {
			dx := (x(b.Time) - x(a.Time)) / 3
			path.CubeTo(x(a.Time)+dx, y(a.Value+h*tangent/3), x(b.Time)-dx, y(b.Value-h*next/3), x(b.Time), y(b.Value))
		}
		tangent = next
	}
	return path
}

// A streaming slope corridor removes dense sampling stairs in O(n) time.
// Every retained endpoint is an original observation, every omitted point is
// within tolerance of its line chord, and reversals/duplicate times are kept.
// This is only display geometry: hover and keyboard still use all observations.
func chartDisplayPoints(points []chartPoint, tolerance float64) []chartPoint {
	if len(points) < 3 || tolerance <= 0 {
		return points
	}
	result := make([]chartPoint, 0, min(len(points), 32))
	result = append(result, points[0])
	anchor := 0
	lower, upper := math.Inf(-1), math.Inf(1)
	direction := float64(0)
	for i := 1; i < len(points); i++ {
		delta := points[i].Value - points[i-1].Value
		turn := delta != 0 && direction != 0 && math.Signbit(delta) != math.Signbit(direction)
		dx := points[i].Time - points[anchor].Time
		slope := float64(0)
		if dx > 0 {
			slope = (points[i].Value - points[anchor].Value) / dx
		}
		if turn || points[i].Time <= points[i-1].Time || slope < lower || slope > upper {
			if anchor != i-1 {
				result = append(result, points[i-1])
				anchor = i - 1
			}
			lower, upper = math.Inf(-1), math.Inf(1)
			dx = points[i].Time - points[anchor].Time
		}
		if dx <= 0 {
			result = append(result, points[i])
			anchor = i
			lower, upper = math.Inf(-1), math.Inf(1)
			direction = 0
			continue
		}
		lower = max(lower, (points[i].Value-points[anchor].Value-tolerance)/dx)
		upper = min(upper, (points[i].Value-points[anchor].Value+tolerance)/dx)
		if delta != 0 {
			direction = delta
		}
	}
	if anchor != len(points)-1 {
		result = append(result, points[len(points)-1])
	}
	return result
}
