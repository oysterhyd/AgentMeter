package nativeapp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"local.codex.monitor/internal/monitor"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updaterNative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

type Options struct {
	Data, Home, Capture, Snapshot       string
	Offline, Hidden, Smoke, SmokeWidget bool
	SmokeMemory                         bool
	RepairDatabase                      bool
	Stdio                               bool
	CheckUpdates                        bool
}
type App struct {
	client                                              *Client
	win                                                 *mygo.Window
	tray                                                *mygo.Tray
	resources                                           fs.FS
	options                                             Options
	data                                                Object
	filter                                              Object
	page                                                int
	viewIndex, metricIndex, settingsSection, quotaIndex int
	recordPage, breakdownPage                           int
	historyMode, callPage, detailPage                   int
	callSearch, expandedCall                            string
	telemetryPortDraft                                  string
	callState, taskCallState                            ui.ListState
	selectedCall                                        int
	search, command, toast, errorText                   string
	busy, loading, commandOpen, customOpen, quitting    bool
	startDate, endDate                                  time.Time
	selectedDay, expanded                               string
	activityMetric                                      int
	chartSelection                                      map[string]int
	chartSource                                         chartSourceCache
	chartRenderer                                       *chartRenderer
	translations, systemTranslations                    map[string]string
	accountDrafts                                       map[string]string
	newAccount                                          string
	priceForm                                           map[string]string
	priceID                                             any
	historyState, recordState                           ui.ListState
	selectedRecord                                      int
	breakdownSearch                                     string
	brand                                               *ui.Bitmap
	pal                                                 *palette
	enterIndex, groupRow, commandIndex                  int
	enterScope                                          string
	hoverDay                                            string
	heatmap                                             heatmap
	// adaptMemory lets the GC budget follow the window's pixel area.
	adaptMemory                 bool
	memoryPixels                [2]int
	lastRenderedPage            int
	breakdownSort               ui.SortOrder
	generation                  uint64
	requested                   bool
	progress                    Object
	widget                      *widgetHost
	widgetVerified              bool
	widgetMode                  bool
	startupApplied              bool
	pageEnterX                  float32
	mainScroll                  ui.ScrollState
	snapshotKey                 string
	snapshotPage                int
	snapshotRevision            uint64
	breakdownCache              breakdownCache
	widgetClosing, reduceMotion bool
	mainFade                    widgetMotion
	mainRefreshPending          bool
	windowVerification          Object
	reduceTransparency          bool
}

func NewApp() *App {
	return &App{pal: &lightPalette, data: Object{}, filter: Object{"range": "today"}, recordPage: 1, breakdownPage: 1, callPage: 1, detailPage: 1, selectedCall: -1, selectedRecord: -1, mainFade: widgetMotion{target: 1}, snapshotPage: -1,
		startDate: time.Now(), endDate: time.Now(), translations: map[string]string{}, systemTranslations: map[string]string{}, accountDrafts: map[string]string{},
		priceForm: map[string]string{"model": "", "input": "", "cached": "", "output": "", "cache_write": "0", "effective": "1970-01-01T00:00"}}
}

var pageIDs = []string{"overview", "activity", "history", "quota", "settings"}
var pageNames = []string{"用量总览", "活动日历", "历史分析", "账户额度", "设置与价格"}

func Run(resources fs.FS) error {
	o := Options{}
	flag.StringVar(&o.Data, "data", os.Getenv("MONITOR_TEST_DATA"), "monitor profile directory")
	flag.StringVar(&o.Home, "home", os.Getenv("MONITOR_CODEX_HOME"), "Codex data directory")
	flag.StringVar(&o.Capture, "capture-dir", "", "save native-window captures")
	flag.StringVar(&o.Snapshot, "snapshot", "", "print a snapshot for a JSON filter and exit")
	flag.BoolVar(&o.Offline, "offline", false, "disable source scans and quota queries")
	flag.BoolVar(&o.Hidden, "hidden", false, "start in tray")
	flag.BoolVar(&o.Smoke, "smoke", false, "capture all native pages and exit")
	flag.BoolVar(&o.SmokeWidget, "smoke-widget", false, "verify native transparent widget")
	flag.BoolVar(&o.SmokeMemory, "smoke-memory", false, "measure isolated native GUI memory states without screenshots")
	flag.BoolVar(&o.Stdio, "stdio", false, "run the Go data service over private stdio")
	flag.BoolVar(&o.CheckUpdates, "check-updates", false, "check the signed update feed and exit without opening monitor data")
	flag.BoolVar(&o.RepairDatabase, "repair-database", false, "back up and repair monitor database, then exit")
	flag.Parse()
	o.Smoke = o.Smoke || o.SmokeMemory
	if o.CheckUpdates {
		mygo.App.SetVersion(appVersion)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		update, err := mygo.Updater.Check(ctx)
		if err != nil {
			return err
		}
		result := Object{"version": appVersion, "upToDate": update == nil}
		if update != nil {
			result["availableVersion"] = update.Version
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if o.Data == "" {
		o.Data = filepath.Join(os.Getenv("APPDATA"), "codex-monitor")
	}
	o.Data, _ = filepath.Abs(o.Data)
	if o.RepairDatabase {
		backup, err := monitor.RepairProfile(o.Data)
		if err == nil {
			fmt.Printf("数据库修复成功；原数据库备份：%s\n", backup)
		}
		return err
	}
	if o.Home == "" {
		o.Home = os.Getenv("CODEX_HOME")
		if o.Home == "" {
			home, _ := os.UserHomeDir()
			o.Home = filepath.Join(home, ".codex")
		}
	}
	if o.Snapshot == "" && !o.Stdio {
		mygo.App.SetName("Codex Monitor Native")
		if o.Smoke {
			mygo.App.SetName("Codex Monitor Native Acceptance")
		}
		mygo.App.SetPath(mygo.PathUserData, filepath.Join(o.Data, "native-shell"))
		if !mygo.App.RequestSingleInstanceLock() {
			return nil
		}
	}
	piHome := os.Getenv("MONITOR_PI_HOME")
	if piHome == "" {
		piHome = os.Getenv("PI_CODING_AGENT_DIR")
	}
	if piHome == "" {
		home, _ := os.UserHomeDir()
		piHome = filepath.Join(home, ".pi", "agent")
	}
	piSessions := os.Getenv("MONITOR_PI_SESSIONS")
	if piSessions == "" {
		piSessions = os.Getenv("PI_CODING_AGENT_SESSION_DIR")
	}
	if piSessions == "" {
		piSessions = filepath.Join(piHome, "sessions")
	}
	if o.Offline || (os.Getenv("MONITOR_TEST_DATA") != "" && os.Getenv("MONITOR_PI_HOME") == "") {
		piHome, piSessions = "", ""
	}
	piExtension, err := fs.ReadFile(resources, "integrations/pi/codex-monitor.ts")
	if err != nil {
		return err
	}
	client, err := startNativeClient(monitor.Config{Data: o.Data, Home: o.Home, PiHome: piHome, PiSessions: piSessions, PiExtension: piExtension, Offline: o.Offline, Version: appVersion})
	if err != nil {
		if o.Snapshot == "" && !o.Stdio {
			mygo.App.WhenReady(func() {
				options := startupErrorDialog(err)
				result, dialogErr := mygo.Dialog.Message(options)
				if dialogErr == nil && errors.Is(err, monitor.ErrDatabaseIntegrity) && result.Button == 1 {
					backup, repairErr := monitor.RepairProfile(o.Data)
					if repairErr == nil {
						_, _ = mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageInfo, Title: "Codex Monitor", Message: "数据库已修复，即将重新启动", Detail: "原数据库已保留在：\n" + backup, Buttons: []string{"重新启动"}})
						err = nil
						mygo.App.Relaunch()
						return
					}
					_, _ = mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageError, Title: "Codex Monitor 修复失败", Message: "无法完整恢复监测数据", Detail: repairErr.Error() + "\n原数据库未被清空。", Buttons: []string{"关闭"}})
				}
				mygo.App.Quit()
			})
			_ = mygo.App.Run()
		}
		return err
	}
	defer client.Close()
	if o.Stdio {
		return serveStdio(client)
	}
	if o.Snapshot != "" {
		var f Object
		if err = json.Unmarshal([]byte(o.Snapshot), &f); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		b, err := client.CallJSON(ctx, "snapshot", f)
		if err == nil {
			fmt.Println(string(b))
		}
		return err
	}
	a := NewApp()
	configureGUIMemory()
	a.adaptMemory = true
	mygo.App.SetVersion(appVersion)
	icon, _ := fs.ReadFile(resources, "assets/icon.png")
	// Use the SDK's native updater: manual checks only, no webview runtime.
	mygo.Use(updaterNative.New(updater.Options{DisableAutomaticChecks: true, Icon: icon}))
	// SDK relaunch starts after OnQuit and before Run returns. Release the
	// profile lock here so the new process never races the deferred Close.
	mygo.App.OnQuit(client.Close)
	a.resources = resources
	a.client = client
	a.options = o
	if o.Smoke && os.Getenv("MONITOR_CHART_PROFILE") == "1" {
		a.filter["range"] = "all"
	}
	for path, dest := range map[string]*map[string]string{"assets/locales/translations.json": &a.translations, "assets/locales/system-translations.json": &a.systemTranslations} {
		b, _ := fs.ReadFile(resources, path)
		_ = json.Unmarshal(b, dest)
	}
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		if a.win != nil {
			a.restore()
		}
	})
	mygo.App.WhenReady(func() {
		a.prepareBrand()
		a.win = mygo.NewWindow(mainWindowOptions(mygo.Screen.PrimaryDisplay().WorkArea, o.Hidden || mygo.App.WasOpenedAtLogin(), ui.View(a.View)))
		a.keepWindowVisible()
		icon, _ := fs.ReadFile(resources, "assets/icon.png")
		_ = a.win.SetIcon(icon)
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				a.win.Hide()
			}
		})
		a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: icon, ToolTip: "Codex Monitor · 本机用量监测"})
		if err == nil {
			a.updateTray()
		} else {
			a.errorText = "托盘初始化失败；窗口关闭将退出应用"
			a.win.OnClose(func(e *mygo.CloseEvent) { a.quitting = true; mygo.App.Quit() })
		}
		a.load()
		go a.events()
		if !o.Smoke && !o.Offline {
			var saved struct {
				Mode bool `json:"mode"`
			}
			b, _ := os.ReadFile(filepath.Join(o.Data, "widget-window.json"))
			_ = json.Unmarshal(b, &saved)
			if saved.Mode {
				a.toggleWidget()
			}
		}
		if o.Smoke {
			if o.SmokeMemory {
				go a.smokeMemory()
			} else {
				go a.smoke()
			}
		}
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		a.quitting = true
		if a.widget != nil {
			a.widget.close()
		}
	})
	return mygo.App.Run()
}

func startupErrorDialog(err error) mygo.MessageOptions {
	options := mygo.MessageOptions{Type: mygo.MessageError, Title: "Codex Monitor 无法启动", Message: "无法打开监测数据", Detail: err.Error(), Buttons: []string{"关闭"}}
	if errors.Is(err, monitor.ErrDatabaseIntegrity) {
		options.Buttons = append(options.Buttons, "备份并修复")
		options.Detail += "\n\n可尝试保留全部可读取的数据并重建索引。修复成功后自动重启；原库及日志文件会保留在数据目录的 database-backups 文件夹中。"
	}
	return options
}
func serveStdio(client *Client) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), 32<<20)
	writer := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var r struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Args   any    `json:"args"`
		}
		if json.Unmarshal(scanner.Bytes(), &r) != nil || r.ID < 1 {
			continue
		}
		if r.Method == "shutdown" {
			return writer.Encode(Object{"id": r.ID, "result": true})
		}
		if r.Method == "_quota" {
			_ = writer.Encode(Object{"id": r.ID, "error": "未知操作"})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		value, err := client.CallJSON(ctx, r.Method, r.Args)
		cancel()
		reply := Object{"id": r.ID, "result": value}
		if err != nil {
			reply = Object{"id": r.ID, "error": err.Error()}
		}
		if err = writer.Encode(reply); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func (a *App) requestFilter() Object {
	f := clone(a.filter)
	f["page"] = pageIDs[a.page]
	f["view"] = []string{"models", "projects", "tasks"}[a.viewIndex]
	f["recordPage"] = a.recordPage
	f["pageSize"] = 50
	if a.historyMode == 1 {
		f["historyMode"] = "calls"
	}
	f["callPage"], f["detailPage"] = a.callPage, a.detailPage
	return f
}
func (a *App) load() {
	if a.client == nil {
		return
	}
	if a.mainFade.active(time.Now()) {
		a.mainRefreshPending = true
		return
	}
	if a.loading {
		a.requested = true
		return
	}
	a.loading = true
	a.requested = false
	f := a.requestFilter()
	generation := a.generation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		next, err := a.client.Object(ctx, "snapshot", f)
		a.win.Update(func() {
			a.loading = false
			if generation == a.generation {
				if err != nil {
					a.errorText = err.Error()
				} else {
					a.data = next
					a.snapshotRevision++
					a.chartSource.points = nil
					a.snapshotPage = a.page
					key, _ := json.Marshal(f)
					a.snapshotKey = string(key)
					a.errorText = ""
					a.progress = nil
					if !a.startupApplied && !a.options.Offline && !a.options.Smoke {
						a.startupApplied = true
						if truth(obj(next["settings"])["autoStart"]) {
							if e := mygo.App.SetOpenAtLogin(true); e != nil {
								a.errorText = e.Error()
							}
						}
					}
					mygo.Theme.SetSource(mygo.ThemeSource(str(obj(next["settings"])["theme"])))
					a.updateTray()
				}
			}
			if a.requested || generation != a.generation {
				a.load()
			}
		})
	}()
}
func (a *App) change(key string, value any) {
	a.filter[key] = value
	if key == "activityYear" {
		a.selectedDay = ""
	}
	a.recordPage = 1
	a.breakdownPage = 1
	a.callPage, a.detailPage = 1, 1
	a.expandedCall = ""
	delete(a.filter, "detailSession")
	delete(a.filter, "detailTurn")
	a.expanded = ""
	a.generation++
	a.load()
}
func (a *App) navigate(page int) {
	if a.page == page {
		return
	}
	a.pageEnterX = 16
	if page > a.page {
		a.pageEnterX = -16
	}
	a.page = page
	a.mainScroll.Y = 0
	a.generation++
	a.load()
}
func (a *App) action(method string, args any, success string) {
	a.actionWith(method, args, success, nil)
}
func (a *App) actionWith(method string, args any, success string, done func()) {
	a.performWith(func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return a.client.Call(ctx, method, args)
	}, success, done)
}
func (a *App) perform(fn func() (any, error), success string) { a.performWith(fn, success, nil) }
func (a *App) performWith(fn func() (any, error), success string, done func()) {
	if a.busy || a.client == nil {
		return
	}
	a.busy = true
	go func() {
		value, err := fn()
		a.win.Update(func() {
			a.busy = false
			if err != nil {
				a.errorText = err.Error()
			} else if value != nil {
				if success != "" {
					a.toast = a.tr(success)
				}
				if done != nil {
					done()
				}
			}
			a.generation++
			a.load()
		})
	}()
}
func (a *App) restore() {
	a.widgetClosing = false
	wasWidget := a.widgetMode
	a.widgetMode = false
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.keepWindowVisible()
	if wasWidget && a.widget != nil && !a.reduceMotion {
		a.mainFade.to(1, time.Now(), motionEnter)
		a.win.SetOpacity(float64(a.mainFade.from))
		a.widget.hide(true)
	} else {
		a.mainFade = widgetMotion{target: 1}
		a.win.SetOpacity(1)
		if a.widget != nil {
			a.widget.hide()
		}
	}
	a.win.Show()
	a.win.Focus()
	a.updateTray()
	a.load()
}
func (a *App) events() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case message, ok := <-a.client.events:
			if !ok {
				a.win.Update(func() { a.errorText = a.tr("采集服务暂不可用") })
				return
			}
			switch message.Event {
			case "progress":
				progress := obj(message.Data)
				a.win.Update(func() { a.progress = progress })
			case "updated", "recovered":
				a.win.Update(func() {
					if a.win.IsVisible() && !a.widgetMode {
						a.load()
					}
					if a.widget != nil {
						a.widget.update()
					}
				})
			case "error":
				text := str(message.Data)
				a.win.Update(func() { a.errorText = a.system(text) })
			case "quota":
				go a.notifyQuota(message.Data)
			}
		case <-ticker.C:
			a.win.Update(func() {
				if a.win.IsVisible() && !a.widgetMode {
					a.load()
				}
			})
		case <-a.client.done:
			return
		}
	}
}
func (a *App) notifyQuota(value any) {
	if a.options.Offline {
		return
	}
	q := obj(value)
	if truth(q["muted"]) {
		return
	}
	for _, group := range objects(q["groups"]) {
		for _, slot := range []string{"primary", "secondary"} {
			w := obj(group[slot])
			if w["usedPercent"] == nil || number(w["resetsAt"])*1000 < float64(time.Now().UnixMilli()) {
				continue
			}
			for _, threshold := range []int{20, 10} {
				if 100-number(w["usedPercent"]) > float64(threshold) {
					continue
				}
				key := fmt.Sprintf("%s:%s:%s:%v:%d", str(q["account"]), str(group["limitId"]), slot, w["resetsAt"], threshold)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				r, err := a.client.Call(ctx, "notice", key)
				cancel()
				if err == nil && r == true {
					_ = mygo.NewNotification(mygo.NotificationOptions{Title: "Codex 额度提醒", Body: fmt.Sprintf("%s · %.0f 分钟窗口剩余 %.0f%%", str(group["limitName"]), number(w["windowDurationMins"]), 100-number(w["usedPercent"]))}).Show()
				}
			}
		}
	}
}
func (a *App) updateTray() {
	if a.tray == nil {
		return
	}
	a.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: a.tr("打开 Codex Monitor"), Click: func(*mygo.MenuItem, *mygo.Window) { a.restore() }},
		{Label: "桌面小组件 / Desktop widget", Type: mygo.MenuItemCheckbox, Checked: a.widgetMode, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleWidget() }},
		{Label: a.tr("刷新额度"), Click: func(*mygo.MenuItem, *mygo.Window) { a.action("refresh", nil, "数据已刷新") }},
		{Label: a.tr("静音提醒"), Type: mygo.MenuItemCheckbox, Checked: truth(obj(a.data["settings"])["muted"]), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.action("settings", Object{"muted": !truth(obj(a.data["settings"])["muted"])}, "")
		}},
		mygo.Separator(), {Label: a.tr("退出"), Click: func(*mygo.MenuItem, *mygo.Window) { a.quitting = true; mygo.App.Quit() }},
	}))
}
func (a *App) exportCSV() {
	f := clone(a.filter)
	if a.page == 2 && a.historyMode == 1 {
		f["exportMode"] = "calls"
		delete(f, "recordSearch")
		delete(f, "recordStatus")
	}
	if a.page == 1 {
		year := int(number(obj(a.data["activity"])["year"]))
		f["range"] = "custom"
		f["start"] = fmt.Sprintf("%d-01-01", year)
		f["end"] = fmt.Sprintf("%d-12-31", year)
		today := time.Now().Format("2006-01-02")
		if year == time.Now().Year() {
			f["end"] = today
		}
	}
	if a.page != 2 {
		delete(f, "recordSearch")
		delete(f, "recordStatus")
	}
	a.perform(func() (any, error) {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: a.tr("导出当前筛选的用量"), DefaultPath: "codex-usage-" + time.Now().Format("2006-01-02") + ".csv", Filters: []mygo.FileFilter{{Name: "CSV", Extensions: []string{"csv"}}}})
		if err != nil || path == "" {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		value, err := a.client.Call(ctx, "export", f)
		if err != nil {
			return nil, err
		}
		text := str(value)
		err = os.WriteFile(path, []byte(text), 0600)
		return path, err
	}, "导出完成")
}
func (a *App) updateWait(fn func()) {
	done := make(chan struct{})
	a.win.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}
func (a *App) smoke() {
	_ = os.MkdirAll(a.options.Capture, 0700)
	a.updateWait(func() { a.win.Show(); a.win.Focus() })
	a.verifyWindowControls()
	for page, id := range pageIDs {
		if os.Getenv("MONITOR_CHART_PROFILE") == "1" {
			fmt.Fprintf(os.Stderr, "monitor: chart-profile begin %s\n", id)
		}
		a.updateWait(func() { a.navigate(page); a.win.Focus() })
		deadline := time.Now().Add(12 * time.Second)
		ready := false
		for time.Now().Before(deadline) {
			a.updateWait(func() { ready = len(a.data) > 0 && !a.loading && a.lastRenderedPage == page })
			if ready {
				break
			}
			time.Sleep(40 * time.Millisecond)
		}
		if !ready {
			a.updateWait(func() { a.errorText = "原生页面未完成绘制：" + id })
			break
		}
		if page == 2 {
			time.Sleep(40 * time.Millisecond)
			if png, e := a.win.CapturePage(); e == nil {
				_ = os.WriteFile(filepath.Join(a.options.Capture, "history-enter.png"), png, 0600)
			}
		}
		time.Sleep(motionReveal + 60*time.Millisecond)
		png, err := a.win.CapturePage()
		if err == nil {
			_ = os.WriteFile(filepath.Join(a.options.Capture, id+".png"), png, 0600)
		}
		if os.Getenv("MONITOR_CHART_PROFILE") == "1" {
			fmt.Fprintf(os.Stderr, "monitor: chart-profile end %s\n", id)
		}
	}
	a.smokeCalls()
	if a.options.SmokeWidget {
		a.updateWait(func() { a.toggleWidget() })
		deadline := time.Now().Add(15 * time.Second)
		verified := false
		for time.Now().Before(deadline) {
			a.updateWait(func() { verified = a.widgetVerified })
			if verified {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if verified {
			a.verifyNativeWidget()
			a.verifySwitching()
		}
		a.updateWait(func() {
			if !verified {
				a.errorText = "原生小窗口数据读取验收失败"
			}
			a.restore()
		})
	}
	a.updateWait(func() {
		b, _ := json.Marshal(Object{"reducedMotion": a.reduceMotion, "window": a.windowVerification, "widgetVerified": a.widgetVerified, "native": a.win.Page() == nil, "error": a.errorText, "page": a.page, "rendered": a.lastRenderedPage, "snapshot": a.data})
		_ = os.WriteFile(filepath.Join(a.options.Capture, "result.json"), b, 0600)
		a.quitting = true
		mygo.App.Quit()
	})
}
