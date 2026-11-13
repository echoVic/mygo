package screencapture

// This file has no build constraint: the conversion of Windows' pixels
// is arithmetic, tested on every platform.

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	winbackend "github.com/egoist/mygo/internal/windows"
)

// windowBounds returns where a window whose frame is r, in the screen's
// pixels, is on Windows in the coordinates of mygo.Display: through the
// backend's own conversion of its windows' bounds, in the scale of the
// display of displays the window is the most on, at fallback DPI
// without displays.
func windowBounds(displays []winbackend.PixelDisplay, fallback int, r platform.Rect) mygo.Rectangle {
	return mygo.Rectangle(winbackend.PixelRectToDIP(displays, fallback, r))
}
