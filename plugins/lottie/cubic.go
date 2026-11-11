package lottie

import "math"

// chords is how many chords measure a cubic Bézier curve's length: a
// keyframe's spatial curve, or the segment of a path that a trim cuts.
const chords = 64

// cubic is a cubic Bézier curve: its start, its two control points and
// its end.
type cubic [4][2]float64

// chordLengths is a curve's length from its start to each parameter
// (i+1)/chords, along chords; the last is its length.
type chordLengths [chords]float32

// newCubic returns the curve from start, along its out tangent, into end
// along its in tangent, the tangents relative, as Lottie writes them.
func newCubic(start, out, in, end point) cubic {
	x0, y0, x1, y1 := float64(start.X), float64(start.Y), float64(end.X), float64(end.Y)
	return cubic{{x0, y0}, {x0 + float64(out.X), y0 + float64(out.Y)}, {x1 + float64(in.X), y1 + float64(in.Y)}, {x1, y1}}
}

// point returns the curve's point at its parameter t.
func (c *cubic) point(t float64) (x, y float64) {
	u := 1 - t
	a, b, d, e := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return a*c[0][0] + b*c[1][0] + d*c[2][0] + e*c[3][0], a*c[0][1] + b*c[1][1] + d*c[2][1] + e*c[3][1]
}

// measure sets lengths to the curve's and returns its length.
func (c *cubic) measure(lengths *chordLengths) float64 {
	length, x, y := 0.0, c[0][0], c[0][1]
	for i := range lengths {
		nx, ny := c.point(float64(i+1) / chords)
		length += math.Hypot(nx-x, ny-y)
		lengths[i] = float32(length)
		x, y = nx, ny
	}
	return float64(lengths[chords-1])
}

// at returns the parameter at which the curve, measured to lengths, is d
// long: 0 at and before its start, 1 at and past its end. Within the
// chord that holds d, it finds the point that far from the chord's
// start, on the curve, rather than as far along its parameter, which
// does not run at an even speed.
func (c *cubic) at(lengths *chordLengths, d float64) float64 {
	if !(d > 0) {
		return 0
	}
	if !(d < float64(lengths[chords-1])) {
		return 1
	}
	// The first chord that ends past d holds it: lengths[i] > d >= from.
	i, hi := 0, chords-1
	for i < hi {
		if mid := (i + hi) / 2; float64(lengths[mid]) > d {
			hi = mid
		} else {
			i = mid + 1
		}
	}
	from := 0.0
	if i > 0 {
		from = float64(lengths[i-1])
	}
	lo, up := float64(i)/chords, float64(i+1)/chords
	x0, y0 := c.point(lo)
	want := d - from
	for range 24 {
		mid := (lo + up) / 2
		if x, y := c.point(mid); math.Hypot(x-x0, y-y0) < want {
			lo = mid
		} else {
			up = mid
		}
	}
	return (lo + up) / 2
}

// split returns the part of the curve from its parameter t0 to t1, the
// same curve, exactly its ends at 0 and 1.
func (c *cubic) split(t0, t1 float64) cubic {
	return cubic{c.blossom(t0, t0, t0), c.blossom(t0, t0, t1), c.blossom(t0, t1, t1), c.blossom(t1, t1, t1)}
}

// blossom is the curve's polar form: de Casteljau's construction with
// u, v and w at its three levels. The control points of the part of the
// curve from t0 to t1 are its values at (t0, t0, t0), (t0, t0, t1),
// (t0, t1, t1) and (t1, t1, t1).
func (c *cubic) blossom(u, v, w float64) [2]float64 {
	mix := func(a, b [2]float64, t float64) [2]float64 {
		return [2]float64{a[0]*(1-t) + b[0]*t, a[1]*(1-t) + b[1]*t}
	}
	p, q, r := mix(c[0], c[1], u), mix(c[1], c[2], u), mix(c[2], c[3], u)
	return mix(mix(p, q, v), mix(q, r, v), w)
}
