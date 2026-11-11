package lottie

import "math"

// spatialSegments is how many chords measure a spatial curve's length:
// the size of the table Load keeps for a keyframe that has one.
const spatialSegments = 64

// spatialCurve moves a point along a keyframe's spatial curve, the cubic
// Bézier from its value, out along its out tangent (to), into the next
// keyframe's value along its in tangent (ti), as After Effects' motion
// paths do: eased progress is a fraction of the curve's length, not of
// its parameter. Progress that overshoots holds the point at the end of
// the curve it passed, as lottie-web does past the far end.
type spatialCurve struct {
	start, end point
	ease       easing
	// p is the curve's control points: start, start+to, end+ti, end.
	p [4][2]float64
	// lengths[i] is the curve's length from its start to its parameter
	// (i+1)/spatialSegments, along chords.
	lengths [spatialSegments]float32
}

// alongCurve is a point's tween along its spatial curve, nil for a curve
// too short to measure, which then moves in a straight line.
func alongCurve(start, end, out, in point, ease easing) tween[point] {
	if c := newSpatialCurve(start, end, out, in, ease); c != nil {
		return c
	}
	return nil
}

// newSpatialCurve measures the curve once, for every frame that samples
// it; nil if it has no length.
func newSpatialCurve(start, end, out, in point, ease easing) *spatialCurve {
	x0, y0, x1, y1 := float64(start.X), float64(start.Y), float64(end.X), float64(end.Y)
	c := &spatialCurve{start: start, end: end, ease: ease, p: [4][2]float64{
		{x0, y0}, {x0 + float64(out.X), y0 + float64(out.Y)}, {x1 + float64(in.X), y1 + float64(in.Y)}, {x1, y1},
	}}
	length, x, y := 0.0, x0, y0
	for i := range c.lengths {
		nx, ny := c.point(float64(i+1) / spatialSegments)
		length += math.Hypot(nx-x, ny-y)
		c.lengths[i] = float32(length)
		x, y = nx, ny
	}
	if !(c.lengths[spatialSegments-1] > 0) {
		return nil
	}
	return c
}

// point returns the curve's point at its parameter t.
func (c *spatialCurve) point(t float64) (x, y float64) {
	u := 1 - t
	a, b, d, e := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	p := &c.p
	return a*p[0][0] + b*p[1][0] + d*p[2][0] + e*p[3][0], a*p[0][1] + b*p[1][1] + d*p[2][1] + e*p[3][1]
}

// at returns the point as far along the curve as t eased says, exactly
// the start or the end at and beyond them.
func (c *spatialCurve) at(t float32) point {
	if c.ease != nil {
		t = c.ease(t)
	}
	total := float64(c.lengths[spatialSegments-1])
	s := float64(t) * total
	if !(s > 0) {
		return c.start
	}
	if !(s < total) {
		return c.end
	}
	// The first chord that ends past s holds it: lengths[i] > s >= from.
	i, hi := 0, spatialSegments-1
	for i < hi {
		if mid := (i + hi) / 2; float64(c.lengths[mid]) > s {
			hi = mid
		} else {
			i = mid + 1
		}
	}
	from := 0.0
	if i > 0 {
		from = float64(c.lengths[i-1])
	}
	x, y := c.point((float64(i) + (s-from)/(float64(c.lengths[i])-from)) / spatialSegments)
	return point{float32(x), float32(y)}
}
