package nativeapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) quota(c *ui.Context) {
	p := a.pal
	quotas := objects(a.data["quotas"])
	if reason := str(obj(a.data["quotaStatus"])["reason"]); reason != "" {
		ui.Row(a.kc(c)).Padding(10, 14).Gap(10).Radius(12).AlignItems(ui.Center).Background(p.orange.Alpha(.12)).Children(func() {
			a.glyph(c, "warning-circle-fill", 16, p.orange)
			ui.Text(c, a.system(reason)).FontSize(12).TextColor(p.label).Grow(1).Basis(0)
		})
	}
	ui.Row(a.kc(c)).Gap(16).Wrap().AlignItems(ui.Stretch).Children(func() {
		for i, q := range quotas {
			remaining := bounded(1-number(q["used"])/100, 0, 1)
			color := a.quotaColor(i)
			card := a.section(c.Key(i), func() {
				ui.Row(c).Gap(18).AlignItems(ui.Center).Children(func() {
					a.ring(c, fmt.Sprint("quota-", i), []ringValue{{remaining, color}}, 96, 12)
					ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, a.quotaWindow(q)).FontSize(13).FontWeight(600).SingleLine()
							if plan := str(q["plan"]); plan != "" {
								a.pill(c, plan, color)
							}
						})
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
			if i == a.quotaIndex && len(quotas) > 1 {
				card.Border(1.5, color.Alpha(.6))
			}
			if card.Clicked() {
				a.quotaIndex = i
			}
		}
		if len(quotas) == 0 {
			a.section(c, func() { a.empty(c, a.tr("尚未获得额度快照")) }).Grow(1)
		}
	})
	if len(quotas) == 0 {
		return
	}
	ui.Column(a.kc(c)).Children(func() {
		a.section(c, func() {
			a.quotaIndex = max(0, min(a.quotaIndex, len(quotas)-1))
			labels := make([]string, len(quotas))
			for i, q := range quotas {
				labels[i] = a.quotaWindow(q)
			}
			a.header(c, a.tr("剩余额度历史"), "", func() {
				if len(labels) > 1 {
					a.segmented(c, &a.quotaIndex, labels, float32(len(labels))*96).Label(a.tr("额度窗口"))
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
			a.chartColored(c, key, points, true, true, "", a.quotaColor(a.quotaIndex))
			a.separator(c, 0)
			ui.Row(c).Gap(8).Children(func() {
				a.glyph(c, "info-fill", 14, p.tertiary)
				ui.Text(c, a.tr("额度属于当前登录账号，可能包含其他设备的用量；Token 统计覆盖本机 Codex 桌面端、CLI 与支持的 pi 记录。")).FontSize(11).LineHeight(1.4).TextColor(p.secondary).Grow(1).Basis(0)
			})
		})
	})
}
