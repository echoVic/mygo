package main

import (
	"bytes"
	"image"
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
