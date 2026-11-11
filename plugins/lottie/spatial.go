package lottie

// spatialCurve moves a point along a keyframe's spatial curve, the cubic
// Bézier from its value, out along its out tangent (to), into the next
// keyframe's value along its in tangent (ti), as After Effects' motion
// paths do: eased progress is a fraction of the curve's length, not of
// its parameter. Progress that overshoots holds the point at the end of
// the curve it passed, as lottie-web does past the far end.
type spatialCurve struct {
	start, end point
	ease       easing
	curve      cubic
	// lengths is the curve's, measured once by Load for every frame that
	// samples it.
	lengths chordLengths
}

// alongCurve is a point's tween along its spatial curve, nil for a curve
// too short to measure, which then moves in a straight line.
func alongCurve(start, end, out, in point, ease easing) tween[point] {
	c := &spatialCurve{start: start, end: end, ease: ease, curve: newCubic(start, out, in, end)}
	if !(c.curve.measure(&c.lengths) > 0) {
		return nil
	}
	return c
}

// at returns the point as far along the curve as t eased says, exactly
// the start or the end at and beyond them.
func (c *spatialCurve) at(t float32) point {
	if c.ease != nil {
		t = c.ease(t)
	}
	s := float64(t) * float64(c.lengths[chords-1])
	if !(s > 0) {
		return c.start
	}
	if !(s < float64(c.lengths[chords-1])) {
		return c.end
	}
	x, y := c.curve.point(c.curve.at(&c.lengths, s))
	return point{float32(x), float32(y)}
}
