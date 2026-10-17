package nativeapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

type navItem struct {
	page  int
	label string
	icon  string
	color func(*palette) ui.Color
}

var navItems = []navItem{
	{0, "总览", "chart-pie-slice-fill", func(p *palette) ui.Color { return p.blue }},
	{1, "活动", "calendar-dots-fill", func(p *palette) ui.Color { return p.red }},
	{2, "历史分析", "clock-counter-clockwise-fill", func(p *palette) ui.Color { return p.indigo }},
	{3, "账户额度", "gauge-fill", func(p *palette) ui.Color { return p.green }},
	{4, "设置与价格", "gear-six-fill", func(p *palette) ui.Color { return p.gray }},
}

const (
	sidebarWidth    = 236
	contentMaxWidth = 1320
	contentInset    = 28
)

func (a *App) View(c *ui.Context) {
	a.skin(c)
	a.reduceMotion = c.Preferences().ReduceMotion
	if a.lastRenderedPage != a.page {
		a.chartSource = chartSourceCache{}
		a.releaseChart()
	}
	a.lastRenderedPage = a.page
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.action("refresh", nil, "数据已刷新")
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) && !a.loading {
		a.exportCSV()
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		a.commandOpen = !a.commandOpen
	}
	for i, key := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5} {
		if c.Shortcut(ui.Alt, key) {
			a.navigate(i)
		}
	}
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
	ui.Row(c.Key("shell")).Absolute().Left(0).Right(0).Top(0).Bottom(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		ui.Column(c.Key("main")).Grow(1).Basis(0).MinWidth(0).Background(a.pal.bg).Children(func() {
			// Content scrolls under the toolbar, which the scroll edge
			// effect keeps legible, as in the apps of macOS 26.
			ui.Scroll(c.Key("main-scroll")).TrackScroll(&a.mainScroll).Grow(1).ClipX().Children(func() {
				ui.Column(c.Key("content")).FillWidth().MaxWidth(contentMaxWidth).Margin(0, ui.Auto).Padding(a.toolbarHeight(c)+6, contentInset, contentInset, contentInset).Gap(16).Children(func() {
					a.content(c)
				})
			})
			edge := ui.Box(c.Key("scroll-edge")).Absolute().Top(0).Left(0).Right(0).Height(a.toolbarHeight(c)).PassThrough()
			edge.Opacity(edge.Animate("shown", map[bool]float32{true: 1, false: 0}[a.mainScroll.Y > 1], motionQuick)).Material(glass.ScrollEdge{Hard: true, Background: a.pal.bg})
			a.toolbar(c)
		})
	})
	a.commandPalette(c)
}

func (a *App) content(c *ui.Context) {
	p := a.pal
	a.enterIndex = 0
	a.enterScope = fmt.Sprintf("%d.%d.%d", a.page, a.settingsSection, a.historyMode)
	if a.errorText != "" {
		ui.Row(c.Key("error")).Padding(12, 16).Gap(12).Radius(12).AlignItems(ui.Center).Background(p.red.Alpha(.1)).Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Y: -8}}).Children(func() {
			a.glyph(c, "warning-circle-fill", 18, p.red)
			ui.Text(c, a.system(a.errorText)).TextColor(p.red).Grow(1).Basis(0)
			if a.button(c, a.tr("重试"), "").Clicked() {
				a.load()
			}
		})
	}
	if len(a.data) == 0 {
		ui.Column(c.Key("connecting")).FillWidth().Padding(120, 0).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).FontSize(22)
			ui.Text(c, a.tr("正在连接本机采集服务…")).FontSize(14).TextColor(p.secondary)
			if a.progress != nil {
				ui.Text(c, fmt.Sprintf("%v / %v", a.progress["scanned"], a.progress["total"])).FontSize(12).TextColor(p.tertiary).FontFeatures("tnum")
			}
		})
		return
	}
	if a.page != 4 {
		a.filters(c)
		if a.customOpen && a.page != 1 {
			a.customDates(c)
		}
	}
	if a.client != nil && a.snapshotPage != a.page {
		ui.Row(c.Key("loading")).FillWidth().Padding(80, 0).Justify(ui.Center).Children(func() { ui.Spinner(c).FontSize(20) })
		return
	}
	switch a.page {
	case 0:
		a.overview(c)
	case 1:
		a.activity(c, false)
	case 2:
		a.history(c)
	case 3:
		a.quota(c)
	case 4:
		a.settings(c)
	}
	coverage := obj(a.data["coverage"])
	ui.Row(c.Key("footer")).Padding(4, 4, 0, 4).Gap(8).Children(func() {
		ui.Text(c, a.tr("本机数据 ·")+" "+shortStamp(coverage["first"])+" "+a.tr("起 ·")+" "+full(coverage["records"])+" "+a.tr("条记录")).FontSize(11).TextColor(p.tertiary).Grow(1)
		ui.Text(c, a.tr("所有数据仅保存在本机")).FontSize(11).TextColor(p.tertiary)
	})
}

// kc keys the next top-level section by page, so that switching pages brings
// in the new page's sections one after another.
func (a *App) kc(c *ui.Context) *ui.Context {
	a.enterIndex++
	return c.Key(a.enterScope + "/" + fmt.Sprint(a.enterIndex))
}

func (a *App) sidebar(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	side := ui.Column(c.Key("sidebar")).Width(sidebarWidth).Shrink(0).Padding(max(bar.Height, 14)+6, 12, 14, 12).Gap(4)
	if a.adaptMemory {
		side.Draw(func(pt *ui.Painter, _ ui.Rect) {
			w, h := c.Size()
			if px := [2]int{int(w * pt.Scale()), int(h * pt.Scale())}; px != a.memoryPixels {
				a.memoryPixels = px
				adjustGUIMemory(px[0], px[1])
			}
		})
	}
	if !c.Vibrancy() || a.reduceTransparency {
		side.Background(p.sidebar)
	} else {
		side.Background(p.sidebar.Alpha(.35))
	}
	side.BorderWidth(0, 1, 0, 0).BorderColor(p.separator)
	side.Children(func() {
		ui.Row(c).Height(40).Gap(10).Padding(0, 6).AlignItems(ui.Center).DragWindow().Label(a.tr("拖动窗口")).Children(func() {
			if a.brand != nil {
				ui.Image(c, a.brand).Size(30, 30).Radius(8)
			}
			ui.Column(c).Gap(0).Children(func() {
				ui.Text(c, "Codex Monitor").FontSize(14).FontWeight(650).Font(fontDisplay).NoWrap()
				ui.Text(c, a.tr("本机用量监测")).FontSize(11).TextColor(p.secondary).NoWrap()
			})
		})
		search := ui.Button(c, "").Label(a.tr("打开命令面板")).Height(30).Padding(0, 10).Gap(8).Radius(8).Margin(10, 0, 6, 0).Background(p.fill).Border(0, ui.Transparent).Justify(ui.Start)
		search.Children(func() {
			a.glyph(c, "magnifying-glass", 14, p.secondary)
			ui.Text(c, a.tr("搜索")).FontSize(13).TextColor(p.secondary).Grow(1)
			ui.Text(c, "Ctrl K").FontSize(11).TextColor(p.tertiary)
		})
		if search.Clicked() {
			a.commandOpen = true
		}
		a.nav(c)
		ui.Spacer(c)
		a.sidebarStatus(c)
	})
}

func (a *App) nav(c *ui.Context) {
	p := a.pal
	var rects [5]ui.Rect
	col := ui.Column(c.Key("nav")).Gap(2).Label(a.tr("主导航"))
	origin := col.Bounds()
	col.Children(func() {
		for _, item := range navItems {
			if item.page == 0 || item.page == 4 {
				title := a.tr("监测")
				if item.page == 4 {
					title = a.tr("偏好")
				}
				ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(p.tertiary).Padding(float32(item.page/4*10+6), 8, 4, 8)
			}
			selected := a.page == item.page
			row := ui.Row(c.Key(item.page)).Height(34).Padding(0, 8).Gap(10).Radius(8).AlignItems(ui.Center).Focusable().Label(a.tr(item.label))
			if row.Hovered() && !selected {
				row.Background(p.hover)
			}
			rects[item.page] = row.Bounds()
			row.Children(func() {
				a.badge(c, item.icon, item.color(p), 22)
				weight := 450
				if selected {
					weight = 600
				}
				ui.Text(c, a.tr(item.label)).FontSize(13).FontWeight(weight).Grow(1).NoWrap()
				if item.page == 3 {
					if remaining, ok := a.primaryRemaining(); ok {
						color := p.secondary
						if remaining <= 20 {
							color = p.orange
						}
						ui.Text(c, fmt.Sprintf("%.0f%%", remaining)).FontSize(11).FontFeatures("tnum").TextColor(color)
					}
				}
			})
			if row.Clicked() || row.Shortcut(0, ui.KeyEnter) {
				a.navigate(item.page)
			}
		}
	})
	target := rects[a.page]
	if target.H == 0 {
		return
	}
	y := col.AnimateTo("pill", target.Y-origin.Y, springSnappy)
	col.Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(ui.Rect{X: r.X, Y: r.Y + y, W: r.W, H: target.H}, p.selection, 8)
	})
}

func (a *App) primaryRemaining() (float64, bool) {
	for _, q := range objects(a.data["quotas"]) {
		if number(q["minutes"]) == 300 && q["used"] != nil {
			return 100 - number(q["used"]), true
		}
	}
	return 0, false
}

func (a *App) sidebarStatus(c *ui.Context) {
	p := a.pal
	ui.Column(c.Key("status")).Padding(12).Gap(10).Radius(12).Background(p.card.Alpha(.6)).Border(1, p.hairline).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.badge(c, "squares-four-fill", p.teal, 22)
			ui.Text(c, a.tr("桌面小组件")).FontSize(13).Grow(1).NoWrap()
			on := a.widgetMode
			if ui.Switch(c, &on).Label(a.tr("桌面小组件模式")).Disabled(a.busy).Changed() {
				a.toggleWidget()
			}
		})
		scan := obj(a.data["scan"])
		label, color := "等待数据源", p.orange
		if truth(scan["sourceExists"]) {
			label, color = "采集运行中", p.green
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(color).Shadow(0, 0, 6, 0, color.Alpha(.6))
			ui.Text(c, a.tr(label)).FontSize(12).TextColor(p.secondary).Grow(1).NoWrap()
			if last := scan["lastScan"]; last != nil {
				ui.Text(c, clock(last)).FontSize(11).TextColor(p.tertiary).FontFeatures("tnum")
			}
		})
	})
}

func (a *App) toolbar(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	// Controls sit below the native caption buttons. Use the content's width
	// and gutters so actions align with the trailing filter at every size.
	ui.Column(c.Key("toolbar")).Absolute().Top(0).Left(0).Right(0).Height(a.toolbarHeight(c)).Padding(bar.Height, 0, 0, 0).DragWindow().Children(func() {
		ui.Row(c.Key("toolbar-content")).FillWidth().MaxWidth(contentMaxWidth).Margin(0, ui.Auto).Grow(1).Padding(0, contentInset).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c.Key("title"), a.tr(pageNames[a.page])).FontSize(22).FontWeight(700).Font(fontDisplay).NoWrap().ContentTransition(ui.ContentTransition{})
			ui.Text(c.Key("subtitle"), a.subtitle()).FontSize(12).TextColor(p.secondary).NoWrap().Padding(6, 0, 0, 0).ContentTransition(ui.ContentTransition{})
			ui.Spacer(c)
			if a.loading || a.busy {
				ui.Spinner(c).FontSize(14).TextColor(p.secondary)
			}
			if a.iconButton(c, "arrows-clockwise", a.tr("刷新数据与额度")).Disabled(a.busy).Clicked() {
				a.action("refresh", nil, "数据已刷新")
			}
			if a.page != 4 {
				if a.button(c, a.tr("导出 CSV"), "export").Disabled(a.busy || a.loading || len(a.data) == 0).Clicked() {
					a.exportCSV()
				}
			}
		})
	})
}

func (a *App) toolbarHeight(c *ui.Context) float32 { return c.TitleBar().Height + 40 }

func (a *App) subtitle() string {
	switch {
	case len(a.data) == 0:
		return ""
	case a.page == 1:
		return str(obj(a.data["activity"])["year"])
	case a.page == 4:
		return "v" + appVersion
	}
	end := dateValue(str(obj(a.data["range"])["end"]))
	return shortStamp(obj(a.data["range"])["start"]) + " — " + shortStamp(end.UnixMilli()-1)
}

var rangeIDs = []string{"today", "7d", "30d", "all", "custom"}

func (a *App) filters(c *ui.Context) {
	ui.Row(c.Key("filters")).Gap(8).AlignItems(ui.Center).Justify(ui.End).Wrap().Children(func() {
		if a.page != 1 {
			index := 0
			for i, id := range rangeIDs {
				if str(a.filter["range"]) == id {
					index = i
				}
			}
			previous := index
			a.segmented(c, &index, []string{a.tr("今日"), a.tr("近 7 天"), a.tr("近 30 天"), a.tr("全部"), a.tr("自定义")}, 380).Label(a.tr("时间范围"))
			if previous != index {
				a.customOpen = rangeIDs[index] == "custom"
				if !a.customOpen {
					a.change("range", rangeIDs[index])
				}
			}
		}
		ui.Spacer(c)
		ids := []string{"", "current"}
		labels := []string{a.tr("全部账号"), a.tr("当前账号") + " · " + a.accountLabel(str(a.data["currentAccount"]))}
		if a.page == 3 {
			ids, labels = ids[1:], labels[1:]
			if str(a.filter["account"]) == "" {
				ids[0] = ""
			}
		}
		for _, account := range objects(a.data["accounts"]) {
			ids = append(ids, str(account["id"]))
			labels = append(labels, str(account["label"]))
		}
		ids = append(ids, "unassigned")
		labels = append(labels, a.tr("未归属"))
		if id, changed := a.popup(c, "账号筛选", str(a.filter["account"]), ids, labels, 190); changed {
			a.quotaIndex = 0
			a.change("account", id)
		}
		if a.page == 3 {
			return
		}
		for _, entry := range []struct{ key, opt, label string }{{"model", "models", "全部模型"}, {"project", "projects", "全部项目"}} {
			items := stringsOf(obj(a.data["options"])[entry.opt])
			ids := append([]string{""}, items...)
			labels := []string{a.tr(entry.label)}
			for _, s := range items {
				if entry.key == "project" {
					s = shortPath(s)
				}
				labels = append(labels, s)
			}
			if id, changed := a.popup(c, entry.label, str(a.filter[entry.key]), ids, labels, 140); changed {
				a.change(entry.key, id)
			}
		}
	})
	var active []string
	for _, key := range []string{"model", "project", "session"} {
		if str(a.filter[key]) != "" {
			active = append(active, key)
		}
	}
	if len(active) == 0 {
		return
	}
	p := a.pal
	ui.Row(c.Key("chips")).Gap(8).AlignItems(ui.Center).Wrap().Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Y: -6}}).Children(func() {
		for _, key := range active {
			value := str(a.filter[key])
			if key == "project" {
				value = shortPath(value)
			}
			chip := ui.Button(c.Key(key), "").Label(a.tr("移除筛选")+" "+value).Height(26).Padding(0, 6, 0, 10).Gap(6).Radius(999).Background(p.accent.Alpha(.12)).Border(0, ui.Transparent)
			chip.Children(func() {
				ui.Text(c, value).FontSize(12).FontWeight(500).TextColor(p.accent).NoWrap()
				a.glyph(c, "x", 12, p.accent)
			})
			if chip.Clicked() {
				a.change(key, "")
			}
		}
		clear := ui.Button(c, a.tr("清除筛选")).Height(26).Padding(0, 8).FontSize(12).Background(ui.Transparent).Border(0, ui.Transparent).TextColor(p.secondary)
		if clear.Clicked() {
			for _, key := range []string{"model", "project", "session"} {
				delete(a.filter, key)
			}
			a.generation++
			a.load()
		}
	})
}

func (a *App) customDates(c *ui.Context) {
	p := a.pal
	ui.Row(c.Key("custom-dates")).Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Y: -8}}).Padding(10, 14).Gap(10).Radius(12).AlignItems(ui.Center).Background(p.card).Border(1, p.hairline).Children(func() {
		a.glyph(c, "calendar-dots-fill", 16, p.red)
		ui.Text(c, a.tr("开始日期")).FontSize(12).TextColor(p.secondary)
		ui.DateInput(c, &a.startDate).Label(a.tr("开始日期"))
		ui.Text(c, "—").TextColor(p.tertiary)
		ui.Text(c, a.tr("结束日期")).FontSize(12).TextColor(p.secondary)
		ui.DateInput(c, &a.endDate).Label(a.tr("结束日期"))
		ui.Spacer(c)
		invalid := a.startDate.Format("2006-01-02") > a.endDate.Format("2006-01-02")
		if a.primary(c, a.tr("应用日期")).Disabled(invalid).Clicked() {
			a.filter["start"] = a.startDate.Format("2006-01-02")
			a.filter["end"] = a.endDate.Format("2006-01-02")
			a.change("range", "custom")
		}
	})
}

type command struct {
	group, label, icon string
	color              ui.Color
	run                func()
	model              bool
}

func (a *App) commands() []command {
	p := a.pal
	query := strings.ToLower(strings.TrimSpace(a.command))
	var list []command
	for _, item := range navItems {
		label := a.tr(pageNames[item.page])
		if query == "" || strings.Contains(strings.ToLower(label), query) {
			page := item.page
			list = append(list, command{a.tr("页面"), label, item.icon, item.color(p), func() { a.navigate(page) }, false})
		}
	}
	if query == "" {
		return list
	}
	for _, entry := range []struct{ key, filter, icon string }{{"models", "model", "cpu-fill"}, {"projects", "project", "folder-open-fill"}} {
		for _, name := range stringsOf(obj(a.data["options"])[entry.key]) {
			if strings.Contains(strings.ToLower(name), query) {
				key, value := entry.filter, name
				group := a.tr("模型")
				if key == "project" {
					group = a.tr("项目")
				}
				list = append(list, command{group, name, entry.icon, p.purple, func() { a.change(key, value); a.navigate(2) }, key == "model"})
			}
		}
	}
	return list
}

// commandPalette is a Spotlight-like panel: type to filter, arrows to move,
// Enter to go.
func (a *App) commandPalette(c *ui.Context) {
	p := a.pal
	ui.DialogBase(c, &a.commandOpen, func(backdrop, panel ui.Element) {
		shade := float32(.18)
		if p.dark {
			shade = .4
		}
		backdrop.Background(ui.RGBA(0, 0, 0, shade)).Justify(ui.Start).Padding(110, 0, 0, 0)
		panel.Width(580).Radius(18).Material(glass.Glass{Tint: p.card.Alpha(.72)}).Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, .28)).Gap(0).Clip()
		panel.Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Scale: .96, Y: -10}})
		list := a.commands()
		a.commandIndex = max(0, min(a.commandIndex, len(list)-1))
		ui.Row(c).Height(56).Padding(0, 18).Gap(12).AlignItems(ui.Center).Children(func() {
			a.glyph(c, "magnifying-glass", 20, p.secondary)
			if ui.TextInput(c, &a.command).Placeholder(a.tr("搜索页面、模型或项目")).AutoFocus().Grow(1).FontSize(18).Background(ui.Transparent).Border(0, ui.Transparent).Padding(0).Label(a.tr("搜索命令")).Changed() {
				a.commandIndex = 0
			}
		})
		a.separator(c, 0)
		if c.Shortcut(0, ui.KeyDown) {
			a.commandIndex = min(a.commandIndex+1, len(list)-1)
		}
		if c.Shortcut(0, ui.KeyUp) {
			a.commandIndex = max(a.commandIndex-1, 0)
		}
		run := func(cmd command) {
			a.commandOpen, a.command = false, ""
			cmd.run()
		}
		if c.Shortcut(0, ui.KeyEnter) && len(list) > 0 {
			run(list[a.commandIndex])
		}
		ui.Column(c).Padding(8).Gap(2).MaxHeight(360).Children(func() {
			if len(list) == 0 {
				ui.Text(c, a.tr("没有匹配的结果")).Padding(18).TextColor(p.secondary)
			}
			group := ""
			for i, cmd := range list {
				if cmd.group != group {
					group = cmd.group
					ui.Text(c, group).FontSize(11).FontWeight(600).TextColor(p.tertiary).Padding(8, 10, 4, 10)
				}
				row := ui.Row(c.Key(cmd.group+cmd.label)).Height(38).Padding(0, 10).Gap(10).Radius(9).AlignItems(ui.Center)
				if row.Hovered() {
					a.commandIndex = i
				}
				selected := i == a.commandIndex
				if selected {
					row.Background(p.accent)
				}
				row.Children(func() {
					if cmd.model {
						a.modelMark(c, cmd.label, 24)
					} else {
						a.badge(c, cmd.icon, cmd.color, 24)
					}
					text := ui.Text(c, cmd.label).FontSize(14).Grow(1).SingleLine()
					if selected {
						text.TextColor(ui.RGB(255, 255, 255))
						a.glyph(c, "arrow-right", 14, ui.RGB(255, 255, 255))
					}
				})
				if row.Clicked() {
					run(cmd)
				}
			}
		})
	})
}

func (a *App) openDay(day string) {
	a.filter["range"] = "custom"
	a.filter["start"] = day
	a.filter["end"] = day
	a.filter["recordSearch"] = ""
	a.filter["recordStatus"] = ""
	a.recordPage = 1
	a.selectedDay = day
	a.generation++
	a.navigate(2)
	a.load()
}

func dateValue(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func stampTime(v any) time.Time {
	if v == nil || str(v) == "" {
		return time.Time{}
	}
	switch v.(type) {
	case float64, int, int64:
		return time.UnixMilli(int64(number(v)))
	}
	return dateValue(str(v))
}

func shortStamp(v any) string {
	t := stampTime(v)
	if t.IsZero() {
		if v == nil || str(v) == "" {
			return "—"
		}
		return str(v)
	}
	return t.Local().Format("01/02 15:04")
}

func clock(v any) string {
	if t := stampTime(v); !t.IsZero() {
		return t.Local().Format("15:04")
	}
	return ""
}
