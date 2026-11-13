//go:build windows && (amd64 || arm64)

package windows

import (
	"sync"
	"syscall"
)

// PixelDisplays returns the displays as screenSpace takes them, and the
// DPI it takes without them, for PixelRectToDIP. Unlike screenDisplays it
// is safe from any goroutine: it lists the monitors through a callback of
// its own, made once, as enumMonitors' is the main thread's.
func PixelDisplays() ([]PixelDisplay, int) {
	pixelDisplays.Lock()
	defer pixelDisplays.Unlock()
	if pixelDisplays.callback == 0 {
		pixelDisplays.callback = syscall.NewCallback(func(m, _, _, _ uintptr) uintptr {
			pixelDisplays.found = append(pixelDisplays.found, PixelDisplay{Bounds: pixelRect(monitorInfo(m).Monitor), DPI: monitorDPI(m)})
			return 1
		})
	}
	pixelDisplays.found = nil
	procEnumDisplayMonitors.Call(0, 0, pixelDisplays.callback, 0)
	found := pixelDisplays.found
	pixelDisplays.found = nil
	return found, dpiOf(0)
}

var pixelDisplays struct {
	sync.Mutex
	callback uintptr
	found    []PixelDisplay
}
