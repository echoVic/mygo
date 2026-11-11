package lottie

import (
	"encoding/json"
	"strings"
	"testing"
)

// Whether a property is keyframed is what its k holds, as lottie-web
// decides: an array of keyframes, whatever a says, or missing; and a
// value, whatever a says.
func TestKeyframesDecidedByValue(t *testing.T) {
	ramp := `[{"t":0,"s":[0]},{"t":10,"s":[100]}]`
	for _, data := range []string{`{"k":` + ramp + `}`, `{"a":0,"k":` + ramp + `}`, `{"a":1,"k":` + ramp + `}`, `{"a":0,"k":[ {"t":0,"s":[0]},{"t":10,"s":[100]}]}`} {
		p, err := parseScalar(json.RawMessage(data), 0)
		if err != nil {
			t.Errorf("%s: %v", data, err)
			continue
		}
		if got := p.sample(5); got != 50 {
			t.Errorf("%s: frame 5 = %v, want 50", data, got)
		}
	}
	for data, want := range map[string]float32{`{"a":1,"k":7}`: 7, `{"a":1,"k":[7]}`: 7, `{"a":0,"k":7}`: 7, `{"k":7}`: 7, `{"a":2,"k":7}`: 7} {
		p, err := parseScalar(json.RawMessage(data), 0)
		if err != nil || p.animated() || p.static != want {
			t.Errorf("%s: %v, animated %v, error %v; want static %v", data, p.static, p.animated(), err, want)
		}
	}
	if p, err := parsePoint(json.RawMessage(`{"a":1,"k":[10,20]}`), point{}); err != nil || p.animated() || p.static != (point{10, 20}) {
		t.Errorf("point a 1 with a value: %v, animated %v, error %v; want static (10,20)", p.static, p.animated(), err)
	}
	path := `{"i":[[0,0],[0,0],[0,0]],"o":[[0,0],[0,0],[0,0]],"v":[[0,0],[10,0],[0,10]],"c":true}`
	moved := strings.Replace(path, `[10,0]`, `[20,0]`, 1)
	if p, err := parsePath(json.RawMessage(`{"a":0,"k":[{"t":0,"s":[` + path + `]},{"t":10,"s":[` + moved + `]}]}`)); err != nil || !p.animated() || p.sample(5).Vertices[1] != (point{15, 0}) {
		t.Errorf("path keyframes with a 0: animated %v, error %v; want them morphing", p.animated(), err)
	}
	if p, err := parsePath(json.RawMessage(`{"a":1,"k":` + path + `}`)); err != nil || p.animated() {
		t.Errorf("path value with a 1: animated %v, error %v; want static", p.animated(), err)
	}
	// What is neither is still an error.
	for _, data := range []string{`{"a":1,"k":[]}`, `{"a":0,"k":[[1,2]]}`, `{"k":"7"}`} {
		if _, err := parseScalar(json.RawMessage(data), 0); err == nil {
			t.Errorf("%s: accepted", data)
		}
	}
}

// Whole numbers written as floats, as some exporters write every number
// (4.0), are those numbers: types, flags and enumerations alike. Other
// fractions are errors that say so.
func TestWholeFloatsAsIntegers(t *testing.T) {
	a := mustLoad(t, `{"ty":4.0,"ddd":0.0,"ind":1.0,"ks":{"o":{"a":1.0,"k":[{"t":0,"s":[100],"h":1.0},{"t":10,"s":[0]}]}},"shapes":[`+
		`{"ty":"rc","d":1.0,"p":{"k":[50,40]},"s":{"k":[40,20]}},{"ty":"fl","r":1.0,"c":{"k":[1,0,0]}},{"ty":"st","lc":2.0,"lj":3.0,"c":{"k":[0,0,0]},"w":{"k":2}}]}`)
	if got := renderAt(a, 5).RGBAAt(50, 40); got.R != 255 || got.G != 0 {
		t.Errorf("held opacity: center %v, want red", got)
	}
	if s := a.layers[0].groups[0].styles[1].(*stroke); s.cap != lineCaps[2] || s.join != lineJoins[3] {
		t.Errorf("stroke cap %v, join %v; want round and bevel", s.cap, s.join)
	}
	for name, layer := range map[string]string{
		"type":      `{"ty":4.5}`,
		"3D":        `{"ty":4,"ddd":0.5}`,
		"cap":       `{"ty":4,"shapes":[{"ty":"st","lc":1.5}]}`,
		"hold":      `{"ty":4,"ks":{"o":{"k":[{"t":0,"s":[0],"h":0.5},{"t":1,"s":[1]}]}}}`,
		"fill rule": `{"ty":4,"shapes":[{"ty":"fl","r":1.5}]}`,
		"direction": `{"ty":4,"shapes":[{"ty":"rc","d":2.5}]}`,
	} {
		_, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[` + layer + `]}`))
		if err == nil || !strings.Contains(err.Error(), "whole number") && !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("%s: error %v, want a fraction rejected", name, err)
		}
	}
	if _, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4.5}]}`)); err == nil || !strings.Contains(err.Error(), "ty: want a whole number, got 4.5") {
		t.Errorf("type 4.5: error %v, want it named", err)
	}
}

// A composition whose size isn't whole says which, and how large: its
// Width and Height are whole numbers.
func TestCompositionSizeNotWholeIsNamed(t *testing.T) {
	_, err := Load([]byte(`{"w":100,"h":58.84,"fr":10,"ip":0,"op":20,"layers":[]}`))
	if err == nil || !strings.Contains(err.Error(), "100×58.84") {
		t.Fatalf("error %v, want the size named", err)
	}
}

// A 2D value written with one number, as an anchor of [9.688], has no
// one meaning: lottie-web reads its missing y as NaN, lottie-ios as 0.
func TestPointOfOneNumberRejected(t *testing.T) {
	_, err := Load([]byte(`{"w":100,"h":80,"fr":10,"ip":0,"op":20,"layers":[{"ty":4,"ks":{"a":{"a":0,"k":[9.688]}}}]}`))
	if err == nil || !strings.Contains(err.Error(), "anchor: static value: want at least 2 numbers, got 1") {
		t.Fatalf("error %v, want the anchor rejected", err)
	}
}
