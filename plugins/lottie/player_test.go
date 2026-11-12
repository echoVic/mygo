package lottie

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func movingAnimation(t *testing.T) *Animation {
	return mustLoad(t, `{"ty":4,"ks":{"p":{"a":1,"k":[{"t":0,"s":[-25,0],"e":[25,0]},{"t":20,"s":[25,0]}]}},"shapes":[`+redRect+`]}`)
}

func playerTester(p *Player, reduced bool) (*ui.Tester, *int, *ui.Services) {
	calls := new(int)
	services := new(ui.Services)
	tester := ui.NewTesterWith(func(c *ui.Context) {
		*services = c.Services()
		ui.Box(c).Size(100, 80).Background(ui.RGB(255, 255, 255)).Draw(func(dst *ui.Painter, r ui.Rect) { *calls++; p.Draw(dst, r) })
	}, 100, 80, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: reduced}})
	return tester, calls, services
}

func TestPlayerControlsAndCompletion(t *testing.T) {
	p := movingAnimation(t).NewPlayer()
	if p.Duration() != 2*time.Second {
		t.Fatalf("duration = %v", p.Duration())
	}
	tester, calls, services := playerTester(p, false)
	start := inkBounds(tester.Image()).Min.X
	tester.Advance(500 * time.Millisecond)
	progressed := inkBounds(tester.Image()).Min.X
	if progressed <= start {
		t.Fatalf("did not advance: %d -> %d", start, progressed)
	}
	p.Pause()
	tester.Advance(100 * time.Millisecond)
	held := inkBounds(tester.Image())
	n := *calls
	tester.Advance(time.Second)
	if *calls != n || inkBounds(tester.Image()) != held {
		t.Fatal("paused player kept requesting frames")
	}
	p.Play()
	services.Invalidate()
	tester.Advance(500 * time.Millisecond)
	if inkBounds(tester.Image()).Min.X <= progressed {
		t.Fatal("play did not resume")
	}
	p.Pause()
	p.Seek(1500 * time.Millisecond)
	services.Invalidate()
	tester.Advance(20 * time.Millisecond)
	if x := inkBounds(tester.Image()).Min.X; x < 40 || x > 50 {
		t.Fatalf("seek x = %d", x)
	}
	p.Reset()
	services.Invalidate()
	tester.Advance(20 * time.Millisecond)
	if x := inkBounds(tester.Image()).Min.X; x != start {
		t.Fatalf("reset x = %d, want %d", x, start)
	}
	p.Seek(-time.Second)
	services.Invalidate()
	tester.Advance(20 * time.Millisecond)
	if inkBounds(tester.Image()).Min.X != start {
		t.Fatal("negative seek did not clamp")
	}
	p.Play()
	services.Invalidate()
	tester.Advance(3 * time.Second)
	if x := inkBounds(tester.Image()).Min.X; x < 54 {
		t.Fatalf("end x = %d", x)
	}
	n = *calls
	tester.Advance(time.Second)
	if *calls != n {
		t.Fatal("completed player requested frames")
	}
	p.Seek(10 * time.Second)
	services.Invalidate()
	tester.Advance(20 * time.Millisecond)
	if inkBounds(tester.Image()).Min.X < 54 {
		t.Fatal("seek beyond end did not clamp")
	}
}

func TestPlayerLoopSpeedAndIndependence(t *testing.T) {
	anim := movingAnimation(t)
	p := anim.NewPlayer(PlayerOptions{Loop: true, Speed: 2})
	tester, calls, _ := playerTester(p, false)
	tester.Advance(1250 * time.Millisecond)
	if x := inkBounds(tester.Image()).Min.X; x < 16 || x > 19 {
		t.Fatalf("loop at 2x x = %d", x)
	}
	n := *calls
	tester.Advance(50 * time.Millisecond)
	if *calls <= n {
		t.Fatal("loop stopped requesting frames")
	}
	other := anim.NewPlayer()
	other.Pause()
	second, _, _ := playerTester(other, false)
	if inkBounds(second.Image()).Min.X != 5 {
		t.Fatal("players share playback state")
	}
}

func TestPlayerInvalidSpeeds(t *testing.T) {
	for _, speed := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		tester, _, _ := playerTester(movingAnimation(t).NewPlayer(PlayerOptions{Speed: speed}), false)
		tester.Advance(500 * time.Millisecond)
		if x := inkBounds(tester.Image()).Min.X; x < 16 || x > 19 {
			t.Fatalf("speed %v x = %d", speed, x)
		}
	}
}

func TestReduceMotion(t *testing.T) {
	anim := movingAnimation(t)
	p := anim.NewPlayer(PlayerOptions{Loop: true})
	tester, calls, services := playerTester(p, true)
	if x := inkBounds(tester.Image()).Min.X; x != 5 {
		t.Fatalf("reduced motion x = %d, want the first frame's 5", x)
	}
	n := *calls
	tester.Advance(10 * time.Second)
	if *calls != n {
		t.Fatal("reduced motion requested frames")
	}
	if x := inkBounds(tester.Image()).Min.X; x != 5 {
		t.Fatalf("reduced motion advanced to x = %d", x)
	}

	// Seek and Reset still choose the frame shown: a jump is not motion.
	p.Seek(time.Second)
	services.Invalidate()
	tester.Advance(20 * time.Millisecond)
	paused := anim.NewPlayer()
	paused.Pause()
	paused.Seek(time.Second)
	want, _, _ := playerTester(paused, false)
	sought := inkBounds(tester.Image())
	if w := inkBounds(want.Image()); sought != w || sought.Min.X == 5 {
		t.Fatalf("reduced motion after seek shows %v, want %v", sought, w)
	}
	n = *calls
	tester.Advance(10 * time.Second)
	if *calls != n || inkBounds(tester.Image()) != sought {
		t.Fatal("reduced motion moved on from the sought frame")
	}
	p.Reset()
	services.Invalidate()
	tester.Advance(20 * time.Millisecond)
	if x := inkBounds(tester.Image()).Min.X; x != 5 {
		t.Fatalf("reduced motion after reset x = %d, want 5", x)
	}
}

func TestZeroValuePlayerIsSafe(t *testing.T) {
	var p Player
	if got := p.Duration(); got != 0 {
		t.Fatalf("zero-value duration = %v, want 0", got)
	}
	p.Play()
	p.Pause()
	p.Reset()
	p.Seek(time.Second)
	tester, _, _ := playerTester(&p, false)
	tester.Advance(time.Second)
}
