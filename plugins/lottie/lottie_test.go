package lottie

import (
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
	if got := anim.duration(); got != 2_000_000_000 {
		t.Fatalf("duration = %v, want 2s", got)
	}
	if len(anim.layers) != 1 || len(anim.layers[0].groups) != 1 {
		t.Fatalf("layers/groups = %d/%d, want 1/1", len(anim.layers), len(anim.layers[0].groups))
	}
	layer := anim.layers[0]
	if got := layer.transform.position.sample(5); got != (point{X: 20, Y: 30}) {
		t.Fatalf("position at frame 5 = %+v, want (20,30)", got)
	}
	if got := layer.opacity.sample(5); got != 75 {
		t.Fatalf("opacity at frame 5 = %v, want 75", got)
	}
	// The synthetic root preserves transforms around nested shape groups.
	group := layer.groups[0].groups[0]
	if len(group.paths) != 1 || len(group.rects) != 1 || len(group.ellipses) != 1 {
		t.Fatalf("group geometry = paths %d, rects %d, ellipses %d; want 1 each", len(group.paths), len(group.rects), len(group.ellipses))
	}
	if len(group.fills) != 1 || len(group.strokes) != 1 {
		t.Fatalf("group styles = fills %d, strokes %d; want 1 each", len(group.fills), len(group.strokes))
	}
	if got := group.fills[0].color.sample(5); got != (color{R: 0.5, G: 0, B: 0.5, A: 1}) {
		t.Fatalf("fill color at frame 5 = %+v, want purple", got)
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
		`{"w":10,"h":10,"fr":10,"ip":0,"op":1,"layers":[{"ty":4,"shapes":[{"ty":"gr","it":[{"ty":"tm"}]}]}]}`,
	}
	for _, data := range cases {
		_, err := Load([]byte(data))
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("Load(%s) error = %v, want unsupported error", data, err)
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
