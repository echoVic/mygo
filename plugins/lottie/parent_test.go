package lottie

import (
	"fmt"
	"image"
	"strings"
	"testing"
)

// square is a red square from (0,0) to (10,10) in its layer's coordinates.
const square = `"shapes":[{"ty":"rc","p":{"k":[5,5]},"s":{"k":[10,10]}},{"ty":"fl","c":{"k":[1,0,0]}}]`

// world returns the matrix of the i-th layer of a at frame: its own
// transform inside its parents'.
func world(a *Animation, i int, frame float32) affine {
	return a.parents(i, frame).mul(a.layers[i].transform.at(frame))
}

func near64(x, y, wantX, wantY float64) bool {
	return abs(float32(x-wantX)) < 1e-4 && abs(float32(y-wantY)) < 1e-4
}

// A child's transform is applied inside its parent's, sampled at the
// same frame: here a null that moves, turns a quarter clockwise and
// doubles in size around its anchor (5,5).
func TestParentedChildLandsWhereComputed(t *testing.T) {
	parent := `{"ty":3,"ind":1,"ks":{"a":{"k":[5,5]},"p":{"a":1,"k":[{"t":0,"s":[10,10],"e":[50,10]},{"t":10,"s":[50,10]}]},"r":{"k":90},"s":{"k":[200,200]}}}`
	child := `{"ty":4,"parent":1,"ks":{"p":{"k":[10,0]}},` + square + `}`
	a := mustLoad(t, child+`,`+parent)
	// At frame 5 the parent is at (30,10). The child's (0,0) is its
	// (10,0); less the parent's anchor, (5,-5); doubled, (10,-10);
	// turned, (10,10); moved, (40,20).
	m := world(a, 0, 5)
	for _, c := range []struct{ x, y, wantX, wantY float64 }{
		{0, 0, 40, 20}, {10, 0, 40, 40}, {0, 10, 20, 20}, {10, 10, 20, 40},
	} {
		if x, y := m.point(point{float32(c.x), float32(c.y)}); !near64(x, y, c.wantX, c.wantY) {
			t.Errorf("frame 5: (%v,%v) maps to (%v,%v), want (%v,%v)", c.x, c.y, x, y, c.wantX, c.wantY)
		}
	}
	for _, c := range []struct {
		frame float32
		want  image.Rectangle
	}{{0, image.Rect(0, 20, 20, 40)}, {5, image.Rect(20, 20, 40, 40)}, {10, image.Rect(40, 20, 60, 40)}} {
		if got := inkBounds(renderAt(a, c.frame)); got != c.want {
			t.Errorf("frame %v: ink %v, want %v", c.frame, got, c.want)
		}
	}
}

// The chain is child, then parent, then the parent's parent, whatever
// the order of the layers.
func TestParentChainOrder(t *testing.T) {
	grandparent := `{"ty":3,"ind":1,"ks":{"p":{"k":[50,10]},"r":{"k":90}}}`
	parent := `{"ty":3,"ind":2,"parent":1,"ks":{"p":{"k":[20,0]}}}`
	child := `{"ty":4,"parent":2,"ks":{"p":{"k":[10,0]}},` + square + `}`
	// The child's square is at (10..20, 0..10) in its parent's
	// coordinates, (30..40, 0..10) in the grandparent's, which turns it
	// to (-10..0, 30..40) and moves it to (40..50, 40..50).
	a := mustLoad(t, child+`,`+grandparent+`,`+parent)
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(40, 40, 50, 50) {
		t.Fatalf("ink %v, want (40,40)-(50,50)", got)
	}
}

// Only a layer's own in and out points, hidden flag and opacity decide
// whether and how it shows: a parent that is transparent, out of its
// range or hidden still moves its children, which show in full.
func TestParentOpacityAndVisibilityNotInherited(t *testing.T) {
	blue := `"shapes":[{"ty":"rc","p":{"k":[20,20]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[0,0,1]}}]`
	child := `{"ty":4,"parent":1,"shapes":[{"ty":"rc","p":{"k":[50,40]},"s":{"k":[20,20]}},{"ty":"fl","c":{"k":[1,0,0]}}]}`
	for name, parent := range map[string]string{
		"transparent":  `{"ty":4,"ind":1,"ks":{"p":{"k":[20,0]},"o":{"k":0}},` + blue + `}`,
		"half":         `{"ty":4,"ind":1,"ks":{"p":{"k":[20,0]},"o":{"k":50}},` + blue + `}`,
		"out of range": `{"ty":4,"ind":1,"ip":10,"op":20,"ks":{"p":{"k":[20,0]}},` + blue + `}`,
		"hidden":       `{"ty":4,"ind":1,"hd":true,"ks":{"p":{"k":[20,0]}},` + blue + `}`,
		"null":         `{"ty":3,"ind":1,"ip":10,"op":20,"ks":{"p":{"k":[20,0]},"o":{"k":0}}}`,
	} {
		img := renderAt(mustLoad(t, child+`,`+parent), 5)
		if got := img.RGBAAt(70, 40); got.R != 255 || got.G != 0 || got.B != 0 || got.A != 255 {
			t.Errorf("%s parent: child's center %v, want opaque red", name, got)
		}
		if got := inkBounds(img); name != "half" && got != image.Rect(60, 30, 80, 50) {
			t.Errorf("%s parent: ink %v, want only the child, moved, (60,30)-(80,50)", name, got)
		}
	}
	// The child's own range and opacity apply as without a parent.
	parent := `{"ty":3,"ind":1,"ks":{"p":{"k":[20,0]}}}`
	ended := strings.Replace(child, `"parent":1`, `"parent":1,"op":5`, 1)
	if n := countInk(renderAt(mustLoad(t, ended+`,`+parent), 6)); n != 0 {
		t.Errorf("child past its out point: %d ink pixels, want none", n)
	}
	faded := strings.Replace(child, `"parent":1`, `"parent":1,"ks":{"o":{"k":50}}`, 1)
	if got := renderAt(mustLoad(t, faded+`,`+parent), 6).RGBAAt(70, 40); got.R != 255 || got.G < 126 || got.G > 129 {
		t.Errorf("half-transparent child's center %v, want pink", got)
	}
}

// Layers draw in their order, the first on top, whatever their parents.
func TestDrawingOrderIgnoresParents(t *testing.T) {
	parent := `{"ty":4,"ind":1,"shapes":[{"ty":"rc","p":{"k":[50,40]},"s":{"k":[60,60]}},{"ty":"fl","c":{"k":[0,0,1]}}]}`
	child := `{"ty":4,"parent":1,"shapes":[` + redRect + `]}`
	if got := renderAt(mustLoad(t, child+`,`+parent), 0).RGBAAt(50, 40); got.R != 255 || got.B != 0 {
		t.Errorf("child listed first: center %v, want the child's red on top", got)
	}
	if got := renderAt(mustLoad(t, parent+`,`+child), 0).RGBAAt(50, 40); got.R != 0 || got.B != 255 {
		t.Errorf("child listed last: center %v, want the parent's blue on top", got)
	}
}

// chain returns n nulls, each the parent of the one before, the first
// the parent of a child, which comes first.
func chain(n int) string {
	layers := []string{`{"ty":4,"parent":1,` + square + `}`}
	for i := 1; i <= n; i++ {
		parent := ""
		if i < n {
			parent = fmt.Sprintf(`,"parent":%d`, i+1)
		}
		layers = append(layers, fmt.Sprintf(`{"ty":3,"ind":%d%s,"ks":{"p":{"k":[1,0]}}}`, i, parent))
	}
	return strings.Join(layers, ",")
}

// Load resolves the links: what names no layer, names several, or leads
// back to the layer that has it is an error; so is a chain longer than
// maxParentDepth.
func TestLoadRejectsBrokenParentLinks(t *testing.T) {
	load := func(layers string) error {
		_, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + layers + `]}`))
		return err
	}
	for name, tc := range map[string]struct{ layers, want string }{
		"dangling":       {`{"ty":4,"parent":7,` + square + `}`, "parent 7: no layer has ind 7"},
		"self":           {`{"ty":4,"ind":1,"parent":1,` + square + `}`, "parent links form a cycle"},
		"two":            {`{"ty":4,"ind":1,"parent":2,` + square + `},{"ty":4,"ind":2,"parent":1,` + square + `}`, "parent links form a cycle"},
		"through nulls":  {`{"ty":4,"parent":1,` + square + `},{"ty":3,"ind":1,"parent":2},{"ty":3,"ind":2,"parent":1}`, "parent links form a cycle"},
		"duplicate":      {`{"ty":4,"parent":1,` + square + `},{"ty":3,"ind":1},{"ty":4,"ind":1,` + square + `}`, "parent 1: 2 layers have ind 1"},
		"too deep":       {chain(maxParentDepth + 1), fmt.Sprintf("parent chain is longer than %d layers", maxParentDepth)},
		"string":         {`{"ty":4,"parent":"1",` + square + `},{"ty":3,"ind":1}`, "want a number"},
		"fraction":       {`{"ty":4,"parent":1.5,` + square + `},{"ty":3,"ind":1}`, "want a whole number"},
		"huge ind":       {`{"ty":4,` + square + `},{"ty":3,"ind":1e8}`, "outside ±1e+07"},
		"hidden, broken": {`{"ty":4,"parent":1,` + square + `},{"ty":4,"hd":true,"ind":1,"parent":9}`, "parent 9: no layer has ind 9"},
	} {
		if err := load(tc.layers); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
	}
	for name, layers := range map[string]string{
		"deepest":             chain(maxParentDepth),
		"duplicates unnamed":  `{"ty":4,"ind":1,` + square + `},{"ty":4,"ind":1,` + square + `}`,
		"null parent":         `{"ty":4,"parent":null,` + square + `}`,
		"whole float":         `{"ty":4,"parent":1.0,` + square + `},{"ty":3,"ind":1}`,
		"unused broken links": `{"ty":4,` + square + `},{"ty":4,"hd":true,"ind":1,"parent":9},{"ty":3,"ind":2,"parent":2}`,
	} {
		if err := load(layers); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The deepest chain moves the child a DIP for each of its nulls.
	a := mustLoad(t, chain(maxParentDepth))
	if got := inkBounds(renderAt(a, 0)); got != image.Rect(maxParentDepth, 0, maxParentDepth+10, 10) {
		t.Errorf("deepest chain: ink %v, want the square moved %d DIPs", got, maxParentDepth)
	}
}

// A layer that only positions others contributes its transform, whatever
// it is: a hidden layer of any type, or a null. What else it carries
// changes nothing, but what changes its transform is checked as on any
// layer.
func TestNonDrawingParents(t *testing.T) {
	child := `{"ty":4,"parent":1,` + square + `}`
	for name, parent := range map[string]string{
		"hidden text":  `{"ty":5,"hd":true,"ind":1,"ks":{"p":{"k":[20,0]}},"t":{"d":"anything"}}`,
		"hidden image": `{"ty":2,"hd":true,"ind":1,"refId":"none","ks":{"p":{"k":[20,0]}},"masksProperties":[{}]}`,
		"null":         `{"ty":3,"ind":1,"ks":{"p":{"k":[20,0]}},"masksProperties":[{"mode":"a"}],"hasMask":true,"ef":[{"ty":5}],"bm":3,"tt":1,"td":1,"tm":{"k":0},"shapes":[{"ty":"xx"}]}`,
	} {
		a := mustLoad(t, child+`,`+parent)
		if got := inkBounds(renderAt(a, 0)); got != image.Rect(20, 0, 30, 10) {
			t.Errorf("%s parent: ink %v, want the child moved to (20,0)-(30,10)", name, got)
		}
	}
	for name, parent := range map[string]string{
		"skew":         `{"ty":3,"ind":1,"ks":{"sk":{"k":10}}}`,
		"3D":           `{"ty":3,"ind":1,"ddd":1}`,
		"rotation 3D":  `{"ty":5,"hd":true,"ind":1,"ks":{"rx":{"k":10}}}`,
		"auto-orient":  `{"ty":3,"ind":1,"ao":1}`,
		"stretch":      `{"ty":3,"ind":1,"sr":2}`,
		"expression":   `{"ty":3,"ind":1,"ks":{"p":{"k":[0,0],"x":"wiggle(1,2)"}}}`,
		"visible text": `{"ty":5,"ind":1}`,
	} {
		if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + child + `,` + parent + `]}`)); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("%s parent: error %v, want it unsupported", name, err)
		}
	}
}

// A null draws nothing, and one that positions nothing is skipped,
// whatever it contains, as a hidden layer is.
func TestNullLayers(t *testing.T) {
	for _, null := range []string{
		`{"ty":3,"ind":1,"ks":{"p":{"k":[50,40]},"o":{"k":100}}}`,
		`{"ty":3,"ddd":1,"ao":1,"sr":3,"ks":{"sk":{"k":30},"p":{"k":[0,0],"x":"time"}},"masksProperties":[{}],"ef":[{}],"tt":2}`,
		`{"ty":3.0,"ind":4.0}`,
	} {
		a, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + null + `]}`))
		if err != nil {
			t.Errorf("%s: %v", null, err)
			continue
		}
		if n := countInk(renderAt(a, 0)); n != 0 {
			t.Errorf("%s: %d ink pixels, want none", null, n)
		}
	}
}

// A solid layer is a rectangle from (0,0) to (sw,sh) in its coordinates,
// filled with sc, under its transform and opacity like any layer.
func TestSolidLayers(t *testing.T) {
	solid := func(fields string) string { return `{"ty":1,"sw":40,"sh":20,"sc":"#ff0000",` + fields + `}` }
	a := mustLoad(t, solid(`"ks":{"p":{"k":[30,30]}}`))
	img := renderAt(a, 0)
	if got := inkBounds(img); got != image.Rect(30, 30, 70, 50) {
		t.Errorf("solid: ink %v, want (30,30)-(70,50)", got)
	}
	if got := img.RGBAAt(50, 40); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Errorf("solid's center %v, want red", got)
	}
	// Around its anchor, the same.
	if !samePixels(renderAt(mustLoad(t, solid(`"ks":{"a":{"k":[20,10]},"p":{"k":[50,40]}}`)), 0), img) {
		t.Error("a solid placed by its anchor draws elsewhere")
	}
	for _, sc := range []string{`#f00`, `#FF0000`, `#Ff0000`} {
		if !samePixels(renderAt(mustLoad(t, strings.Replace(solid(`"ks":{"p":{"k":[30,30]}}`), `#ff0000`, sc, 1)), 0), img) {
			t.Errorf("%s draws differently from #ff0000", sc)
		}
	}
	if got := renderAt(mustLoad(t, `{"ty":1,"sw":100,"sh":80,"sc":"#00a0ff"}`), 0).RGBAAt(50, 40); got.R != 0 || got.G != 0xa0 || got.B != 0xff {
		t.Errorf("#00a0ff: %v", got)
	}
	if got := renderAt(mustLoad(t, solid(`"ks":{"p":{"k":[30,30]},"o":{"k":50}}`)), 0).RGBAAt(50, 40); got.R != 255 || got.G < 126 || got.G > 129 {
		t.Errorf("half-transparent solid's center %v, want pink", got)
	}
	// Turned by a parent, and moving a child.
	parented := mustLoad(t, `{"ty":1,"parent":1,"sw":10,"sh":10,"sc":"#ff0000"},{"ty":3,"ind":1,"ks":{"p":{"k":[50,10]},"r":{"k":90}}}`)
	if got := inkBounds(renderAt(parented, 0)); got != image.Rect(40, 10, 50, 20) {
		t.Errorf("solid under a turned null: ink %v, want (40,10)-(50,20)", got)
	}
	parenting := mustLoad(t, `{"ty":4,"parent":1,`+square+`},{"ty":1,"ind":1,"sw":0,"sh":0,"sc":"#000000","ks":{"p":{"k":[60,50]}}}`)
	if got := inkBounds(renderAt(parenting, 0)); got != image.Rect(60, 50, 70, 60) {
		t.Errorf("shape under a solid: ink %v, want (60,50)-(70,60)", got)
	}
	// Of no size, it draws nothing.
	for _, size := range []string{`"sw":0,"sh":20`, `"sw":40,"sh":0`, `"sw":0,"sh":0`} {
		if n := countInk(renderAt(mustLoad(t, `{"ty":1,`+size+`,"sc":"#ff0000"}`), 0)); n != 0 {
			t.Errorf("solid %s: %d ink pixels, want none", size, n)
		}
	}
}

func TestLoadRejectsBadSolids(t *testing.T) {
	for name, tc := range map[string]struct{ layer, want string }{
		"no color":      {`{"ty":1,"sw":10,"sh":10}`, "solid color"},
		"named color":   {`{"ty":1,"sw":10,"sh":10,"sc":"red"}`, `unsupported solid color "red"`},
		"no hash":       {`{"ty":1,"sw":10,"sh":10,"sc":"ff0000"}`, `unsupported solid color "ff0000"`},
		"four digits":   {`{"ty":1,"sw":10,"sh":10,"sc":"#ff00"}`, `unsupported solid color "#ff00"`},
		"eight digits":  {`{"ty":1,"sw":10,"sh":10,"sc":"#ff000080"}`, `unsupported solid color "#ff000080"`},
		"not hex":       {`{"ty":1,"sw":10,"sh":10,"sc":"#gg0000"}`, `unsupported solid color "#gg0000"`},
		"array":         {`{"ty":1,"sw":10,"sh":10,"sc":[1,0,0]}`, "solid color"},
		"negative size": {`{"ty":1,"sw":-10,"sh":10,"sc":"#ff0000"}`, "solid size"},
		"no size":       {`{"ty":1,"sc":"#ff0000"}`, "solid size"},
		"string size":   {`{"ty":1,"sw":"10","sh":10,"sc":"#ff0000"}`, "want a number"},
		"huge size":     {`{"ty":1,"sw":1e30,"sh":10,"sc":"#ff0000"}`, "outside"},
		"masks":         {`{"ty":1,"sw":10,"sh":10,"sc":"#ff0000","masksProperties":[{}]}`, "unsupported masks"},
		"matte source":  {`{"ty":1,"sw":10,"sh":10,"sc":"#ff0000","td":1}`, "unsupported matte source"},
	} {
		_, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + tc.layer + `]}`))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
	}
}

// Players draw parented layers at once on several goroutines: Load
// resolved the links, and Draw only reads them.
func TestParentedAnimationDrawsConcurrently(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"parent":2,`+square+`},{"ty":1,"parent":1,"sw":10,"sh":10,"sc":"#0000ff"},`+
		`{"ty":3,"ind":1,"parent":2,"ks":{"r":{"a":1,"k":[{"t":0,"s":[0],"e":[90]},{"t":20,"s":[90]}]},"p":{"k":[20,0]}}},`+
		`{"ty":1,"ind":2,"sw":5,"sh":5,"sc":"#00ff00","ks":{"p":{"a":1,"k":[{"t":0,"s":[10,10],"e":[60,50]},{"t":20,"s":[60,50]}]},"s":{"k":[150,150]}}}`)
	for _, frame := range []float32{3, 11} {
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
