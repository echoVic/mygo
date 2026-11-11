package lottie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

type point struct {
	X, Y float32
}

type color struct {
	R, G, B, A float32
}

type easing func(float32) float32

type rawProperty struct {
	Expression json.RawMessage `json:"x"`
	Split      bool            `json:"s"`
	Animated   int             `json:"a"`
	K          json.RawMessage `json:"k"`
}

type rawBezier struct{ X, Y []float32 }

func (b *rawBezier) UnmarshalJSON(data []byte) error {
	var raw struct{ X, Y json.RawMessage }
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, v := range []struct {
		raw json.RawMessage
		dst *[]float32
	}{{raw.X, &b.X}, {raw.Y, &b.Y}} {
		if len(v.raw) == 0 {
			continue
		}
		if v.raw[0] == '[' {
			if err := json.Unmarshal(v.raw, v.dst); err != nil {
				return err
			}
		} else {
			var n float32
			if err := json.Unmarshal(v.raw, &n); err != nil {
				return err
			}
			*v.dst = []float32{n}
		}
	}
	return nil
}

type rawKeyframe struct {
	To, Ti json.RawMessage
	T      float32         `json:"t"`
	S      json.RawMessage `json:"s"`
	E      json.RawMessage `json:"e"`
	H      int             `json:"h"`
	I      rawBezier       `json:"i"`
	O      rawBezier       `json:"o"`
}

type scalarKeyframe struct {
	t     float32
	start float32
	end   float32
	hold  bool
	ease  easing
}

type scalarProperty struct {
	static float32
	keys   []scalarKeyframe
}

func (p scalarProperty) sample(frame float32) float32 {
	if len(p.keys) == 0 {
		return p.static
	}
	if frame <= p.keys[0].t {
		return p.keys[0].start
	}
	for i := 0; i+1 < len(p.keys); i++ {
		k, next := p.keys[i], p.keys[i+1]
		if frame >= next.t {
			continue
		}
		if k.hold || next.t <= k.t {
			return k.start
		}
		t := (frame - k.t) / (next.t - k.t)
		if k.ease != nil {
			t = k.ease(t)
		}
		return k.start + (k.end-k.start)*t
	}
	return p.keys[len(p.keys)-1].start
}

type pointKeyframe struct {
	t     float32
	start point
	end   point
	hold  bool
	ease  easing
}

type pointProperty struct {
	static point
	keys   []pointKeyframe
}

func (p pointProperty) sample(frame float32) point {
	if len(p.keys) == 0 {
		return p.static
	}
	if frame <= p.keys[0].t {
		return p.keys[0].start
	}
	for i := 0; i+1 < len(p.keys); i++ {
		k, next := p.keys[i], p.keys[i+1]
		if frame >= next.t {
			continue
		}
		if k.hold || next.t <= k.t {
			return k.start
		}
		t := (frame - k.t) / (next.t - k.t)
		if k.ease != nil {
			t = k.ease(t)
		}
		return point{X: k.start.X + (k.end.X-k.start.X)*t, Y: k.start.Y + (k.end.Y-k.start.Y)*t}
	}
	return p.keys[len(p.keys)-1].start
}

type colorKeyframe struct {
	t     float32
	start color
	end   color
	hold  bool
	ease  easing
}

type colorProperty struct {
	static color
	keys   []colorKeyframe
}

func (p colorProperty) sample(frame float32) color {
	if len(p.keys) == 0 {
		return p.static
	}
	if frame <= p.keys[0].t {
		return p.keys[0].start
	}
	for i := 0; i+1 < len(p.keys); i++ {
		k, next := p.keys[i], p.keys[i+1]
		if frame >= next.t {
			continue
		}
		if k.hold || next.t <= k.t {
			return k.start
		}
		t := (frame - k.t) / (next.t - k.t)
		if k.ease != nil {
			t = k.ease(t)
		}
		return color{
			R: k.start.R + (k.end.R-k.start.R)*t,
			G: k.start.G + (k.end.G-k.start.G)*t,
			B: k.start.B + (k.end.B-k.start.B)*t,
			A: k.start.A + (k.end.A-k.start.A)*t,
		}
	}
	return p.keys[len(p.keys)-1].start
}

type pathData struct {
	Vertices []point
	In       []point
	Out      []point
	Closed   bool
}

type pathKeyframe struct {
	t     float32
	start pathData
	end   pathData
	hold  bool
	ease  easing
}

type pathProperty struct {
	static pathData
	keys   []pathKeyframe
}

func (p pathProperty) sample(frame float32) pathData {
	if len(p.keys) == 0 {
		return p.static
	}
	if frame <= p.keys[0].t {
		return p.keys[0].start
	}
	for i := 0; i+1 < len(p.keys); i++ {
		k, next := p.keys[i], p.keys[i+1]
		if frame >= next.t {
			continue
		}
		if k.hold || next.t <= k.t || len(k.start.Vertices) != len(k.end.Vertices) {
			return k.start
		}
		t := (frame - k.t) / (next.t - k.t)
		if k.ease != nil {
			t = k.ease(t)
		}
		return lerpPath(k.start, k.end, t)
	}
	return p.keys[len(p.keys)-1].start
}

func lerpPath(a, b pathData, t float32) pathData {
	out := pathData{Closed: a.Closed}
	out.Vertices = make([]point, len(a.Vertices))
	out.In = make([]point, len(a.In))
	out.Out = make([]point, len(a.Out))
	for i := range out.Vertices {
		out.Vertices[i] = lerpPoint(a.Vertices[i], b.Vertices[i], t)
	}
	for i := range out.In {
		out.In[i] = lerpPoint(a.In[i], b.In[i], t)
	}
	for i := range out.Out {
		out.Out[i] = lerpPoint(a.Out[i], b.Out[i], t)
	}
	return out
}

func lerpPoint(a, b point, t float32) point {
	return point{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t}
}

func parseScalar(data json.RawMessage, def float32) (scalarProperty, error) {
	if len(data) == 0 {
		return scalarProperty{static: def}, nil
	}
	var raw rawProperty
	if err := decodeProperty(data, &raw); err != nil {
		return scalarProperty{}, fmt.Errorf("property: %w", err)
	}
	if raw.Animated == 0 {
		var value float32
		if err := json.Unmarshal(raw.K, &value); err != nil {
			return scalarProperty{}, fmt.Errorf("static scalar: %w", err)
		}
		return scalarProperty{static: value}, nil
	}
	var frames []rawKeyframe
	if err := decodeKeyframes(raw.K, &frames); err != nil {
		return scalarProperty{}, fmt.Errorf("animated scalar: %w", err)
	}
	if len(frames) == 0 {
		return scalarProperty{static: def}, nil
	}
	keys := make([]scalarKeyframe, len(frames))
	for i, frame := range frames {
		start, err := firstFloat(frame.S)
		if err != nil {
			return scalarProperty{}, fmt.Errorf("scalar keyframe %d start: %w", i, err)
		}
		end := start
		if len(frame.E) > 0 {
			end, err = firstFloat(frame.E)
			if err != nil {
				return scalarProperty{}, fmt.Errorf("scalar keyframe %d end: %w", i, err)
			}
		} else if i+1 < len(frames) {
			end, err = firstFloat(frames[i+1].S)
			if err != nil {
				return scalarProperty{}, fmt.Errorf("scalar keyframe %d next start: %w", i, err)
			}
		}
		keys[i] = scalarKeyframe{t: frame.T, start: start, end: end, hold: frame.H == 1, ease: frameEase(frame)}
	}
	return scalarProperty{keys: keys}, nil
}

func parsePoint(data json.RawMessage, def point) (pointProperty, error) {
	if len(data) == 0 {
		return pointProperty{static: def}, nil
	}
	var raw rawProperty
	if err := decodeProperty(data, &raw); err != nil {
		return pointProperty{}, fmt.Errorf("point property: %w", err)
	}
	if raw.Animated == 0 {
		value, err := pointFromJSON(raw.K)
		if err != nil {
			return pointProperty{}, fmt.Errorf("static point: %w", err)
		}
		return pointProperty{static: value}, nil
	}
	var frames []rawKeyframe
	if err := decodeKeyframes(raw.K, &frames); err != nil {
		return pointProperty{}, fmt.Errorf("animated point: %w", err)
	}
	keys := make([]pointKeyframe, len(frames))
	for i, frame := range frames {
		start, err := pointFromJSON(frame.S)
		if err != nil {
			return pointProperty{}, fmt.Errorf("point keyframe %d start: %w", i, err)
		}
		end := start
		if len(frame.E) > 0 {
			end, err = pointFromJSON(frame.E)
			if err != nil {
				return pointProperty{}, fmt.Errorf("point keyframe %d end: %w", i, err)
			}
		} else if i+1 < len(frames) {
			end, err = pointFromJSON(frames[i+1].S)
			if err != nil {
				return pointProperty{}, fmt.Errorf("point keyframe %d next start: %w", i, err)
			}
		}
		keys[i] = pointKeyframe{t: frame.T, start: start, end: end, hold: frame.H == 1, ease: frameEase(frame)}
	}
	return pointProperty{keys: keys}, nil
}

func parseColor(data json.RawMessage, def color) (colorProperty, error) {
	if len(data) == 0 {
		return colorProperty{static: def}, nil
	}
	var raw rawProperty
	if err := decodeProperty(data, &raw); err != nil {
		return colorProperty{}, fmt.Errorf("color property: %w", err)
	}
	if raw.Animated == 0 {
		value, err := colorFromJSON(raw.K)
		if err != nil {
			return colorProperty{}, fmt.Errorf("static color: %w", err)
		}
		return colorProperty{static: value}, nil
	}
	var frames []rawKeyframe
	if err := decodeKeyframes(raw.K, &frames); err != nil {
		return colorProperty{}, fmt.Errorf("animated color: %w", err)
	}
	keys := make([]colorKeyframe, len(frames))
	for i, frame := range frames {
		start, err := colorFromJSON(frame.S)
		if err != nil {
			return colorProperty{}, fmt.Errorf("color keyframe %d start: %w", i, err)
		}
		end := start
		if len(frame.E) > 0 {
			end, err = colorFromJSON(frame.E)
			if err != nil {
				return colorProperty{}, fmt.Errorf("color keyframe %d end: %w", i, err)
			}
		} else if i+1 < len(frames) {
			end, err = colorFromJSON(frames[i+1].S)
			if err != nil {
				return colorProperty{}, fmt.Errorf("color keyframe %d next start: %w", i, err)
			}
		}
		keys[i] = colorKeyframe{t: frame.T, start: start, end: end, hold: frame.H == 1, ease: frameEase(frame)}
	}
	return colorProperty{keys: keys}, nil
}

func parsePath(data json.RawMessage) (pathProperty, error) {
	if len(data) == 0 {
		return pathProperty{}, fmt.Errorf("missing path property")
	}
	var raw rawProperty
	if err := decodeProperty(data, &raw); err != nil {
		return pathProperty{}, fmt.Errorf("path property: %w", err)
	}
	if raw.Animated == 0 {
		value, err := pathFromJSON(raw.K)
		if err != nil {
			return pathProperty{}, fmt.Errorf("static path: %w", err)
		}
		return pathProperty{static: value}, nil
	}
	var frames []rawKeyframe
	if err := decodeKeyframes(raw.K, &frames); err != nil {
		return pathProperty{}, fmt.Errorf("animated path: %w", err)
	}
	keys := make([]pathKeyframe, len(frames))
	for i, frame := range frames {
		start, err := pathFromFrameJSON(frame.S)
		if err != nil {
			return pathProperty{}, fmt.Errorf("path keyframe %d start: %w", i, err)
		}
		end := start
		if len(frame.E) > 0 {
			end, err = pathFromFrameJSON(frame.E)
			if err != nil {
				return pathProperty{}, fmt.Errorf("path keyframe %d end: %w", i, err)
			}
		} else if i+1 < len(frames) {
			end, err = pathFromFrameJSON(frames[i+1].S)
			if err != nil {
				return pathProperty{}, fmt.Errorf("path keyframe %d next start: %w", i, err)
			}
		}
		if start.Closed != end.Closed || len(start.Vertices) != len(end.Vertices) || len(start.In) != len(end.In) || len(start.Out) != len(end.Out) {
			return pathProperty{}, fmt.Errorf("path keyframe %d changes vertex count", i)
		}
		keys[i] = pathKeyframe{t: frame.T, start: start, end: end, hold: frame.H == 1, ease: frameEase(frame)}
	}
	return pathProperty{keys: keys}, nil
}

func frameEase(frame rawKeyframe) easing {
	if len(frame.I.X) == 0 || len(frame.I.Y) == 0 || len(frame.O.X) == 0 || len(frame.O.Y) == 0 {
		return nil
	}
	return cubicBezier(frame.O.X[0], frame.O.Y[0], frame.I.X[0], frame.I.Y[0])
}

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
		if x <= 0 || x >= 1 {
			return x
		}
		t := x
		for i := 0; i < 8; i++ {
			e := curveX(t) - x
			d := (3*ax*t+2*bx)*t + cx
			if math.Abs(float64(d)) < 1e-6 {
				break
			}
			if math.Abs(float64(e)) < 1e-6 {
				return curveY(t)
			}
			t = clamp(t-e/d, 0, 1)
		}
		lo, hi := float32(0), float32(1)
		for i := 0; i < 32; i++ {
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

func pointFromJSON(data json.RawMessage) (point, error) {
	var values []float32
	if err := json.Unmarshal(data, &values); err != nil {
		return point{}, err
	}
	if len(values) < 2 {
		return point{}, fmt.Errorf("want two coordinates")
	}
	return point{X: values[0], Y: values[1]}, nil
}

func colorFromJSON(data json.RawMessage) (color, error) {
	var values []float32
	if err := json.Unmarshal(data, &values); err != nil {
		return color{}, err
	}
	if len(values) < 3 {
		return color{}, fmt.Errorf("want at least three channels")
	}
	a := float32(1)
	if len(values) > 3 {
		a = values[3]
	}
	return color{R: values[0], G: values[1], B: values[2], A: a}, nil
}

func pathFromFrameJSON(data json.RawMessage) (pathData, error) {
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return pathData{}, err
	}
	if len(values) == 0 {
		return pathData{}, fmt.Errorf("empty path keyframe")
	}
	return pathFromJSON(values[0])
}

func pathFromJSON(data json.RawMessage) (pathData, error) {
	var raw struct {
		I [][]float32 `json:"i"`
		O [][]float32 `json:"o"`
		V [][]float32 `json:"v"`
		C bool        `json:"c"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return pathData{}, err
	}
	if len(raw.V) == 0 || len(raw.I) != len(raw.V) || len(raw.O) != len(raw.V) {
		return pathData{}, fmt.Errorf("path vertices and tangents must have equal non-zero lengths")
	}
	vertices, err := pointsFromArrays(raw.V)
	if err != nil {
		return pathData{}, fmt.Errorf("vertices: %w", err)
	}
	in, err := pointsFromArrays(raw.I)
	if err != nil {
		return pathData{}, fmt.Errorf("in tangents: %w", err)
	}
	out, err := pointsFromArrays(raw.O)
	if err != nil {
		return pathData{}, fmt.Errorf("out tangents: %w", err)
	}
	return pathData{Vertices: vertices, In: in, Out: out, Closed: raw.C}, nil
}

func firstFloat(data json.RawMessage) (float32, error) {
	var values []float32
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &values); err != nil {
			return 0, err
		}
		if len(values) == 0 {
			return 0, fmt.Errorf("empty value")
		}
		return values[0], nil
	}
	var value float32
	if err := json.Unmarshal(data, &value); err != nil {
		return 0, err
	}
	return value, nil
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (c color) ui(alpha float32) ui.Color {
	return ui.RGBA(uint8(clamp(c.R, 0, 1)*255+0.5), uint8(clamp(c.G, 0, 1)*255+0.5), uint8(clamp(c.B, 0, 1)*255+0.5), clamp(c.A*alpha, 0, 1))
}

func pointsFromArrays(values [][]float32) ([]point, error) {
	points := make([]point, len(values))
	for i, value := range values {
		if len(value) < 2 {
			return nil, fmt.Errorf("point %d has fewer than two coordinates", i)
		}
		points[i] = point{X: value[0], Y: value[1]}
	}
	return points, nil
}

func decodeProperty(data json.RawMessage, dst *rawProperty) error {
	if err := json.Unmarshal(data, dst); err != nil {
		return err
	}
	if dst.Split || (len(dst.Expression) > 0 && !bytes.Equal(dst.Expression, []byte(`""`)) && !bytes.Equal(dst.Expression, []byte("null"))) {
		return fmt.Errorf("unsupported expression or split position")
	}
	if dst.Animated != 0 && dst.Animated != 1 {
		return fmt.Errorf("unsupported animation flag %d", dst.Animated)
	}
	return nil
}
func decodeKeyframes(data json.RawMessage, dst *[]rawKeyframe) error {
	if err := json.Unmarshal(data, dst); err != nil {
		return err
	}
	frames := *dst
	if len(frames) == 0 {
		return fmt.Errorf("animated property has no keyframes")
	}
	for i := range frames {
		f := &frames[i]
		if i > 0 && !(f.T > frames[i-1].T) {
			return fmt.Errorf("keyframe times must increase")
		}
		for _, tangent := range []json.RawMessage{f.To, f.Ti} {
			if len(tangent) == 0 {
				continue
			}
			var values []float32
			if err := json.Unmarshal(tangent, &values); err != nil {
				return fmt.Errorf("invalid spatial tangents: %w", err)
			}
			for _, v := range values {
				if v != 0 {
					return fmt.Errorf("unsupported spatial tangents")
				}
			}
		}
		for _, b := range []rawBezier{f.I, f.O} {
			for _, v := range [][]float32{b.X, b.Y} {
				for j := 1; j < len(v); j++ {
					if v[j] != v[0] {
						return fmt.Errorf("unsupported component-specific easing")
					}
				}
			}
		}
		if len(f.S) == 0 || bytes.Equal(f.S, []byte("null")) {
			if i != len(frames)-1 || i == 0 {
				return fmt.Errorf("keyframe missing start value")
			}
			f.S = frames[i-1].E
			if len(f.S) == 0 {
				f.S = frames[i-1].S
			}
		}
	}
	return nil
}
