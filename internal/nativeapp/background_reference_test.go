package nativeapp

import "github.com/egoist/mygo/ui"

// Original SVGs serve as the pixel oracle for the specialized renderer.
var backgrounds = map[bool]*ui.SVG{
	false: ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1380 960"><defs><radialGradient id="green" cx="86%" cy="0%" r="90%"><stop offset="0" stop-color="#d5f5eb" stop-opacity="0.82"/><stop offset="0.48" stop-color="#d5f5eb" stop-opacity="0"/></radialGradient><radialGradient id="blue" cx="4%" cy="65%" r="100%"><stop offset="0" stop-color="#80baff" stop-opacity="0.10"/><stop offset="0.56" stop-color="#80baff" stop-opacity="0"/></radialGradient></defs><rect width="1380" height="960" fill="#eaf3f8"/><rect width="1380" height="960" fill="url(#blue)"/><rect width="1380" height="960" fill="url(#green)"/></svg>`)),
	true:  ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1380 960"><defs><radialGradient id="green" cx="86%" cy="0%" r="90%"><stop offset="0" stop-color="#234d49" stop-opacity="0.82"/><stop offset="0.48" stop-color="#234d49" stop-opacity="0"/></radialGradient><radialGradient id="blue" cx="4%" cy="65%" r="100%"><stop offset="0" stop-color="#80baff" stop-opacity="0.10"/><stop offset="0.56" stop-color="#80baff" stop-opacity="0"/></radialGradient></defs><rect width="1380" height="960" fill="#101d29"/><rect width="1380" height="960" fill="url(#blue)"/><rect width="1380" height="960" fill="url(#green)"/></svg>`)),
}
