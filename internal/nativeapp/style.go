package nativeapp

import (
	"github.com/egoist/mygo/ui"
	"strings"
)

func (a *App) skin(c *ui.Context) {
	t := ui.LightTheme()
	if c.Theme().Dark {
		t = ui.DarkTheme()
	}
	t.Spacing = 3
	t.FontSize = 12
	t.Radius = 9
	t.Font = "Segoe UI"
	if t.Dark {
		t.Background = ui.Hex("#101d29")
		t.Surface = ui.Hex("#1b2c3e")
		t.SurfaceHover = ui.Hex("#293f50")
		t.Text = ui.Hex("#e9f3ff")
		t.TextMuted = ui.Hex("#a6bbcf")
		t.Border = ui.Hex("#354c60")
		t.Accent = ui.Hex("#71e2c5")
		t.AccentText = ui.Hex("#102237")
	} else {
		t.Background = ui.Hex("#eaf3f8")
		t.Surface = ui.RGBA(255, 255, 255, .85)
		t.SurfaceHover = ui.Hex("#e5eff3")
		t.Text = ui.Hex("#102237")
		t.TextMuted = ui.Hex("#596e84")
		t.Border = ui.Hex("#d5e3ed")
		t.Accent = ui.Hex("#00785e")
		t.AccentText = ui.RGB(255, 255, 255)
	}
	t.Focus = t.Accent
	c.SetTheme(t)
	c.Root().Draw(func(p *ui.Painter, r ui.Rect) {
		// A display connected after startup may need a larger source level.
		if a.prepareBrand(p.Scale()) {
			c.Invalidate()
		}
		a.background.draw(p, r, t.Dark)
	})
}
func edge(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return ui.Hex("#94b9d3").Alpha(.19)
	}
	return ui.RGB(255, 255, 255).Alpha(.93)
}
func softAccent(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return ui.Hex("#234d49")
	}
	return ui.Hex("#d5f5eb")
}
func button(c *ui.Context, label string) *ui.Element {
	return ui.Button(c, label).Height(35).Padding(7, 12).FontSize(12).Radius(9).Background(c.Theme().Surface.Alpha(.75)).Border(1, c.Theme().Border).Shadow(0, 2, 5, 0, ui.RGBA(54, 108, 148, .06))
}
func iconButton(c *ui.Context, name, label string) *ui.Element {
	b := ui.Button(c, "").Label(label).Size(34, 34).Padding(0).Radius(9).Background(ui.Transparent).Border(0, ui.Transparent)
	b.Children(func() { icon(c, name, 17) })
	return b
}
func segment(c *ui.Context, selected *int, labels []string, names []string, width, height float32) *ui.Element {
	t := c.Theme()
	row := ui.Row(c).Height(height).Padding(3).Gap(0).Radius(10).Background(t.Surface.Alpha(.48)).Border(1, t.Border)
	if width > 0 {
		row.Width(width)
	}
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		position := float32(*selected)
		if !c.Preferences().ReduceMotion {
			position = row.AnimateWith("lens-position", position, motionLens, motionEase)
		}
		w := (r.W - 8) / float32(max(1, len(labels)))
		lens := ui.Rect{X: r.X + 4 + w*position, Y: r.Y + 4, W: w, H: r.H - 8}
		p.FillGradient(lens, ui.LinearGradient{From: t.Surface, To: softAccent(c), Angle: 135}, 7)
		p.Stroke(lens, edge(c), 7, 1)
	})
	row.Children(func() {
		for i, label := range labels {
			b := ui.Button(c, "").Label(label).Grow(1).Basis(0).Height(height-8).Padding(4, 9).Radius(7).Border(0, ui.Transparent).Background(ui.Transparent).TextColor(t.TextMuted)
			if *selected == i {
				b.TextColor(t.Accent).FontWeight(600)
			}
			b.Transition(ui.ElementTransition{Duration: motionQuick, Ease: motionEase, Colors: true})
			b.Children(func() {
				if len(names) > i && names[i] != "" {
					icon(c, names[i], 16)
				}
				ui.Text(c, label).FontSize(12).NoWrap()
			})
			if b.Clicked() {
				*selected = i
			}
		}
	})
	return row
}
func surface(c *ui.Context, grow, height float32, content func()) *ui.Element {
	t := c.Theme()
	p := ui.Column(c).Shrink(0).Padding(20).Gap(14).Radius(17).Gradient(t.Surface, t.Surface.Alpha(.81), 145).Border(1, edge(c)).Shadow(0, 6, 24, 0, ui.RGBA(54, 108, 148, .06))
	if grow > 0 {
		p.Grow(grow).Basis(0)
	}
	if height > 0 {
		p.Height(height)
	}
	p.Children(content)
	return p
}
func panelHeading(c *ui.Context, title, meta string) {
	ui.Row(c).Height(21).Gap(8).Children(func() {
		name := "chart"
		if strings.Contains(title, "活动") {
			name = "calendar"
		}
		if strings.Contains(title, "设置") || strings.Contains(title, "后台") {
			name = "settings"
		}
		ui.Icon(c, icons[name]).Size(16, 16).TextColor(c.Theme().TextMuted)
		ui.Text(c, title).FontSize(13).FontWeight(650).Grow(1)
		if meta != "" {
			ui.Text(c, meta).FontSize(11).TextColor(c.Theme().TextMuted)
		}
	})
}
func panel(c *ui.Context, title, meta string, content func()) {
	surface(c, 0, 0, func() {
		if title != "" {
			panelHeading(c, title, meta)
		}
		content()
	})
}
