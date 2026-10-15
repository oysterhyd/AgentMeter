package nativeapp

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Use the same Phosphor SVG paths as the Electron interface.
//
//go:embed icons/*.svg
var iconFiles embed.FS
var icons = func() map[string]*ui.SVG {
	result := map[string]*ui.SVG{}
	entries, _ := fs.ReadDir(iconFiles, "icons")
	for _, entry := range entries {
		b, _ := iconFiles.ReadFile("icons/" + entry.Name())
		result[strings.TrimSuffix(entry.Name(), ".svg")] = ui.MustParseSVG(b)
	}
	return result
}()
