package nativeapp

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"math/rand/v2"
	"testing"

	"local.codex.monitor/internal/monitor"
)

func TestDirectClientPreservesJSONContractAndDetachedSnapshots(t *testing.T) {
	a := fixtureApp(t)
	c, err := startNativeClient(monitor.Config{Data: a.options.Data, Home: a.options.Data, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, page := range []string{"overview", "activity", "history", "calls", "quota", "settings", "widget"} {
		f := Object{"page": page, "range": "all", "pageSize": 50}
		method := "snapshot"
		if page == "calls" {
			f["page"], f["historyMode"] = "history", "calls"
		}
		if page == "widget" {
			method, f["now"] = "widget", 1791360000000.
		}
		got, err := c.Object(context.Background(), method, f)
		if err != nil {
			t.Fatal(err)
		}
		want, err := c.CallJSON(context.Background(), method, f)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(got)
		if err != nil || !bytes.Equal(encoded, want) {
			t.Fatalf("%s changed the JSON contract: %v", page, err)
		}
		if page == "settings" {
			objects(got["prices"])[0]["input"] = -999
			next, err := c.Object(context.Background(), method, f)
			if err != nil || number(objects(next["prices"])[0]["input"]) < 0 {
				t.Fatal("UI mutation reached the store", err)
			}
		}
	}
}

// A slow scalar convolution is an independent pixel oracle for the sliding sum.
func TestBoxBlurPreservesEveryPixelAndDifferentStrides(t *testing.T) {
	rng := rand.New(rand.NewPCG(31, 97))
	for _, size := range [][2]int{{1, 1}, {7, 5}, {16, 13}} {
		parent := image.NewRGBA(image.Rect(0, 0, size[0]+3, size[1]+2))
		for i := range parent.Pix {
			parent.Pix[i] = byte(rng.Uint32())
		}
		src := parent.SubImage(image.Rect(1, 1, 1+size[0], 1+size[1])).(*image.RGBA)
		for _, horizontal := range []bool{false, true} {
			for _, radius := range []int{0, 1, 3, 20} {
				dst := image.NewRGBA(image.Rect(0, 0, size[0], size[1]))
				widgetBoxBlur(src, dst, radius, horizontal)
				divisor := 2*radius + 1
				for y := 0; y < size[1]; y++ {
					for x := 0; x < size[0]; x++ {
						for channel := 0; channel < 4; channel++ {
							sum := 0
							for delta := -radius; delta <= radius; delta++ {
								px, py := x, y
								if horizontal {
									px += delta
								} else {
									py += delta
								}
								if px >= 0 && py >= 0 && px < size[0] && py < size[1] {
									sum += int(src.Pix[py*src.Stride+px*4+channel])
								}
							}
							want := byte((sum + divisor/2) / divisor)
							if got := dst.Pix[y*dst.Stride+x*4+channel]; got != want {
								t.Fatalf("size=%v radius=%d horizontal=%t x=%d y=%d channel=%d: %d != %d", size, radius, horizontal, x, y, channel, got, want)
							}
						}
					}
				}
			}
		}
	}
}
