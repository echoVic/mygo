package lottie

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
)

// line is an open path from (x0,y0) to (x1,y1), its tangents along it but
// not equal, so that its parameter does not run at an even speed: a trim
// still cuts it by length.
func line(x0, y0, x1, y1 float32) pathData {
	dx, dy := (x1-x0)*0.6, (y1-y0)*0.6
	return pathData{
		Vertices: []point{{x0, y0}, {x1, y1}},
		In:       []point{{}, {-dx / 6, -dy / 6}},
		Out:      []point{{dx, dy}, {}},
	}
}

// square is a closed path around (0,0)-(s,s), clockwise from (0,0).
func squarePath(s float32) pathData {
	return pathData{Vertices: []point{{0, 0}, {s, 0}, {s, s}, {0, s}}, In: make([]point, 4), Out: make([]point, 4), Closed: true}
}

func measured(paths ...pathData) outline {
	o := outline{paths: paths}
	o.measure()
	return o
}

// ends returns where a piece starts and ends.
func ends(p pathData) (point, point) { return p.Vertices[0], p.Vertices[len(p.Vertices)-1] }

// onLine reports whether every point and control point of p lies on the
// horizontal line y, between x0 and x1.
func onLine(p pathData, y, x0, x1 float32) bool {
	for i, v := range p.Vertices {
		for _, q := range []point{v, {v.X + p.In[i].X, v.Y + p.In[i].Y}, {v.X + p.Out[i].X, v.Y + p.Out[i].Y}} {
			if abs(q.Y-y) > 1e-4 || q.X < x0-1e-3 || q.X > x1+1e-3 {
				return false
			}
		}
	}
	return true
}

// A trim keeps the part of a line's length between its fractions,
// measured along the curve, not its parameter; the piece stays a cubic on
// the line.
func TestTrimCutsALineByLength(t *testing.T) {
	l := measured(line(0, 0, 100, 0))
	if got := l.length(); math.Abs(got-100) > 1e-4 {
		t.Fatalf("length %v, want 100", got)
	}
	for _, c := range []struct{ s, e, from, to float64 }{
		{0, 0.5, 0, 50}, {0.25, 0.75, 25, 75}, {0.5, 1, 50, 100}, {0.1, 0.2, 10, 20},
	} {
		got := l.trim(c.s, c.e)
		if len(got.paths) != 1 {
			t.Fatalf("[%v,%v]: %d pieces, want 1", c.s, c.e, len(got.paths))
		}
		p := got.paths[0]
		a, b := ends(p)
		if !near(a, point{float32(c.from), 0}, 1e-3) || !near(b, point{float32(c.to), 0}, 1e-3) {
			t.Errorf("[%v,%v]: piece from %v to %v, want %v to %v", c.s, c.e, a, b, c.from, c.to)
		}
		if p.Closed || len(p.Vertices) != 2 || !onLine(p, 0, float32(c.from), float32(c.to)) {
			t.Errorf("[%v,%v]: piece %+v, want one open cubic along the line", c.s, c.e, p)
		}
	}
}

// A circle's quarters are its four segments: an eighth of it ends half
// way along the first, at the parameter's middle by symmetry, and is that
// segment split there: the same curve.
func TestTrimCutsACircle(t *testing.T) {
	c := measured(ellipsePath(point{0, 0}, point{20, 20}))
	if got, want := c.length(), 2*math.Pi*10; math.Abs(got-want) > 0.01 {
		t.Fatalf("circle's length %v, want about %v", got, want)
	}
	quarter := c.trim(0, 0.25).paths
	if len(quarter) != 1 {
		t.Fatalf("a quarter is %d pieces", len(quarter))
	}
	if a, b := ends(quarter[0]); !near(a, point{0, -10}, 1e-3) || !near(b, point{10, 0}, 1e-3) {
		t.Errorf("a quarter runs from %v to %v, want from the top (0,-10) clockwise to (10,0)", a, b)
	}
	eighth := c.trim(0, 0.125).paths[0]
	if len(eighth.Vertices) != 2 {
		t.Fatalf("an eighth has %d vertices, want one segment", len(eighth.Vertices))
	}
	whole := c.paths[0].segment(0)
	half := whole.split(0, 0.5)
	got := eighth.segment(0)
	for i := range got {
		if math.Abs(got[i][0]-half[i][0]) > 1e-3 || math.Abs(got[i][1]-half[i][1]) > 1e-3 {
			t.Fatalf("an eighth's controls %v, want the first quarter's halved: %v", got, half)
		}
	}
	if x, y := got.point(1); math.Abs(x-10*math.Sqrt2/2) > 0.01 || math.Abs(y+10*math.Sqrt2/2) > 0.01 {
		t.Errorf("an eighth ends at (%v,%v), want 45° clockwise from the top", x, y)
	}
}

// Past its end, a trim wraps to the path's start: an open path in two
// pieces; a closed one in one, through its start point, without a cap.
func TestTrimWrapsAround(t *testing.T) {
	open := measured(line(0, 0, 100, 0))
	pieces := open.trim(0.75, 1.25).paths
	if len(pieces) != 2 {
		t.Fatalf("an open line's wrapped trim: %d pieces, want 2", len(pieces))
	}
	if a, b := ends(pieces[0]); !near(a, point{75, 0}, 1e-3) || !near(b, point{100, 0}, 1e-3) {
		t.Errorf("first piece %v to %v, want 75 to 100", a, b)
	}
	if a, b := ends(pieces[1]); !near(a, point{0, 0}, 1e-3) || !near(b, point{25, 0}, 1e-3) {
		t.Errorf("second piece %v to %v, want 0 to 25", a, b)
	}
	closed := measured(squarePath(100))
	joined := closed.trim(0.875, 1.125).paths
	if len(joined) != 1 || joined[0].Closed {
		t.Fatalf("a square's wrapped trim: %d pieces, want one open piece", len(joined))
	}
	want := []point{{0, 50}, {0, 0}, {50, 0}}
	if got := joined[0].Vertices; len(got) != len(want) {
		t.Fatalf("wrapped piece's vertices %v, want %v: up the left side, through the start, along the top", got, want)
	}
	for i, v := range want {
		if !near(joined[0].Vertices[i], v, 1e-3) {
			t.Fatalf("wrapped piece's vertices %v, want %v", joined[0].Vertices, want)
		}
	}
	// Past the end by a whole fraction, it is the range one length back.
	if a, b := ends(open.trim(1.25, 1.5).paths[0]); !near(a, point{25, 0}, 1e-3) || !near(b, point{50, 0}, 1e-3) {
		t.Errorf("[1.25,1.5]: piece %v to %v, want 25 to 50", a, b)
	}
}

// trimRange computes what lottie-web does: start and end clamped to
// [0, 100] percent, offset by the offset's turns, in order, to 1e-4.
func TestTrimRange(t *testing.T) {
	for _, c := range []struct {
		start, end, offset float32
		s, e               float64
	}{
		{0, 100, 0, 0, 1},
		{25, 75, 0, 0.25, 0.75},
		{75, 25, 0, 0.25, 0.75},
		{-20, 150, 0, 0, 1},
		{-20, 150, 90, 0.25, 1.25},
		{25, 75, 90, 0.5, 1},
		{50, 100, 90, 0.75, 1.25},
		{25, 75, -90, 1, 1.5},
		{25, 75, 720 + 36, 0.35, 0.85},
		{10, 10, 45, 0.225, 0.225},
		{33.333333, 66.666666, 0, 0.3333, 0.6667},
		{float32(math.NaN()), 50, 0, 0, 0.5},
	} {
		if s, e := trimRange(c.start, c.end, c.offset); math.Abs(s-c.s) > 1e-9 || math.Abs(e-c.e) > 1e-9 {
			t.Errorf("trimRange(%v, %v, %v) = %v, %v; want %v, %v", c.start, c.end, c.offset, s, e, c.s, c.e)
		}
	}
}

// What a trim keeps, from a trim's own values: s > e is e to s; s == e is
// nothing; 0 to 100 is the path as it was, closed; values past 0 to 100
// clamp.
func TestTrimApply(t *testing.T) {
	apply := func(start, end, offset float32, paths ...pathData) []outline {
		tr := trim{start: property[float32]{static: start}, end: property[float32]{static: end}, offset: property[float32]{static: offset}, hi: len(paths)}
		outlines := make([]outline, len(paths))
		for i, p := range paths {
			outlines[i] = outline{paths: []pathData{p}}
		}
		tr.apply(outlines, 0)
		return outlines
	}
	sq := squarePath(100)
	if got := apply(0, 100, 0, sq)[0].paths; len(got) != 1 || !got[0].Closed || &got[0].Vertices[0] != &sq.Vertices[0] {
		t.Errorf("0 to 100: %v, want the square itself, closed", got)
	}
	if got := apply(-50, 300, 0, sq)[0].paths; len(got) != 1 || !got[0].Closed {
		t.Errorf("-50 to 300: %v, want the square, closed", got)
	}
	for _, c := range []struct{ start, end, offset float32 }{{40, 40, 0}, {0, 0, 0}, {100, 100, 30}, {-10, -5, 0}} {
		if got := apply(c.start, c.end, c.offset, sq)[0].paths; len(got) != 0 {
			t.Errorf("%v to %v: %d pieces, want none", c.start, c.end, len(got))
		}
	}
	swapped, ordered := apply(75, 25, 0, sq)[0].paths, apply(25, 75, 0, sq)[0].paths
	if fmt.Sprint(swapped) != fmt.Sprint(ordered) {
		t.Errorf("75 to 25 = %v, want 25 to 75's %v", swapped, ordered)
	}
	// Over the whole length from an offset, a closed path draws open,
	// from the offset round to it: a quarter of the way, at (100,0).
	got := apply(0, 100, 90, sq)[0].paths
	if len(got) != 1 || got[0].Closed || !near(got[0].Vertices[0], point{100, 0}, 1e-3) || !near(got[0].Vertices[len(got[0].Vertices)-1], point{100, 0}, 1e-3) {
		t.Errorf("0 to 100 offset 90°: %v, want one open piece from (100,0) round to it", got)
	}
}

// Individually, a trim lays its paths end to end in their order, and cuts
// that one length.
func TestTrimIndividually(t *testing.T) {
	a, b := line(0, 0, 100, 0), line(0, 10, 300, 10)
	apply := func(start, end, offset float32) []outline {
		tr := trim{start: property[float32]{static: start}, end: property[float32]{static: end}, offset: property[float32]{static: offset}, individually: true, hi: 2}
		outlines := []outline{{paths: []pathData{a}}, {paths: []pathData{b}}}
		tr.apply(outlines, 0)
		return outlines
	}
	check := func(name string, got outline, want ...[2]float32) {
		t.Helper()
		if len(got.paths) != len(want) {
			t.Fatalf("%s: %d pieces, want %d", name, len(got.paths), len(want))
		}
		for i, w := range want {
			if from, to := ends(got.paths[i]); abs(from.X-w[0]) > 1e-3 || abs(to.X-w[1]) > 1e-3 {
				t.Errorf("%s: piece %d from %v to %v, want x %v to %v", name, i, from, to, w[0], w[1])
			}
		}
	}
	// 0 to 50% of 400: all of the first line, a third of the second.
	got := apply(0, 50, 0)
	check("0-50 first", got[0], [2]float32{0, 100})
	check("0-50 second", got[1], [2]float32{0, 100})
	// 25 to 75%: 100 to 300, which leaves the first line, ending at 100.
	got = apply(25, 75, 0)
	check("25-75 first", got[0])
	check("25-75 second", got[1], [2]float32{0, 200})
	// Offset by 270°: 75% round to 25%, the end of the second line and
	// all of the first.
	got = apply(0, 50, 270)
	check("wrapped first", got[0], [2]float32{0, 100})
	check("wrapped second", got[1], [2]float32{200, 300})
	// One path alone is trimmed as simultaneously: wrapping around a
	// closed path, its pieces join.
	one := trim{start: property[float32]{static: 50}, end: property[float32]{static: 100}, offset: property[float32]{static: 90}, individually: true, hi: 1}
	sq := []outline{{paths: []pathData{squarePath(100)}}}
	one.apply(sq, 0)
	if len(sq[0].paths) != 1 {
		t.Errorf("one closed path, individually, wrapping: %d pieces, want 1, joined", len(sq[0].paths))
	}
}

// Paths of no length, or with segments of none, measure and cut without
// dividing by zero: what has no length draws nothing when trimmed.
func TestTrimDegeneratePaths(t *testing.T) {
	dot := pathData{Vertices: []point{{5, 5}}, In: make([]point, 1), Out: make([]point, 1)}
	still := pathData{Vertices: []point{{5, 5}, {5, 5}}, In: make([]point, 2), Out: make([]point, 2), Closed: true}
	stutter := pathData{Vertices: []point{{0, 0}, {0, 0}, {100, 0}}, In: make([]point, 3), Out: make([]point, 3)}
	for name, p := range map[string]pathData{"dot": dot, "still": still} {
		o := measured(p)
		if o.length() != 0 {
			t.Errorf("%s: length %v, want 0", name, o.length())
		}
		for _, r := range [][2]float64{{0.25, 0.75}, {0.75, 1.25}, {0, 0.5}} {
			if got := o.trim(r[0], r[1]).paths; len(got) != 0 {
				t.Errorf("%s %v: %d pieces, want none", name, r, len(got))
			}
		}
		tr := trim{start: property[float32]{static: 0}, end: property[float32]{static: 50}, offset: property[float32]{static: 0}, individually: true, hi: 2}
		outlines := []outline{{paths: []pathData{p}}, {paths: []pathData{p}}}
		tr.apply(outlines, 0)
		if len(outlines[0].paths)+len(outlines[1].paths) != 0 {
			t.Errorf("%s individually: %v, want nothing", name, outlines)
		}
	}
	o := measured(stutter)
	got := o.trim(0, 0.5).paths
	if len(got) != 1 {
		t.Fatalf("stutter: %d pieces", len(got))
	}
	for _, v := range got[0].Vertices {
		if !finitePoint(v) {
			t.Fatalf("stutter: piece %v", got[0])
		}
	}
	if _, b := ends(got[0]); !near(b, point{50, 0}, 1e-3) {
		t.Errorf("stutter: piece ends at %v, want (50,0)", b)
	}
}

// strokeLayer is a shape layer that strokes shapes black, 4 wide, with
// butt caps, after the items between.
func strokeLayer(items string) string {
	return `{"ty":4,"shapes":[` + items + `,{"ty":"st","lc":1,"c":{"k":[0,0,0]},"w":{"k":4}}]}`
}

// hline is a path from (10,40) to (90,40).
const hline = `{"ty":"sh","ks":{"k":{"i":[[0,0],[0,0]],"o":[[0,0],[0,0]],"v":[[10,40],[90,40]],"c":false}}}`

func tm(s, e, o string) string {
	return `{"ty":"tm","s":{"k":` + s + `},"e":{"k":` + e + `},"o":{"k":` + o + `},"m":1}`
}

// inkIn counts ink pixels within r.
func inkIn(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if p := img.RGBAAt(x, y); p.R < 248 || p.G < 248 || p.B < 248 {
				n++
			}
		}
	}
	return n
}

// A stroke trimmed to its first half has ink on that side only.
func TestTrimmedStrokeDrawsHalf(t *testing.T) {
	img := renderAt(mustLoad(t, strokeLayer(hline+`,`+tm(`0`, `50`, `0`))), 0)
	if got := inkBounds(img); got != image.Rect(10, 38, 50, 42) {
		t.Errorf("half a line: ink %v, want (10,38)-(50,42)", got)
	}
	// Animated, it draws on.
	a := mustLoad(t, strokeLayer(hline+`,{"ty":"tm","s":{"k":0},"e":{"a":1,"k":[{"t":0,"s":[0]},{"t":10,"s":[100]}]},"o":{"k":0}}`))
	for _, c := range []struct {
		frame float32
		x     int
	}{{2.5, 30}, {5, 50}, {10, 90}} {
		if got := inkBounds(renderAt(a, c.frame)); got.Max.X != c.x || got.Min.X != 10 {
			t.Errorf("frame %v: ink %v, want from 10 to %d", c.frame, got, c.x)
		}
	}
	if n := countInk(renderAt(a, 0)); n != 0 {
		t.Errorf("frame 0, trimmed to nothing: %d ink pixels", n)
	}
}

// A trim cuts the geometry before it in its group, and in the groups
// before it, whatever their transforms; not what comes after it.
func TestTrimReachesNestedGroups(t *testing.T) {
	nested := `{"ty":"gr","it":[` + hline + `,{"ty":"tr","p":{"k":[0,-20]}}]}`
	img := renderAt(mustLoad(t, strokeLayer(nested+`,`+tm(`0`, `50`, `0`)+`,`+hline)), 0)
	if got := inkIn(img, image.Rect(0, 15, 100, 25)); got == 0 || inkIn(img, image.Rect(52, 15, 100, 25)) != 0 {
		t.Errorf("the nested line, moved up 20: %d ink pixels, some past its middle", got)
	}
	if got := inkBounds(image.NewRGBA(image.Rect(0, 0, 0, 0))); got != image.Rect(0, 0, 0, 0) {
		t.Fatal(got)
	}
	// The line after the trim is whole.
	if inkIn(img, image.Rect(85, 38, 90, 42)) == 0 {
		t.Error("the line after the trim was trimmed")
	}
}

// A trim's cut paths are what every style draws, the fills before it in
// the list and the strokes after it alike; a fill closes an open piece.
func TestTrimCutsFillsAndStrokes(t *testing.T) {
	disc := `{"ty":"el","d":1,"p":{"k":[50,40]},"s":{"k":[60,60]}}`
	fill := `{"ty":"fl","c":{"k":[1,0,0]}}`
	// The first half of a circle, clockwise from its top: its right half.
	for name, shapes := range map[string]string{
		"fill before": disc + `,` + fill + `,` + tm(`0`, `50`, `0`),
		"fill after":  disc + `,` + tm(`0`, `50`, `0`) + `,` + fill,
	} {
		img := renderAt(mustLoad(t, `{"ty":4,"shapes":[`+shapes+`]}`), 0)
		if got := inkBounds(img); got != image.Rect(50, 10, 80, 70) {
			t.Errorf("%s: ink %v, want the right half disc, (50,10)-(80,70)", name, got)
		}
		if got := img.RGBAAt(65, 40); got.R != 255 || got.G != 0 {
			t.Errorf("%s: (65,40) = %v, want red", name, got)
		}
	}
}

// Rectangles start at their top right corner and run clockwise, down
// their right side, and d 3 reverses them, along their top; ellipses
// start at their top, and run clockwise, d 3 counterclockwise. Missing,
// d is 1.
func TestTrimShowsWhereShapesStart(t *testing.T) {
	for _, c := range []struct {
		name, shape string
		want        image.Rectangle
	}{
		{"rectangle", `{"ty":"rc","d":1,"p":{"k":[50,40]},"s":{"k":[40,40]},"r":{"k":0}}`, image.Rect(68, 20, 72, 60)},
		{"rectangle without d", `{"ty":"rc","p":{"k":[50,40]},"s":{"k":[40,40]},"r":{"k":0}}`, image.Rect(68, 20, 72, 60)},
		{"rectangle d 2", `{"ty":"rc","d":2,"p":{"k":[50,40]},"s":{"k":[40,40]},"r":{"k":0}}`, image.Rect(68, 20, 72, 60)},
		{"reversed rectangle", `{"ty":"rc","d":3,"p":{"k":[50,40]},"s":{"k":[40,40]},"r":{"k":0}}`, image.Rect(30, 18, 70, 22)},
		{"mirrored rectangle", `{"ty":"rc","d":1,"p":{"k":[50,40]},"s":{"k":[-40,40]},"r":{"k":0}}`, image.Rect(28, 20, 32, 60)},
		{"ellipse", `{"ty":"el","d":1,"p":{"k":[50,40]},"s":{"k":[40,40]}}`, image.Rect(50, 18, 72, 40)},
		{"reversed ellipse", `{"ty":"el","d":3,"p":{"k":[50,40]},"s":{"k":[40,40]}}`, image.Rect(28, 18, 50, 40)},
	} {
		// A quarter of the rectangle, a side; of the ellipse, an arc.
		got := inkBounds(renderAt(mustLoad(t, strokeLayer(c.shape+`,`+tm(`0`, `25`, `0`))), 0))
		if d := got.Min.Sub(c.want.Min); abs(float32(d.X)) > 1 || abs(float32(d.Y)) > 1 || abs(float32(got.Max.X-c.want.Max.X)) > 1 || abs(float32(got.Max.Y-c.want.Max.Y)) > 1 {
			t.Errorf("%s: a quarter's ink %v, want about %v", c.name, got, c.want)
		}
	}
	// Rounded, a rectangle starts below its corner's curve, as lottie-web's.
	r := rectPath(point{50, 40}, point{40, 40}, 5)
	if !r.Closed || r.Vertices[0] != (point{70, 25}) || r.Vertices[1] != (point{70, 55}) {
		t.Errorf("rounded rectangle starts %v, then %v; want (70,25), then (70,55)", r.Vertices[0], r.Vertices[1])
	}
}

// Several trims in a group cut in turn, each what the one before left.
func TestSeveralTrimsCompose(t *testing.T) {
	img := renderAt(mustLoad(t, strokeLayer(hline+`,`+tm(`0`, `50`, `0`)+`,`+tm(`0`, `50`, `0`))), 0)
	if got := inkBounds(img); got != image.Rect(10, 38, 30, 42) {
		t.Errorf("half of half a line: ink %v, want (10,38)-(30,42)", got)
	}
	// Nested: the inner group's trim first, then the outer one.
	nested := `{"ty":"gr","it":[` + hline + `,` + tm(`50`, `100`, `0`) + `]}`
	if got := inkBounds(renderAt(mustLoad(t, strokeLayer(nested+`,`+tm(`0`, `50`, `0`))), 0)); got != image.Rect(50, 38, 70, 42) {
		t.Errorf("an outer trim of an inner one: ink %v, want (50,38)-(70,42)", got)
	}
}

// A style draws all the geometry before it in its group, and in the
// groups before it, under their transforms, but not their opacity; its
// width is its own group's. The first in the list draws on top, nested
// groups' styles included.
func TestStylesDrawNestedGeometry(t *testing.T) {
	// A red square in a group that moves it 40 right and is transparent,
	// drawn by a blue fill outside it.
	nested := `{"ty":"gr","it":[{"ty":"rc","p":{"k":[20,40]},"s":{"k":[20,20]}},{"ty":"tr","p":{"k":[40,0]},"o":{"k":0}}]}`
	img := renderAt(mustLoad(t, `{"ty":4,"shapes":[`+nested+`,{"ty":"fl","c":{"k":[0,0,1]}}]}`), 0)
	if got := inkBounds(img); got != image.Rect(50, 30, 70, 50) {
		t.Errorf("outer fill of a nested square: ink %v, want (50,30)-(70,50)", got)
	}
	if got := img.RGBAAt(60, 40); got.B != 255 || got.R != 0 {
		t.Errorf("outer fill: %v, want opaque blue", got)
	}
	// The nested group's own fill draws on top of the outer one.
	own := `{"ty":"gr","it":[{"ty":"rc","p":{"k":[50,40]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[1,0,0]}}]}`
	img = renderAt(mustLoad(t, `{"ty":4,"shapes":[`+own+`,{"ty":"rc","p":{"k":[50,40]},"s":{"k":[40,40]}},{"ty":"fl","c":{"k":[0,0,1]}}]}`), 0)
	if got := img.RGBAAt(50, 40); got.R != 255 || got.B != 0 {
		t.Errorf("nested fill over outer fill: center %v, want red", got)
	}
	if got := img.RGBAAt(35, 40); got.B != 255 || got.R != 0 {
		t.Errorf("outer fill around it: %v, want blue", got)
	}
	// A stroke's width is its own group's: a nested group that doubles
	// its line doesn't widen it.
	doubled := `{"ty":"gr","it":[{"ty":"sh","ks":{"k":{"i":[[0,0],[0,0]],"o":[[0,0],[0,0]],"v":[[10,20],[40,20]],"c":false}}},{"ty":"tr","s":{"k":[200,200]}}]}`
	if got := inkBounds(renderAt(mustLoad(t, strokeLayer(doubled)), 0)); got != image.Rect(20, 38, 80, 42) {
		t.Errorf("stroke of a doubled line: ink %v, want (20,38)-(80,42), 4 wide", got)
	}
}

// A style draws only the geometry before it, and several draw each, the
// first on top.
func TestSeveralStylesInAGroup(t *testing.T) {
	a := `{"ty":"rc","p":{"k":[30,40]},"s":{"k":[20,20]}}`
	b := `{"ty":"rc","p":{"k":[70,40]},"s":{"k":[20,20]}}`
	img := renderAt(mustLoad(t, `{"ty":4,"shapes":[`+a+`,{"ty":"fl","c":{"k":[1,0,0]}},`+b+`,{"ty":"fl","c":{"k":[0,0,1]}}]}`), 0)
	if got := img.RGBAAt(30, 40); got.R != 255 || got.B != 0 {
		t.Errorf("first square, red over blue: %v", got)
	}
	if got := img.RGBAAt(70, 40); got.B != 255 || got.R != 0 {
		t.Errorf("second square, blue only: %v", got)
	}
	// Two strokes of one line: the wider, listed second, under the first.
	img = renderAt(mustLoad(t, `{"ty":4,"shapes":[`+hline+`,{"ty":"st","c":{"k":[1,0,0]},"w":{"k":2}},{"ty":"st","c":{"k":[0,0,1]},"w":{"k":8}}]}`), 0)
	if got, edge := img.RGBAAt(50, 40), img.RGBAAt(50, 37); got.R != 255 || edge.B != 255 || edge.R != 0 {
		t.Errorf("two strokes: center %v, edge %v; want red over blue", got, edge)
	}
}

// Load retains a static trim of static geometry, and makes the outlines
// of an animated one each frame from the measures Load retained.
func TestStaticTrimIsRetained(t *testing.T) {
	static := `{"ty":"gr","it":[` + hline + `,` + tm(`0`, `50`, `0`) + `,{"ty":"st","c":{"k":[0,0,0]},"w":{"k":2}}]}`
	a := mustLoad(t, `{"ty":4,"shapes":[`+static+`]}`)
	sh := &a.layers[0].shapes
	if sh.animated || len(sh.trims) != 1 || sh.trims[0].animated {
		t.Fatalf("a static trim: animated %v, trims %d", sh.animated, len(sh.trims))
	}
	want := sh.outlines[0].paths
	if len(want) != 1 || !near(want[0].Vertices[1], point{50, 40}, 1e-3) {
		t.Fatalf("retained %v, want the line's first half", want)
	}
	for _, frame := range []float32{0, 7, 19} {
		if got := sh.outlinesAt(frame); &got[0] != &sh.outlines[0] {
			t.Fatalf("frame %v: outlines made again", frame)
		}
	}
	// Beside an animated trim, the static one's pieces stay the same.
	animated := `{"ty":"gr","it":[` + hline + `,{"ty":"tm","s":{"k":0},"e":{"a":1,"k":[{"t":0,"s":[0]},{"t":10,"s":[100]}]},"o":{"k":0}},{"ty":"st","c":{"k":[0,0,0]},"w":{"k":2}}]}`
	a = mustLoad(t, `{"ty":4,"shapes":[`+static+`,`+animated+`]}`)
	sh = &a.layers[0].shapes
	if !sh.animated || sh.trims[0].animated || !sh.trims[1].animated {
		t.Fatalf("animated %v, trims animated %v, %v; want true, false, true", sh.animated, sh.trims[0].animated, sh.trims[1].animated)
	}
	if sh.outlines[1].lengths == nil {
		t.Error("the animated trim's static line was not measured at Load")
	}
	first, second := sh.outlinesAt(3), sh.outlinesAt(8)
	if &first[0].paths[0] != &sh.outlines[0].paths[0] || &second[0].paths[0] != &sh.outlines[0].paths[0] {
		t.Error("the static trim's pieces were made again")
	}
	if _, b := ends(first[1].paths[0]); !near(b, point{34, 40}, 1e-3) {
		t.Errorf("frame 3: the animated trim ends at %v, want (34,40)", b)
	}
	if _, b := ends(second[1].paths[0]); !near(b, point{74, 40}, 1e-3) {
		t.Errorf("frame 8: the animated trim ends at %v, want (74,40)", b)
	}
	if &sh.outlines[1].paths[0].Vertices[0] == &first[1].paths[0].Vertices[0] || len(sh.outlines[1].paths[0].Vertices) != 2 {
		t.Error("an animated trim changed what Load retained")
	}
}

func TestLoadRejectsBadTrims(t *testing.T) {
	many := hline
	for range maxTrims + 1 {
		many += `,` + tm(`0`, `100`, `0`)
	}
	for name, tc := range map[string]struct{ shapes, want string }{
		"mode":       {hline + `,{"ty":"tm","m":3}`, "unsupported trim mode"},
		"expression": {hline + `,{"ty":"tm","s":{"k":0,"x":"time"}}`, "unsupported expression"},
		"start":      {hline + `,{"ty":"tm","s":{"k":"0"}}`, "trim start"},
		"too many":   {many, fmt.Sprintf("more than %d trims", maxTrims)},
		"direction":  {`{"ty":"rc","d":4,"s":{"k":[10,10]}}`, "unsupported shape direction"},
		"path d":     {`{"ty":"sh","d":3,"ks":{"k":{"i":[[0,0]],"o":[[0,0]],"v":[[0,0]],"c":false}}}`, "unsupported path direction"},
	} {
		_, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"shapes":[` + tc.shapes + `]}]}`))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
	}
	// As many as maxTrims load; a hidden one doesn't count, nor cut.
	many = hline
	for range maxTrims {
		many += `,` + tm(`0`, `100`, `0`)
	}
	mustLoad(t, `{"ty":4,"shapes":[`+many+`]}`)
	hidden := `{"ty":"tm","hd":true,"s":{"k":0},"e":{"k":50},"o":{"k":0}}`
	if got := inkBounds(renderAt(mustLoad(t, strokeLayer(hline+`,`+hidden)), 0)); got != image.Rect(10, 38, 90, 42) {
		t.Errorf("hidden trim: ink %v, want the whole line", got)
	}
	// Individually is m 2, written as a whole float too.
	if a := mustLoad(t, `{"ty":4,"shapes":[`+hline+`,{"ty":"tm","m":2.0}]}`); !a.layers[0].trims[0].individually {
		t.Error("m 2.0 is not individually")
	}
}

// What styles paint is bounded: each paints the shapes and groups before
// it, and at most maxStyles may paint one, an empty group too, which
// each walks. n shapes followed by n fills would paint n² every frame.
func TestLoadBoundsPainting(t *testing.T) {
	shapes := func(item string, n, styles int) string {
		items := []string{}
		for range n {
			items = append(items, item)
		}
		for range styles {
			items = append(items, `{"ty":"fl","c":{"k":[1,0,0]}}`)
		}
		return strings.Join(items, ",")
	}
	want := fmt.Sprintf("more than %d styles", maxStyles)
	for name, item := range map[string]string{"shapes": `{"ty":"rc","s":{"k":[1,1]}}`, "groups": `{"ty":"gr","it":[]}`} {
		if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"shapes":[` + shapes(item, 100, maxStyles+1) + `]}]}`)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%d fills of 100 %s: error %v, want %q", maxStyles+1, name, err, want)
		}
		mustLoad(t, `{"ty":4,"shapes":[`+shapes(item, 100, maxStyles)+`]}`)
	}
	// Styles in nested groups paint what is before them there, and those
	// outside what is before them outside: they add up where they meet.
	nested := `{"ty":"gr","it":[` + shapes(`{"ty":"rc","s":{"k":[1,1]}}`, 1, maxStyles/2) + `]}`
	mustLoad(t, `{"ty":4,"shapes":[`+shapes(nested, 1, maxStyles/2)+`]}`)
	if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"shapes":[` + shapes(nested, 1, maxStyles/2+1) + `]}]}`)); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("nested styles: error %v, want %q", err, want)
	}
}

// Players draw trims, animated and static, at once on several goroutines:
// Draw only reads what Load made, and makes the rest anew.
func TestTrimmedAnimationDrawsConcurrently(t *testing.T) {
	animated := `{"ty":"tm","s":{"a":1,"k":[{"t":0,"s":[0]},{"t":20,"s":[60]}]},"e":{"a":1,"k":[{"t":0,"s":[20]},{"t":20,"s":[100]}]},"o":{"a":1,"k":[{"t":0,"s":[0]},{"t":20,"s":[180]}]},"m":2}`
	a := mustLoad(t, `{"ty":4,"shapes":[{"ty":"gr","it":[{"ty":"el","p":{"k":[30,40]},"s":{"k":[30,30]}},{"ty":"rc","p":{"a":1,"k":[{"t":0,"s":[60,40]},{"t":20,"s":[70,40]}]},"s":{"k":[20,30]}},`+animated+`,{"ty":"st","c":{"k":[0,0,0]},"w":{"k":3}}]},`+
		`{"ty":"gr","it":[`+hline+`,`+tm(`10`, `60`, `30`)+`,{"ty":"fl","c":{"k":[1,0,0]}}]},{"ty":"st","c":{"k":[0,0,1]},"w":{"k":1}}]}`)
	for _, frame := range []float32{4, 13} {
		want := renderAt(a, frame)
		images := make(chan *image.RGBA)
		for range 4 {
			go func() { images <- renderAt(a, frame) }()
		}
		for range 4 {
			if !samePixels(<-images, want) {
				t.Errorf("frame %v: a concurrent Draw drew differently", frame)
			}
		}
	}
}
