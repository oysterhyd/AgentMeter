package nativeapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

// Capture the redesigned settings groups and both native appearances using
// only the isolated acceptance profile. Desktop preferences are never changed.
func (a *App) smokeAppearance() {
	checks := Object{}
	capture := func(name string) {
		time.Sleep(springSmooth.Spring.Duration + 400*time.Millisecond)
		png, err := a.win.CapturePage()
		if err == nil {
			err = os.WriteFile(filepath.Join(a.options.Capture, name+".png"), png, 0600)
		}
		checks[name] = err == nil
	}
	for _, source := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark} {
		a.updateWait(func() { mygo.Theme.SetSource(source) })
		for section, name := range []string{"general", "accounts", "prices", "data"} {
			a.updateWait(func() { a.settingsSection = section; a.mainScroll.Y = 0 })
			capture("settings-" + name + "-" + string(source))
			a.updateWait(func() { checks["theme-"+string(source)] = a.pal.dark == (source == mygo.ThemeDark) })
		}
	}
	// README's dark history preview is captured from the native window too.
	a.updateWait(func() { a.navigate(2) })
	deadline := time.Now().Add(12 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		a.updateWait(func() { ready = !a.loading && a.snapshotPage == 2 && a.lastRenderedPage == 2 })
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	checks["history-ready"] = ready
	if ready {
		a.updateWait(func() { mygo.Theme.SetSource(mygo.ThemeDark) })
		capture("history-dark")
	}
	a.updateWait(func() {
		a.navigate(4)
		a.settingsSection = 0
		mygo.Theme.SetSource(mygo.ThemeSystem)
		checks["preferences"] = mygo.Theme.Preferences()
		for key, value := range checks {
			if ok, isBool := value.(bool); isBool && !ok {
				a.errorText = "原生外观验收失败：" + key
			}
		}
	})
	data, _ := json.MarshalIndent(checks, "", "  ")
	_ = os.WriteFile(filepath.Join(a.options.Capture, "appearance-result.json"), data, 0600)
}
