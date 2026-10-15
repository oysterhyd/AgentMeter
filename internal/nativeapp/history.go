package nativeapp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// One derived list per snapshot/filter/order. Animation frames reuse it.
type breakdownCache struct {
	revision   uint64
	key, query string
	order      ui.SortOrder
	valid      bool
	rows       []Object
}

func (a *App) breakdownRows(key string) []Object {
	query := strings.ToLower(a.breakdownSearch)
	cache := &a.breakdownCache
	if cache.valid && cache.revision == a.snapshotRevision && cache.key == key && cache.query == query && cache.order == a.breakdownSort {
		return cache.rows
	}
	rows := []Object{}
	for _, row := range objects(a.data[key]) {
		if query == "" || strings.Contains(strings.ToLower(str(row["name"])+" "+str(row["project"])), query) {
			rows = append(rows, row)
		}
	}
	column, descending := a.breakdownSort.Column, a.breakdownSort.Descending
	if column == "" {
		column, descending = "total", true
	}
	// The service already sorts totals descending, with stable tie order.
	if column != "total" || !descending {
		sort.SliceStable(rows, func(i, j int) bool {
			left, right := rows[i], rows[j]
			if descending {
				left, right = right, left
			}
			if column == "name" {
				return str(left["name"]) < str(right["name"])
			}
			return number(left[column]) < number(right[column])
		})
	}
	*cache = breakdownCache{revision: a.snapshotRevision, key: key, query: query, order: a.breakdownSort, valid: true, rows: rows}
	return rows
}

// searchField is a rounded search field with a magnifying glass.
func (a *App) searchField(c *ui.Context, value *string, placeholder, label string) ui.Element {
	p := a.pal
	var input ui.Element
	ui.Row(c).Height(30).Grow(1).Basis(0).MinWidth(160).Padding(0, 10).Gap(6).Radius(8).AlignItems(ui.Center).Background(p.fill).Children(func() {
		a.glyph(c, "magnifying-glass", 14, p.secondary)
		input = ui.TextInput(c, value).Placeholder(placeholder).Label(label).Grow(1).Height(28).FontSize(13).Padding(0).Background(ui.Transparent).Border(0, ui.Transparent)
		if *value != "" {
			clear := ui.Button(c, "").Label(a.tr("清除搜索")).Size(18, 18).Padding(0).Radius(9).Background(p.tertiary.Alpha(.5)).Border(0, ui.Transparent)
			clear.Children(func() { a.glyph(c, "x", 10, p.card) })
			if clear.Clicked() {
				*value = ""
				input.SetChanged()
			}
		}
	})
	return input
}

type column struct {
	label string
	grow  float32
	end   bool
}

func (a *App) tableHeader(c *ui.Context, columns []column) {
	ui.Row(c).Height(30).Padding(0, 12).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(a.pal.separator).Children(func() {
		for _, col := range columns {
			e := ui.Text(c, a.tr(col.label)).Grow(col.grow).Basis(0).MinWidth(0).FontSize(11).FontWeight(600).TextColor(a.pal.secondary).SingleLine()
			if col.end {
				e.TextAlign(ui.End)
			}
		}
	})
}

// tableRow is a hoverable, focusable table line.
func (a *App) tableRow(c *ui.Context, height float32, label string, selected bool) ui.Element {
	p := a.pal
	row := ui.Row(c).Height(height).Padding(0, 12).Gap(12).AlignItems(ui.Center).Radius(8).Focusable().Label(label)
	switch {
	case selected:
		row.Background(p.accent.Alpha(.12))
	case row.Hovered():
		row.Background(p.hover)
	}
	return row
}

func (a *App) cell(c *ui.Context, text string, grow float32, end bool) ui.Element {
	e := ui.Text(c, text).Grow(grow).Basis(0).MinWidth(0).FontSize(12).FontFeatures("tnum").SingleLine()
	if end {
		e.TextAlign(ui.End)
	}
	return e
}

// pager steps through pages; move gets -1 or +1.
func (a *App) pager(c *ui.Context, page, pages int, move func(int)) {
	if pages <= 1 {
		return
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		if a.iconButton(c, "caret-left", a.tr("上一页")).Disabled(page <= 1).Clicked() {
			move(-1)
		}
		ui.Text(c.Key("page"), fmt.Sprintf("%d / %d", page, pages)).FontSize(12).TextColor(a.pal.secondary).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		if a.iconButton(c, "caret-right", a.tr("下一页")).Disabled(page >= pages).Clicked() {
			move(1)
		}
	})
}

var statusNames = map[string]string{"completed": "已完成", "failed": "失败", "aborted": "已取消", "running": "未结束", "usage": "用量记录", "retry": "重试事件"}

func (a *App) statusPill(c *ui.Context, status string) ui.Element {
	p := a.pal
	color := map[string]ui.Color{"completed": p.green, "usage": p.green, "failed": p.red, "retry": p.orange, "running": p.blue}[status]
	if color == (ui.Color{}) {
		color = p.gray
	}
	text := statusNames[status]
	if text == "" {
		text = status
	}
	if status == "failed" {
		text = "失败事件"
		if a.historyMode == 0 {
			text = "失败"
		}
	}
	return a.pill(c, a.tr(text), color)
}

func (a *App) history(c *ui.Context) {
	a.kpis(c)
	previousMode := a.historyMode
	ui.Row(a.kc(c)).Children(func() {
		a.segmented(c, &a.historyMode, []string{a.tr("任务分析"), a.tr("逐次调用")}, 240).Label(a.tr("分析方式"))
	})
	if previousMode != a.historyMode {
		a.mainScroll.Y = 0
		a.generation++
		a.load()
	}
	if a.historyMode == 1 {
		a.calls(c)
		return
	}
	a.breakdown(c)
	a.metrics(c)
	a.records(c)
}

func (a *App) breakdown(c *ui.Context) {
	p := a.pal
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			previous := a.viewIndex
			key := []string{"models", "projects", "tasks"}[a.viewIndex]
			a.header(c, a.tr("消耗明细"), a.tr("共 {0} 条", len(objects(a.data[key]))), func() {
				a.segmented(c, &a.viewIndex, []string{a.tr("按模型"), a.tr("按项目"), a.tr("按任务")}, 220).Label(a.tr("明细分组"))
			})
			if previous != a.viewIndex {
				a.breakdownPage = 1
				a.generation++
				a.load()
				key = []string{"models", "projects", "tasks"}[a.viewIndex]
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				a.searchField(c, &a.breakdownSearch, a.tr("搜索名称"), a.tr("搜索消耗明细"))
				sortKeys := []string{"total", "cost", "output", "name"}
				current := a.breakdownSort.Column
				if current == "" {
					current = "total"
				}
				if id, changed := a.popup(c, "明细排序", current, sortKeys, []string{a.tr("Token 最多"), a.tr("费用最高"), a.tr("输出最多"), a.tr("名称顺序")}, 130); changed {
					a.breakdownSort.Column = id
					a.breakdownSort.Descending = id != "name"
				}
			})
			rows := a.breakdownRows(key)
			pages := max(1, (len(rows)+49)/50)
			a.breakdownPage = max(1, min(a.breakdownPage, pages))
			start := (a.breakdownPage - 1) * 50
			shown := rows[min(start, len(rows)):min(start+50, len(rows))]
			name := map[string]string{"models": "模型", "projects": "项目", "tasks": "任务 ID"}[key]
			a.tableHeader(c, []column{{name, 2.4, false}, {"输入", 1, true}, {"缓存", 1, true}, {"输出", 1, true}, {"等值 USD", 1, true}})
			peak := 1.0
			for _, row := range rows {
				peak = max(peak, number(row["total"]))
			}
			ui.List(c, &a.historyState, len(shown), func(i int) {
				row := shown[i]
				e := a.tableRow(c, 44, str(row["name"]), false)
				e.Children(func() {
					label := str(row["name"])
					if key == "projects" {
						label = shortPath(label)
					}
					ui.Column(c).Grow(2.4).Basis(0).MinWidth(0).Gap(5).Children(func() {
						ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine().Tooltip(str(row["name"]))
						share := float32(number(row["total"]) / peak)
						ui.Box(c).Height(4).FillWidth().Radius(2).Background(p.fill).Draw(func(pt *ui.Painter, r ui.Rect) {
							pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.H, r.W*share), H: r.H}, p.series(i), 2)
						})
					})
					for _, k := range []string{"input", "cached", "output"} {
						a.cell(c, full(row[k]), 1, true)
					}
					cost := money(row["cost"])
					if number(row["requests"]) > 0 && number(row["requests"]) == number(row["unpriced"]) {
						cost = a.tr("未定价")
					}
					a.cell(c, cost, 1, true).FontWeight(600)
				})
				if e.Clicked() || e.Shortcut(0, ui.KeyEnter) {
					a.change(map[string]string{"models": "model", "projects": "project", "tasks": "session"}[key], str(row["name"]))
				}
			}).Height(float32(min(10, len(shown))) * 44).Label(a.tr("历史分析表格"))
			if len(rows) == 0 {
				a.empty(c, a.tr("没有匹配的结果"))
			}
			a.pager(c, a.breakdownPage, pages, func(d int) { a.breakdownPage += d })
		})
	})
}

func (a *App) records(c *ui.Context) {
	p := a.pal
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			a.header(c, a.tr("任务运行记录"), a.tr("共 {0} 条", obj(a.data["records"])["total"]), nil)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if a.searchField(c, &a.search, a.tr("搜索任务、项目或模型"), a.tr("搜索任务记录")).Changed() {
					a.change("recordSearch", a.search)
				}
				if id, changed := a.popup(c, "任务状态筛选", str(a.filter["recordStatus"]), []string{"", "completed", "failed", "aborted", "running"}, []string{a.tr("全部状态"), a.tr("已完成"), a.tr("失败"), a.tr("已取消"), a.tr("未结束")}, 110); changed {
					a.change("recordStatus", id)
				}
				current := str(a.filter["recordOrder"])
				if current == "" {
					current = "newest"
				}
				if id, changed := a.popup(c, "记录排序", current, []string{"newest", "oldest"}, []string{a.tr("最新优先"), a.tr("最早优先")}, 110); changed {
					a.change("recordOrder", id)
				}
			})
			rows := objects(a.data["turns"])
			a.recordState.Key = func(i int) any { return str(rows[i]["session"]) + ":" + str(rows[i]["id"]) }
			a.recordState.Selected = &a.selectedRecord
			columns := []column{{"开始时间 / 任务", 2.6, false}, {"模型", 1.5, false}, {"状态", 1, false}, {"输入 Token", 1, true}, {"输出 Token", 1, true}, {"估算费用", 1.2, true}, {"耗时", .9, true}, {"首 Token", .9, true}}
			a.tableHeader(c, columns)
			ui.List(c, &a.recordState, len(rows), func(i int) {
				v := rows[i]
				identity := str(v["session"]) + ":" + str(v["id"])
				row := a.tableRow(c, 58, str(v["id"]), a.expanded == identity)
				row.Children(func() {
					ui.Column(c).Grow(2.6).Basis(0).MinWidth(0).Gap(3).Children(func() {
						ui.Text(c, shortStamp(v["started"])).FontSize(13).FontWeight(500).FontFeatures("tnum")
						session := str(v["session"])
						ui.Text(c, session[:min(8, len(session))]+" · "+shortPath(str(v["project"]))).FontSize(11).TextColor(p.secondary).SingleLine()
					})
					ui.Text(c, str(v["model"])).Grow(1.5).Basis(0).MinWidth(0).FontSize(12).SingleLine()
					ui.Row(c).Grow(1).Basis(0).Children(func() { a.statusPill(c, str(v["status"])) })
					a.cell(c, full(v["input"]), 1, true)
					a.cell(c, full(v["output"]), 1, true)
					ui.Column(c).Grow(1.2).Basis(0).MinWidth(0).Gap(2).AlignItems(ui.End).Children(func() {
						ui.Text(c, a.recordCost(v)).FontSize(12).FontWeight(600).FontFeatures("tnum")
						ui.Text(c, a.tr("输入")+" "+money(v["inputCost"])).FontSize(10).TextColor(p.tertiary)
					})
					a.cell(c, a.duration(v["duration"]), .9, true)
					a.cell(c, a.duration(v["ttft"]), .9, true)
				})
				if row.Clicked() || row.Shortcut(0, ui.KeyEnter) {
					a.toggleRecord(v, identity)
				}
			}).Height(float32(min(8, len(rows))) * 58).Label(a.tr("任务运行记录表格"))
			if len(rows) == 0 {
				a.empty(c, a.tr("这个时间范围内还没有记录"))
			}
			r := obj(a.data["records"])
			a.pager(c, int(number(r["page"])), int(number(r["pages"])), func(d int) {
				a.recordPage = int(number(r["page"])) + d
				a.generation++
				a.load()
			})
			for _, row := range rows {
				if a.expanded == str(row["session"])+":"+str(row["id"]) {
					a.recordDetail(c, row)
				}
			}
		})
	})
}

func (a *App) toggleRecord(v Object, identity string) {
	if a.expanded == identity {
		a.expanded = ""
	} else {
		a.expanded = identity
	}
	a.detailPage = 1
	a.expandedCall = ""
	delete(a.filter, "detailSession")
	delete(a.filter, "detailTurn")
	if a.expanded != "" {
		a.filter["detailSession"], a.filter["detailTurn"] = v["session"], v["id"]
	}
	a.generation++
	a.load()
}

func (a *App) recordDetail(c *ui.Context, row Object) {
	p := a.pal
	ui.Column(c.Key(a.expanded)).Padding(16).Gap(12).Radius(12).Background(p.fill.Alpha(.5)).Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Y: -8}}).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.badge(c, "terminal-window-fill", p.indigo, 26)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, str(row["id"])).FontSize(13).FontWeight(600).SingleLine().Selectable()
				a.caption(c, str(row["project"])+" · "+str(row["origin"])).SingleLine()
			})
			if a.iconButton(c, "x", a.tr("收起")).Clicked() {
				a.toggleRecord(row, a.expanded)
			}
		})
		for _, model := range objects(row["models"]) {
			ui.Row(c).Gap(16).Padding(8, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.separator).Children(func() {
				ui.Text(c, str(model["model"])).Width(190).FontWeight(500).SingleLine()
				for _, item := range [][2]string{{"输入", full(model["input"])}, {"缓存输入", full(model["cached"])}, {"输出", full(model["output"])}} {
					ui.Text(c, a.tr(item[0])+" "+item[1]).FontSize(12).Grow(1).Basis(0).FontFeatures("tnum")
				}
				ui.Text(c, a.rowCost(model)).Width(150).FontSize(12).FontWeight(600).TextAlign(ui.End)
			})
		}
		a.caption(c, a.tr("任务耗时")+" "+a.duration(row["duration"])+" · "+a.tr("首 Token 延迟")+" "+a.duration(row["ttft"]))
		a.taskCalls(c, row)
	})
}
