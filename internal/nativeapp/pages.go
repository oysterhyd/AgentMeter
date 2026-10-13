package nativeapp

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) overview(c *ui.Context) {
	s, p := obj(a.data["sums"]), obj(a.data["performance"])
	a.summaryStrip(c)
	a.activity(c, true)
	ui.Row(c).Gap(18).AlignItems(ui.Stretch).Children(func() {
		surface(c, 2, 326, func() {
			ui.Row(c).Height(36).Children(func() {
				ui.Row(c).Gap(8).Grow(1).Children(func() {
					ui.Icon(c, icons["chart"]).Size(16, 16).TextColor(c.Theme().TextMuted)
					ui.Text(c, a.tr("Token 使用趋势")).FontSize(13).FontWeight(650)
				})
				segment(c, &a.metricIndex, []string{a.tr("总量"), a.tr("输出"), "USD"}, nil, 155, 35)
			})
			ui.Row(c).Gap(18).Children(func() {
				for i, item := range [][2]string{{"输入（含缓存）", compact(s["input"])}, {"输出", compact(s["output"])}} {
					color := c.Theme().Accent
					if i == 1 {
						color = ui.Hex("#416bd3")
					}
					ui.Box(c).Size(6, 6).Radius(2).Background(color)
					ui.Text(c, a.tr(item[0])).FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Text(c, item[1]).FontSize(11).Font("Consolas")
				}
			})
			metric := []string{"total", "output", "cost"}[a.metricIndex]
			unit := "tokens"
			if metric == "cost" {
				unit = "USD"
			}
			a.chart(c, "usage-"+metric, a.timelinePoints(metric), false, false, unit)
		})
		surface(c, 1, 326, func() {
			panelHeading(c, a.tr("任务平均输出速率"), "")
			ui.Row(c).Gap(8).Margin(18, 0, 0, 0).AlignItems(ui.End).Children(func() {
				animatedValue(c, "latest-tps", p["latestTps"], func(v any) string {
					if v == nil {
						return "—"
					}
					return fmt.Sprintf("%.1f", number(v))
				}, false).FontSize(42).FontWeight(550).LetterSpacing(-1.5).TextColor(c.Theme().Accent)
				ui.Text(c, "tokens / sec").FontSize(11).TextColor(c.Theme().TextMuted).Padding(0, 0, 8, 0)
			})
			context := a.tr("尚无带完整耗时的已完成任务")
			if p["latestAt"] != nil {
				context = a.tr("最近完成 · ") + shortStamp(p["latestAt"])
			}
			ui.Text(c, context).FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Row(c).Margin(8, 0, 0, 0).Padding(12, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Gap(22).Children(func() {
				ui.Column(c).Grow(1).Gap(7).Children(func() {
					ui.Text(c, a.tr("范围内加权平均")).FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Text(c, fmt.Sprintf("%.1f", number(p["taskTps"]))+" tok/s").FontSize(20).FontWeight(550)
				})
				ui.Column(c).Grow(1).Gap(7).Children(func() {
					ui.Text(c, a.tr("近期活跃任务")).FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Text(c, full(p["active"])).FontSize(20).FontWeight(550)
				})
			})
		})
	})
	ui.Row(c).Gap(18).AlignItems(ui.Stretch).Children(func() {
		surface(c, 1, 0, func() {
			panelHeading(c, a.tr("模型分布"), "")
			for _, row := range objects(a.data["models"]) {
				r := ui.Column(c).Padding(9, 7).Gap(7).Focusable().Label(str(row["name"]))
				r.Children(func() {
					ui.Row(c).Children(func() {
						ui.Text(c, str(row["name"])).Grow(1)
						ui.Text(c, compact(row["total"])).FontSize(13).FontWeight(600)
					})
					animatedProgress(c, str(row["name"]), number(row["total"])/max(1, number(s["total"]))).Height(5)
					ui.Text(c, a.rowCost(row)).FontSize(10).TextColor(c.Theme().TextMuted)
				})
				if r.Clicked() {
					a.change("model", str(row["name"]))
					a.navigate(2)
				}
			}
		})
		surface(c, 1, 0, func() {
			panelHeading(c, a.tr("账户额度"), a.tr("在线查询正常"))
			for _, q := range objects(a.data["quotas"]) {
				a.quotaCard(c, q)
			}
		})
	})
	a.performance(c)
}
func (a *App) rowCost(row Object) string {
	cost := any(row["cost"])
	if number(row["requests"]) > 0 && number(row["requests"]) == number(row["unpriced"]) {
		cost = nil
	}
	result := money(cost)
	if number(row["unpriced"]) > 0 {
		result += a.tr(" + 未定价")
	}
	return result
}
func (a *App) performance(c *ui.Context) {
	p, s := obj(a.data["performance"]), obj(a.data["sums"])
	panel(c, a.tr("运行指标"), "", func() {
		for _, metrics := range [][][2]string{{{"请求次数", full(s["requests"])}, {"任务成功率", ratio(p["successRate"])}, {"失败任务", full(p["failed"])}, {"平均首 Token 延迟", a.duration(p["ttft"])}}, {{"平均任务耗时", a.duration(p["avgDuration"])}, {"缓存节省估算", money(s["saved"])}, {"推理输出", compact(s["reasoning"])}, {"缓存写入", compact(s["cache_write"])}}} {
			ui.Row(c).Gap(15).Children(func() {
				for _, metric := range metrics {
					ui.Column(c).Grow(1).Basis(0).Gap(7).Padding(0, 0, 0, 14).BorderWidth(0, 0, 0, 1).BorderColor(c.Theme().Border).Children(func() {
						ui.Text(c, a.tr(metric[0])).FontSize(11).TextColor(c.Theme().TextMuted)
						ui.Text(c, metric[1]).FontSize(18).FontWeight(550)
					})
				}
			})
		}
	})
}
func (a *App) activity(c *ui.Context, compactView bool) {
	activity := obj(a.data["activity"])
	if len(activity) == 0 {
		return
	}
	year := int(number(activity["year"]))
	stats := obj(activity["stats"])
	days := map[string]Object{}
	maximum := 0.0
	metric := []string{"total", "requests", "cost"}[a.activityMetric]
	for _, day := range objects(activity["days"]) {
		days[str(day["date"])] = day
		maximum = max(maximum, number(day[metric]))
	}
	height := float32(305)
	if a.selectedDay != "" {
		height += 50
	}
	surface(c, 0, height, func() {
		ui.Row(c).Height(36).Gap(8).Children(func() {
			ui.Icon(c, icons["calendar"]).Size(16, 16).TextColor(c.Theme().Accent)
			ui.Text(c, a.tr("活动")).FontSize(13).FontWeight(650)
			if iconButton(c, "left", a.tr("上一年")).Size(25, 25).Disabled(year <= int(number(activity["firstYear"]))).Clicked() {
				a.change("activityYear", year-1)
			}
			ui.Text(c, fmt.Sprint(year)).FontSize(12).TextColor(c.Theme().TextMuted)
			if iconButton(c, "right", a.tr("下一年")).Size(25, 25).Disabled(year >= time.Now().Year()).Clicked() {
				a.change("activityYear", year+1)
			}
			ui.Spacer(c)
			segment(c, &a.activityMetric, []string{"Token", a.tr("用量记录"), "USD"}, nil, 185, 35)
			if compactView {
				if button(c, a.tr("查看活动")+" ↗").Border(0, ui.Transparent).Background(ui.Transparent).Clicked() {
					a.navigate(1)
				}
			}
		})
		ui.Row(c).Height(20).Gap(22).Children(func() {
			for i, key := range []string{"activeDays", "currentStreak", "longestStreak"} {
				labels := []string{"活跃天数", "当前连续", "最长连续"}
				ui.Row(c).Gap(5).Children(func() {
					if i == 1 {
						icon(c, "flame", 14)
					}
					ui.Text(c, full(stats[key])).FontSize(14).FontWeight(600)
					ui.Text(c, a.tr(labels[i])).FontSize(11).TextColor(c.Theme().TextMuted)
				})
			}
			ui.Spacer(c)
			ui.Text(c, compact(stats["total"])+" tokens · "+full(stats["requests"])+" "+a.tr("用量记录")).FontSize(11)
		})
		start := time.Date(year, 1, 1, 12, 0, 0, 0, time.Local)
		weekday := (int(start.Weekday()) + 6) % 7
		start = start.AddDate(0, 0, -weekday)
		end := time.Date(year+1, 1, 1, 12, 0, 0, 0, time.Local)
		weeks := (int(end.Sub(start).Hours()/24) + 6) / 7
		monthLabels := map[int]string{}
		for m := 1; m <= 12; m++ {
			d := time.Date(year, time.Month(m), 1, 12, 0, 0, 0, time.Local)
			monthLabels[int(d.Sub(start).Hours()/24)/7] = fmt.Sprintf("%d月", m)
		}
		ui.Column(c).Gap(0).Children(func() {
			ui.Row(c).Height(23).Gap(4).Padding(0, 0, 0, 28).Children(func() {
				for week := 0; week < weeks; week++ {
					ui.Text(c, monthLabels[week]).Grow(1).Basis(0).NoWrap().FontSize(10).TextColor(c.Theme().TextMuted)
				}
			})
			var focus *ui.Element
			cells := map[string]*ui.Element{}
			target := ""
			ui.Row(c).Height(122).Gap(8).Children(func() {
				ui.Column(c).Width(20).Gap(4).Children(func() {
					for day := 0; day < 7; day++ {
						label := ""
						if day%2 == 0 {
							label = []string{"一", "三", "五", "日"}[day/2]
						}
						ui.Text(c, a.tr(label)).Height(14).FontSize(9).TextColor(c.Theme().TextMuted)
					}
				})
				grid := ui.Row(c).Key(fmt.Sprintf("calendar-%d-%s", year, metric)).Grow(1).Gap(4).Label(a.tr("活动热力图"))
				// Register pointer tracking once for the grid; paint-only frames reuse its layout.
				grid.PointerPosition()
				grid.Hovered()
				started := ui.Local(grid, "reveal", func() time.Time { return c.Now() })
				light := ui.Local(grid, "light", func() widgetMotion { return widgetMotion{} })
				grid.Children(func() {
					for week := 0; week < weeks; week++ {
						ui.Column(c).Grow(1).Basis(0).Gap(4).Children(func() {
							for day := 0; day < 7; day++ {
								d := start.AddDate(0, 0, week*7+day)
								date := d.Format("2006-01-02")
								if d.Year() != year {
									ui.Box(c).FillWidth().Height(14)
									continue
								}
								row := days[date]
								if row == nil {
									row = Object{"total": 0, "requests": 0, "cost": 0}
								}
								v := number(row[metric])
								level := 0
								if v > 0 && maximum > 0 {
									level = min(4, max(1, int(math.Ceil(math.Sqrt(v/maximum)*4))))
								}
								color := c.Theme().SurfaceHover.Mix(c.Theme().Surface, .25)
								border := c.Theme().Border.Alpha(.8)
								if level > 0 {
									weight := []float32{0, .23, .43, .68, 1}[level]
									color = c.Theme().Surface.Mix(c.Theme().Accent, weight)
									border = c.Theme().Border.Mix(c.Theme().Accent, []float32{0, .18, .4, .65, 1}[level])
								}
								label := date + " · " + full(row[metric])
								if metric == "cost" {
									label = date + " · " + a.rowCost(row)
								}
								if metric == "total" {
									label += " tokens"
								}
								cell := ui.Box(c).Key(date).FillWidth().Height(14).Radius(3).Label(label).Tooltip(label).Disabled(date > str(activity["today"]))
								cell.Hovered()
								if date <= str(activity["today"]) {
									cell.Focusable()
								} else {
									cell.Opacity(.25).Background(ui.Transparent)
								}
								cells[date] = cell
								cell.Draw(func(p *ui.Painter, r ui.Rect) {
									calendarCell(c, p, grid, cell, r, color, border, *started, week, light, date <= str(activity["today"]), date == str(activity["today"]), date == a.selectedDay)
								})
								if cell.Clicked() {
									a.selectedDay = date
								}
								if cell.Focused() {
									focus = cell
									for _, key := range []ui.Key{ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown} {
										if cell.Shortcut(0, key) {
											offset := map[ui.Key]int{ui.KeyLeft: -7, ui.KeyRight: 7, ui.KeyUp: -1, ui.KeyDown: 1}[key]
											target = d.AddDate(0, 0, offset).Format("2006-01-02")
										}
									}
								}
							}
						})
					}
				})
			})
			if focus != nil && target != "" && target <= str(activity["today"]) {
				if cell := cells[target]; cell != nil {
					cell.Focus()
				}
			}
		})
		ui.Row(c).Height(18).Gap(4).Children(func() {
			ui.Text(c, a.tr("按本地日期统计 · 点击日期查看记录")).FontSize(10).TextColor(c.Theme().TextMuted).Grow(1)
			ui.Text(c, a.tr("少")).FontSize(10)
			for _, weight := range []float32{0, .23, .43, .68, 1} {
				ui.Box(c).Size(10, 10).Radius(3).Background(c.Theme().Surface.Mix(c.Theme().Accent, weight))
			}
			ui.Text(c, a.tr("多")).FontSize(10)
		})
		if a.selectedDay != "" {
			row := days[a.selectedDay]
			if row == nil {
				row = Object{"total": 0, "requests": 0, "cost": 0}
			}
			ui.Row(c).Key("day-" + a.selectedDay).Transition(enterMotion(0, 5)).Gap(14).Children(func() {
				ui.Text(c, a.selectedDay).Bold()
				ui.Text(c, compact(row["total"])+" tokens").Grow(1)
				ui.Text(c, full(row["requests"])+" "+a.tr("用量记录")+" · "+full(row["sessions"])+" "+a.tr("个任务")).FontSize(11)
				ui.Text(c, a.rowCost(row))
				if button(c, a.tr("查看当天记录")).Clicked() {
					a.openDay(a.selectedDay)
				}
				if iconButton(c, "close", a.tr("收起日期详情")).Clicked() {
					a.selectedDay = ""
				}
			})
		}
	}).Padding(18, 20).Gap(14)
	if !compactView {
		a.activityDetails(c, activity)
	}
}
func (a *App) activityDetails(c *ui.Context, activity Object) {
	stats := obj(activity["stats"])
	textScale := max(float32(1), c.Preferences().TextScale)
	ui.Row(c).Gap(18).Wrap().AlignItems(ui.Stretch).Children(func() {
		surface(c, 1, 0, func() {
			panelHeading(c, a.tr("活跃时段"), a.tr("本地时间 · 年度汇总"))
			a.activityHours(c, activity)
			ui.Row(c).Padding(13, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
				ui.Text(c, a.tr("高峰日期")+"  "+str(obj(stats["peak"])["date"])).FontSize(11).Grow(1)
				ui.Text(c, compact(obj(stats["peak"])["total"])+" tokens").FontSize(11).TextColor(c.Theme().TextMuted)
			})
		}).MinHeight(374).MinWidth(320 * textScale)
		surface(c, 1, 0, func() {
			panelHeading(c, a.tr("最近活动日"), a.tr("{0} 个任务", stats["sessions"]))
			list := objects(activity["days"])
			ui.Column(c).Key("recent-days").Label(a.tr("最近活动日列表")).Shrink(0).Gap(4).Children(func() {
				for i := len(list) - 1; i >= max(0, len(list)-8); i-- {
					day := list[i]
					row := ui.Row(c).Key(str(day["date"])).Shrink(0).MinHeight(32).Gap(10).Padding(6, 8).Radius(7).Focusable().Label(str(day["date"]))
					activityRowHover(c, row)
					row.Children(func() {
						ui.Text(c, str(day["date"])).Width(84 * textScale).FontSize(11).NoWrap()
						animatedProgress(c, "day-fill", number(day["total"])/max(1, number(obj(stats["peak"])["total"]))).Height(4).Grow(1).Opacity(.5)
						ui.Text(c, compact(day["total"])).Width(55 * textScale).FontSize(11).NoWrap()
						icon(c, "arrow", 14)
					})
					if row.Clicked() {
						a.openDay(str(day["date"]))
					}
				}
			})
			if len(list) == 0 {
				ui.Text(c, a.tr("这个时间范围内还没有记录")).TextColor(c.Theme().TextMuted)
			}
		}).Label(a.tr("最近活动日卡片")).MinHeight(374).MinWidth(320 * textScale)
	})
}
func (a *App) quotaCard(c *ui.Context, q Object) {
	remaining := 100 - number(q["used"])
	window := a.tr("{0} 小时窗口", number(q["minutes"])/60)
	if number(q["minutes"]) >= 1440 {
		window = a.tr("{0} 天窗口", number(q["minutes"])/1440)
	}
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, window).FontSize(12).Grow(1)
			ui.Text(c, fmt.Sprintf("%.0f", remaining)).FontSize(24).FontWeight(550)
			ui.Text(c, a.tr("% 剩余")).FontSize(10).TextColor(c.Theme().TextMuted)
		})
		animatedProgress(c, "quota-fill-"+str(q["account"])+":"+str(q["bucket"])+":"+str(q["slot"]), bounded(remaining/100, 0, 1)).Height(5).Label(a.tr("剩余额度"))
		reset := any(nil)
		if q["resets"] != nil {
			reset = number(q["resets"]) * 1000
		}
		ui.Row(c).Children(func() {
			text := a.tr("重置 ") + shortStamp(reset)
			if valid(reset) && number(reset) < float64(time.Now().UnixMilli()) {
				text = a.tr("已过重置时间，等待新快照")
			}
			ui.Text(c, text).FontSize(10).TextColor(c.Theme().TextMuted).Grow(1)
			ui.Text(c, a.system(str(q["source"]))+" · "+shortStamp(q["ts"])).FontSize(10).TextColor(c.Theme().TextMuted)
		})
	})
}
func (a *App) quota(c *ui.Context) {
	quotas := objects(a.data["quotas"])
	if reason := str(obj(a.data["quotaStatus"])["reason"]); reason != "" {
		ui.Text(c, a.system(reason)).TextColor(c.Theme().Warning)
	}
	ui.Row(c).Gap(18).Wrap().Children(func() {
		for _, q := range quotas {
			surface(c, 1, 168, func() { panelHeading(c, str(q["bucket"]), str(q["plan"])); a.quotaCard(c, q) }).MinWidth(280)
		}
	})
	surface(c, 0, 375, func() {
		panelHeading(c, a.tr("剩余额度历史"), "")
		if len(quotas) == 0 {
			return
		}
		a.quotaIndex = max(0, min(a.quotaIndex, len(quotas)-1))
		labels := make([]string, len(quotas))
		for i, q := range quotas {
			labels[i] = a.tr("{0} 小时窗口", number(q["minutes"])/60)
			if number(q["minutes"]) >= 1440 {
				labels[i] = a.tr("{0} 天窗口", number(q["minutes"])/1440)
			}
		}
		segment(c, &a.quotaIndex, labels, nil, float32(len(labels))*85, 35)
		selected := quotas[a.quotaIndex]
		key := "quota-" + strings.Join([]string{str(selected["account"]), str(selected["bucket"]), str(selected["slot"])}, ":")
		points := a.chartPoints(key, func() []chartPoint {
			var points []chartPoint
			for _, q := range objects(a.data["quotaHistory"]) {
				if str(q["bucket"]) != str(selected["bucket"]) || str(q["slot"]) != str(selected["slot"]) || str(q["account"]) != str(selected["account"]) {
					continue
				}
				t, _ := time.Parse(time.RFC3339, str(q["ts"]))
				points = append(points, chartPoint{Time: float64(t.UnixMilli()), Value: 100 - number(q["used"]), Name: shortStamp(q["ts"]), Reset: number(q["resets"]), Gap: truth(q["gapBefore"])})
			}
			return points
		})
		a.chart(c, key, points, true, true, "")
		ui.Text(c, a.tr("额度属于当前登录账号，可能包含其他设备的用量；Token 统计覆盖本机 Codex 桌面端、CLI 与支持的 pi 记录。")).Padding(13, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).FontSize(11).TextColor(c.Theme().TextMuted)
	})
}
