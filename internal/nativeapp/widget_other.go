//go:build !windows

package nativeapp

import (
	"context"
	"fmt"
	"image"
)

type widgetWindow struct{}

func newWidgetWindow(*widgetHost, context.Context) (*widgetWindow, error) {
	return nil, fmt.Errorf("原生桌面小窗口需要 Windows")
}
func (*widgetWindow) context() context.Context { return context.Background() }
func (*widgetWindow) scale() float32           { return 1 }
func (*widgetWindow) wake()                    {}
func (*widgetWindow) show()                    {}
func (*widgetWindow) hide()                    {}
func (*widgetWindow) close()                   {}
func (*widgetWindow) releasePixels()           {}
func (*widgetWindow) pixelsReleased() bool     { return true }
func (*widgetWindow) topmost(bool)             {}
func (*widgetWindow) anchor()                  {}
func (*widgetWindow) present(*image.RGBA)      {}
func (*widgetWindow) fade(float32)             {}
func (*widgetWindow) key(uintptr)              {}
func (*widgetWindow) renderFailure() error     { return nil }
func (*widgetWindow) verify() Object           { return Object{} }
func (*widgetWindow) verifyDrag(Object)        {}
