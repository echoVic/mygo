package lottie

import (
	"encoding/json"
	"image"
	"math"
	"strings"
	"testing"
)

// keys returns a property animated from `from` at frame 0 to `to` at frame
// 10, its first keyframe with the fields extra too.
func keys(from, to, extra string) json.RawMessage {
	return json.RawMessage(`{"a":1,"k":[{"t":0,"s":` + from + `,"e":` + to + extra + `},{"t":10,"s":` + to + `}]}`)
}

func near(a, b point, tol float32) bool { return abs(a.X-b.X) <= tol && abs(a.Y-b.Y) <= tol }

func finitePoint(p point) bool { return finite(float64(p.X)) && finite(float64(p.Y)) }

// Each component of a point eases on its own curve, component n on the
// n-th of each control.
func TestPerComponentEasing(t *testing.T) {
	p, err := parsePoint(keys(`[0,0]`, `[100,100]`, `,"o":{"x":[0,0.9],"y":[0,0]},"i":{"x":[1,1],"y":[1,1]}`), point{})
	if err != nil {
		t.Fatal(err)
	}
	linear, slow := cubicBezier(0, 0, 1, 1), cubicBezier(0.9, 0, 1, 1)
	for _, f := range []float32{0, 2.5, 5, 7.5, 9.9, 10} {
		want := point{lerp(0, 100, linear(f/10)), lerp(0, 100, slow(f/10))}
		if got := p.sample(f); got != want {
			t.Errorf("frame %v: %v, want %v", f, got, want)
		}
	}
	if mid := p.sample(5); !(mid.Y < mid.X-10) {
		t.Fatalf("frame 5: %v, want y eased far behind x", mid)
	}
}

// Scale is exported with three components, and three entries in each
// control: the third is z's, which a 2D transform ignores, whatever it is.
func TestScaleEasingIgnoresThirdComponent(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"ks":{"s":{"a":1,"k":[{"t":0,"s":[100,100,100],"e":[200,300,100],"o":{"x":[0.5,0.2,0.9],"y":[0,0,0]},"i":{"x":[0.5,0.8,0.1],"y":[1,1,1]}},{"t":10,"s":[200,300,100]}]}},"shapes":[`+redRect+`]}`)
	x, y := cubicBezier(0.5, 0, 0.5, 1), cubicBezier(0.2, 0, 0.8, 1)
	if got, want := a.layers[0].transform.scale.sample(3), (point{lerp(100, 200, x(0.3)), lerp(100, 300, y(0.3))}); got != want {
		t.Fatalf("scale at frame 3 = %v, want %v", got, want)
	}
	// Alike in x and y, the curves are one, whatever z's is.
	a = mustLoad(t, `{"ty":4,"ks":{"s":{"a":1,"k":[{"t":0,"s":[100,100,100],"e":[200,300,100],"o":{"x":[0.4,0.4,0.9],"y":[0,0,0.5]},"i":{"x":[0.6,0.6,0.1],"y":[1,1,0]}},{"t":10,"s":[200,300,100]}]}},"shapes":[`+redRect+`]}`)
	if k := a.layers[0].transform.scale.keys[0]; k.tween != nil || k.ease == nil {
		t.Fatalf("scale eased alike in x and y: tween %T, ease %v; want one curve", k.tween, k.ease != nil)
	}
}

// Components beyond the controls' entries take the first entry's curve,
// as lottie-web resolves them: a color's blue and alpha here.
func TestComponentEasingBeyondEntriesTakesFirst(t *testing.T) {
	p, err := parseColor(keys(`[0,0,0,0]`, `[1,1,1,1]`, `,"o":{"x":[0.9,0.1],"y":[0,0]},"i":{"x":[1,0.2],"y":[1,1]}`), color{})
	if err != nil {
		t.Fatal(err)
	}
	first, second := cubicBezier(0.9, 0, 1, 1)(0.4), cubicBezier(0.1, 0, 0.2, 1)(0.4)
	if got, want := p.sample(4), (color{first, second, first, first}); got != want {
		t.Fatalf("color at frame 4 = %v, want %v", got, want)
	}
}

// Controls written as arrays whose entries agree are one curve, sampled
// as fast as a number's: no tween.
func TestEasingAlikeIsOneCurve(t *testing.T) {
	for _, ease := range []string{
		`,"o":{"x":[0.3,0.3],"y":[0,0]},"i":{"x":[0.7,0.7],"y":[1,1]}`,
		`,"o":{"x":[0.3],"y":[0]},"i":{"x":[0.7],"y":[1]}`,
		`,"o":{"x":0.3,"y":0},"i":{"x":0.7,"y":1}`,
	} {
		p, err := parsePoint(keys(`[0,0]`, `[100,100]`, ease), point{})
		if err != nil {
			t.Fatal(err)
		}
		if k := p.keys[0]; k.tween != nil || k.ease == nil {
			t.Errorf("%s: tween %T, ease %v; want one curve", ease, k.tween, k.ease != nil)
		}
	}
}

// A scalar has one component, which takes the first entry of each
// control.
func TestScalarEasingTakesFirstEntry(t *testing.T) {
	p, err := parseScalar(keys(`[0]`, `[100]`, `,"o":{"x":[0.9,0.1],"y":[0,0]},"i":{"x":[1,0.2],"y":[1,1]}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.sample(5), lerp(0, 100, cubicBezier(0.9, 0, 1, 1)(0.5)); got != want {
		t.Fatalf("frame 5 = %v, want %v", got, want)
	}
}

// What has a single curve rejects controls whose entries differ: a path,
// and a position moving along a spatial curve.
func TestSingleCurveRejectsComponentEasing(t *testing.T) {
	ease := `,"o":{"x":[0.9,0.1],"y":[0,0]},"i":{"x":[1,0.2],"y":[1,1]}`
	path := `{"i":[[0,0]],"o":[[0,0]],"v":[[0,0]],"c":false}`
	if _, err := parsePath(keys(path, path, ease)); err == nil || !strings.Contains(err.Error(), "unsupported component-specific easing") {
		t.Errorf("path: error %v, want component-specific easing rejected", err)
	}
	if _, err := parsePoint(keys(`[0,0]`, `[100,0]`, `,"to":[0,60],"ti":[0,60]`+ease), point{}); err == nil || !strings.Contains(err.Error(), "unsupported component-specific easing") {
		t.Errorf("spatial: error %v, want component-specific easing rejected", err)
	}
}

// A keyframe with spatial tangents moves along the cubic Bézier from its
// value, out along to, into the next value along ti: exactly at its ends.
func TestSpatialTangentsFollowTheCurve(t *testing.T) {
	// An arch from (0, 0) to (100, 0), both tangents 60 down.
	p, err := parsePoint(keys(`[0,0]`, `[100,0]`, `,"to":[0,60],"ti":[0,60]`), point{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.keys[0].tween.(*spatialCurve); !ok {
		t.Fatalf("tween = %T, want a spatial curve", p.keys[0].tween)
	}
	if got := p.sample(0); got != (point{0, 0}) {
		t.Errorf("frame 0 = %v, want (0,0) exactly", got)
	}
	if got := p.sample(10); got != (point{100, 0}) {
		t.Errorf("frame 10 = %v, want (100,0) exactly", got)
	}
	if got := p.sample(5); !near(got, point{50, 45}, 0.01) {
		t.Errorf("frame 5 = %v, want the arch's top, (50,45)", got)
	}
	// Equal times cover equal lengths of the curve.
	var steps []float64
	prev := p.sample(0)
	for f := float32(0.5); f <= 10; f += 0.5 {
		cur := p.sample(f)
		steps = append(steps, math.Hypot(float64(cur.X-prev.X), float64(cur.Y-prev.Y)))
		prev = cur
	}
	lo, hi := steps[0], steps[0]
	for _, s := range steps {
		lo, hi = min(lo, s), max(hi, s)
	}
	if hi-lo > 0.01*hi {
		t.Errorf("steps of equal time cover %.4f to %.4f, want equal lengths", lo, hi)
	}
}

// Eased progress is a fraction of the curve's length, not of its
// parameter: on this line, whose controls crowd its end, the parameter's
// midpoint is at x 83.75.
func TestSpatialProgressIsArcLength(t *testing.T) {
	p, err := parsePoint(keys(`[0,0]`, `[100,0]`, `,"to":[90,0],"ti":[0,0]`), point{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []float32{2.5, 5, 7.5} {
		if got := p.sample(f); !near(got, point{f * 10, 0}, 0.05) {
			t.Errorf("frame %v = %v, want (%v,0)", f, got, f*10)
		}
	}
}

// Easing that overshoots, past the end or back before the start, holds
// the value at that end of the curve.
func TestSpatialOvershootClamps(t *testing.T) {
	p, err := parsePoint(keys(`[0,0]`, `[100,0]`, `,"to":[0,60],"ti":[0,60],"o":{"x":0.5,"y":-1},"i":{"x":0.5,"y":2}`), point{})
	if err != nil {
		t.Fatal(err)
	}
	ease := cubicBezier(0.5, -1, 0.5, 2)
	var before, past bool
	for f := float32(0); f <= 10; f += 0.25 {
		e, got := ease(f/10), p.sample(f)
		switch {
		case !finitePoint(got):
			t.Fatalf("frame %v = %v", f, got)
		case e < 0 && got != (point{0, 0}):
			t.Errorf("frame %v, progress %v: %v, want the start", f, e, got)
		case e > 1 && got != (point{100, 0}):
			t.Errorf("frame %v, progress %v: %v, want the end", f, e, got)
		}
		before, past = before || e < 0, past || e > 1
	}
	if !before || !past {
		t.Fatal("the easing does not overshoot both ends")
	}
}

// Curves of no length, or that stop on the way, draw without dividing by
// zero.
func TestSpatialDegenerateCurves(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, tangents string
		mid                      point
	}{
		{"still", `[5,5]`, `[5,5]`, `"to":[0,0],"ti":[0,0]`, point{5, 5}},
		{"out and back", `[5,5]`, `[5,5]`, `"to":[10,0],"ti":[10,0]`, point{12.5, 5}},
		{"too short to measure", `[5,5]`, `[5,5]`, `"to":[1e-45,0],"ti":[0,0]`, point{5, 5}},
		{"stopping midway", `[0,0]`, `[100,0]`, `"to":[100,0],"ti":[-100,0]`, point{50, 0}},
	} {
		p, err := parsePoint(keys(tc.from, tc.to, `,`+tc.tangents), point{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for f := float32(0); f <= 10; f += 0.25 {
			if got := p.sample(f); !finitePoint(got) {
				t.Fatalf("%s: frame %v = %v", tc.name, f, got)
			}
		}
		if got := p.sample(5); !near(got, tc.mid, 0.05) {
			t.Errorf("%s: frame 5 = %v, want %v", tc.name, got, tc.mid)
		}
	}
}

// Positions everywhere move along their curves: a layer's, a group's, a
// rectangle's and an ellipse's, and a layer's anchor; other values reject
// tangents.
func TestSpatialTangentsOnTransformsAndShapes(t *testing.T) {
	arch := `{"a":1,"k":[{"t":0,"s":[20,20],"e":[80,20],"to":[0,40],"ti":[0,40]},{"t":10,"s":[80,20]}]}`
	a := mustLoad(t, `{"ty":4,"ks":{"p":`+arch+`},"shapes":[{"ty":"rc","p":{"k":[0,0]},"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[1,0,0]}}]}`)
	if got := inkBounds(renderAt(a, 5)); got != image.Rect(45, 45, 55, 55) {
		t.Errorf("layer along its arch at frame 5: ink %v, want (45,45)-(55,55)", got)
	}
	a = mustLoad(t, `{"ty":4,"ks":{"a":`+arch+`},"shapes":[{"ty":"gr","it":[{"ty":"rc","p":`+arch+`,"s":{"k":[10,10]}},{"ty":"el","p":`+arch+`,"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[1,0,0]}},{"ty":"tr","p":`+arch+`}]}]}`)
	shapes := a.layers[0].groups[0].groups[0]
	for name, p := range map[string]property[point]{
		"anchor":    a.layers[0].transform.anchor,
		"group":     shapes.transform.position,
		"rectangle": shapes.geometry[0].(rectShape).position,
		"ellipse":   shapes.geometry[1].(ellipseShape).position,
	} {
		if got := p.sample(5); !near(got, point{50, 50}, 0.01) {
			t.Errorf("%s at frame 5 = %v, want the arch's top, (50,50)", name, got)
		}
	}
	for name, data := range map[string]string{
		"rotation": `{"ty":4,"ks":{"r":{"a":1,"k":[{"t":0,"s":[0],"to":[5],"ti":[0]},{"t":10,"s":[90]}]}}}`,
		"color":    `{"ty":4,"shapes":[{"ty":"rc","s":{"k":[10,10]}},{"ty":"fl","c":{"a":1,"k":[{"t":0,"s":[1,0,0],"to":[0,1,0],"ti":[0,0,0]},{"t":10,"s":[0,0,1]}]}}]}`,
	} {
		if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + data + `]}`)); err == nil || !strings.Contains(err.Error(), "unsupported spatial tangents") {
			t.Errorf("%s: error %v, want spatial tangents rejected", name, err)
		}
	}
}

// Positions split into x and y animate each on its own.
func TestSplitPosition(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"ks":{"p":{"s":true,"x":{"a":1,"k":[{"t":0,"s":[0],"e":[40]},{"t":10,"s":[40]}]},"y":{"a":0,"k":30}}},"shapes":[`+redRect+`]}`)
	if got := a.layers[0].transform.at(5); got.e != 20 || got.f != 30 {
		t.Fatalf("split position at frame 5 = (%v,%v), want (20,30)", got.e, got.f)
	}
	for _, p := range []string{`{"s":true,"x":{"k":0}}`, `{"s":true,"x":{"k":0,"x":"time"},"y":{"k":0}}`} {
		if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"ks":{"p":` + p + `}}]}`)); err == nil {
			t.Errorf("Load accepted the split position %s", p)
		}
	}
}

// Players draw curves and per-component easing at once on several
// goroutines: what Load measured, Draw only reads.
func TestSpatialAnimationDrawsConcurrently(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"ks":{"p":{"a":1,"k":[{"t":0,"s":[20,20],"e":[80,20],"to":[0,40],"ti":[0,40]},{"t":20,"s":[80,20]}]},"s":{"a":1,"k":[{"t":0,"s":[100,100,100],"e":[200,50,100],"o":{"x":[0.5,0.2,0],"y":[0,0,0]},"i":{"x":[0.5,0.8,1],"y":[1,1,1]}},{"t":20,"s":[200,50,100]}]}},"shapes":[{"ty":"rc","p":{"k":[0,0]},"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[1,0,0]}}]}`)
	want := renderAt(a, 7)
	images := make(chan *image.RGBA)
	for range 4 {
		go func() { images <- renderAt(a, 7) }()
	}
	for range 4 {
		if !samePixels(<-images, want) {
			t.Error("a concurrent Draw drew differently")
		}
	}
}
