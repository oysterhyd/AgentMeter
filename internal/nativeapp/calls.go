package nativeapp

import (
	"fmt"
	"github.com/egoist/mygo/ui"
)

func (a *App) calls(c *ui.Context) {
	p := a.pal
	data := obj(a.data["calls"])
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			a.header(c, a.tr("逐次调用明细"), a.tr("共 {0} 条", obj(data["pagination"])["total"]), nil)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if a.searchField(c, &a.callSearch, a.tr("搜索响应、任务、项目或模型"), a.tr("搜索逐次调用")).Changed() {
					a.change("callSearch", a.callSearch)
				}
				if id, changed := a.popup(c, "调用状态", str(a.filter["callStatus"]), []string{"", "usage", "failed", "retry"}, []string{a.tr("全部记录"), a.tr("用量记录"), a.tr("失败事件"), a.tr("重试事件")}, 120); changed {
					a.change("callStatus", id)
				}
				order := str(a.filter["callOrder"])
				if order == "" {
					order = "newest"
				}
				if id, changed := a.popup(c, "调用排序", order, []string{"newest", "oldest", "tokens", "cost", "ttft"}, []string{a.tr("最新优先"), a.tr("最早优先"), a.tr("Token 最多"), a.tr("费用最高"), a.tr("首字最慢")}, 120); changed {
					a.change("callOrder", id)
				}
			})
			a.callSummary(c, obj(data["summary"]), false)
			if len(data) == 0 {
				ui.Row(c).Padding(24).Justify(ui.Center).Children(func() { ui.Spinner(c) })
				return
			}
			a.callList(c, objects(data["rows"]), false)
			a.callPagination(c, obj(data["pagination"]), false)
		})
	})
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			telemetry := obj(a.data["telemetry"])
			coverage := obj(a.data["timingCoverage"])
			ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
				a.badge(c, "broadcast-fill", p.teal, 36)
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, a.tr("首字与请求诊断")).FontSize(15).FontWeight(600).Font(fontDisplay)
					ui.Text(c, a.tr("首字时间为请求发出至首次输出的等待时间；缺失时显示未知。失败与重试是观测到的诊断事件，缺失 Token 的事件不参与费用计算。")).FontSize(12).LineHeight(1.4).TextColor(p.secondary)
					ui.Text(c, a.tr("已采集首字 {0} 条，已关联用量 {1} 条", coverage["received"], coverage["associated"])).FontSize(11).TextColor(p.tertiary)
					if !truth(telemetry["listening"]) {
						ui.Text(c, a.tr("在设置中开启本机首字采集，并配置 Codex 或加载 Pi 扩展后，新调用会记录首字时间。")).FontSize(12).TextColor(p.orange)
					}
				})
				if a.button(c, a.tr("打开采集设置"), "").Clicked() {
					a.settingsSection = 0
					a.navigate(4)
				}
			})
		})
	})
}

func (a *App) callSummary(c *ui.Context, s Object, task bool) {
	if len(s) == 0 {
		return
	}
	p := a.pal
	ui.Row(c).Gap(0).Padding(12, 0).Radius(12).Background(p.fill.Alpha(.45)).Children(func() {
		for i, item := range []struct{ label, value string }{
			{a.tr("用量记录"), full(s["usageRecords"])},
			{a.tr("失败事件"), full(s["failedEvents"])},
			{a.tr("重试事件"), full(s["retryEvents"])},
			{a.tr("平均首字"), a.ttft(s["avgTtft"])},
			{a.tr("P95 首字"), a.ttft(s["p95Ttft"])},
		} {
			col := ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(0, 16).Gap(4)
			if i > 0 {
				col.BorderWidth(0, 0, 0, 1).BorderColor(p.separator)
			}
			col.Children(func() {
				ui.Text(c, item.label).FontSize(11).TextColor(p.secondary).SingleLine()
				ui.Text(c, item.value).FontSize(18).FontWeight(600).Font(fontDisplay).FontFeatures("tnum").SingleLine().ContentTransition(ui.ContentTransition{Numeric: true})
			})
		}
	})
	note := a.tr("用量首字样本 {0} / {1}", s["ttftSamples"], s["usageRecords"])
	if task && str(s["largestId"]) != "" {
		note += " · " + a.tr("展开记录可查看任务内消耗占比；缓存和推理 Token 已包含在输入和输出中。")
	}
	ui.Text(c, note).FontSize(11).TextColor(p.tertiary)
}

func (a *App) ttft(v any) string {
	if v == nil {
		return a.tr("未知")
	}
	return fmt.Sprintf("%.2f", number(v)/1000) + a.tr(" 秒")
}

func callPercent(v any) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", number(v)*100)
}

func callMoney(v any) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("$%.6f", number(v))
}

func (a *App) callList(c *ui.Context, rows []Object, task bool) {
	p := a.pal
	a.tableHeader(c, []column{{"时间 / 状态", 2.3, false}, {"模型", 2, false}, {"首字时间", 1.3, false}, {"输入 / 输出", 2, true}, {"等值估算", 1.6, true}})
	state := &a.callState
	if task {
		state = &a.taskCallState
	}
	state.Key = func(i int) any { return str(rows[i]["id"]) }
	state.Selected = &a.selectedCall
	ui.List(c, state, len(rows), func(i int) {
		r := rows[i]
		usage := r["status"] == "usage"
		row := a.tableRow(c, 60, str(r["id"]), a.expandedCall == str(r["id"]))
		row.Children(func() {
			ui.Column(c).Grow(2.3).Basis(0).MinWidth(0).Gap(4).AlignItems(ui.Start).Children(func() {
				ui.Text(c, dateValue(str(r["ts"])).Local().Format("01-02 15:04:05")).FontSize(13).FontWeight(500).FontFeatures("tnum").SingleLine()
				a.statusPill(c, str(r["status"]))
			})
			ui.Column(c).Grow(2).Basis(0).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, str(r["model"])).FontSize(12).FontWeight(500).SingleLine().Tooltip(str(r["model"]))
				sub := shortPath(str(r["project"]))
				if task && r["sequence"] != nil {
					sub = a.tr("第 {0} 次用量", r["sequence"])
				}
				ui.Text(c, sub).FontSize(11).TextColor(p.secondary).SingleLine()
			})
			ui.Column(c).Grow(1.3).Basis(0).MinWidth(0).Gap(3).AlignItems(ui.Start).Children(func() {
				color := p.green
				if r["ttft"] == nil {
					color = p.gray
				} else if number(r["ttft"]) > 5000 {
					color = p.orange
				}
				a.pill(c, a.ttft(r["ttft"]), color)
				if !usage && str(r["errorLabel"]) != "" {
					ui.Text(c, a.tr(str(r["errorLabel"]))).FontSize(11).TextColor(p.secondary).SingleLine()
				}
			})
			ui.Column(c).Grow(2).Basis(0).MinWidth(0).Gap(3).AlignItems(ui.End).Children(func() {
				value := "—"
				if usage {
					value = full(r["input"]) + " / " + full(r["output"])
				}
				ui.Text(c, value).FontSize(13).FontWeight(500).FontFeatures("tnum")
				if usage {
					ui.Text(c, a.tr("缓存命中")+" "+full(r["cached"])).FontSize(11).TextColor(p.secondary)
				} else if r["attempt"] != nil {
					ui.Text(c, a.tr("尝试序号 {0}", r["attempt"])).FontSize(11).TextColor(p.secondary)
				}
			})
			ui.Column(c).Grow(1.6).Basis(0).MinWidth(0).Gap(3).AlignItems(ui.End).Children(func() {
				value := "—"
				if usage {
					value = a.tr("未定价")
					if r["cost"] != nil {
						value = fmt.Sprintf("$%.6f", number(r["cost"]))
					}
				}
				ui.Text(c, value).FontSize(13).FontWeight(600).FontFeatures("tnum")
				if r["priceInput"] != nil {
					ui.Text(c, fmt.Sprintf("$%s / $%s /M", shortNumber(number(r["priceInput"])), shortNumber(number(r["priceOutput"])))).FontSize(10).TextColor(p.tertiary).Tooltip(a.tr("普通输入 / 输出单价；缓存输入按独立价格计算"))
				}
			})
		})
		if row.Clicked() || row.Shortcut(0, ui.KeyEnter) {
			if a.expandedCall == str(r["id"]) {
				a.expandedCall = ""
			} else {
				a.expandedCall = str(r["id"])
			}
		}
	}).Height(float32(min(8, len(rows))) * 60).Label(a.tr("逐次调用列表"))
	if len(rows) == 0 {
		a.empty(c, a.tr("没有匹配的调用记录"))
	}
	for _, r := range rows {
		if a.expandedCall == str(r["id"]) {
			a.callDetail(c, r, task)
			break
		}
	}
}

func (a *App) callDetail(c *ui.Context, r Object, task bool) {
	p := a.pal
	ui.Column(c.Key("call-"+str(r["id"]))).Padding(14, 16).Gap(0).Radius(12).Background(p.fill.Alpha(.5)).Transition(ui.ElementTransition{Animation: &springSnappy, Enter: &ui.Motion{Y: -8}}).Children(func() {
		id := str(r["response_id"])
		if id == "" {
			id = str(r["usage_id"])
		}
		if id == "" {
			id = str(r["id"])
		}
		items := [][2]string{{"响应 / 记录 ID", id}, {"任务 ID", str(r["turn"])}, {"会话", str(r["session"])}}
		if r["status"] == "usage" {
			items = append(items, [][2]string{
				{"缓存命中率", callPercent(r["cacheRate"])}, {"推理 Token", full(r["reasoning"])}, {"缓存写入", full(r["cache_write"])},
				{"首字时间", a.ttft(r["ttft"]) + " · " + a.tr(str(r["timingNote"]))},
				{"输入费用", callMoney(r["inputCost"])}, {"输出费用", callMoney(r["outputCost"])}, {"记录方式", a.system(str(r["kind"]))},
			}...)
			if task {
				items = append(items, [2]string{"任务 Token 占比", callPercent(r["tokenShare"])}, [2]string{"已定价费用占比", callPercent(r["costShare"])})
			}
		} else {
			items = append(items, [2]string{"错误类型", a.tr(str(r["errorLabel"])) + " · HTTP " + str(r["http_status"])})
		}
		ui.Text(c, a.tr("调用详情")).FontSize(13).FontWeight(600).Padding(0, 0, 6, 0)
		for i, item := range items {
			ui.Row(c).MinHeight(30).Gap(12).AlignItems(ui.Center).BorderWidth(map[bool]float32{true: 0, false: 1}[i == 0], 0, 0, 0).BorderColor(p.separator).Children(func() {
				ui.Text(c, a.tr(item[0])).FontSize(12).TextColor(p.secondary).Width(130)
				ui.Text(c, item[1]).FontSize(12).FontFeatures("tnum").Grow(1).Basis(0).SingleLine().Selectable().Tooltip(item[1])
			})
		}
		if r["status"] != "usage" {
			a.caption(c, a.tr("此事件没有独立用量，Token 和费用保持未知。")).Padding(6, 0, 0, 0)
		}
	})
}

func (a *App) callPagination(c *ui.Context, p Object, task bool) {
	page := int(number(p["page"]))
	a.pager(c, page, int(number(p["pages"])), func(d int) {
		if task {
			a.detailPage = page + d
		} else {
			a.callPage = page + d
		}
		a.generation++
		a.load()
	})
}

func (a *App) taskCalls(c *ui.Context, task Object) {
	d := obj(a.data["callDetails"])
	ui.Text(c, a.tr("任务内调用分析")).FontSize(14).FontWeight(600).Padding(6, 0, 0, 0)
	if str(d["session"]) != str(task["session"]) || str(d["turn"]) != str(task["id"]) {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).FontSize(13)
			a.caption(c, a.tr("正在加载调用明细…"))
		})
		return
	}
	a.callSummary(c, obj(d["summary"]), true)
	a.callList(c, objects(d["rows"]), true)
	a.callPagination(c, obj(d["pagination"]), true)
}
