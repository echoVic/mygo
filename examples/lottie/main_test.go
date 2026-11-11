package main

import (
	"bytes"
	"image"
	"image/draw"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/lottie"
	"github.com/egoist/mygo/ui"
)

func TestEmbeddedAnimation(t *testing.T) {
	animation, err := lottie.Load(animationData)
	if err != nil {
		t.Fatal(err)
	}
	player := animation.NewPlayer()
	player.Pause()
	render := func(at time.Duration) *image.RGBA {
		player.Seek(at)
		return ui.Render(func(c *ui.Context) {
			ui.Box(c).Size(240, 200).Background(ui.RGB(255, 255, 255)).Draw(player.Draw)
		}, 240, 200, 1)
	}
	start, middle := render(0), render(500*time.Millisecond)
	if bytes.Equal(start.Pix, middle.Pix) {
		t.Fatal("embedded animation does not move")
	}
	if start.RGBAAt(116, 111).R != 255 {
		t.Fatal("white check stroke missing at check vertex")
	}
	if c := start.RGBAAt(140, 100); c.B <= c.R {
		t.Fatalf("blue badge missing: %v", c)
	}
}

// The demo plays the badge in a loop, and its buttons pause, reset, seek
// and play it, as the tester's clock moves on.
func TestDemo(t *testing.T) {
	animation, err := lottie.Load(animationData)
	if err != nil {
		t.Fatal(err)
	}
	d := &demo{player: animation.NewPlayer(lottie.PlayerOptions{Loop: true})}
	tt := ui.NewTester(d.view, 460, 450)
	r, ok := tt.Find("An orbiting success badge")
	if !ok || r.W != 300 || r.H != 250 {
		t.Fatalf("badge: found %v at %v, want 300×250", ok, r)
	}
	// badge copies what the badge's box shows.
	badge := func() *image.RGBA {
		box := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
		out := image.NewRGBA(box)
		draw.Draw(out, box, tt.Image(), box.Min, draw.Src)
		return out
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	start := badge()
	if !tt.FramePending() {
		t.Fatal("a looping player asks for no frames")
	}
	tt.Advance(500 * time.Millisecond)
	if bytes.Equal(badge().Pix, start.Pix) {
		t.Fatal("the badge does not move")
	}

	click("Pause")
	held := badge()
	if tt.FramePending() {
		t.Fatal("a paused player asks for frames")
	}
	tt.Advance(time.Second)
	if !bytes.Equal(badge().Pix, held.Pix) {
		t.Fatal("a paused player moved")
	}

	click("Reset")
	if !bytes.Equal(badge().Pix, start.Pix) {
		t.Fatal("Reset does not show the first frame")
	}
	click("Seek 1s")
	if bytes.Equal(badge().Pix, start.Pix) || tt.FramePending() {
		t.Fatalf("Seek 1s: moved %v, a frame pending %v; want moved, none", !bytes.Equal(badge().Pix, start.Pix), tt.FramePending())
	}

	click("Play")
	if !tt.FramePending() {
		t.Fatal("Play asks for no frames")
	}
	seek := badge()
	tt.Advance(250 * time.Millisecond)
	if bytes.Equal(badge().Pix, seek.Pix) {
		t.Fatal("Play does not move the badge")
	}
}
