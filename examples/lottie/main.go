// Lottie shows a shape-only animation with native playback controls.
//
//	go run ./examples/lottie
package main

import (
	_ "embed"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/lottie"
	"github.com/egoist/mygo/ui"
)

// orbit.json is an original fixture for this example, under MyGo's license.
//
//go:embed orbit.json
var animationData []byte

type gallery struct{ player *lottie.Player }

func (g *gallery) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		ui.Text(c, "Lottie, drawn by MyGo").FontSize(22).Bold()
		ui.Box(c).Size(300, 250).Label("An orbiting success badge").Draw(g.player.Draw)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("play"), "Play").Clicked() {
				g.player.Play()
				c.Services().Invalidate()
			}
			if ui.Button(c.Key("pause"), "Pause").Clicked() {
				g.player.Pause()
				c.Services().Invalidate()
			}
			if ui.Button(c.Key("reset"), "Reset").Clicked() {
				g.player.Reset()
				c.Services().Invalidate()
			}
			if ui.Button(c.Key("seek"), "Seek 1s").Clicked() {
				g.player.Seek(time.Second)
				c.Services().Invalidate()
			}
		})
		ui.Text(c, "Original JSON · 2 seconds · looping").FontSize(12).TextColor(t.TextMuted)
	})
}

func main() {
	animation, err := lottie.Load(animationData)
	if err != nil {
		log.Fatal(err)
	}
	g := &gallery{player: animation.NewPlayer(lottie.PlayerOptions{Loop: true})}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title: "Lottie", Width: 460, Height: 450, Content: ui.View(g.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
