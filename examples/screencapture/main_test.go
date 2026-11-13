package main

import (
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/screencapture"
	"github.com/egoist/mygo/ui"
)

// The window takes the display, lists the windows when asked, and takes
// the one whose button is clicked, without reaching the screen.
func TestCapture(t *testing.T) {
	var took []screencapture.Source
	listed := 0
	m := &model{}
	m.capture = func(src screencapture.Source, what string) { took = append(took, src) }
	m.list = func() { listed++ }
	tt := ui.NewTester(m.view, 960, 640)
	for _, s := range []string{"No windows listed yet.", "Nothing taken yet."} {
		if _, ok := tt.Find(s); !ok {
			t.Errorf("%q isn't shown", s)
		}
	}

	if err := tt.Click("Take the display"); err != nil {
		t.Fatal(err)
	}
	// Busy, the buttons wait for the capture.
	if err := tt.Click("List windows"); err != nil {
		t.Fatal(err)
	}
	if len(took) != 1 || took[0] != (screencapture.Source{}) || listed != 0 {
		t.Fatalf("took %v and listed %d times, want the main display and no list while busy", took, listed)
	}
	m.show(image.NewRGBA(image.Rect(0, 0, 3456, 2234)), "The main display", nil)
	tt.Frame()
	if _, ok := tt.Find("The main display, 3456×2234 pixels"); !ok {
		t.Errorf("the screenshot's size isn't shown: %q", tt.Texts())
	}

	if err := tt.Click("List windows"); err != nil {
		t.Fatal(err)
	}
	m.showWindows([]screencapture.Window{
		{ID: 7, Title: "Notes", App: "TextEdit", Bounds: mygo.Rectangle{X: 10, Y: 40, Width: 800, Height: 600}},
		{ID: 9, App: "Finder", Bounds: mygo.Rectangle{X: -1440, Y: 0, Width: 300, Height: 200}},
	}, nil)
	tt.Frame()
	for _, s := range []string{"Notes", "TextEdit · 10, 40 · 800×600", "Finder · -1440, 0 · 300×200"} {
		if _, ok := tt.Find(s); !ok {
			t.Errorf("%q isn't shown: %q", s, tt.Texts())
		}
	}
	if err := tt.Click("Take Finder"); err != nil {
		t.Fatal(err)
	}
	if listed != 1 || len(took) != 2 || took[1] != screencapture.WindowSource(9) {
		t.Errorf("took %v and listed %d times, want window 9 after the list", took, listed)
	}

	m.show(nil, "", screencapture.ErrDenied)
	tt.Frame()
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "allow Screen Recording") {
		t.Errorf("a denied capture doesn't say what to do: %q", tt.Texts())
	}
}

// Where the system lists no windows, the window lets the user pick one.
func TestNoList(t *testing.T) {
	var took []screencapture.Source
	m := &model{capture: func(src screencapture.Source, what string) { took = append(took, src) }}
	m.showWindows(nil, errors.ErrUnsupported)
	tt := ui.NewTester(m.view, 960, 640)
	if err := tt.Click("Pick a window"); err != nil {
		t.Fatal(err)
	}
	if len(took) != 1 || !took[0].IsWindow() || m.err != nil {
		t.Errorf("took %v, error %v: want a window picked, and no error", took, m.err)
	}
}
