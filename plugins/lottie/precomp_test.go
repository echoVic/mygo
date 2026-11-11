package lottie

import (
	"fmt"
	"image"
	"strings"
	"sync/atomic"
	"testing"
)

// loadDoc loads a 100×80 composition at 10 frames a second, from frame 0
// to op, of layers, with assets.
func loadDoc(op int, layers, assets string) (*Animation, error) {
	return Load([]byte(fmt.Sprintf(`{"w":100,"h":80,"fr":10,"ip":0,"op":%d,"layers":[%s],"assets":[%s]}`, op, layers, assets)))
}

func mustLoadDoc(t *testing.T, op int, layers, assets string) *Animation {
	t.Helper()
	a, err := loadDoc(op, layers, assets)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// showing is a precomposition layer of the asset id, 100×80, starting
// at frame 0, with the fields more, each after a comma.
func showing(id, more string) string {
	return fmt.Sprintf(`{"ty":0,"refId":%q,"w":100,"h":80,"st":0%s}`, id, more)
}

// asset is a precomposition asset of layers.
func asset(id, layers string) string { return fmt.Sprintf(`{"id":%q,"layers":[%s]}`, id, layers) }

// mover is a 10×10 red square layer, from (0,0), moving right 4 DIPs a
// frame from frame 0 to 10, and holding there: at x 4f at frame f.
const mover = `{"ty":4,"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"e":[40,0]},{"t":10,"s":[40,0]}]}},` + square + `}`

// A precomposition layer draws its asset's layers, a composition of their
// own: their order, their parent links among themselves and their in and
// out points.
func TestPrecompositionDrawsItsLayers(t *testing.T) {
	a := mustLoadDoc(t, 20, showing("sq", `,"ks":{"p":{"k":[20,30]}}`), asset("sq", `{"ty":4,`+square+`}`))
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(20, 30, 30, 40) {
		t.Errorf("square moved by its precomposition: ink %v, want (20,30)-(30,40)", got)
	}
	// The first layer is on top, inside as outside.
	blue := `{"ty":4,"shapes":[{"ty":"rc","p":{"k":[10,10]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[0,0,1]}}]}`
	a = mustLoadDoc(t, 20, showing("two", ""), asset("two", `{"ty":4,`+square+`},`+blue))
	if got := renderAt(a, 0).RGBAAt(5, 5); got.R != 255 || got.B != 0 {
		t.Errorf("inner layers' order: (5,5) is %v, want the first layer's red on top", got)
	}
	// Inside, a parent link names the asset's layers, whatever the
	// composition around has: the null of ind 1 inside moves the square.
	inner := `{"ty":4,"parent":1,` + square + `},{"ty":3,"ind":1,"ks":{"p":{"k":[30,0]}}}`
	a = mustLoadDoc(t, 20, `{"ty":3,"ind":1,"ks":{"p":{"k":[0,50]}}},`+showing("linked", ""), asset("linked", inner))
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(30, 0, 40, 10) {
		t.Errorf("inner parent link: ink %v, want (30,0)-(40,10)", got)
	}
	// Inner in and out points are in the asset's frames.
	a = mustLoadDoc(t, 20, showing("brief", ""), asset("brief", `{"ty":4,"ip":5,"op":10,`+square+`}`))
	for frame, want := range map[float32]bool{4: false, 5: true, 9.5: true, 10: false} {
		if got := countInk(renderAt(a, frame)) > 0; got != want {
			t.Errorf("inner layer from 5 to 10, at frame %v: shows %v, want %v", frame, got, want)
		}
	}
}

// Inside a precomposition layer time is shifted by the layer's start time
// and divided by its stretch, (frame - st) / sr, as lottie-web's
// CompElement.prepareFrame has it, for the inner layers' keyframes and
// their in and out points alike. Their own start times cancel out, as at
// the top. The layer's own transform is in the frames around it.
func TestPrecompositionTime(t *testing.T) {
	brief := `{"ty":4,"ip":2,"op":6,"st":4,"shapes":[{"ty":"rc","p":{"k":[95,5]},"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[0,0,1]}}]}`
	moved := strings.Replace(mover, `{"ty":4,`, `{"ty":4,"st":3,`, 1)
	a := mustLoadDoc(t, 100, showing("move", `,"st":5,"sr":2,"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"e":[0,40]},{"t":20,"s":[0,40]}]}}`), asset("move", moved+`,`+brief))
	for _, c := range []struct {
		frame float32
		x, y  int
		brief bool
	}{
		// Inner frame (F-5)/2, the square at x 4 times that, from 0 to
		// 40; the layer's own position at y 2F, until 40.
		{0, 0, 0, false},
		{5, 0, 10, false},
		{8, 6, 16, false},
		{9, 8, 18, true},
		{10, 10, 20, true},
		{15, 20, 30, true},
		{16.5, 23, 33, true},
		{17, 24, 34, false},
		{25, 40, 40, false},
	} {
		img := renderAt(a, c.frame)
		if got := inkBounds(img).Min; got.X != c.x || got.Y != c.y {
			t.Errorf("frame %v: square at %v, want (%d,%d)", c.frame, got, c.x, c.y)
		}
		// The inner layer from 2 to 6 shows from outer frame 9 to 17.
		if got := countColor(img, 0, 0, 255) > 0; got != c.brief {
			t.Errorf("frame %v: the inner layer from 2 to 6 shows %v, want %v", c.frame, got, c.brief)
		}
	}
	// A negative stretch runs time backwards: here 10 - F.
	a = mustLoadDoc(t, 100, showing("move", `,"st":10,"sr":-1`), asset("move", mover))
	for frame, x := range map[float32]int{0: 40, 2.5: 30, 5: 20, 10: 0} {
		if got := inkBounds(renderAt(a, frame)).Min.X; got != x {
			t.Errorf("reversed, frame %v: x %d, want %d", frame, got, x)
		}
	}
}

// Time remapping (tm) gives the inner frame instead, in seconds, which the
// root composition's frame rate makes frames; the layer's start time and
// stretch don't count then. At the layer's out point, it shows the frame
// before, as lottie-web does.
func TestPrecompositionTimeRemap(t *testing.T) {
	// Asset "move", which says it is at 60 frames a second, runs at the
	// root's 10: tm from 1 s down to 0 s over frames 0 to 10 is inner
	// frame 10 down to 0.
	moving := `{"id":"move","fr":60,"layers":[` + mover + `]}`
	a := mustLoadDoc(t, 100, showing("move", `,"st":7,"sr":3,"tm":{"a":1,"k":[{"t":0,"s":[1],"e":[0]},{"t":10,"s":[0]}]}`), moving)
	for frame, x := range map[float32]int{0: 40, 2.5: 30, 5: 20, 10: 0, 15: 0} {
		if got := inkBounds(renderAt(a, frame)).Min.X; got != x {
			t.Errorf("remapped, frame %v: x %d, want %d", frame, got, x)
		}
	}
	// A static remapping holds one frame.
	a = mustLoadDoc(t, 100, showing("move", `,"tm":{"a":0,"k":0.25}`), moving)
	for _, frame := range []float32{0, 7, 30} {
		if got := inkBounds(renderAt(a, frame)).Min.X; got != 10 {
			t.Errorf("held at 0.25 s, frame %v: x %d, want 10", frame, got)
		}
	}
	// The inner layer ends at frame 30. Remapped to the precomposition
	// layer's out point, 30, it shows frame 29; remapped past it, nothing.
	ending := asset("end", `{"ty":4,"ip":0,"op":30,`+square+`}`)
	for tm, shows := range map[string]bool{`2.95`: true, `3`: true, `3.1`: false} {
		a = mustLoadDoc(t, 100, showing("end", `,"ip":0,"op":30,"tm":{"k":`+tm+`}`), ending)
		if got := countInk(renderAt(a, 10)) > 0; got != shows {
			t.Errorf("remapped to %s s, the layer that ends at 3 s: shows %v, want %v", tm, got, shows)
		}
	}
}

// The precomposition layer's transform and its parents' place what is
// inside; its opacity multiplies into the inner layers', each shape's on
// its own, as lottie-web's canvas renderer has it.
func TestPrecompositionPlacementAndOpacity(t *testing.T) {
	// Around its anchor (5,5), doubled, turned a quarter, at (50,40), and
	// moved 10 right by its parent: the square is at (50..70, 30..50).
	layers := showing("sq", `,"parent":9,"ks":{"a":{"k":[5,5]},"p":{"k":[50,40]},"r":{"k":90},"s":{"k":[200,200]}}`) + `,{"ty":3,"ind":9,"ks":{"p":{"k":[10,0]}}}`
	a := mustLoadDoc(t, 20, layers, asset("sq", `{"ty":4,`+square+`}`))
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(50, 30, 70, 50) {
		t.Errorf("placed square: ink %v, want (50,30)-(70,50)", got)
	}
	// Half transparent, around a half transparent layer: a quarter.
	a = mustLoadDoc(t, 20, showing("sq", `,"ks":{"o":{"k":50}}`), asset("sq", `{"ty":4,"ks":{"o":{"k":50}},`+square+`}`))
	if got := renderAt(a, 0).RGBAAt(5, 5); got.R != 255 || got.G < 189 || got.G > 193 {
		t.Errorf("a quarter opaque: %v, want light pink", got)
	}
	// Shapes that overlap inside a translucent precomposition show through
	// each other: each is faded, not the group as one image.
	overlapping := `{"ty":4,"shapes":[{"ty":"rc","p":{"k":[20,20]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[1,0,0]}}]},` +
		`{"ty":4,"shapes":[{"ty":"rc","p":{"k":[30,20]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[1,0,0]}}]}`
	img := renderAt(mustLoadDoc(t, 20, showing("two", `,"ks":{"o":{"k":50}}`), asset("two", overlapping)), 0)
	if one, both := img.RGBAAt(15, 20), img.RGBAAt(25, 20); !(both.G < one.G-30) {
		t.Errorf("overlap %v, one shape %v: want the overlap darker", both, one)
	}
	// Transparent, it draws nothing.
	if n := countInk(renderAt(mustLoadDoc(t, 20, showing("sq", `,"ks":{"o":{"k":0}}`), asset("sq", `{"ty":4,`+square+`}`)), 0)); n != 0 {
		t.Errorf("transparent precomposition: %d ink pixels, want none", n)
	}
}

// What is inside a precomposition shows only within its w×h rectangle
// from its origin, under its transform: turned, the rectangle turns.
// Nested ones' rectangles intersect.
func TestPrecompositionClipsToItsRectangle(t *testing.T) {
	red := asset("red", `{"ty":1,"sw":300,"sh":300,"sc":"#ff0000","ks":{"p":{"k":[-100,-100]}}}`)
	clipped := func(fields string) string {
		return fmt.Sprintf(`{"ty":0,"refId":"red","st":0%s}`, fields)
	}
	a := mustLoadDoc(t, 20, clipped(`,"w":40,"h":30,"ks":{"p":{"k":[10,20]}}`), red)
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(10, 20, 50, 50) {
		t.Errorf("clipped: ink %v, want (10,20)-(50,50)", got)
	}
	// Turned 45° around its center, (50,40), the 40×40 rectangle is a
	// diamond: what is beyond its edges, though within the square it was,
	// doesn't show.
	a = mustLoadDoc(t, 20, clipped(`,"w":40,"h":40,"ks":{"a":{"k":[20,20]},"p":{"k":[50,40]},"r":{"k":45}}`), red)
	img := renderAt(a, 0)
	for _, c := range []struct {
		x, y int
		in   bool
	}{{50, 40, true}, {50, 15, true}, {74, 40, true}, {33, 23, false}, {67, 57, false}, {31, 40, true}, {50, 69, false}} {
		if got := img.RGBAAt(c.x, c.y); (got.G < 128) != c.in {
			t.Errorf("turned rectangle: (%d,%d) is %v, want inside %v", c.x, c.y, got, c.in)
		}
	}
	if got := inkBounds(img); !got.In(image.Rect(20, 10, 80, 70)) || !image.Rect(25, 15, 75, 65).In(got) {
		t.Errorf("turned rectangle: ink %v, want about (22,12)-(78,68)", got)
	}
	// Nested: the inner one's rectangle at (30,30) inside the outer's
	// at (0,0), both 60×60, leaves their intersection.
	nested := asset("outer", `{"ty":0,"refId":"red","w":60,"h":60,"st":0,"ks":{"p":{"k":[30,30]}}}`) + `,` + red
	a = mustLoadDoc(t, 20, `{"ty":0,"refId":"outer","w":60,"h":60,"st":0}`, nested)
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(30, 30, 60, 60) {
		t.Errorf("nested: ink %v, want (30,30)-(60,60)", got)
	}
	// A rectangle of no size shows nothing.
	for _, size := range []string{`,"w":0,"h":30`, `,"w":40,"h":0`} {
		if n := countInk(renderAt(mustLoadDoc(t, 20, clipped(size), red), 0)); n != 0 {
			t.Errorf("%s: %d ink pixels, want none", size, n)
		}
	}
}

// Precompositions nest: each one's time is its own layer's, from the
// frames of the composition it is in.
func TestNestedPrecompositions(t *testing.T) {
	assets := asset("outer", `{"ty":0,"refId":"move","w":100,"h":80,"st":2,"ks":{"p":{"k":[0,10]}}},`+
		`{"ty":4,"shapes":[{"ty":"rc","p":{"k":[95,5]},"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[0,0,1]}}]}`) + `,` + asset("move", mover)
	a := mustLoadDoc(t, 100, showing("outer", `,"st":3,"ks":{"p":{"k":[0,20]}}`), assets)
	// Outer frame F is frame F-3 of "outer" and F-5 of "move": the square
	// is at x 4(F-5), 30 down.
	for frame, x := range map[float32]int{0: 0, 5: 0, 7.5: 10, 10: 20, 20: 40} {
		img := renderAt(a, frame)
		if got := inkBounds(img).Min; got.X != x || got.Y != 20 {
			t.Errorf("frame %v: ink from %v, want (%d,20)", frame, got, x)
		}
		if got := img.RGBAAt(95, 25); got.B != 255 || got.R != 0 {
			t.Errorf("frame %v: the outer asset's own layer at (95,25) is %v, want blue", frame, got)
		}
	}
}

// Precomposition layers have parents, and are parents, like any layer: a
// layer parented to one moves with its transform, but isn't clipped by
// its rectangle, which is only what is inside's.
func TestPrecompositionParenting(t *testing.T) {
	sq := asset("sq", `{"ty":4,`+square+`}`)
	a := mustLoadDoc(t, 20, `{"ty":3,"ind":1,"ks":{"p":{"k":[30,0]}}},`+showing("sq", `,"parent":1`), sq)
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(30, 0, 40, 10) {
		t.Errorf("precomposition under a null: ink %v, want (30,0)-(40,10)", got)
	}
	child := `{"ty":4,"parent":2,"ks":{"p":{"k":[0,20]}},"shapes":[{"ty":"rc","p":{"k":[5,5]},"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[0,0,1]}}]}`
	a = mustLoadDoc(t, 20, child+`,{"ty":0,"ind":2,"refId":"sq","w":5,"h":5,"st":0,"ks":{"p":{"k":[20,10]}}}`, sq)
	img := renderAt(a, 0)
	if got := inkBounds(img); got != image.Rect(20, 10, 30, 40) {
		t.Errorf("child of a precomposition: ink %v, want (20,10)-(30,40)", got)
	}
	if n, want := countColor(img, 0, 0, 255), 100; n != want {
		t.Errorf("child of a precomposition: %d blue pixels, want all %d of it, unclipped", n, want)
	}
	if n, want := countColor(img, 255, 0, 0), 25; n != want {
		t.Errorf("the precomposition's own square: %d red pixels, want the %d its 5×5 rectangle shows", n, want)
	}
}

// Load rejects what it can't draw right, whatever precomposition it is
// in, and documents that would make a frame's work grow without bound:
// an asset that contains itself, nesting deeper than maxPrecompDepth, or
// that expands to more than maxLayers layers or maxShapes shapes.
func TestLoadRejectsBadPrecompositions(t *testing.T) {
	sq := asset("sq", `{"ty":4,`+square+`}`)
	for name, tc := range map[string]struct{ layers, assets, want string }{
		"self":            {showing("a", ""), asset("a", showing("a", "")), `precomposition "a" contains itself`},
		"mutual":          {showing("a", ""), asset("a", showing("b", "")) + `,` + asset("b", `{"ty":4},`+showing("a", "")), `precomposition "a" contains itself`},
		"too deep":        {showing("a0", ""), nesting(maxPrecompDepth + 1), fmt.Sprintf("precompositions nest deeper than %d", maxPrecompDepth)},
		"too many":        {showing("d13", ""), doubling(13, `{"ty":4,`+square+`}`), fmt.Sprintf("more than %d layers", maxLayers)},
		"too many shapes": {showing("d10", ""), doubling(10, `{"ty":4,"shapes":[`+strings.Repeat(`{"ty":"rc","s":{"k":[1,1]}},`, 1500)+`{"ty":"fl"}]}`), fmt.Sprintf("more than %d shapes", maxShapes)},
		"missing":         {showing("nope", ""), sq, `no asset has id "nope"`},
		"image":           {showing("img", ""), `{"id":"img","w":10,"h":10,"u":"images/","p":"img.png"}`, `asset "img" is an image`},
		"no layers":       {showing("odd", ""), `{"id":"odd"}`, `asset "odd" has no layers`},
		"duplicate":       {showing("sq", ""), sq + `,` + sq, `2 assets have id "sq"`},
		"no size":         {`{"ty":0,"refId":"sq","st":0,"h":80}`, sq, "size (w, h)"},
		"no refId":        {`{"ty":0,"w":100,"h":80,"st":0}`, sq, "refId"},
		"number refId":    {`{"ty":0,"refId":3,"w":100,"h":80,"st":0}`, `{"id":3,"layers":[]}`, "refId"},
		"no stretch":      {showing("sq", `,"sr":0`), sq, "time stretch (sr) 0"},
		"remap script":    {showing("sq", `,"tm":{"k":0,"x":"time"}`), sq, "time remapping: unsupported expression"},
		"inner masks":     {showing("m", ""), asset("m", `{"ty":4,"masksProperties":[{}]}`), `precomposition "m": layer 0: unsupported masks`},
		"inner parents":   {showing("p", ""), asset("p", `{"ty":4,"ind":1,"parent":1}`), `precomposition "p": layer 0: parent links form a cycle`},
		"huge stretch":    {showing("sq", `,"sr":1e30`), sq, "outside"},
		"string size":     {`{"ty":0,"refId":"sq","w":"100","h":80,"st":0}`, sq, "want a number"},
		"matte":           {showing("sq", `,"tt":1`), sq, "unsupported track matte"},
	} {
		if _, err := loadDoc(20, tc.layers, tc.assets); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
	}
	// What no layer that draws refers to is never read: images, broken
	// compositions and duplicates alike; and what a hidden layer refers to.
	for name, tc := range map[string]struct{ layers, assets string }{
		"unused image":      {showing("sq", ""), sq + `,{"id":"img","w":10,"h":10,"u":"images/","p":"img.png","e":0}`},
		"unused self":       {`{"ty":4,` + square + `}`, asset("a", showing("a", ""))},
		"unused broken":     {showing("sq", ""), sq + `,{"id":"bad","layers":[{"ty":5},{"ty":4,"masksProperties":[{}]}]},{"id":"worse","layers":"none"},7,{"id":9}`},
		"unused duplicates": {showing("sq", ""), sq + `,` + asset("dup", "") + `,` + asset("dup", "")},
		"hidden":            {showing("nope", `,"hd":true`), ""},
		"deepest":           {showing("a0", ""), nesting(maxPrecompDepth)},
		"no assets":         {`{"ty":4,` + square + `}`, ""},
	} {
		if _, err := loadDoc(20, tc.layers, tc.assets); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The deepest nesting draws its square.
	if got := inkBounds(renderAt(mustLoadDoc(t, 20, showing("a0", ""), nesting(maxPrecompDepth)), 0)); got != image.Rect(0, 0, 10, 10) {
		t.Errorf("deepest nesting: ink %v, want the square at (0,0)-(10,10)", got)
	}
}

// nesting returns n assets, a0 to a(n-1), each a precomposition of the
// next, the last of the square.
func nesting(n int) string {
	var assets []string
	for i := range n - 1 {
		assets = append(assets, asset(fmt.Sprint("a", i), showing(fmt.Sprint("a", i+1), "")))
	}
	return strings.Join(append(assets, asset(fmt.Sprint("a", n-1), `{"ty":4,`+square+`}`)), ",")
}

// doubling returns assets d0, of layer, to dn, each two layers of the one
// before: dn expands to 2^n times layer, from a few kilobytes.
func doubling(n int, layer string) string {
	assets := []string{asset("d0", layer)}
	for i := 1; i <= n; i++ {
		before := fmt.Sprint("d", i-1)
		assets = append(assets, asset(fmt.Sprint("d", i), showing(before, "")+","+showing(before, "")))
	}
	return strings.Join(assets, ",")
}

// A parent that names no layer is no parent, as lottie-web and lottie-ios
// have it.
func TestParentNamingNoLayerIsIgnored(t *testing.T) {
	for _, layers := range []string{
		`{"ty":4,"parent":7,` + square + `}`,
		`{"ty":4,"parent":7,` + square + `},{"ty":3,"ind":8,"ks":{"p":{"k":[20,0]}}}`,
		`{"ty":4,"parent":1,` + square + `},{"ty":3,"ind":1,"parent":9,"ks":{"p":{"k":[20,0]}}}`,
	} {
		a := mustLoad(t, layers)
		want := image.Rect(0, 0, 10, 10)
		if strings.Contains(layers, `"ind":1`) {
			want = image.Rect(20, 0, 30, 10) // its parent is there; the parent's is not
		}
		if got := inkBounds(renderAt(a, 0)); got != want {
			t.Errorf("%s: ink %v, want %v", layers, got, want)
		}
	}
}

// A path's direction (d) changes nothing, 3 too, as lottie-web and
// lottie-ios ignore it: a path runs in the order of its vertices.
func TestPathDirectionIgnored(t *testing.T) {
	path := func(d string) string {
		return `{"ty":4,"shapes":[{"ty":"sh"` + d + `,"ks":{"k":{"i":[[0,0],[0,0],[0,0]],"o":[[0,0],[0,0],[0,0]],"v":[[20,70],[50,10],[80,70]],"c":true}}},` +
			`{"ty":"tm","s":{"k":0},"e":{"k":40}},{"ty":"st","c":{"k":[1,0,0]},"w":{"k":4}}]}`
	}
	want := renderAt(mustLoad(t, path(``)), 0)
	for _, d := range []string{`,"d":1`, `,"d":2`, `,"d":3`} {
		if !samePixels(renderAt(mustLoad(t, path(d)), 0), want) {
			t.Errorf("path with %s draws differently from one without", d)
		}
	}
}

// A time stretch (sr) means nothing on layers other than precompositions,
// as in lottie-web and lottie-ios: exporters write their keyframes in the
// composition's frames already.
func TestTimeStretchIgnoredOutsidePrecompositions(t *testing.T) {
	moving := `"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"e":[40,0]},{"t":10,"s":[40,0]}]}}`
	want := renderAt(mustLoad(t, `{"ty":4,`+moving+`,`+square+`}`), 5)
	for _, layers := range []string{
		`{"ty":4,"sr":2,` + moving + `,` + square + `}`,
		`{"ty":4,"sr":0.5,"parent":1,` + square + `},{"ty":3,"ind":1,"sr":3,` + moving + `}`,
	} {
		if !samePixels(renderAt(mustLoad(t, layers), 5), want) {
			t.Errorf("%s draws differently from the layer without sr", layers)
		}
	}
}

// Every layer that shows an asset shares its composition, read once,
// with the static outlines Load built in it; the counts that bound a
// frame's work count it again for each.
func TestPrecompositionAssetsAreShared(t *testing.T) {
	assets := asset("sq", `{"ty":4,`+square+`}`) + `,` + asset("outer", showing("sq", "")+`,`+showing("sq", ""))
	a := mustLoadDoc(t, 20, showing("outer", "")+`,`+showing("sq", "")+`,`+showing("outer", ""), assets)
	outer, sq := a.layers[0].precomp.comp, a.layers[1].precomp.comp
	if a.layers[2].precomp.comp != outer || outer.layers[0].precomp.comp != sq || outer.layers[1].precomp.comp != sq {
		t.Fatal("the layers showing an asset read it again")
	}
	if sh := &sq.layers[0].shapes; sh.animated || len(sh.outlines) != 1 || len(sh.outlines[0].paths) != 1 {
		t.Errorf("the asset's static square has outlines %v, want one built at Load", sh.outlines)
	}
	// sq: 1 layer of 2 items; outer: 2 layers showing sq; the root: 3
	// layers, two showing outer, one sq.
	if sq.layerCount != 1 || outer.layerCount != 4 || a.layerCount != 3+2*4+1 {
		t.Errorf("layer counts %d, %d, %d, want 1, 4, 12", sq.layerCount, outer.layerCount, a.layerCount)
	}
	if sq.shapeCount != 2 || outer.shapeCount != 4 || a.shapeCount != 2*4+2 {
		t.Errorf("shape counts %d, %d, %d, want 2, 4, 10", sq.shapeCount, outer.shapeCount, a.shapeCount)
	}
	if sq.depth != 0 || outer.depth != 1 || a.depth != 2 {
		t.Errorf("depths %d, %d, %d, want 0, 1, 2", sq.depth, outer.depth, a.depth)
	}
}

// countingShape is a 10×10 square that counts how often it is sampled.
type countingShape struct{ calls *atomic.Int64 }

func (s countingShape) outline(float32) pathData {
	s.calls.Add(1)
	return pathData{Vertices: []point{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, In: make([]point, 4), Out: make([]point, 4), Closed: true}
}

func (countingShape) animated() bool { return true }

// A precomposition that shows nothing at a frame, transparent, out of its
// layer's range, of no area or hidden, samples nothing inside.
func TestHiddenPrecompositionSamplesNothing(t *testing.T) {
	inner := asset("sq", `{"ty":4,`+square+`}`)
	for _, tc := range []struct {
		name, layer string
		frame       float32
		draws       bool
	}{
		{"shown", showing("sq", ""), 5, true},
		{"transparent", showing("sq", `,"ks":{"o":{"a":1,"k":[{"t":0,"s":[100],"e":[0]},{"t":5,"s":[0]}]}}`), 5, false},
		{"before its in point", showing("sq", `,"ip":6,"op":20`), 5, false},
		{"after its out point", showing("sq", `,"ip":0,"op":5`), 5, false},
		{"no width", `{"ty":0,"refId":"sq","w":0,"h":80}`, 5, false},
		{"flattened", showing("sq", `,"ks":{"s":{"k":[0,100]}}`), 5, false},
	} {
		a := mustLoadDoc(t, 20, tc.layer, inner)
		var calls atomic.Int64
		sh := &a.layers[0].precomp.comp.layers[0].shapes
		sh.geometry[0], sh.moving, sh.animated = countingShape{&calls}, []int{0}, true
		ink := countInk(renderAt(a, tc.frame))
		if got := calls.Load() > 0; got != tc.draws || (ink > 0) != tc.draws {
			t.Errorf("%s: sampled %d times, %d pixels of ink, want drawn %v", tc.name, calls.Load(), ink, tc.draws)
		}
	}
	// A hidden one is not even read: its asset need not exist, and as a
	// parent it places its child all the same.
	a := mustLoadDoc(t, 20, `{"ty":4,"parent":1,`+square+`},`+showing("nope", `,"hd":true,"ind":1,"ks":{"p":{"k":[20,0]}}`), "")
	if len(a.layers) != 2 || a.layers[1].precomp != nil {
		t.Errorf("hidden parent precomposition read: %d layers, precomp %v", len(a.layers), a.layers[1].precomp)
	}
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(20, 0, 30, 10) {
		t.Errorf("child of a hidden precomposition: ink %v, want (20,0)-(30,10)", got)
	}
}

// Rotated precompositions inside each other clip to the intersection of
// their rotated rectangles: two 40×40 squares, the inner shifted right by
// 20 inside the outer, which turns 45° about its centre at (50,40), clip
// a red field to 20×40 of the outer's coordinates, 800 DIPs, around
// (57,47); the outer's half alone, around (43,33), shows nothing.
func TestNestedRotatedPrecompositionsIntersect(t *testing.T) {
	field := asset("field", `{"ty":1,"sw":300,"sh":300,"sc":"#ff0000","ks":{"p":{"k":[-100,-100]}}}`)
	outer := asset("outer", `{"ty":0,"refId":"field","w":40,"h":40,"ks":{"p":{"k":[20,0]}}}`)
	a := mustLoadDoc(t, 20, `{"ty":0,"refId":"outer","w":40,"h":40,"ks":{"a":{"k":[20,20]},"p":{"k":[50,40]},"r":{"k":45}}}`, field+`,`+outer)
	img := renderAt(a, 0)
	solid := 0
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			if img.RGBAAt(x, y).G < 128 {
				solid++
			}
		}
	}
	// Half-covered pixels along the 120 DIPs of edge fall either way.
	if solid < 800-60 || solid > 800+60 {
		t.Errorf("%d pixels over half red, want about 800", solid)
	}
	if c := img.RGBAAt(57, 47); c.G > 8 {
		t.Errorf("inside both: %v, want red", c)
	}
	if c := img.RGBAAt(43, 33); c.G < 247 {
		t.Errorf("inside only the outer: %v, want white", c)
	}
}

// Players draw a document of precompositions at once on several
// goroutines: shared assets, nested, rotated, time remapped and
// stretched, with parents into and out of them, draw the same.
func TestPrecompositionsDrawConcurrently(t *testing.T) {
	assets := asset("move", mover+`,{"ty":1,"ind":3,"parent":2,"sw":6,"sh":6,"sc":"#0000ff"},{"ty":3,"ind":2,"ks":{"r":{"a":1,"k":[{"t":0,"s":[0],"e":[90]},{"t":10,"s":[90]}]},"p":{"k":[50,40]}}}`) + `,` +
		asset("outer", showing("move", `,"sr":0.5`)+`,`+showing("move", `,"st":4,"ks":{"r":{"k":30},"p":{"k":[10,10]}}`))
	a := mustLoadDoc(t, 40, showing("outer", `,"ind":1,"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"e":[0,20]},{"t":20,"s":[0,20]}]}}`)+`,`+
		`{"ty":4,"parent":1,"ks":{"p":{"k":[80,60]}},`+square+`},`+
		showing("move", `,"tm":{"a":1,"k":[{"t":0,"s":[1],"e":[0]},{"t":30,"s":[0]}]},"ks":{"r":{"k":-20}}`), assets)
	for _, frame := range []float32{3, 11, 27} {
		want := renderAt(a, frame)
		if countInk(want) == 0 {
			t.Fatalf("frame %v draws nothing", frame)
		}
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
