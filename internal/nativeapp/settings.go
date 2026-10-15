package nativeapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

func (a *App) setting(key string, value any) {
	a.perform(func() (any, error) {
		if key == "autoStart" && !a.options.Offline {
			if err := mygo.App.SetOpenAtLogin(truth(value)); err != nil {
				return nil, err
			}
		}
		return a.call("settings", Object{key: value})
	}, "")
}

func (a *App) call(method string, args any) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.client.Call(ctx, method, args)
}

// toggle is a settings row ending in a switch.
func (a *App) toggle(c *ui.Context, settings Object, key, iconName string, color ui.Color, title, description string) {
	a.row(c, iconName, color, a.tr(title), a.tr(description), func() {
		value := truth(settings[key])
		if ui.Switch(c, &value).Label(a.tr(title)).Disabled(a.busy).Changed() {
			a.setting(key, value)
		}
	})
}

func (a *App) settings(c *ui.Context) {
	settings := obj(a.data["settings"])
	ui.Row(c.Key("settings-tabs")).Justify(ui.Center).Children(func() {
		a.segmented(c, &a.settingsSection, []string{a.tr("通用"), a.tr("账号"), a.tr("模型价格"), a.tr("数据与存储")}, 440).Label(a.tr("设置分类"))
	})
	ui.Column(a.kc(c)).FillWidth().MaxWidth(760).Margin(0, ui.Auto).Gap(22).Children(func() {
		switch a.settingsSection {
		case 0:
			a.generalSettings(c, settings)
		case 1:
			a.accountSettings(c)
		case 2:
			a.prices(c)
		case 3:
			a.dataSettings(c, settings)
		}
	})
}

func (a *App) generalSettings(c *ui.Context, settings Object) {
	p := a.pal
	a.group(c, a.tr("外观"), "", func() {
		a.row(c, "globe-fill", p.blue, a.tr("语言"), a.tr("界面语言立即生效，并在下次启动时保留"), func() {
			if id, changed := a.popup(c, "语言", str(settings["language"]), []string{"zh-CN", "en"}, []string{"简体中文", "English"}, 130); changed {
				a.setting("language", id)
			}
		})
		a.row(c, "circle-half-fill", p.indigo, a.tr("主题"), "", func() {
			ids := []string{"system", "light", "dark"}
			index := 0
			for i, id := range ids {
				if str(settings["theme"]) == id {
					index = i
				}
			}
			previous := index
			a.segmented(c, &index, []string{a.tr("系统"), a.tr("浅色"), a.tr("深色")}, 210).Label(a.tr("主题"))
			if previous != index {
				a.setting("theme", ids[index])
			}
		})
	})
	a.group(c, a.tr("启动与提醒"), "", func() {
		a.toggle(c, settings, "autoStart", "power-fill", p.green, "开机启动", "登录 Windows 后静默进入托盘")
		a.toggle(c, settings, "muted", "bell-simple-fill", p.red, "静音额度提醒", "剩余 20% 和 10% 时提醒")
		a.row(c, "timer-fill", p.orange, a.tr("额度查询间隔"), "", func() {
			if id, changed := a.popup(c, "额度查询间隔", fmt.Sprint(settings["quotaInterval"]), []string{"60", "120", "300"}, []string{"1 " + a.tr("分钟"), "2 " + a.tr("分钟"), "5 " + a.tr("分钟")}, 110); changed {
				value, _ := strconv.Atoi(id)
				a.setting("quotaInterval", value)
			}
		})
	})
	a.group(c, a.tr("软件更新"), a.tr("手动检查 GitHub 正式版本，下载后验证签名。更新保留监测数据与设置。"), func() {
		a.row(c, "arrow-circle-down-fill", p.blue, a.tr("当前版本")+" "+appVersion, "", func() {
			if a.button(c, a.tr("检查更新"), "").Clicked() {
				updater.CheckForUpdates()
			}
		})
	})
	a.telemetrySettings(c, settings)
}

func (a *App) telemetrySettings(c *ui.Context, settings Object) {
	p := a.pal
	status := obj(a.data["telemetry"])
	label, color := "采集未开启", p.gray
	if truth(status["listening"]) {
		label, color = "接收就绪，等待新调用", p.blue
		if number(status["ttftSamples"]) > 0 {
			label, color = "已收到首字数据", p.green
		}
	}
	a.group(c, a.tr("本机首字采集"), a.tr("历史日志没有首字时间时无法补算；客户端未导出的内部重试无法观测。"), func() {
		a.toggle(c, settings, "telemetryEnabled", "broadcast-fill", p.teal, "记录逐次首字时间与失败重试", "保留 Desktop、CLI 和 Pi 用量采集；额外接收本机请求诊断，不保存对话、工具内容或登录凭据。")
		if a.telemetryPortDraft == "" {
			a.telemetryPortDraft = str(settings["telemetryPort"])
		}
		a.row(c, "plug-fill", p.purple, a.tr("本机端口"), "127.0.0.1:"+str(settings["telemetryPort"]), func() {
			ui.TextInput(c, &a.telemetryPortDraft).Width(80).Height(28).FontSize(13).Radius(7).Label(a.tr("本机端口")).FontFeatures("tnum")
			if a.button(c, a.tr("应用端口"), "").Disabled(a.busy || a.telemetryPortDraft == str(settings["telemetryPort"])).Clicked() {
				port, err := strconv.Atoi(a.telemetryPortDraft)
				if err != nil || port < 1024 || port > 65535 {
					a.errorText = a.tr("采集端口须为 1024–65535 的整数")
				} else {
					a.setting("telemetryPort", port)
				}
			}
		})
		detail := ""
		if status["receivedAt"] != nil {
			detail = a.tr("最近接收") + " " + shortStamp(status["receivedAt"])
		}
		if str(status["error"]) != "" {
			detail, color = a.tr(str(status["error"])), p.red
		}
		a.row(c, "", ui.Color{}, a.tr("状态"), detail, func() { a.pill(c, a.tr(label), color) })
		a.row(c, "", ui.Color{}, a.tr("自动配置"), a.tr("自动配置 Codex Desktop / CLI 和 Pi，无需编辑文件或复制加载命令。"), func() {
			if a.button(c, a.tr("重新检查自动配置"), "").Disabled(a.busy).Clicked() {
				a.perform(func() (any, error) { return a.call("telemetrySetup", Object{}) }, "自动配置已检查")
			}
		})
		for _, client := range []string{"codex", "pi"} {
			binding := obj(status[client])
			name := "Codex Desktop / CLI"
			if client == "pi" {
				name = "Pi"
			}
			switch {
			case str(binding["error"]) != "":
				a.row(c, "", ui.Color{}, name, a.tr(str(binding["error"])), func() { a.pill(c, a.tr("错误"), p.red) })
			case str(binding["state"]) == "ready":
				a.row(c, "", ui.Color{}, name, "", func() { a.pill(c, a.tr("已自动配置"), p.green) })
			}
		}
		if truth(obj(status["codex"])["restartRequired"]) || truth(obj(status["pi"])["restartRequired"]) {
			a.row(c, "", ui.Color{}, a.tr("需要重启"), a.tr("配置已更新：正在运行的 Codex Desktop / CLI 需重启一次；Pi 可执行 /reload。之后的新调用自动采集。"), nil)
		}
	})
}

func (a *App) accountSettings(c *ui.Context) {
	p := a.pal
	a.group(c, a.tr("账号管理"), a.tr("自动识别本机登录账号；仅保存账号标识摘要和显示名称，不保存登录凭据。可添加历史账号并修改显示名称。"), func() {
		for _, account := range objects(a.data["accounts"]) {
			id := str(account["id"])
			short := id[:min(6, len(id))]
			current := id == str(a.data["currentAccount"])
			description := short
			if current {
				description += " · " + a.tr("当前登录")
			}
			color := p.gray
			if current {
				color = p.blue
			}
			a.row(c.Key(id), "user-circle-fill", color, str(account["label"]), description, func() {
				value, exists := a.accountDrafts[id]
				if !exists {
					value = str(account["label"])
				}
				if ui.TextInput(c, &value).Width(200).Height(28).FontSize(13).Radius(7).Label(a.tr("账号名称") + " " + short).Changed() {
					a.accountDrafts[id] = value
				}
				if a.button(c, a.tr("保存"), "").Disabled(a.busy || value == str(account["label"]) || strings.TrimSpace(value) == "").Clicked() {
					a.action("account", Object{"id": id, "label": value}, "账号已保存")
				}
			})
		}
		a.row(c, "plus", p.green, a.tr("添加账号"), "", func() {
			ui.TextInput(c, &a.newAccount).Width(200).Height(28).FontSize(13).Radius(7).Label(a.tr("账号名称")).Placeholder(a.tr("账号名称"))
			if a.primary(c, a.tr("添加")).Disabled(a.busy || strings.TrimSpace(a.newAccount) == "").Clicked() {
				label := a.newAccount
				a.actionWith("account", Object{"label": label}, "账号已保存", func() { a.newAccount = "" })
			}
		})
	})
}

func (a *App) dataSettings(c *ui.Context, settings Object) {
	p := a.pal
	paths := obj(a.data["paths"])
	a.group(c, a.tr("数据来源"), a.tr("自动统计 pi 中官方登录的 Codex 用量，不含 API Key 和其他供应商。历史账号无法确认时保留为未归属，不改变额度查询来源。"), func() {
		for _, entry := range []struct{ label, key, icon string }{{"Codex 数据目录", "home", "folder-open-fill"}, {"pi 会话目录", "piSessions", "folder-open-fill"}, {"监测数据库目录", "data", "database-fill"}} {
			value := str(paths[entry.key])
			if value == "" {
				value = "—"
			}
			a.row(c, entry.icon, p.blue, a.tr(entry.label), "", func() {
				ui.Text(c, value).FontSize(12).TextColor(p.secondary).Selectable().SingleLine().MaxWidth(360).Tooltip(value)
			})
		}
		a.row(c, "", ui.Color{}, "", "", func() {
			ui.Spacer(c)
			if a.button(c, a.tr("打开数据目录"), "folder-open-fill").Clicked() {
				go func() { _ = mygo.Shell.OpenPath(a.options.Data) }()
			}
		})
	})
	program := str(settings["codexExecutable"])
	if program == "" {
		program = a.tr("自动发现本机 Codex App Server")
	}
	a.group(c, a.tr("额度查询程序"), "", func() {
		a.row(c, "terminal-window-fill", p.gray, program, "", nil).Selectable()
		a.row(c, "", ui.Color{}, "", "", func() {
			ui.Spacer(c)
			if a.button(c, a.tr("恢复自动发现"), "").Disabled(a.busy || str(settings["codexExecutable"]) == "").Clicked() {
				a.setting("codexExecutable", "")
			}
			if a.button(c, a.tr("选择 codex.exe"), "").Disabled(a.busy).Clicked() {
				a.perform(func() (any, error) {
					paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: a.tr("选择 codex.exe"), Filters: []mygo.FileFilter{{Name: "Codex executable", Extensions: []string{"exe"}}}})
					if err != nil || len(paths) == 0 {
						return nil, err
					}
					return a.call("settings", Object{"codexExecutable": paths[0]})
				}, "查询程序已更新")
			}
		})
	})
	a.group(c, a.tr("数据与存储"), a.tr("所有数据留在本机，无遥测、无跨设备同步；清空历史仅删除监测数据，保留价格与设置。"), func() {
		a.row(c, "trash-fill", p.red, a.tr("清空监测历史"), a.tr("Codex 与 pi 原始文件不会被修改。"), func() {
			clear := a.button(c, a.tr("清空监测历史"), "").Disabled(a.busy).TextColor(p.red)
			if clear.Clicked() {
				a.perform(func() (any, error) {
					r, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning, Title: a.tr("清空监测历史"), Message: a.tr("删除本应用已采集的用量和额度历史？"), Detail: a.tr("Codex 与 pi 原始文件不会被修改。仅继续采集此刻之后的数据；已有历史不会自动重新导入。价格和设置保留。"), Buttons: []string{a.tr("取消"), a.tr("清空历史")}, DefaultButton: 0, CancelButton: 0})
					if err != nil || r.Button != 1 {
						return nil, err
					}
					return a.call("clear", nil)
				}, "监测历史已清空")
			}
		})
		if recovery := a.data["recovery"]; recovery != nil {
			b, _ := json.Marshal(recovery)
			a.row(c, "warning-circle-fill", p.orange, a.tr("数据库恢复记录"), string(b), nil).Selectable()
		}
	})
}

func (a *App) prices(c *ui.Context) {
	p := a.pal
	labels := map[string]string{"model": "模型", "effective": "生效时间", "input": "输入", "cached": "缓存输入", "output": "输出", "cache_write": "缓存写入"}
	title := "新增价格版本"
	if a.priceID != nil {
		title = "更新价格版本"
	}
	a.group(c, a.tr(title), a.tr("按 API 标准价估算，不代表订阅账单。")+" "+a.tr("USD / 百万 tokens")+" · YYYY-MM-DDTHH:mm "+a.tr("本地时间"), func() {
		ui.Column(c).Padding(14, 16).Gap(12).Children(func() {
			for _, keys := range [][]string{{"model", "effective"}, {"input", "cached", "output", "cache_write"}} {
				ui.Row(c).Gap(12).Children(func() {
					for _, key := range keys {
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(5).Children(func() {
							ui.Text(c, a.tr(labels[key])).FontSize(11).FontWeight(500).TextColor(p.secondary)
							value := a.priceForm[key]
							if ui.TextInput(c, &value).Label(a.tr(labels[key])).Height(30).FontSize(13).Radius(7).Changed() {
								a.priceForm[key] = value
							}
						})
					}
				})
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.button(c, a.tr("取消编辑"), "").Clicked() {
					a.resetPrice()
				}
				if a.primary(c, a.tr(title)).Disabled(a.busy).Clicked() {
					a.savePrice()
				}
			})
		})
	})
	a.group(c, a.tr("模型价格"), "", func() {
		prices := objects(a.data["prices"])
		if len(prices) == 0 {
			a.empty(c, a.tr("没有价格版本"))
		}
		for _, price := range prices {
			manual := str(price["source"]) == "手动设置" && !truth(price["retired"])
			description := stamp(price["effective"]) + " · " + a.system(str(price["source"])) + "\n" + fmt.Sprintf("%s %s · %s %s · %s %s · %s %s", a.tr("输入"), money(price["input"]), a.tr("缓存输入"), money(price["cached"]), a.tr("输出"), money(price["output"]), a.tr("缓存写入"), money(price["cache_write"]))
			color := p.orange
			if manual {
				color = p.purple
			}
			a.row(c.Key(str(price["id"])), "currency-dollar-fill", color, str(price["model"])+" · v"+str(price["id"]), description, func() {
				if manual {
					if a.button(c, a.tr("编辑"), "").Disabled(a.busy).Clicked() {
						a.editPrice(price, true)
					}
					if a.button(c, a.tr("删除"), "").Disabled(a.busy).TextColor(p.red).Clicked() {
						a.deletePrice(price)
					}
				}
				if a.button(c, a.tr("用作模板"), "").Clicked() {
					a.editPrice(price, false)
				}
			})
		}
	})
}

func (a *App) resetPrice() {
	a.priceID = nil
	a.priceForm = map[string]string{"model": "", "input": "", "cached": "", "output": "", "cache_write": "0", "effective": "1970-01-01T00:00"}
}
func (a *App) editPrice(price Object, edit bool) {
	for _, key := range []string{"model", "input", "cached", "output", "cache_write"} {
		a.priceForm[key] = str(price[key])
	}
	a.priceForm["effective"] = dateValue(str(price["effective"])).Local().Format("2006-01-02T15:04")
	a.priceID = nil
	if edit {
		a.priceID = price["id"]
	}
}
func (a *App) savePrice() {
	p := Object{"model": strings.TrimSpace(a.priceForm["model"])}
	for _, key := range []string{"input", "cached", "output", "cache_write"} {
		value, err := strconv.ParseFloat(strings.TrimSpace(a.priceForm[key]), 64)
		if err != nil || value < 0 {
			a.errorText = a.tr("价格须为有限的非负数")
			return
		}
		p[key] = value
	}
	effective := dateValue(a.priceForm["effective"])
	if effective.IsZero() {
		a.errorText = a.tr("生效时间无效")
		return
	}
	p["effective"] = effective.UTC().Format(time.RFC3339)
	if a.priceID != nil {
		p["id"] = a.priceID
	}
	a.actionWith("price", p, "价格版本已保存", a.resetPrice)
}
func (a *App) deletePrice(price Object) {
	model, id := str(price["model"]), price["id"]
	a.perform(func() (any, error) {
		r, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning, Title: a.tr("删除价格版本"), Message: a.tr("确认删除 {0} 的价格版本 v{1}？", model, id), Detail: a.tr("删除后，相关历史记录可能使用其他价格版本或显示为未定价。"), Buttons: []string{a.tr("取消"), a.tr("删除")}, DefaultButton: 0, CancelButton: 0})
		if err != nil || r.Button != 1 {
			return nil, err
		}
		return a.call("deletePrice", id)
	}, "价格版本已删除")
}
