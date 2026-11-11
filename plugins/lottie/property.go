package lottie

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

type point struct{ X, Y float32 }

type color struct{ R, G, B, A float32 }

// pathData is a Bézier path as Lottie has it: each vertex with the
// tangents into and out of it, relative to it.
type pathData struct {
	Vertices, In, Out []point
	Closed            bool
}

// easing maps a keyframe's progress to its eased progress.
type easing func(float32) float32

// property is a value, static or keyframed in composition frames.
type property[T any] struct {
	static T
	keys   []keyframe[T]
	lerp   func(a, b T, t float32) T
}

type keyframe[T any] struct {
	t          float32
	start, end T
	hold       bool
	ease       easing
	// tween, when set, interpolates toward the next keyframe in place of
	// lerp and ease: a position along its spatial curve, or a value whose
	// components each ease on a curve of their own.
	tween tween[T]
}

// tween is a keyframe's own interpolation: the value at t, the linear
// progress of time from the keyframe (0) to the next (1).
type tween[T any] interface{ at(t float32) T }

func (p property[T]) animated() bool { return len(p.keys) > 0 }

// sample returns the value at frame: before the first keyframe its start,
// after the last its value, and between two the first's interpolated
// toward its end.
func (p property[T]) sample(frame float32) T {
	if len(p.keys) == 0 {
		return p.static
	}
	if frame <= p.keys[0].t {
		return p.keys[0].start
	}
	for i := 0; i+1 < len(p.keys); i++ {
		k, next := &p.keys[i], p.keys[i+1].t
		if frame >= next {
			continue
		}
		if k.hold {
			return k.start
		}
		t := (frame - k.t) / (next - k.t)
		if k.tween != nil {
			return k.tween.at(t)
		}
		if k.ease != nil {
			t = k.ease(t)
		}
		return p.lerp(k.start, k.end, t)
	}
	return p.keys[len(p.keys)-1].start
}

type rawProperty struct {
	Expression json.RawMessage `json:"x"`
	Split      bool            `json:"s"`
	Animated   int             `json:"a"`
	K          json.RawMessage `json:"k"`
}

type rawKeyframe struct {
	T      num             `json:"t"`
	S      json.RawMessage `json:"s"`
	E      json.RawMessage `json:"e"`
	H      int             `json:"h"`
	I      rawBezier       `json:"i"`
	O      rawBezier       `json:"o"`
	To, Ti json.RawMessage
	// out and in are its spatial tangents, to and ti, which
	// decodeKeyframes decodes: zero where missing, and in 2D.
	out, in point
}

// rawBezier is a keyframe's easing handle, its controls a number or one per
// component.
type rawBezier struct{ X, Y []float32 }

func (b *rawBezier) UnmarshalJSON(data []byte) error {
	var raw struct{ X, Y json.RawMessage }
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, c := range []struct {
		raw json.RawMessage
		dst *[]float32
	}{{raw.X, &b.X}, {raw.Y, &b.Y}} {
		if len(c.raw) == 0 {
			continue
		}
		values, err := numbers(c.raw, 0)
		if err != nil {
			return err
		}
		for _, v := range values {
			*c.dst = append(*c.dst, float32(v))
		}
	}
	return nil
}

// valueKind is how a property's values decode and interpolate.
type valueKind[T any] struct {
	decode func(json.RawMessage) (T, error)
	lerp   func(a, b T, t float32) T
	// components is how many components of a value may each ease on a
	// curve of its own; 0 for a value that has one curve, which every
	// entry of the easing controls must agree on.
	components int
	// each returns the tween that eases each component of start toward
	// end on its own curve, of eases; nil for values of one component.
	each func(start, end T, eases []easing) tween[T]
	// spatial returns the tween along a keyframe's spatial curve, nil
	// where the curve has no length; nil for values without tangents.
	spatial func(start, end T, out, in point, ease easing) tween[T]
}

var (
	scalarKind = &valueKind[float32]{decode: scalarValue, lerp: lerp, components: 1}
	pointKind  = &valueKind[point]{decode: pointValue, lerp: lerpPoint, components: 2, each: eachPoint, spatial: alongCurve}
	colorKind  = &valueKind[color]{decode: colorValue, lerp: lerpColor, components: 4, each: eachColor}
	pathKind   = &valueKind[pathData]{decode: pathValue, lerp: lerpPath}
)

// parseProperty decodes a property of values of kind, def when it is
// missing.
func parseProperty[T any](data json.RawMessage, def T, kind *valueKind[T]) (property[T], error) {
	if len(data) == 0 {
		return property[T]{static: def}, nil
	}
	raw, err := decodeProperty(data)
	if err != nil {
		return property[T]{}, err
	}
	if raw.Animated == 0 {
		v, err := kind.decode(raw.K)
		if err != nil {
			return property[T]{}, fmt.Errorf("static value: %w", err)
		}
		return property[T]{static: v}, nil
	}
	frames, err := decodeKeyframes(raw.K)
	if err != nil {
		return property[T]{}, err
	}
	p := property[T]{keys: make([]keyframe[T], len(frames)), lerp: kind.lerp}
	for i := range frames {
		f, k := &frames[i], &p.keys[i]
		k.t, k.hold = float32(f.T), f.H == 1
		if k.start, err = kind.decode(f.S); err != nil {
			return property[T]{}, fmt.Errorf("keyframe %d start: %w", i, err)
		}
		k.end = k.start
		end := f.E
		if len(end) == 0 && i+1 < len(frames) {
			end = frames[i+1].S
		}
		if len(end) > 0 {
			if k.end, err = kind.decode(end); err != nil {
				return property[T]{}, fmt.Errorf("keyframe %d end: %w", i, err)
			}
		}
		// A hold or the last keyframe doesn't move toward a next one.
		if !k.hold && i+1 < len(frames) {
			if err := kind.motion(k, f); err != nil {
				return property[T]{}, fmt.Errorf("keyframe %d: %w", i, err)
			}
		}
	}
	return p, nil
}

// motion sets how a keyframe moves toward the next: along its spatial
// curve when it has tangents, which has one easing curve, as lottie-web
// has it; or each component on its own easing curve; or all on one.
func (kind *valueKind[T]) motion(k *keyframe[T], f *rawKeyframe) error {
	ease, eases := f.curves(kind.components)
	spatial := f.out != (point{}) || f.in != (point{})
	switch {
	case spatial && kind.spatial == nil:
		return fmt.Errorf("unsupported spatial tangents")
	case eases != nil && spatial:
		return fmt.Errorf("unsupported component-specific easing along spatial tangents")
	case eases != nil && kind.each == nil:
		return fmt.Errorf("unsupported component-specific easing")
	case eases != nil:
		k.tween = kind.each(k.start, k.end, eases)
	default:
		k.ease = ease
		if spatial {
			k.tween = kind.spatial(k.start, k.end, f.out, f.in, ease)
		}
	}
	return nil
}

func parseScalar(data json.RawMessage, def float32) (property[float32], error) {
	return parseProperty(data, def, scalarKind)
}

func parsePoint(data json.RawMessage, def point) (property[point], error) {
	return parseProperty(data, def, pointKind)
}

func parseColor(data json.RawMessage, def color) (property[color], error) {
	return parseProperty(data, def, colorKind)
}

// parsePath decodes a shape's path, whose keyframes morph between paths of
// as many vertices, open or closed alike.
func parsePath(data json.RawMessage) (property[pathData], error) {
	if len(data) == 0 {
		return property[pathData]{}, fmt.Errorf("missing path property")
	}
	p, err := parseProperty(data, pathData{}, pathKind)
	if err != nil {
		return p, err
	}
	for i, k := range p.keys {
		if k.start.Closed != k.end.Closed || len(k.start.Vertices) != len(k.end.Vertices) {
			return p, fmt.Errorf("path keyframe %d changes vertex count", i)
		}
	}
	return p, nil
}

func scalarValue(data json.RawMessage) (float32, error) {
	v, err := numbers(data, 1)
	if err != nil {
		return 0, err
	}
	return float32(v[0]), nil
}

func pointValue(data json.RawMessage) (point, error) {
	v, err := numbers(data, 2)
	if err != nil {
		return point{}, err
	}
	return point{float32(v[0]), float32(v[1])}, nil
}

func colorValue(data json.RawMessage) (color, error) {
	v, err := numbers(data, 3)
	if err != nil {
		return color{}, err
	}
	c := color{float32(v[0]), float32(v[1]), float32(v[2]), 1}
	if len(v) > 3 {
		c.A = float32(v[3])
	}
	return c, nil
}

// pathValue decodes a path, which keyframes wrap in an array.
func pathValue(data json.RawMessage) (pathData, error) {
	if len(data) > 0 && data[0] == '[' {
		var paths []json.RawMessage
		if err := json.Unmarshal(data, &paths); err != nil {
			return pathData{}, err
		}
		if len(paths) == 0 {
			return pathData{}, fmt.Errorf("empty path keyframe")
		}
		data = paths[0]
	}
	var raw struct {
		I, O, V [][]num
		C       bool
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return pathData{}, err
	}
	n := len(raw.V)
	if n == 0 || len(raw.I) != n || len(raw.O) != n {
		return pathData{}, fmt.Errorf("path vertices and tangents must have equal non-zero lengths")
	}
	d := pathData{Vertices: make([]point, n), In: make([]point, n), Out: make([]point, n), Closed: raw.C}
	for _, c := range []struct {
		name string
		src  [][]num
		dst  []point
	}{{"vertex", raw.V, d.Vertices}, {"in tangent", raw.I, d.In}, {"out tangent", raw.O, d.Out}} {
		for i, v := range c.src {
			if len(v) < 2 {
				return pathData{}, fmt.Errorf("%s %d has fewer than two coordinates", c.name, i)
			}
			c.dst[i] = point{float32(v[0]), float32(v[1])}
		}
	}
	return d, nil
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }

func lerpPoint(a, b point, t float32) point { return point{lerp(a.X, b.X, t), lerp(a.Y, b.Y, t)} }

func lerpColor(a, b color, t float32) color {
	return color{lerp(a.R, b.R, t), lerp(a.G, b.G, t), lerp(a.B, b.B, t), lerp(a.A, b.A, t)}
}

func lerpPath(a, b pathData, t float32) pathData {
	points := func(a, b []point) []point {
		out := make([]point, len(a))
		for i := range out {
			out[i] = lerpPoint(a[i], b[i], t)
		}
		return out
	}
	return pathData{Vertices: points(a.Vertices, b.Vertices), In: points(a.In, b.In), Out: points(a.Out, b.Out), Closed: a.Closed}
}

// eachPoint eases a point's x and y each on its own curve.
func eachPoint(start, end point, eases []easing) tween[point] {
	return &pointEasing{start, end, eases[0], eases[1]}
}

type pointEasing struct {
	start, end point
	x, y       easing
}

func (e *pointEasing) at(t float32) point {
	return point{lerp(e.start.X, e.end.X, e.x(t)), lerp(e.start.Y, e.end.Y, e.y(t))}
}

// eachColor eases a color's channels each on its own curve.
func eachColor(start, end color, eases []easing) tween[color] {
	return &colorEasing{start, end, [4]easing(eases)}
}

type colorEasing struct {
	start, end color
	eases      [4]easing
}

func (e *colorEasing) at(t float32) color {
	a, b := e.start, e.end
	return color{lerp(a.R, b.R, e.eases[0](t)), lerp(a.G, b.G, e.eases[1](t)), lerp(a.B, b.B, e.eases[2](t)), lerp(a.A, b.A, e.eases[3](t))}
}

// curves returns the keyframe's temporal easing for a value of n
// components: one curve, nil for linear, when all its components ease
// alike, or else one per component. Component c eases on the c-th entry
// of each control, or on the first where a control has fewer, as
// lottie-web resolves them; n 0 compares every entry, for a value that
// has one curve. The curves' y controls may leave [0, 1], as back and
// overshoot easings do; their x controls are clamped there, which keeps
// time moving forward.
func (f *rawKeyframe) curves(n int) (easing, []easing) {
	controls := [4][]float32{f.O.X, f.O.Y, f.I.X, f.I.Y}
	every := 0
	for _, c := range controls {
		if len(c) == 0 {
			return nil, nil
		}
		every = max(every, len(c))
	}
	if n == 0 {
		n = every
	}
	curve := func(c int) (k [4]float32) {
		for i, v := range controls {
			k[i] = v[0]
			if c < len(v) {
				k[i] = v[c]
			}
		}
		return k
	}
	first := curve(0)
	alike := true
	for c := 1; c < n && alike; c++ {
		alike = curve(c) == first
	}
	if alike {
		return cubicBezier(first[0], first[1], first[2], first[3]), nil
	}
	eases := make([]easing, n)
	for c := range eases {
		if k := curve(c); c > 0 && k == curve(c-1) {
			eases[c] = eases[c-1]
		} else {
			eases[c] = cubicBezier(k[0], k[1], k[2], k[3])
		}
	}
	return nil, eases
}

// cubicBezier returns CSS's cubic-bezier(x1, y1, x2, y2): exactly 0 and 1
// at the ends, and between them y where the curve's x is the progress,
// found by Newton's method, or by bisection where its slope is flat.
func cubicBezier(x1, y1, x2, y2 float32) easing {
	x1 = clamp(x1, 0, 1)
	x2 = clamp(x2, 0, 1)
	cx, bx := 3*x1, 3*(x2-x1)-3*x1
	ax := 1 - cx - bx
	cy, by := 3*y1, 3*(y2-y1)-3*y1
	ay := 1 - cy - by
	curveX := func(t float32) float32 { return ((ax*t+bx)*t + cx) * t }
	curveY := func(t float32) float32 { return ((ay*t+by)*t + cy) * t }
	return func(x float32) float32 {
		if x <= 0 {
			return 0
		}
		if x >= 1 {
			return 1
		}
		t := x
		for range 8 {
			e := curveX(t) - x
			if math.Abs(float64(e)) < 1e-6 {
				return curveY(t)
			}
			d := (3*ax*t+2*bx)*t + cx
			if math.Abs(float64(d)) < 1e-6 {
				break
			}
			t = clamp(t-e/d, 0, 1)
		}
		// x1 and x2 in [0, 1] make the curve's x rise monotonically.
		lo, hi := float32(0), float32(1)
		for range 32 {
			if curveX(t) < x {
				lo = t
			} else {
				hi = t
			}
			t = (lo + hi) / 2
		}
		return curveY(t)
	}
}

// clamp returns v within [lo, hi], and lo for NaN.
func clamp(v, lo, hi float32) float32 {
	if !(v > lo) {
		return lo
	}
	return min(v, hi)
}

// ui returns the color, its channels clamped to [0, 1], with its alpha
// times alpha.
func (c color) ui(alpha float32) ui.Color {
	channel := func(v float32) uint8 { return uint8(clamp(v, 0, 1)*255 + 0.5) }
	return ui.RGBA(channel(c.R), channel(c.G), channel(c.B), clamp(c.A*alpha, 0, 1))
}
