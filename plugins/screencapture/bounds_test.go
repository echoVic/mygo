package screencapture

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	winbackend "github.com/egoist/mygo/internal/windows"
)

// On Windows, a window's bounds are in the coordinates of mygo.Display,
// on displays of different scales too: a 100% primary display, a 200%
// one to its right and a 150% one to its left. In DIPs the right one is
// at 1920, 0 and the left one at -2880, 0, both 1920×1080.
func TestWindowBounds(t *testing.T) {
	displays := []winbackend.PixelDisplay{
		{Bounds: platform.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}, DPI: 96},
		{Bounds: platform.Rect{X: 1920, Y: 0, Width: 3840, Height: 2160}, DPI: 192},
		{Bounds: platform.Rect{X: -2880, Y: 0, Width: 2880, Height: 1620}, DPI: 144},
	}
	for _, c := range []struct {
		name   string
		pixels platform.Rect
		want   mygo.Rectangle
	}{
		{"on the primary display", platform.Rect{X: 100, Y: 200, Width: 800, Height: 600}, mygo.Rectangle{X: 100, Y: 200, Width: 800, Height: 600}},
		// 1000 pixels into the 200% display: 500 DIPs past its 1920.
		{"on the 200% display", platform.Rect{X: 2920, Y: 400, Width: 1600, Height: 1200}, mygo.Rectangle{X: 2420, Y: 200, Width: 800, Height: 600}},
		{"filling the 200% display", platform.Rect{X: 1920, Y: 0, Width: 3840, Height: 2160}, mygo.Rectangle{X: 1920, Y: 0, Width: 1920, Height: 1080}},
		// 280 pixels into the 150% display: 187 DIPs past its -2880.
		{"on the 150% display to the left", platform.Rect{X: -2600, Y: 300, Width: 1440, Height: 900}, mygo.Rectangle{X: -2693, Y: 200, Width: 960, Height: 600}},
		{"filling the 150% display to the left", platform.Rect{X: -2880, Y: 0, Width: 2880, Height: 1620}, mygo.Rectangle{X: -2880, Y: 0, Width: 1920, Height: 1080}},
		// Mostly on the 200% display, whose scale it takes, as the
		// backend's own windows do.
		{"across two displays", platform.Rect{X: 1800, Y: 100, Width: 1000, Height: 400}, mygo.Rectangle{X: 1860, Y: 50, Width: 500, Height: 200}},
	} {
		if got := windowBounds(displays, 96, c.pixels); got != c.want {
			t.Errorf("%s: pixels %v are %v, want %v", c.name, c.pixels, got, c.want)
		}
	}
}
