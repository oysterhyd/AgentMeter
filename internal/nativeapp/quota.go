package nativeapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) quota(c *ui.Context) {
	p := a.pal
	groups := quotaGroups(objects(a.data["quotas"]))
	// One flat order for the history selector: Codex windows, then Claude's.
	quotas := []Object{}
	for _, group := range groups {
		quotas = append(quotas, group...)
	}
	if reason := str(obj(a.data["quotaStatus"])["reason"]); reason != "" && str(a.filter["agent"]) != agentClaude {
		ui.Row(a.kc(c)).Padding(10, 14).Gap(10).Radius(12).AlignItems(ui.Center).Background(p.orange.Alpha(.12)).Children(func() {
			a.glyph(c, "warning-circle-fill", 16, p.orange)
			ui.Text(c, "Codex · "+a.system(reason)).FontSize(12).TextColor(p.label).Grow(1).Basis(0)
		})
	}
	if len(quotas) == 0 {
		ui.Column(a.kc(c)).Children(func() {
			a.section(c, func() { a.empty(c, a.tr("尚未获得额度快照")) })
		})
		return
	}
	index := 0
	for _, group := range groups {
		agent := quotaAgent(group[0])
		ui.Column(a.kc(c)).Gap(10).Children(func() {
			a.quotaGroupHeader(c, agent, group[0])
			ui.Row(c).Gap(16).Wrap().AlignItems(ui.Stretch).Children(func() {
				for i, q := range group {
					a.quotaCard(c, q, index, i)
					index++
				}
			})
		})
	}
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			a.quotaIndex = max(0, min(a.quotaIndex, len(quotas)-1))
			labels := make([]string, len(quotas))
			for i, q := range quotas {
				labels[i] = a.quotaWindow(q)
				if len(groups) > 1 {
					labels[i] = map[string]string{agentCodex: "Codex", agentClaude: "Claude"}[quotaAgent(q)] + " · " + labels[i]
				}
			}
			a.header(c, a.tr("剩余额度历史"), "", func() {
				if len(labels) > 1 {
					width := float32(96)
					if len(groups) > 1 {
						width = 132
					}
					a.segmented(c, &a.quotaIndex, labels, float32(len(labels))*width).Label(a.tr("额度窗口"))
				}
			})
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
			color := a.quotaColor(a.quotaIndex)
			if quotaAgent(selected) == agentClaude {
				color = p.agent(agentClaude)
			}
			a.chartColored(c, key, points, true, true, "", color)
			a.separator(c, 0)
			ui.Row(c).Gap(8).Children(func() {
				a.glyph(c, "info-fill", 14, p.tertiary)
				ui.Text(c, a.tr("额度属于各自当前登录的账号，可能包含其他设备的用量。Codex 额度来自 App Server 在线查询；Claude Code 额度读取其自身缓存的用量快照，随 Claude Code 使用而更新，不读取登录凭据。")).FontSize(11).LineHeight(1.4).TextColor(p.secondary).Grow(1).Basis(0)
			})
		})
	})
}

// quotaGroupHeader names an agent's quota section: its mark, account and
// where the numbers came from.
func (a *App) quotaGroupHeader(c *ui.Context, agent string, sample Object) {
	p := a.pal
	ui.Row(c).Padding(4, 4, 0, 4).Gap(10).AlignItems(ui.Center).Children(func() {
		a.agentMark(c, agent, 26)
		ui.Column(c).Gap(0).Grow(1).Basis(0).MinWidth(0).Children(func() {
			ui.Text(c, agentName(agent)).FontSize(15).FontWeight(650).Font(fontDisplay).SingleLine()
			ui.Text(c, a.accountLabel(str(sample["account"]))).FontSize(11).TextColor(p.secondary).SingleLine()
		})
		if plan := str(sample["plan"]); plan != "" {
			a.pill(c, plan, p.agent(agent))
		}
		status := obj(a.data["quotaStatus"])
		if agent == agentClaude {
			status = obj(a.data["claudeQuotaStatus"])
		}
		if at := status["attempted"]; at != nil {
			ui.Text(c, a.tr("采样于")+" "+shortStamp(at)).FontSize(11).TextColor(p.tertiary).FontFeatures("tnum").NoWrap()
		}
	})
}

func (a *App) quotaCard(c *ui.Context, q Object, index, slot int) {
	p := a.pal
	remaining := bounded(1-number(q["used"])/100, 0, 1)
	color := a.quotaColor(slot)
	card := a.section(c.Key(index), func() {
		ui.Row(c).Gap(18).AlignItems(ui.Center).Children(func() {
			a.ring(c, fmt.Sprint("quota-", index), []ringValue{{remaining, color}}, 96, 12)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, a.quotaTitle(q)).FontSize(13).FontWeight(600).SingleLine()
				ui.Row(c).Gap(3).AlignItems(ui.End).Children(func() {
					ui.Text(c, fmt.Sprintf("%.0f", remaining*100)).FontSize(34).FontWeight(650).Font(fontDisplay).FontFeatures("tnum").LetterSpacing(-.8).ContentTransition(ui.ContentTransition{Numeric: true})
					ui.Text(c, a.tr("% 剩余")).FontSize(12).TextColor(p.secondary).Padding(0, 0, 6, 0)
				})
				a.caption(c, a.resetText(q))
				ui.Text(c, str(q["bucket"])+" · "+a.system(str(q["source"]))+" · "+shortStamp(q["ts"])).FontSize(11).TextColor(p.tertiary).SingleLine()
			})
		})
	}).Grow(1).Basis(0).MinWidth(300)
	a.lift(card)
	if index == a.quotaIndex {
		card.Border(1.5, color.Alpha(.6))
	}
	if card.Clicked() {
		a.quotaIndex = index
	}
}

// quotaTitle names a window, including Claude's model-specific weekly limits.
func (a *App) quotaTitle(q Object) string {
	switch str(q["bucket"]) {
	case "claude-opus":
		return a.quotaWindow(q) + " · Opus"
	case "claude-sonnet":
		return a.quotaWindow(q) + " · Sonnet"
	}
	return a.quotaWindow(q)
}
