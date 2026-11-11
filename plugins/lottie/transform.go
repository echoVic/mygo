package lottie

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// rawTransform is a layer's ks, or a group's tr item: its anchor (a),
// position (p), scale (s), rotation (r) and opacity (o); its skew, which
// the subset accepts only as none, and 3D's rotations and orientation,
// which it rejects.
type rawTransform struct {
	A, P, S, R, O          json.RawMessage
	Sk, Sa, Rx, Ry, Rz, Or json.RawMessage
}

// transform is Lottie's 2D transform: around its anchor, scaled in
// percent, turned clockwise in degrees, then moved to its position; and its
// opacity, in percent.
type transform struct {
	anchor, position, scale property[point]
	rotation, opacity       property[float32]
}

// parseTransform decodes a transform, its missing properties neutral.
func parseTransform(raw rawTransform) (transform, error) {
	for _, v := range []json.RawMessage{raw.Sk, raw.Sa} {
		if len(v) == 0 {
			continue
		}
		skew, err := parseScalar(v, 0)
		if err != nil || skew.animated() || skew.static != 0 {
			return transform{}, fmt.Errorf("unsupported skew transform")
		}
	}
	for _, v := range []json.RawMessage{raw.Rx, raw.Ry, raw.Rz, raw.Or} {
		if len(v) > 0 {
			return transform{}, fmt.Errorf("unsupported skew or 3D transform")
		}
	}
	var t transform
	var err error
	for _, p := range []struct {
		name string
		dst  *property[point]
		raw  json.RawMessage
		def  point
	}{{"anchor", &t.anchor, raw.A, point{}}, {"position", &t.position, raw.P, point{}}, {"scale", &t.scale, raw.S, point{100, 100}}} {
		if *p.dst, err = parsePoint(p.raw, p.def); err != nil {
			return transform{}, fmt.Errorf("%s: %w", p.name, err)
		}
	}
	if t.rotation, err = parseScalar(raw.R, 0); err != nil {
		return transform{}, fmt.Errorf("rotation: %w", err)
	}
	if t.opacity, err = parseScalar(raw.O, 100); err != nil {
		return transform{}, fmt.Errorf("opacity: %w", err)
	}
	return t, nil
}

// at returns the transform's matrix at frame.
func (t transform) at(frame float32) affine {
	anchor, position, scale := t.anchor.sample(frame), t.position.sample(frame), t.scale.sample(frame)
	sin, cos := math.Sincos(float64(t.rotation.sample(frame)) * math.Pi / 180)
	sx, sy := float64(scale.X)/100, float64(scale.Y)/100
	m := affine{a: cos * sx, b: sin * sx, c: -sin * sy, d: cos * sy}
	ax, ay := m.point(anchor)
	m.e, m.f = float64(position.X)-ax, float64(position.Y)-ay
	return m
}

// percent returns a percentage property at frame as a fraction in [0, 1].
func percent(p property[float32], frame float32) float32 { return clamp(p.sample(frame)/100, 0, 1) }

// affine maps (x, y) to (a·x + c·y + e, b·x + d·y + f). Draw composes the
// transforms from the composition's viewport down to a group's geometry in
// float64, and maps the geometry's points itself, so that ui rasterizes
// paths at the size they show rather than scaling up a mask drawn at the
// composition's size.
type affine struct{ a, b, c, d, e, f float64 }

func scaling(s float64) affine { return affine{a: s, d: s} }

// mul returns the transform that applies n, then m.
func (m affine) mul(n affine) affine {
	return affine{
		a: m.a*n.a + m.c*n.b, b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d, d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e, f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m affine) point(p point) (x, y float64) {
	return m.a*float64(p.X) + m.c*float64(p.Y) + m.e, m.b*float64(p.X) + m.d*float64(p.Y) + m.f
}

func (m affine) finite() bool {
	for _, v := range [...]float64{m.a, m.b, m.c, m.d, m.e, m.f} {
		if !finite(v) {
			return false
		}
	}
	return true
}

// similarity reports whether m scales evenly in every direction, to a
// millionth, turning and perhaps mirroring what it maps, and by how much:
// then a stroke whose points m maps is as wide as its width times that.
func (m affine) similarity() (float64, bool) {
	s := math.Hypot(m.a, m.b)
	tol := 1e-6 * s
	turns := math.Abs(m.a-m.d) <= tol && math.Abs(m.b+m.c) <= tol
	mirrors := math.Abs(m.a+m.d) <= tol && math.Abs(m.b-m.c) <= tol
	return s, turns || mirrors
}

// maxScale returns how much m stretches what it maps in the direction it
// stretches most: its largest singular value.
func (m affine) maxScale() float64 {
	sum := m.a*m.a + m.b*m.b + m.c*m.c + m.d*m.d
	det := m.a*m.d - m.b*m.c
	return math.Sqrt((sum + math.Sqrt(max(sum*sum-4*det*det, 0))) / 2)
}

// transform returns m as a painter's transform, which scales, slants,
// turns, then moves, and reports false if m flattens what it maps to a line
// or moves it farther than a painter draws (maxDIPs). m's columns must be
// no longer than 1, as those of a transform that maxScale divided.
func (m affine) transform() (ui.Transform, bool) {
	// m = rotation · [[p, q], [0, r]], and that is a skew of a scaling.
	p := math.Hypot(m.a, m.b)
	if !(p > 0) || !drawable(m.e) || !drawable(m.f) {
		return ui.Transform{}, false
	}
	cos, sin := m.a/p, m.b/p
	q, r := cos*m.c+sin*m.d, cos*m.d-sin*m.c
	if !(math.Abs(p*r) >= 1e-6) {
		return ui.Transform{}, false
	}
	degrees := func(radians float64) float32 { return float32(radians * 180 / math.Pi) }
	return ui.Scaling(float32(p), float32(r)).
		Then(ui.Skewing(degrees(math.Atan(q/r)), 0)).
		Then(ui.Rotation(degrees(math.Atan2(sin, cos)))).
		Then(ui.Translation(float32(m.e), float32(m.f))), true
}

// maxDIPs bounds the coordinates and widths Draw gives the painter. ui
// draws no path wider than 1<<22 device pixels, and beyond 1<<24 float32
// does not hold whole DIPs; what reaches farther is not drawn.
const maxDIPs = 1 << 24

func drawable(v float64) bool { return math.Abs(v) <= maxDIPs }
