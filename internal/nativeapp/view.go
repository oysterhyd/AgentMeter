package nativeapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) View(c *ui.Context) {
	a.skin(c)
	t := c.Theme()
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
	shell := ui.Column(c).Key("shell").Absolute().Left(0).Right(0)
	shell.Top(0).Bottom(0)
	shell.Children(func() {
		a.header(c)
		ui.Scroll(c).Key("main-scroll").TrackScroll(&a.mainScroll).Grow(1).ClipX().Children(func() {
			ui.Column(c).Key("content").FillWidth().MaxWidth(1440).Margin(0, ui.Auto).Padding(22, 36, 18, 36).Gap(18).Children(func() {
				a.title(c)
				if a.errorText != "" {
					ui.Row(c).Padding(12).Gap(14).Radius(12).Background(t.Danger.Alpha(.1)).Children(func() {
						ui.Text(c, a.system(a.errorText)).TextColor(t.Danger).Grow(1)
						if ui.Button(c, a.tr("重试")).Clicked() {
							a.load()
						}
					})
				}
				if a.toast != "" {
					ui.Row(c).Padding(10).Background(t.Accent.Alpha(.08)).Children(func() {
						ui.Text(c, a.toast).Grow(1)
						if ui.Button(c, a.tr("关闭")).Clicked() {
							a.toast = ""
						}
					})
				}
				if len(a.data) == 0 {
					ui.Text(c, a.tr("正在连接本机采集服务…")).Padding(30)
					if a.progress != nil {
						ui.Text(c, fmt.Sprintf("%v / %v", a.progress["scanned"], a.progress["total"]))
					}
					return
				}
				if a.page != 4 {
					a.filters(c)
					if a.customOpen && a.page != 1 {
						a.customDates(c)
					}
				}
				if a.client != nil && a.snapshotPage != a.page {
					ui.Text(c, a.tr("正在连接本机采集服务…")).Padding(30)
					return
				}
				ui.Column(c).Key("body-" + pageIDs[a.page]).FillWidth().Shrink(0).Gap(18).Transition(enterMotion(a.pageEnterX, 7)).Children(func() {
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
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, a.tr("本机数据 ·")+" "+shortStamp(obj(a.data["coverage"])["first"])+" "+a.tr("起 ·")+" "+full(obj(a.data["coverage"])["records"])+" "+a.tr("条记录")).FontSize(10).TextColor(t.TextMuted).Grow(1)
						ui.Text(c, a.tr("采集更新")).FontSize(10).TextColor(t.TextMuted)
					})
				})
			})
		})
	})
	ui.Modal(c, &a.commandOpen, func() {
		ui.Text(c, a.tr("命令面板")).FontSize(20).Bold()
		ui.TextInput(c, &a.command).Placeholder(a.tr("搜索页面、模型或项目")).AutoFocus().Width(520).Label(a.tr("搜索命令"))
		query := strings.ToLower(strings.TrimSpace(a.command))
		ui.Column(c).Gap(6).Children(func() {
			for i, name := range pageNames {
				if query == "" || strings.Contains(strings.ToLower(a.tr(name)), query) {
					if ui.Button(c, a.tr(name)).Clicked() {
						a.commandOpen = false
						a.navigate(i)
					}
				}
			}
			for _, key := range []string{"models", "projects"} {
				for _, name := range stringsOf(obj(a.data["options"])[key]) {
					if query != "" && strings.Contains(strings.ToLower(name), query) {
						if ui.Button(c, name).Clicked() {
							a.commandOpen = false
							a.change(map[string]string{"models": "model", "projects": "project"}[key], name)
							a.navigate(2)
						}
					}
				}
			}
		})
	})
}
func (a *App) selectKey(c *ui.Context, label, current string, ids, labels []string, width float32) (string, bool) {
	chosen := ""
	for i, id := range ids {
		if id == current {
			chosen = labels[i]
			break
		}
	}
	if chosen == "" && len(labels) > 0 {
		chosen = labels[0]
	}
	changed := ui.Select(c, &chosen, labels).Width(width).Height(35).FontSize(12).Radius(9).Label(a.tr(label)).Changed()
	if changed {
		for i, v := range labels {
			if v == chosen {
				return ids[i], true
			}
		}
	}
	return current, false
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
