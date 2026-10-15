package nativeapp

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The design system follows Apple's Human Interface Guidelines: system colors,
// grouped surfaces, a restrained type ramp and spring motion.
const (
	fontText    = "Segoe UI Variable Text, Segoe UI, Microsoft YaHei UI"
	fontDisplay = "Segoe UI Variable Display, Segoe UI, Microsoft YaHei UI"
	fontMono    = "Cascadia Mono, Consolas, Microsoft YaHei UI"
)

type palette struct {
	dark                                               bool
	bg, sidebar, card, control, fill, hover, selection ui.Color
	separator, hairline, label, secondary, tertiary    ui.Color
	accent, blue, green, orange, red, purple, indigo   ui.Color
	teal, pink, yellow, gray                           ui.Color
	shadow                                             ui.Color
}

var lightPalette = palette{
	bg: ui.Hex("#f5f5f7"), sidebar: ui.Hex("#ebebef"), card: ui.Hex("#ffffff"), control: ui.Hex("#ffffff"),
	fill: ui.RGBA(118, 118, 128, .12), hover: ui.RGBA(0, 0, 0, .045), selection: ui.RGBA(0, 0, 0, .075),
	separator: ui.RGBA(60, 60, 67, .14), hairline: ui.RGBA(0, 0, 0, .06),
	label: ui.Hex("#1d1d1f"), secondary: ui.Hex("#6e6e73"), tertiary: ui.Hex("#a1a1a6"),
	accent: ui.Hex("#007aff"), blue: ui.Hex("#007aff"), green: ui.Hex("#34c759"), orange: ui.Hex("#ff9500"),
	red: ui.Hex("#ff3b30"), purple: ui.Hex("#af52de"), indigo: ui.Hex("#5856d6"), teal: ui.Hex("#30b0c7"),
	pink: ui.Hex("#ff2d55"), yellow: ui.Hex("#ffcc00"), gray: ui.Hex("#8e8e93"), shadow: ui.RGBA(0, 0, 0, .05),
}

var darkPalette = palette{
	dark: true,
	bg:   ui.Hex("#1c1c1e"), sidebar: ui.Hex("#252528"), card: ui.Hex("#2c2c2e"), control: ui.Hex("#3a3a3c"),
	fill: ui.RGBA(118, 118, 128, .24), hover: ui.RGBA(255, 255, 255, .06), selection: ui.RGBA(255, 255, 255, .1),
	separator: ui.RGBA(255, 255, 255, .1), hairline: ui.RGBA(255, 255, 255, .07),
	label: ui.Hex("#f5f5f7"), secondary: ui.Hex("#98989d"), tertiary: ui.Hex("#6c6c70"),
	accent: ui.Hex("#0a84ff"), blue: ui.Hex("#0a84ff"), green: ui.Hex("#30d158"), orange: ui.Hex("#ff9f0a"),
	red: ui.Hex("#ff453a"), purple: ui.Hex("#bf5af2"), indigo: ui.Hex("#5e5ce6"), teal: ui.Hex("#40c8e0"),
	pink: ui.Hex("#ff375f"), yellow: ui.Hex("#ffd60a"), gray: ui.Hex("#8e8e93"), shadow: ui.RGBA(0, 0, 0, .3),
}

// Series colors in Apple's chart order, for models and quota windows.
func (p *palette) series(i int) ui.Color {
	colors := [...]ui.Color{p.blue, p.purple, p.orange, p.green, p.teal, p.pink, p.indigo, p.yellow}
	return colors[i%len(colors)]
}

// Springs tuned to feel like UIKit/SwiftUI defaults.
var (
	springSnappy = ui.Animation{Spring: ui.SpringOptions{Duration: 380 * time.Millisecond, Bounce: .12}}
	springSmooth = ui.Animation{Spring: ui.SpringOptions{Duration: 520 * time.Millisecond}}
	springGentle = ui.Animation{Spring: ui.SpringOptions{Duration: 900 * time.Millisecond, Bounce: .05}}
)

func (a *App) skin(c *ui.Context) {
	a.enterIndex = 0
	// MyGO 0.5.2 exposes this preference on Theme, but not ui.Preferences.
	// ShowsMaterial still reports true when Windows disables transparency.
	if a.win != nil {
		a.reduceTransparency = mygo.Theme.Preferences().ReduceTransparency
	}
	dark := c.Theme().Dark
	a.pal = &lightPalette
	t := ui.LightTheme()
	if dark {
		a.pal = &darkPalette
		t = ui.DarkTheme()
	}
	p := a.pal
	t.Spacing, t.FontSize, t.Radius, t.Font = 4, 13, 8, fontText
	t.Background, t.Surface = p.bg, p.control
	t.SurfaceHover, t.SurfacePressed = p.control.Mix(p.label, .04), p.control.Mix(p.label, .08)
	t.Border, t.Text, t.TextMuted = p.separator, p.label, p.secondary
	t.Accent, t.AccentText = p.accent, ui.RGB(255, 255, 255)
	t.AccentHover, t.AccentPressed = p.accent.Mix(ui.RGB(255, 255, 255), .1), p.accent.Mix(ui.RGB(0, 0, 0), .12)
	t.Danger, t.Warning, t.Success = p.red, p.orange, p.green
	t.Focus, t.Selection = p.accent.Alpha(.6), p.accent.Alpha(.25)
	t.Scrollbar, t.ScrollbarWidth = p.label.Alpha(.22), 7
	c.SetTheme(t)
	if c.Vibrancy() && !a.reduceTransparency {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(p.bg)
	}
}

// card is the grouped surface every section sits on.
func (a *App) card(c *ui.Context) ui.Element {
	p := a.pal
	e := ui.Column(c).Shrink(0).Padding(18, 20).Gap(14).Radius(14).Background(p.card)
	if p.dark {
		e.Border(1, p.hairline)
	} else {
		e.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .04)).Shadow(0, 6, 20, 0, p.shadow)
	}
	return e
}

// enter staggers a page's sections as they appear: each rises and fades in,
// a little after the one before it.
func (a *App) enter(e ui.Element) ui.Element {
	motion := springSmooth
	motion.Delay = time.Duration(min(a.enterIndex, 8)) * 40 * time.Millisecond
	a.enterIndex++
	return e.Transition(ui.ElementTransition{Animation: &motion, Position: true, Enter: &ui.Motion{Y: 14}})
}

func (a *App) section(c *ui.Context, content func()) ui.Element {
	return a.enter(a.card(c).Children(content))
}

// header is a card's title row, with optional trailing controls.
func (a *App) header(c *ui.Context, title, meta string, trailing func()) {
	ui.Row(c).MinHeight(30).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600).Font(fontDisplay).NoWrap()
		if meta != "" {
			ui.Text(c, meta).FontSize(12).TextColor(a.pal.secondary).NoWrap()
		}
		ui.Spacer(c)
		if trailing != nil {
			trailing()
		}
	})
}

// badge is a rounded square in a system color holding a white glyph, as the
// icons of System Settings.
func (a *App) badge(c *ui.Context, name string, color ui.Color, size float32) ui.Element {
	b := ui.Box(c).Size(size, size).Shrink(0).Radius(size * .27).Center().
		LinearGradient(ui.LinearGradient{From: color.Mix(ui.RGB(255, 255, 255), .16), To: color, Angle: 180})
	b.Children(func() { ui.Icon(c, icons[name]).Size(size*.6, size*.6).TextColor(ui.RGB(255, 255, 255)) })
	return b
}

func (a *App) glyph(c *ui.Context, name string, size float32, color ui.Color) ui.Element {
	return ui.Icon(c, icons[name]).Size(size, size).Shrink(0).TextColor(color)
}

// pill is a capsule label tinted by its meaning.
func (a *App) pill(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(11).FontWeight(600).NoWrap().Padding(2, 8).Radius(999).Background(color.Alpha(.14)).TextColor(color)
}

func (a *App) caption(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(12).TextColor(a.pal.secondary)
}

// button is a bordered push button; plain removes its bezel until hovered.
func (a *App) button(c *ui.Context, label, iconName string) ui.Element {
	p := a.pal
	b := ui.Button(c, "").Label(label).Height(30).Padding(0, 12).Gap(6).Radius(8).FontSize(13).
		Background(p.control).Border(1, p.separator)
	if !p.dark {
		b.Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, .04))
	}
	b.Children(func() {
		if iconName != "" {
			a.glyph(c, iconName, 15, p.label)
		}
		if label != "" {
			ui.Text(c, label).NoWrap()
		}
	})
	return b
}

func (a *App) primary(c *ui.Context, label string) ui.Element {
	return ui.PrimaryButton(c, label).Height(30).Padding(0, 14).Radius(8).FontSize(13).FontWeight(600)
}

func (a *App) iconButton(c *ui.Context, name, label string) ui.Element {
	b := ui.Button(c, "").Label(label).Tooltip(label).Size(30, 30).Padding(0).Radius(8).Background(ui.Transparent).Border(0, ui.Transparent)
	if b.Hovered() {
		b.Background(a.pal.hover)
	}
	b.Children(func() { a.glyph(c, name, 17, a.pal.secondary) })
	return b
}

// segmented is a segmented control: a white thumb springs between options.
func (a *App) segmented(c *ui.Context, selected *int, labels []string, width float32) ui.Element {
	p := a.pal
	track := ui.Row(c).Height(30).Padding(2).Gap(0).Radius(9).Background(p.fill).Shrink(0)
	if width > 0 {
		track.Width(width)
	}
	n := max(1, len(labels))
	position := track.AnimateTo("thumb", float32(*selected), springSnappy)
	track.Draw(func(pt *ui.Painter, r ui.Rect) {
		w := (r.W - 4) / float32(n)
		thumb := ui.Rect{X: r.X + 2 + w*position, Y: r.Y + 2, W: w, H: r.H - 4}
		if !p.dark {
			pt.Shadow(thumb, 7, 0, 1, 3, 0, ui.RGBA(0, 0, 0, .12))
		}
		thumb.W -= 0
		color := ui.RGB(255, 255, 255)
		if p.dark {
			color = ui.Hex("#636366")
		}
		pt.Fill(thumb, color, 7)
	})
	track.Children(func() {
		for i, label := range labels {
			b := ui.Button(c, "").Label(label).Grow(1).Basis(0).Height(26).Padding(0, 10).Radius(7).Border(0, ui.Transparent).Background(ui.Transparent)
			weight := 400
			if *selected == i {
				weight = 600
			}
			b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(weight).NoWrap() })
			if b.Clicked() {
				*selected = i
			}
		}
	})
	return track
}

// popup is a pop-up button choosing one id by its label.
func (a *App) popup(c *ui.Context, label, current string, ids, labels []string, width float32) (string, bool) {
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
	s := ui.Select(c, &chosen, labels).Width(width).Height(30).FontSize(13).Radius(8).Label(a.tr(label)).Background(a.pal.control).Border(1, a.pal.separator)
	if !a.pal.dark {
		s.Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, .04))
	}
	if s.Changed() {
		for i, v := range labels {
			if v == chosen {
				return ids[i], true
			}
		}
	}
	return current, false
}

// stat is a label over a value, the unit of every metrics grid.
func (a *App) stat(c *ui.Context, label, value string) ui.Element {
	return ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(a.pal.secondary).NoWrap()
		ui.Text(c, value).FontSize(20).FontWeight(600).Font(fontDisplay).FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
	})
}

// separator is a hairline; inset aligns it with text after an icon.
func (a *App) separator(c *ui.Context, inset float32) {
	ui.Box(c).Height(1).Shrink(0).Margin(0, 0, 0, inset).Background(a.pal.separator)
}

// group is an inset grouped list, as the forms of System Settings: a title,
// rows on a rounded surface, and a footnote.
func (a *App) group(c *ui.Context, title, footer string, rows func()) {
	a.enter(ui.Column(c).Shrink(0).Gap(6)).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(13).FontWeight(600).Padding(0, 4).TextColor(a.pal.label)
		}
		g := a.card(c).Padding(0).Gap(0).Clip()
		a.groupRow = 0
		g.Children(rows)
		if footer != "" {
			ui.Text(c, footer).FontSize(12).LineHeight(1.4).Padding(0, 4).TextColor(a.pal.secondary)
		}
	})
}

// row is one line of a group: a badge, a title over a description, and the
// control that changes it.
func (a *App) row(c *ui.Context, iconName string, color ui.Color, title, description string, control func()) ui.Element {
	if a.groupRow > 0 {
		inset := float32(16)
		if iconName != "" {
			inset = 56
		}
		a.separator(c, inset)
	}
	a.groupRow++
	r := ui.Row(c).MinHeight(52).Padding(10, 16).Gap(12).AlignItems(ui.Center)
	r.Children(func() {
		if iconName != "" {
			a.badge(c, iconName, color, 28)
		}
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(500)
			if description != "" {
				ui.Text(c, description).FontSize(12).LineHeight(1.35).TextColor(a.pal.secondary)
			}
		})
		if control != nil {
			control()
		}
	})
	return r
}

func (a *App) empty(c *ui.Context, text string) {
	ui.Column(c).FillWidth().Padding(28).Gap(8).AlignItems(ui.Center).Children(func() {
		a.glyph(c, "tray", 28, a.pal.tertiary)
		ui.Text(c, text).FontSize(13).TextColor(a.pal.secondary)
	})
}
