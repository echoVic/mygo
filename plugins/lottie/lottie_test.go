package lottie

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

const parserFixture = `{
  "w": 100,
  "h": 80,
  "fr": 10,
  "ip": 0,
  "op": 20,
  "layers": [{
    "ddd": 0,
    "ind": 1,
    "ty": 4,
    "nm": "main",
    "ks": {
      "a": {"a": 0, "k": [0, 0]},
      "p": {"a": 1, "k": [
        {"t": 0, "s": [10, 20], "e": [30, 40]},
        {"t": 10, "s": [30, 40]}
      ]},
      "s": {"a": 0, "k": [100, 100, 100]},
      "r": {"a": 0, "k": 0},
      "o": {"a": 1, "k": [
        {"t": 0, "s": [100], "e": [50]},
        {"t": 10, "s": [50]}
      ]}
    },
    "shapes": [{
      "ty": "gr",
      "it": [
        {"ty": "rc", "p": {"a": 0, "k": [50, 40]}, "s": {"a": 0, "k": [40, 20]}, "r": {"a": 0, "k": 4}},
        {"ty": "el", "p": {"a": 0, "k": [50, 40]}, "s": {"a": 0, "k": [20, 20]}},
        {"ty": "sh", "ks": {"a": 0, "k": {
          "i": [[-10, -10], [10, -10], [0, 10]],
          "o": [[10, 10], [-10, 10], [0, -10]],
          "v": [[40, 30], [60, 30], [50, 50]],
          "c": true
        }}},
        {"ty": "fl", "c": {"a": 1, "k": [
          {"t": 0, "s": [1, 0, 0, 1], "e": [0, 0, 1, 1]},
          {"t": 10, "s": [0, 0, 1, 1]}
        ]}, "o": {"a": 0, "k": 100}},
        {"ty": "st", "c": {"a": 0, "k": [0, 0, 0, 1]}, "o": {"a": 0, "k": 100}, "w": {"a": 0, "k": 2}},
        {"ty": "tr", "p": {"a": 0, "k": [0, 0]}, "a": {"a": 0, "k": [0, 0]}, "s": {"a": 0, "k": [100, 100, 100]}, "r": {"a": 0, "k": 0}, "o": {"a": 0, "k": 100}}
      ]
    }]
  }]
}`

func TestLoadCompositionAndProperties(t *testing.T) {
	anim, err := Load([]byte(parserFixture))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if anim.Width != 100 || anim.Height != 80 || anim.FrameRate != 10 {
		t.Fatalf("metadata = %dx%d @ %v, want 100x80 @ 10", anim.Width, anim.Height, anim.FrameRate)
	}
	if got := anim.Duration(); got != 2_000_000_000 {
		t.Fatalf("duration = %v, want 2s", got)
	}
	if len(anim.layers) != 1 || len(anim.layers[0].items) != 1 || anim.layers[0].items[0].group == nil {
		t.Fatalf("layers = %d, want 1, its shapes in a group", len(anim.layers))
	}
	layer := anim.layers[0]
	if got := layer.transform.position.sample(5); got != (point{X: 20, Y: 30}) {
		t.Fatalf("position at frame 5 = %+v, want (20,30)", got)
	}
	if got := layer.transform.opacity.sample(5); got != 75 {
		t.Fatalf("opacity at frame 5 = %v, want 75", got)
	}
	// The layer's shapes are a group of their own, which keeps a top-level
	// transform around their nested groups.
	group := layer.items[0].group.items[0].group
	kinds := map[string]int{}
	for _, s := range layer.geometry {
		kinds[fmt.Sprintf("%T", s)]++
	}
	if len(layer.geometry) != 3 || kinds["lottie.pathShape"] != 1 || kinds["lottie.rectShape"] != 1 || kinds["lottie.ellipseShape"] != 1 {
		t.Fatalf("geometry = %v; want a path, a rectangle and an ellipse", kinds)
	}
	if len(group.items) != 5 || group.items[3].style == nil || group.items[4].style == nil {
		t.Fatalf("group items = %+v; want the geometry, a fill and a stroke", group.items)
	}
	fill, ok := group.items[3].style.(*fill)
	if !ok {
		t.Fatalf("first style = %T, want the fill", group.items[3].style)
	}
	if got := fill.color.sample(5); got != (color{R: 0.5, G: 0, B: 0.5, A: 1}) {
		t.Fatalf("fill color at frame 5 = %+v, want purple", got)
	}
	if s, ok := group.items[4].style.(*stroke); !ok || s.miter != 4 || s.cap != ui.ButtCap || s.join != ui.MiterJoin {
		t.Fatalf("second style = %+v, want a stroke with Lottie's defaults: butt caps, miter joins, limit 4", group.items[4].style)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	if _, err := Load([]byte(`{"layers":`)); err == nil {
		t.Fatal("Load accepted malformed JSON")
	}
}

func TestLoadRejectsUnsupportedFeatures(t *testing.T) {
	cases := []string{
		`{"w":10,"h":10,"fr":10,"ip":0,"op":1,"layers":[{"ty":5}]}`,
		`{"w":10,"h":10,"fr":10,"ip":0,"op":1,"layers":[{"ty":4,"shapes":[{"ty":"gr","it":[{"ty":"rp"}]}]}]}`,
	}
	for _, data := range cases {
		_, err := Load([]byte(data))
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("Load(%s) error = %v, want unsupported error", data, err)
		}
	}
	// The errors name what they reject, in words.
	for want, layer := range map[string]string{
		"unsupported image layer (ty 2)": `{"ty":2}`,
		"unsupported layer type 42":      `{"ty":42}`,
		`unsupported repeater (ty "rp")`: `{"ty":4,"shapes":[{"ty":"rp"}]}`,
		`unsupported shape type "xx"`:    `{"ty":4,"shapes":[{"ty":"xx"}]}`,
	} {
		_, err := Load([]byte(`{"w":10,"h":10,"fr":10,"ip":0,"op":1,"layers":[` + layer + `]}`))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%s) error = %v, want %q", layer, err, want)
		}
	}
}

func TestDrawRendersShapeLayers(t *testing.T) {
	anim, err := Load([]byte(parserFixture))
	if err != nil {
		t.Fatal(err)
	}
	img := renderAt(anim, 0)
	if got := countInk(img); got < 100 {
		t.Fatalf("ink pixels = %d, want at least 100", got)
	}
	if got := countColor(img, 255, 0, 0); got == 0 {
		t.Fatal("rendered fixture has no red fill pixels")
	}
	if got := countColor(img, 0, 0, 0); got == 0 {
		t.Fatal("rendered fixture has no stroke pixels")
	}
}

func TestDrawAppliesAnimatedLayerTransform(t *testing.T) {
	anim, err := Load([]byte(parserFixture))
	if err != nil {
		t.Fatal(err)
	}
	start := renderAt(anim, 0)
	end := renderAt(anim, 10)
	if samePixels(start, end) {
		t.Fatal("layer transform did not change the rendered frame")
	}
	startBox := inkBounds(start)
	endBox := inkBounds(end)
	if endBox.Min.X <= startBox.Min.X || endBox.Min.Y <= startBox.Min.Y {
		t.Fatalf("animated transform bounds = %v -> %v, want a positive translation", startBox, endBox)
	}
}

func renderAt(anim *Animation, frame float32) *image.RGBA {
	return ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(float32(anim.Width), float32(anim.Height)).Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
			anim.drawAt(p, r, frame)
		})
	}, anim.Width, anim.Height, 1)
}

func countInk(img *image.RGBA) int {
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R < 248 || p.G < 248 || p.B < 248 {
				count++
			}
		}
	}
	return count
}

func countColor(img *image.RGBA, r, g, b uint8) int {
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R == r && p.G == g && p.B == b {
				count++
			}
		}
	}
	return count
}

func samePixels(a, b *image.RGBA) bool {
	if !a.Rect.Eq(b.Rect) {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

func inkBounds(img *image.RGBA) image.Rectangle {
	bounds := image.Rectangle{}
	found := false
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R >= 248 && p.G >= 248 && p.B >= 248 {
				continue
			}
			if !found {
				bounds = image.Rect(x, y, x+1, y+1)
				found = true
			} else {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return bounds
}

const redRect = `{"ty":"rc","p":{"k":[50,40]},"s":{"k":[40,20]},"r":{"k":0}}, {"ty":"fl","c":{"k":[1,0,0,1]}}`

func mustLoad(t *testing.T, layers string) *Animation {
	t.Helper()
	a, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + layers + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDrawTopLevelShapesShareStyle(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"shapes":[`+redRect+`]}`)
	if got := renderAt(a, 0).RGBAAt(50, 40); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Fatalf("center = %v, want red", got)
	}
}

func TestDrawLayerStackAndVisibility(t *testing.T) {
	red := `{"ty":4,"ip":2,"op":8,"shapes":[{"ty":"gr","it":[` + redRect + `]}]}`
	blue := strings.ReplaceAll(red, `[1,0,0,1]`, `[0,0,1,1]`)
	blue = strings.ReplaceAll(blue, `"ip":2,"op":8`, `"ip":0,"op":20`)
	a := mustLoad(t, red+`,`+blue)
	for _, tc := range []struct {
		frame float32
		red   bool
	}{{0, false}, {2, true}, {7, true}, {8, false}} {
		got := renderAt(a, tc.frame).RGBAAt(50, 40)
		if tc.red && (got.R != 255 || got.B != 0) || !tc.red && (got.R != 0 || got.B != 255) {
			t.Errorf("frame %v center = %v, want red=%v", tc.frame, got, tc.red)
		}
	}
}

func TestDrawRoundedRectangleIsSymmetric(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"shapes":[{"ty":"gr","it":[`+strings.ReplaceAll(redRect, `"k":0`, `"k":8`)+`]}]}`)
	img := renderAt(a, 0)
	for y := 30; y < 50; y++ {
		for x := 30; x < 50; x++ {
			left, right := img.RGBAAt(x, y), img.RGBAAt(99-x, y)
			if diff := int(left.G) - int(right.G); diff < -3 || diff > 3 {
				t.Fatalf("rectangle is asymmetric at (%d,%d): %v vs %v", x, y, left, right)
			}
		}
	}
}

// Back and overshoot easings move outside the keyframes' values, and a
// curve's y controls need not be in order: Lottie allows both.
func TestLoadAcceptsOvershootEasing(t *testing.T) {
	for _, ease := range []string{
		`"o":{"x":0.42,"y":2},"i":{"x":0.58,"y":-1}`,
		`"o":{"x":0.1,"y":0.9},"i":{"x":0.9,"y":0.1}`,
		`"o":{"x":0.34,"y":1.56},"i":{"x":0.64,"y":1}`,
		`"o":{"x":-1,"y":-0.5},"i":{"x":3,"y":1.5}`,
	} {
		p, err := parseScalar(json.RawMessage(`{"a":1,"k":[{"t":0,"s":[0],"e":[100],`+ease+`},{"t":10,"s":[100]}]}`), 0)
		if err != nil {
			t.Fatalf("%s: %v", ease, err)
		}
		if p.sample(0) != 0 || p.sample(10) != 100 {
			t.Errorf("%s: ends = %v, %v, want 0, 100", ease, p.sample(0), p.sample(10))
		}
	}
	back, err := parseScalar(json.RawMessage(`{"a":1,"k":[{"t":0,"s":[0],"e":[100],"o":{"x":0.34,"y":1.56},"i":{"x":0.64,"y":1}},{"t":10,"s":[100]}]}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	peak := float32(0)
	for f := float32(0); f <= 10; f += 0.25 {
		peak = max(peak, back.sample(f))
	}
	if peak <= 100 {
		t.Fatalf("back easing peaks at %v, want past 100", peak)
	}
	data := `{"w":10,"h":10,"fr":10,"ip":0,"op":2,"layers":[{"ty":4,"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"e":[10,0],"i":{"x":0.58,"y":-1},"o":{"x":0.42,"y":2}},{"t":2,"s":[10,0]}]}}}]}`
	if _, err := Load([]byte(data)); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// renderBox draws anim at frame into a w×h box on white.
func renderBox(anim *Animation, w, h int, frame float32) *image.RGBA {
	return ui.Render(func(c *ui.Context) {
		ui.Box(c).Size(float32(w), float32(h)).Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
			anim.drawAt(p, r, frame)
		})
	}, w, h, 1)
}

// edgePixels counts the pixels that are neither the white background nor
// a channel's full ink: the antialiased edges of what is drawn.
func edgePixels(img *image.RGBA) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if g := img.RGBAAt(x, y).G; g > 8 && g < 247 {
				n++
			}
		}
	}
	return n
}

// A shape layer's keyframes are in composition frames: its start time
// moves nothing but what a precomposition shows (TestPrecompositionTime).
func TestLayerStartTimeKeepsCompositionFrames(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"st":10,"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"e":[40,0]},{"t":10,"s":[40,0]}]}},"shapes":[{"ty":"rc","p":{"k":[20,40]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[1,0,0,1]}}]}`)
	for _, tc := range []struct {
		frame float32
		x     int
	}{{0, 10}, {5, 30}, {10, 50}} {
		if got := inkBounds(renderAt(a, tc.frame)).Min.X; got != tc.x {
			t.Errorf("frame %v: x = %d, want %d", tc.frame, got, tc.x)
		}
	}
}

// What a composition draws outside its own rectangle does not show.
func TestDrawClipsToComposition(t *testing.T) {
	// A 100×80 composition in a 300×80 box sits at x 100 to 200; its
	// rectangle at x 130 to 170 of the composition would reach 230 to 270.
	a := mustLoad(t, `{"ty":4,"shapes":[{"ty":"rc","p":{"k":[150,40]},"s":{"k":[40,20]}},{"ty":"rc","p":{"k":[50,40]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[1,0,0,1]}}]}`)
	img := renderBox(a, 300, 80, 0)
	if got := inkBounds(img); got != image.Rect(140, 30, 160, 50) {
		t.Fatalf("ink = %v, want only the rectangle inside the composition, (140,30)-(160,50)", got)
	}
}

// A small composition drawn large is as sharp as the same shape drawn at
// that size: its edges are no wider than the painter's own.
func TestDrawStaysSharpWhenScaledUp(t *testing.T) {
	const circle = `{"ty":"el","p":{"k":[5,5]},"s":{"k":[8,8]}}`
	direct := func(draw func(p *ui.Painter, path *ui.Path)) int {
		return edgePixels(ui.Render(func(c *ui.Context) {
			ui.Box(c).Size(200, 200).Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
				var path ui.Path
				draw(p, &path)
			})
		}, 200, 200, 1))
	}
	load := func(shapes string) *Animation {
		a, err := Load([]byte(`{"w":10,"h":10,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"shapes":[` + shapes + `]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	fill := direct(func(p *ui.Painter, path *ui.Path) { p.FillPath(path.Circle(100, 100, 80), ui.RGB(255, 0, 0)) })
	if got := edgePixels(renderBox(load(circle+`,{"ty":"fl","c":{"k":[1,0,0,1]}}`), 200, 200, 0)); got > fill*3/2 {
		t.Errorf("filled circle 20× its size: %d edge pixels, want about %d", got, fill)
	}
	stroke := direct(func(p *ui.Painter, path *ui.Path) { p.StrokePath(path.Circle(100, 100, 80), 20, ui.RGB(255, 0, 0)) })
	if got := edgePixels(renderBox(load(circle+`,{"ty":"st","c":{"k":[1,0,0,1]},"w":{"k":1}}`), 200, 200, 0)); got > stroke*3/2 {
		t.Errorf("stroked circle 20× its size: %d edge pixels, want about %d", got, stroke)
	}
	// Scaled unevenly, a stroke is drawn at the larger scale and only
	// shrunk along the other axis.
	uneven := load(`{"ty":"gr","it":[` + circle + `,{"ty":"st","c":{"k":[1,0,0,1]},"w":{"k":1}},{"ty":"tr","a":{"k":[5,5]},"p":{"k":[5,5]},"s":{"k":[100,50]}}]}`)
	if got := edgePixels(renderBox(uneven, 200, 200, 0)); got > stroke*3/2 {
		t.Errorf("stroked ellipse 20× its size: %d edge pixels, want about %d", got, stroke)
	}
}

// A miter join without a miter limit (ml) gets Lottie's default of 4,
// not a bevel.
func TestStrokeMiterLimitDefaultsToFour(t *testing.T) {
	triangle := func(ml string) *image.RGBA {
		return renderAt(mustLoad(t, `{"ty":4,"shapes":[{"ty":"sh","ks":{"k":{"i":[[0,0],[0,0],[0,0]],"o":[[0,0],[0,0],[0,0]],"v":[[20,70],[50,10],[80,70]],"c":true}}},{"ty":"st","lj":1,`+ml+`"c":{"k":[0,0,0,1]},"w":{"k":6}}]}`), 0)
	}
	absent, four, one := triangle(``), triangle(`"ml":4,`), triangle(`"ml":1,`)
	if !samePixels(absent, four) {
		t.Error("a stroke without ml draws differently from ml 4")
	}
	if samePixels(absent, one) {
		t.Error("a stroke without ml draws bevelled, as ml 1")
	}
}

// Width and height written as whole floats (100.0) load as whole numbers.
func TestLoadCompositionSizeAsWholeFloat(t *testing.T) {
	a, err := Load([]byte(`{"w":100.0,"h":8e1,"fr":10,"ip":0,"op":20,"layers":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 100 || a.Height != 80 {
		t.Fatalf("size = %dx%d, want 100x80", a.Width, a.Height)
	}
	for _, size := range []string{`"w":100.5,"h":80`, `"w":0,"h":80`, `"w":100,"h":-80`, `"w":"100","h":80`} {
		if _, err := Load([]byte(`{` + size + `,"fr":10,"ip":0,"op":20,"layers":[]}`)); err == nil {
			t.Errorf("Load accepted %s", size)
		}
	}
}

// Groups nest as deep as maxGroupDepth; deeper, Load says so rather than
// recursing without bound.
func TestGroupNestingDepth(t *testing.T) {
	nest := func(depth int) string {
		s := redRect
		for range depth {
			s = `{"ty":"gr","it":[` + s + `]}`
		}
		return `{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"shapes":[` + s + `]}]}`
	}
	a, err := Load([]byte(nest(maxGroupDepth)))
	if err != nil {
		t.Fatalf("%d nested groups: %v", maxGroupDepth, err)
	}
	if got := renderAt(a, 0).RGBAAt(50, 40); got.R != 255 || got.G != 0 {
		t.Fatalf("deepest group's center = %v, want red", got)
	}
	if _, err := Load([]byte(nest(maxGroupDepth + 1))); err == nil || !strings.Contains(err.Error(), "nest deeper") {
		t.Fatalf("%d nested groups: error %v, want a nesting error", maxGroupDepth+1, err)
	}
}

// Load makes the outlines of static geometry once, and every frame draws
// those; only animated geometry is sampled again.
func TestStaticGeometryIsRetained(t *testing.T) {
	growing := `{"ty":"rc","p":{"k":[50,40]},"s":{"a":1,"k":[{"t":0,"s":[10,10],"e":[40,20]},{"t":10,"s":[40,20]}]}},{"ty":"fl","c":{"k":[0,0,1]}}`
	a := mustLoad(t, `{"ty":4,"shapes":[{"ty":"gr","it":[`+redRect+`]}]}`)
	sh := &a.layers[0].shapes
	if len(sh.outlines) != 1 || len(sh.outlines[0].paths) != 1 || sh.animated {
		t.Fatalf("static shapes: retained %v, animated %v; want one outline, false", sh.outlines, sh.animated)
	}
	for _, frame := range []float32{0, 7, 19} {
		if got := sh.outlinesAt(frame); len(got) != 1 || &got[0] != &sh.outlines[0] {
			t.Fatalf("frame %v: static outlines were made again", frame)
		}
	}
	a = mustLoad(t, `{"ty":4,"shapes":[{"ty":"gr","it":[`+redRect+`]},{"ty":"gr","it":[`+growing+`]}]}`)
	sh = &a.layers[0].shapes
	if !sh.animated || len(sh.moving) != 1 || sh.moving[0] != 1 || sh.outlines[1].paths != nil {
		t.Fatalf("animated rectangle: moving %v, retained %v; want it sampled every frame", sh.moving, sh.outlines[1])
	}
	for _, frame := range []float32{0, 7} {
		if got := sh.outlinesAt(frame); &got[0].paths[0].Vertices[0] != &sh.outlines[0].paths[0].Vertices[0] {
			t.Fatalf("frame %v: the static rectangle's outline was made again", frame)
		}
	}
	if a, b := sh.outlinesAt(0)[1].paths[0].Vertices[0], sh.outlinesAt(10)[1].paths[0].Vertices[0]; a == b {
		t.Fatalf("animated rectangle did not change: %v at both frames", a)
	}
}

// Players of one animation draw it at once on several goroutines: Draw
// only reads what Load made.
func TestSharedAnimationDrawsConcurrently(t *testing.T) {
	a := movingAnimation(t)
	want := renderAt(a, 10)
	images := make(chan *image.RGBA)
	for range 4 {
		go func() { images <- renderAt(a, 10) }()
	}
	for range 4 {
		if !samePixels(<-images, want) {
			t.Error("a concurrent Draw drew differently")
		}
	}
}
