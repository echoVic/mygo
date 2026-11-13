// Screencapture takes the screen with the screencapture plugin, and shows
// what it took in native UI: the main display, or one of the windows on
// the screen, which it lists front first with where they are.
//
//	go run ./examples/screencapture
//
// Nothing is taken or listed before a button asks. On macOS the first
// capture asks for Screen Recording, which a program run from a terminal
// asks for the terminal; build the app (go run ./cmd/mygo build
// examples/screencapture) to see it given to the app. On Linux the
// desktop asks what to share each time, and lists no windows.
package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/screencapture"
	"github.com/egoist/mygo/ui"
)

// model is the state the window shows, and how it reaches the screen,
// which the test replaces.
type model struct {
	wins   []screencapture.Window
	listed bool
	// noList is set where the system lists no windows, and lets the user
	// pick one instead.
	noList bool
	shot   *ui.Bitmap
	size   image.Point
	what   string // what shot shows
	err    error
	busy   bool

	capture func(src screencapture.Source, what string)
	list    func()
}

func (m *model) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(16, 16, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "Screen capture").FontSize(20).Bold()
				ui.Text(c, "Take the main display, or list the windows on the screen and take one.").
					TextColor(t.TextMuted)
			})
			if ui.Button(c.Key("list"), "List windows").Disabled(m.busy).Clicked() {
				m.busy = true
				m.list()
			}
			if ui.PrimaryButton(c.Key("display"), "Take the display").Disabled(m.busy).Clicked() {
				m.busy = true
				m.capture(screencapture.Source{}, "The main display")
			}
		})
		if m.err != nil {
			ui.Text(c, m.err.Error()).TextColor(t.Danger).PaddingX(16)
		}
		ui.Row(c).Grow(1).Gap(12).Padding(8, 16, 16).AlignItems(ui.Stretch).Children(func() {
			ui.Scroll(c).Width(300).Children(func() {
				ui.Column(c).Gap(8).Children(func() { m.windowsView(c) })
			})
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				if m.shot == nil {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, "Nothing taken yet.").TextColor(t.TextMuted)
					})
					return
				}
				ui.Text(c, fmt.Sprintf("%s, %d×%d pixels", m.what, m.size.X, m.size.Y)).
					FontSize(12).TextColor(t.TextMuted)
				ui.Image(c, m.shot).Fit(ui.Contain).Grow(1).Label(m.what)
			})
		})
	})
}

// windowsView lists the windows, each with a button that takes it.
func (m *model) windowsView(c *ui.Context) {
	t := c.Theme()
	switch {
	case m.noList:
		ui.Text(c, "This system lists no windows: its desktop lets you pick the one to take.").
			TextColor(t.TextMuted)
		if ui.Button(c.Key("pick"), "Pick a window").Disabled(m.busy).Clicked() {
			m.busy = true
			m.capture(screencapture.WindowSource(0), "The window picked")
		}
		return
	case !m.listed:
		ui.Text(c, "No windows listed yet.").TextColor(t.TextMuted)
		return
	case len(m.wins) == 0:
		ui.Text(c, "No windows on the screen.").TextColor(t.TextMuted)
		return
	}
	for _, w := range m.wins {
		name := w.Title
		if name == "" {
			name = w.App
		}
		b := w.Bounds
		ui.Row(c).Gap(8).Padding(8, 10).AlignItems(ui.Center).Radius(t.Radius).
			Background(t.Surface).Border(1, t.Border).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, name).Bold().SingleLine()
				ui.Text(c, fmt.Sprintf("%s · %d, %d · %d×%d", w.App, b.X, b.Y, b.Width, b.Height)).
					FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			if ui.Button(c.Key(w.ID), "Take").Label("Take " + name).Disabled(m.busy).Clicked() {
				m.busy = true
				m.capture(w.Source(), name)
			}
		})
	}
}

// showWindows shows the windows Windows found, or why it found none.
func (m *model) showWindows(wins []screencapture.Window, err error) {
	m.busy = false
	if errors.Is(err, errors.ErrUnsupported) {
		m.noList, m.err = true, nil
		return
	}
	m.wins, m.listed, m.err = wins, err == nil, explain(err)
}

// show shows what Capture took, or why it took nothing.
func (m *model) show(img *image.RGBA, what string, err error) {
	m.busy = false
	if err != nil {
		m.err = explain(err)
		return
	}
	m.shot, m.size, m.what, m.err = ui.NewBitmap(img), img.Rect.Size(), what, nil
}

// explain says what the user can do about an error.
func explain(err error) error {
	if errors.Is(err, screencapture.ErrDenied) {
		return fmt.Errorf("%w: allow Screen Recording in the system's settings, and start the app again", err)
	}
	return err
}

func main() {
	m := &model{}
	var win *mygo.Window
	m.capture = func(src screencapture.Source, what string) {
		go func() {
			img, err := screencapture.Capture(context.Background(), src, screencapture.Options{HideCursor: src.IsWindow()})
			win.Update(func() { m.show(img, what, err) })
		}()
	}
	m.list = func() {
		go func() {
			wins, err := screencapture.Windows(context.Background())
			win.Update(func() { m.showWindows(wins, err) })
		}()
	}
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:   "Screen capture",
			Width:   960,
			Height:  640,
			Content: ui.View(m.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
