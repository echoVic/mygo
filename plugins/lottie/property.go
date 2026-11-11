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
}

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

// parseProperty decodes a property whose values decode decodes, def when
// it is missing.
func parseProperty[T any](data json.RawMessage, def T, decode func(json.RawMessage) (T, error), lerp func(a, b T, t float32) T) (property[T], error) {
	if len(data) == 0 {
		return property[T]{static: def}, nil
	}
	raw, err := decodeProperty(data)
	if err != nil {
		return property[T]{}, err
	}
	if raw.Animated == 0 {
		v, err := decode(raw.K)
		if err != nil {
			return property[T]{}, fmt.Errorf("static value: %w", err)
		}
		return property[T]{static: v}, nil
	}
	frames, err := decodeKeyframes(raw.K)
	if err != nil {
		return property[T]{}, err
	}
	p := property[T]{keys: make([]keyframe[T], len(frames)), lerp: lerp}
	for i, f := range frames {
		k := &p.keys[i]
		k.t, k.hold, k.ease = float32(f.T), f.H == 1, f.easing()
		if k.start, err = decode(f.S); err != nil {
			return property[T]{}, fmt.Errorf("keyframe %d start: %w", i, err)
		}
		k.end = k.start
		end := f.E
		if len(end) == 0 && i+1 < len(frames) {
			end = frames[i+1].S
		}
		if len(end) > 0 {
			if k.end, err = decode(end); err != nil {
				return property[T]{}, fmt.Errorf("keyframe %d end: %w", i, err)
			}
		}
	}
	return p, nil
}

func parseScalar(data json.RawMessage, def float32) (property[float32], error) {
	return parseProperty(data, def, scalarValue, lerp)
}

func parsePoint(data json.RawMessage, def point) (property[point], error) {
	return parseProperty(data, def, pointValue, lerpPoint)
}

func parseColor(data json.RawMessage, def color) (property[color], error) {
	return parseProperty(data, def, colorValue, lerpColor)
}

// parsePath decodes a shape's path, whose keyframes morph between paths of
// as many vertices, open or closed alike.
func parsePath(data json.RawMessage) (property[pathData], error) {
	if len(data) == 0 {
		return property[pathData]{}, fmt.Errorf("missing path property")
	}
	p, err := parseProperty(data, pathData{}, pathValue, lerpPath)
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

// easing returns the keyframe's temporal easing, nil for linear. Its y
// controls may leave [0, 1], as back and overshoot easings do; its x
// controls are clamped there, which keeps time moving forward.
func (f *rawKeyframe) easing() easing {
	if len(f.I.X) == 0 || len(f.I.Y) == 0 || len(f.O.X) == 0 || len(f.O.Y) == 0 {
		return nil
	}
	return cubicBezier(f.O.X[0], f.O.Y[0], f.I.X[0], f.I.Y[0])
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
