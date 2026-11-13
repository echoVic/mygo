package screencapture

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// TestDevice captures the real screen, where MYGO_SCREENCAPTURE=1: it
// lists the windows, takes a screenshot of the main display, records it
// for two seconds after its first frame, and the frontmost window for
// one, and writes the images to MYGO_SCREENCAPTURE_OUT, the temporary
// directory by default, for a look. On Linux it goes through the
// portals, which ask.
func TestDevice(t *testing.T) {
	if os.Getenv("MYGO_SCREENCAPTURE") != "1" {
		t.Skip("set MYGO_SCREENCAPTURE=1 to capture the screen")
	}
	out := os.Getenv("MYGO_SCREENCAPTURE_OUT")
	if out == "" {
		out = t.TempDir()
	}
	t.Logf("access: %v", Access())
	if !RequestAccess() {
		t.Fatal("no access to the screen")
	}

	// Each step has a context of its own, as on Linux each waits for the
	// user to answer the portal's dialog.
	wins, err := Windows(wait.Context(t))
	if errors.Is(err, errors.ErrUnsupported) {
		t.Log("windows: unsupported")
	} else if err != nil {
		t.Fatalf("windows: %v", err)
	} else {
		for i, w := range wins {
			if i < 10 {
				t.Logf("window %d %q of %q at %+v", w.ID, w.Title, w.App, w.Bounds)
			}
		}
	}

	start := time.Now()
	img, err := Capture(wait.Context(t), Source{}, Options{})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	t.Logf("screenshot: %v in %v", img.Rect, time.Since(start))
	save(t, filepath.Join(out, "screenshot.png"), img)

	record(t, Source{}, Options{FrameRate: 30}, 2*time.Second, filepath.Join(out, "display-last.png"))
	if len(wins) > 0 {
		record(t, wins[0].Source(), Options{FrameRate: 10, HideCursor: true}, time.Second, filepath.Join(out, "window-last.png"))
	}
	t.Logf("images in %s", out)
}

func record(t *testing.T, src Source, opts Options, d time.Duration, path string) {
	t.Helper()
	rec, err := Record(wait.Context(t), src, opts)
	if err != nil {
		t.Fatalf("record %v: %v", src, err)
	}
	defer rec.Stop()
	// The first frame comes when the system has one. The recording then
	// runs for d, for the log to tell how many frames came, which nothing
	// checks: a still screen makes few.
	last := wait.For(t, "the first frame of the "+src.String(), rec.Frames())
	if last.Image == nil {
		t.Fatalf("record %v: stopped before a frame: %v", src, rec.Err())
	}
	n := 1
	timer := time.AfterFunc(d, func() { rec.Stop() })
	defer timer.Stop()
	for f := range rec.Frames() {
		n++
		last = f
	}
	if err := rec.Err(); err != nil {
		t.Fatalf("record %v: %v", src, err)
	}
	t.Logf("record %v: %d frames, the last %v at %v", src, n, last.Image.Bounds(), last.Time)
	save(t, path, last.Image)
}

func save(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
