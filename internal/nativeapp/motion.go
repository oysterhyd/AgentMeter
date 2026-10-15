package nativeapp

import "time"

// Keep the original src/theme.css timings and cubic-bezier(.22,1,.36,1).
const (
	motionQuick  = 180 * time.Millisecond
	motionEnter  = 320 * time.Millisecond
	motionFill   = 640 * time.Millisecond
	motionReveal = 780 * time.Millisecond
)

func motionEase(t float32) float32 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	lo, hi := float32(0), float32(1)
	for i := 0; i < 14; i++ {
		m := (lo + hi) / 2
		u := 1 - m
		x := 3*u*u*m*.22 + 3*u*m*m*.36 + m*m*m
		if x < t {
			lo = m
		} else {
			hi = m
		}
	}
	m := (lo + hi) / 2
	return 1 - (1-m)*(1-m)*(1-m)
}
