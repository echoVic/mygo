package screencapture

import (
	"errors"
	"testing"
	"time"
)

// newRecording returns a recording at rate frames a second whose clock
// stands still at *now, which the test moves.
func newRecording(rate int, now *time.Duration) *Recording {
	if now == nil {
		now = new(time.Duration)
	}
	return &Recording{
		frames:   make(chan Frame, frameBuffer),
		done:     make(chan struct{}),
		interval: time.Second / time.Duration(Options{FrameRate: rate}.frameRate()),
		clock:    func() time.Duration { return *now },
	}
}

func TestToRGBA(t *testing.T) {
	// 2×2 pixels in rows of 12 bytes, with padding.
	bgra := []byte{
		1, 2, 3, 0, 4, 5, 6, 0, 9, 9, 9, 9,
		7, 8, 9, 0, 10, 11, 12, 0,
	}
	img := toRGBA(bgra, 2, 2, 12, orderBGRA)
	want := []byte{3, 2, 1, 255, 6, 5, 4, 255, 9, 8, 7, 255, 12, 11, 10, 255}
	if string(img.Pix) != string(want) {
		t.Errorf("BGRA: got %v, want %v", img.Pix, want)
	}
	img = toRGBA(bgra, 2, 2, 12, orderRGBA)
	want = []byte{1, 2, 3, 255, 4, 5, 6, 255, 7, 8, 9, 255, 10, 11, 12, 255}
	if string(img.Pix) != string(want) {
		t.Errorf("RGBA: got %v, want %v", img.Pix, want)
	}
}

func TestDeliverDropsTheOldest(t *testing.T) {
	var now time.Duration
	r := newRecording(240, &now)
	for i := range 5 {
		r.deliver([]byte{byte(i), 0, 0, 0}, 1, 1, 4, orderRGBA)
		now += r.interval
	}
	var got []byte
	for range frameBuffer {
		f := <-r.Frames()
		got = append(got, f.Image.Pix[0])
	}
	if string(got) != string([]byte{3, 4}) {
		t.Errorf("frames %v, want the newest, 3 and 4", got)
	}
}

func TestDeliverKeepsTheFrameRate(t *testing.T) {
	var now time.Duration
	r := newRecording(10, &now)
	if !r.wants() {
		t.Fatal("a new recording wants a frame")
	}
	r.deliver([]byte{1, 2, 3, 4}, 1, 1, 4, orderRGBA)
	if r.wants() {
		t.Error("wants a frame right after one")
	}
	r.deliver([]byte{1, 2, 3, 4}, 1, 1, 4, orderRGBA)
	if n := len(r.frames); n != 1 {
		t.Errorf("%d frames, want 1", n)
	}
	// A frame a little early still counts, as frames come at the
	// display's rate, which is not a multiple of FrameRate's interval.
	now = 80 * time.Millisecond
	if r.wants() {
		t.Error("wants a frame 80 ms after one, at 10 a second")
	}
	now = 90 * time.Millisecond
	if !r.wants() {
		t.Error("wants no frame 90 ms after one, at 10 a second")
	}
	r.deliver([]byte{1, 2, 3, 4}, 1, 1, 4, orderRGBA)
	if n := len(r.frames); n != 2 {
		t.Errorf("%d frames 90 ms later, want 2", n)
	}
	<-r.Frames()
	if f := <-r.Frames(); f.Time != 90*time.Millisecond {
		t.Errorf("the second frame came at %v, want 90ms", f.Time)
	}
}

func TestDeliverChecksTheSize(t *testing.T) {
	r := newRecording(0, nil)
	r.deliver(make([]byte, 7), 1, 2, 4, orderBGRA)
	r.deliver(make([]byte, 8), 2, 1, 4, orderBGRA)
	if len(r.frames) != 0 {
		t.Error("delivered pixels shorter than their size")
	}
}

func TestStop(t *testing.T) {
	r := newRecording(0, nil)
	stopped := 0
	r.stopNative = func() { stopped++ }
	if err := r.Stop(); err != nil || stopped != 1 {
		t.Fatalf("Stop: %v, stopped %d times", err, stopped)
	}
	if _, ok := <-r.Frames(); ok {
		t.Error("Frames is open after Stop")
	}
	<-r.Done()
	r.deliver([]byte{1, 2, 3, 4}, 1, 1, 4, orderRGBA) // no panic
	r.Stop()
	if stopped != 1 {
		t.Errorf("stopped %d times", stopped)
	}
}

func TestEndKeepsTheError(t *testing.T) {
	r := newRecording(0, nil)
	r.stopNative = func() {}
	gone := errors.New("gone")
	r.end(gone)
	r.end(errors.New("later"))
	if err := r.Stop(); err != gone {
		t.Errorf("Stop: %v, want %v", err, gone)
	}
}

func TestSource(t *testing.T) {
	for _, c := range []struct {
		src    Source
		window bool
		id     uint64
		str    string
	}{
		{Source{}, false, 0, "main display"},
		{DisplaySource(69734208), false, 69734208, "display 69734208"},
		{WindowSource(42), true, 42, "window 42"},
		{Window{ID: 7}.Source(), true, 7, "window 7"},
	} {
		if c.src.IsWindow() != c.window || c.src.ID() != c.id || c.src.String() != c.str {
			t.Errorf("%#v: %v %d %q", c.src, c.src.IsWindow(), c.src.ID(), c.src.String())
		}
	}
}
