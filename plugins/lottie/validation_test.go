package lottie

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
)

func TestLoadRejectsUnsupportedControls(t *testing.T) {
	tests := []struct{ name, layer string }{
		{"matte", `{"ty":4,"tt":1}`},
		{"matte-source", `{"ty":4,"td":1}`},
		{"blend", `{"ty":4,"bm":1}`},
		{"stretch", `{"ty":4,"sr":2}`},
		{"auto-orient", `{"ty":4,"ao":1}`},
		{"mask", `{"ty":4,"masksProperties":[{}]}`},
		{"effect", `{"ty":4,"ef":[{}]}`},
		{"time-remap", `{"ty":4,"tm":{"k":0}}`},
		{"skew", `{"ty":4,"ks":{"sk":{"k":20}}}`},
		{"expression", `{"ty":4,"ks":{"r":{"k":0,"x":"time*10"}}}`},
		{"split-rotation", `{"ty":4,"ks":{"r":{"s":true,"k":0}}}`},
		{"split-position-expression", `{"ty":4,"ks":{"p":{"s":true,"x":{"k":0,"x":"time"},"y":{"k":0}}}}`},
		{"scalar-tangents", `{"ty":4,"ks":{"r":{"a":1,"k":[{"t":0,"s":[0],"to":[1],"ti":[0]},{"t":10,"s":[10]}]}}}`},
		{"dash", `{"ty":4,"shapes":[{"ty":"st","d":[{"n":"d","v":{"k":4}}]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + tt.layer + `]}`))
			if err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("want contextual unsupported error, got %v", err)
			}
		})
	}
}

func TestKeyframeValidationAndTerminalSentinel(t *testing.T) {
	p, err := parseScalar(json.RawMessage(`{"a":1,"k":[{"t":0,"s":[0],"e":[100],"o":{"x":0.42,"y":0},"i":{"x":0.58,"y":1}},{"t":10}]}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.sample(5); got < 49.9 || got > 50.1 {
		t.Fatalf("midpoint = %v", got)
	}
	if got := p.sample(10); got != 100 {
		t.Fatalf("sentinel = %v", got)
	}
	for _, data := range []string{
		`{"a":1,"k":[]}`,
		`{"a":1,"k":[{"t":1,"s":[0]},{"t":1,"s":[1]}]}`,
		`{"a":1,"k":[{"t":2,"s":[0]},{"t":1,"s":[1]}]}`,
	} {
		if _, err := parseScalar(json.RawMessage(data), 0); err == nil {
			t.Errorf("accepted invalid keyframes: %s", data)
		}
	}
}

func TestLoadRejectsUnrepresentableDuration(t *testing.T) {
	for _, span := range []string{`"fr":1e-30,"ip":0,"op":20`, `"fr":1e30,"ip":0,"op":20`} {
		if _, err := Load([]byte(`{"w":100,"h":80,` + span + `,"layers":[]}`)); err == nil {
			t.Fatalf("accepted invalid duration: %s", span)
		}
	}
}

func TestCompoundFillAndHiddenShape(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"shapes":[{"ty":"rc","p":{"k":[50,40]},"s":{"k":[80,60]}},{"ty":"rc","p":{"k":[50,40]},"s":{"k":[40,20]}},{"ty":"fl","r":2,"c":{"k":[1,0,0]}}]}`)
	img := renderAt(a, 0)
	r, g, b, _ := img.At(50, 40).RGBA()
	if r != 65535 || g != 65535 || b != 65535 {
		t.Fatalf("compound hole filled: %v", img.At(50, 40))
	}
	r, g, b, _ = img.At(15, 40).RGBA()
	if r != 65535 || g != 0 || b != 0 {
		t.Fatalf("outer ring missing: %v", img.At(15, 40))
	}
	a = mustLoad(t, `{"ty":4,"shapes":[{"ty":"rc","hd":true,"p":{"k":[50,40]},"s":{"k":[40,20]}},{"ty":"fl","c":{"k":[1,0,0]}}]}`)
	if got := countInk(renderAt(a, 0)); got != 0 {
		t.Fatalf("hidden geometry ink=%d", got)
	}
}

func TestNeutralExporterControls(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"ks":{"sk":{"a":0,"k":0},"sa":{"k":0},"p":{"a":1,"k":[{"t":0,"s":[0,0],"to":[0,0],"ti":[0,0]},{"t":10,"s":[20,0]}]}},"shapes":[`+redRect+`]}`)
	if n := countInk(renderAt(a, 5)); n == 0 {
		t.Fatal("neutral exporter controls hide animation")
	}
}

func TestRejectUnsupportedShapeControls(t *testing.T) {
	for _, data := range []string{`{"ty":4,"shapes":[{"ty":"rc","d":4}]}`, `{"ty":4,"shapes":[{"ty":"tr"},{"ty":"tr"}]}`} {
		if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + data + `]}`)); err == nil {
			t.Fatalf("silently accepted unsupported control: %s", data)
		}
	}
}

func TestRootGroupTransform(t *testing.T) {
	a := mustLoad(t, `{"ty":4,"shapes":[{"ty":"gr","it":[`+redRect+`]}, {"ty":"tr","p":{"k":[20,0]}}]}`)
	if got := inkBounds(renderAt(a, 0)).Min.X; got != 50 {
		t.Fatalf("root transform lost: x = %d, want 50", got)
	}
}

// Load rejects numbers beyond maxMagnitude, wherever they are: in every
// kind of property, keyframe times, easing, and the composition's own.
func TestLoadRejectsHugeNumbers(t *testing.T) {
	layer := func(ks, shapes string) string {
		return `{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"ks":{` + ks + `},"shapes":[` + shapes + `]}]}`
	}
	path := func(v, tangent string) string {
		return `{"ty":"sh","ks":{"k":{"i":[[0,0],[` + tangent + `,0],[0,0]],"o":[[0,0],[0,0],[0,0]],"v":[[0,0],[` + v + `,0],[0,10]],"c":true}}},{"ty":"fl","c":{"k":[1,0,0]}}`
	}
	stroke := func(w, ml string) string {
		return `{"ty":"rc","s":{"k":[10,10]}},{"ty":"st","c":{"k":[1,0,0]},"w":{"k":` + w + `},"ml":` + ml + `}`
	}
	for name, data := range map[string]string{
		"anchor":        layer(`"a":{"k":[3e38,0]}`, redRect),
		"scale":         layer(`"s":{"k":[3e38,3e38]}`, redRect),
		"position-keys": layer(`"p":{"a":1,"k":[{"t":0,"s":[-3e38,0],"e":[3e38,0]},{"t":10,"s":[3e38,0]}]}`, redRect),
		"rotation":      layer(`"r":{"k":1e30}`, redRect),
		"opacity":       layer(`"o":{"k":1e30}`, redRect),
		"key-time":      layer(`"r":{"a":1,"k":[{"t":0,"s":[0]},{"t":1e30,"s":[1]}]}`, redRect),
		"easing":        layer(`"r":{"a":1,"k":[{"t":0,"s":[0],"e":[1],"o":{"x":0.5,"y":1e30},"i":{"x":0.5,"y":1}},{"t":10,"s":[1]}]}`, redRect),
		"color":         layer(``, `{"ty":"rc","s":{"k":[10,10]}},{"ty":"fl","c":{"k":[1e30,0,0]}}`),
		"rect-size":     layer(``, `{"ty":"rc","s":{"k":[1e30,10]}},{"ty":"fl","c":{"k":[1,0,0]}}`),
		"vertex":        layer(``, path(`1e30`, `0`)),
		"tangent":       layer(``, path(`10`, `1e30`)),
		"stroke-width":  layer(``, stroke(`1e30`, `4`)),
		"miter":         layer(``, stroke(`1`, `1e30`)),
		"width":         `{"w":1e30,"h":80,"fr":10,"ip":0,"op":20,"layers":[]}`,
		"out-point":     `{"w":100,"h":80,"fr":10,"ip":0,"op":1e30,"layers":[]}`,
		"layer-out":     `{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"op":1e30}]}`,
	} {
		if _, err := Load([]byte(data)); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Errorf("%s: error %v, want a number outside the bound", name, err)
		}
	}
}

// Whatever Load accepts, Draw draws without panicking: values at the
// bound, overshooting easings that carry them far past it, and transforms
// nested until their product overflows float32 skip what can't be drawn.
func TestDrawSurvivesExtremeValues(t *testing.T) {
	const big = `1e7`
	overshoot := `"o":{"x":0.5,"y":` + big + `},"i":{"x":0.5,"y":-` + big + `}`
	keys := func(from, to string) string {
		return `{"a":1,"k":[{"t":0,"s":` + from + `,"e":` + to + `,` + overshoot + `},{"t":20,"s":` + to + `}]}`
	}
	// Static scales of 1e5 a level overflow float32 within 8 levels;
	// overshooting ones, past 1e9 a level, overflow even float64's product.
	nested, overflowing := redRect, redRect
	for range 8 {
		nested = `{"ty":"gr","it":[` + nested + `,{"ty":"tr","s":{"k":[` + big + `,` + big + `]},"r":{"k":` + big + `}}]}`
	}
	for range maxGroupDepth {
		overflowing = `{"ty":"gr","it":[` + overflowing + `,{"ty":"tr","s":` + keys(`[100,100]`, `[1e7,1e7]`) + `}]}`
	}
	shapes := []string{
		`{"ty":"gr","it":[{"ty":"rc","p":` + keys(`[0,0]`, `[1e7,-1e7]`) + `,"s":` + keys(`[40,20]`, `[-1e7,1e7]`) + `,"r":` + keys(`[0]`, `[1e7]`) + `},` +
			`{"ty":"fl","c":` + keys(`[0,0,0,1]`, `[1e7,-1e7,1,1e7]`) + `,"o":` + keys(`[0]`, `[1e7]`) + `},` +
			`{"ty":"st","lj":1,"ml":1e7,"c":{"k":[0,0,1]},"w":` + keys(`[-1e7]`, `[1e7]`) + `}]}`,
		`{"ty":"gr","it":[{"ty":"sh","ks":{"a":1,"k":[{"t":0,"s":[{"i":[[0,0],[1e7,1e7]],"o":[[-1e7,0],[0,0]],"v":[[0,0],[1e7,-1e7]],"c":false}],"e":[{"i":[[1e7,0],[0,0]],"o":[[0,0],[0,-1e7]],"v":[[-1e7,1e7],[0,0]],"c":false}],` + overshoot + `},{"t":20}]}},` +
			`{"ty":"st","lc":3,"lj":1,"c":{"k":[1,0,0]},"w":{"k":1e7}},{"ty":"tr","a":` + keys(`[0,0]`, `[1e7,1e7]`) + `,"s":` + keys(`[100,100]`, `[1e7,0]`) + `,"r":` + keys(`[0]`, `[1e7]`) + `}]}`,
		nested,
		overflowing,
	}
	for i, shape := range shapes {
		a := mustLoad(t, `{"ty":4,"ks":{"p":`+keys(`[0,0]`, `[1e7,1e7]`)+`,"s":`+keys(`[100,100]`, `[-1e7,1e7]`)+`},"shapes":[`+shape+`]}`)
		for f := float32(0); f < 20; f += 0.5 {
			t.Run("", func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("shape %d at frame %v: Draw panicked: %v", i, f, r)
					}
				}()
				renderAt(a, f)
				renderBox(a, 400, 30, f)
			})
		}
	}
}

// Overshooting easing can carry a rectangle's size below zero, or its
// roundness past half its side: it draws as the rectangle of its size's
// magnitude, its corners no rounder than a pill's.
func TestNegativeRectangleSizeDrawsItsMagnitude(t *testing.T) {
	rect := func(size string) *image.RGBA {
		return renderAt(mustLoad(t, `{"ty":4,"shapes":[{"ty":"rc","p":{"k":[50,40]},"s":{"k":`+size+`},"r":{"k":30}},{"ty":"fl","c":{"k":[1,0,0]}}]}`), 0)
	}
	if !samePixels(rect(`[-40,-20]`), rect(`[40,20]`)) {
		t.Fatal("a rectangle of negative size draws differently from its magnitude")
	}
}
