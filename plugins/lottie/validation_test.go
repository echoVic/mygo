package lottie

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadRejectsUnsupportedControls(t *testing.T) {
	tests := []struct{ name, layer string }{
		{"parent", `{"ty":4,"parent":1}`},
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
		{"split-position", `{"ty":4,"ks":{"p":{"s":true,"x":{"k":0},"y":{"k":0}}}}`},
		{"spatial-curve", `{"ty":4,"ks":{"p":{"a":1,"k":[{"t":0,"s":[0,0],"to":[1,2],"ti":[0,0]},{"t":10,"s":[10,10]}]}}}`},
		{"multiple-fills", `{"ty":4,"shapes":[` + redRect + `,{"ty":"fl","c":{"k":[0,0,1]}}]}`},
		{"interleaved-styles", `{"ty":4,"shapes":[` + redRect + `,{"ty":"el","s":{"k":[10,10]}}]}`},
		{"group-style-inheritance", `{"ty":4,"shapes":[{"ty":"gr","it":[]},{"ty":"fl","c":{"k":[1,0,0]}}]}`},
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
		`{"a":1,"k":[{"t":0,"s":[0],"i":{"x":[0.5,0.8],"y":[1,1]},"o":{"x":[0.5,0.5],"y":[0,0]}},{"t":10,"s":[1]}]}`,
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
	for _, data := range []string{`{"ty":4,"shapes":[{"ty":"rc","d":3}]}`, `{"ty":4,"shapes":[{"ty":"tr"},{"ty":"tr"}]}`} {
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
