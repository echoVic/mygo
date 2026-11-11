package lottie

import (
	"fmt"
	"math"
)

// trim is a trim paths item (tm). It cuts the geometry before it in its
// group, nested groups' included, to the part from its start to its end,
// percentages of their length, turned by its offset, in degrees, of which
// 360 are the whole length. Simultaneously (m 1), it cuts each geometry's
// paths on their own; individually (m 2), it lays the geometry's paths
// end to end, in the order they are listed, and cuts those. What it
// leaves is what every style that paints the geometry paints, those
// before it in the list too. It follows lottie-web's TrimModifier.js.
type trim struct {
	start, end, offset property[float32]
	individually       bool
	// lo and hi bound the geometry it cuts, in the layer's list: what
	// was listed between its group's start and it.
	lo, hi int
	// animated is false for a trim whose cut Load makes once: of static
	// values, cutting outlines that are static where it cuts them.
	animated bool
}

// parseTrim reads a trim paths item, whose start (s), end (e) and offset
// (o) are each static or keyframed, and whose mode (m) is 1 or 2.
func parseTrim(it *rawShape) (trim, error) {
	var t trim
	var err error
	for _, p := range []struct {
		name string
		raw  []byte
		def  float32
		dst  *property[float32]
	}{{"start", it.S, 0, &t.start}, {"end", it.End, 100, &t.end}, {"offset", it.O, 0, &t.offset}} {
		if *p.dst, err = parseScalar(p.raw, p.def); err != nil {
			return trim{}, fmt.Errorf("trim %s: %w", p.name, err)
		}
	}
	mode, ok := enum(it.Mode, 3)
	if !ok {
		return trim{}, fmt.Errorf("unsupported trim mode %s", it.Mode)
	}
	t.individually = mode == 2
	return t, nil
}

// trimRange returns where a trim cuts at frame, as lottie-web's
// TrimModifier.processShapes has it: start and end as fractions of the
// length, clamped to [0, 1], plus the offset's turn in [0, 1), in order,
// rounded to 1e-4. Past 1, a cut wraps around to the start.
func trimRange(start, end, offset float32) (s, e float64) {
	o := math.Mod(float64(offset), 360) / 360
	if o < 0 {
		o++
	}
	if !finite(o) {
		o = 0
	}
	at := func(v float32) float64 {
		switch f := float64(v) / 100; {
		case f > 1:
			return 1 + o
		case f >= 0:
			return f + o
		default: // below 0, or NaN
			return o
		}
	}
	s, e = at(start), at(end)
	if s > e {
		s, e = e, s
	}
	return math.Round(s*1e4) / 1e4, math.Round(e*1e4) / 1e4
}

// apply cuts outlines[t.lo:t.hi] at frame, in place: it never changes
// what their paths hold, only which paths the outlines hold.
func (t *trim) apply(outlines []outline, frame float32) {
	s, e := trimRange(t.start.sample(frame), t.end.sample(frame), t.offset.sample(frame))
	cover := outlines[t.lo:t.hi]
	switch {
	case s == e:
		clear(cover)
	case s == 0 && e == 1:
	case t.individually && len(cover) > 1:
		trimIndividually(cover, s, e)
	default:
		for i := range cover {
			cover[i] = cover[i].trim(s, e)
		}
	}
}

// outline is a geometry's paths, as the trims before have cut them: its
// one path, the pieces they left, or none.
type outline struct {
	paths []pathData
	// lengths is the length of each of the paths' segments, path after
	// path, once measure has measured them.
	lengths []float64
}

// segments returns how many segments a path has: between its vertices,
// and back to the first when it is closed.
func (p *pathData) segments() int {
	if p.Closed {
		return len(p.Vertices)
	}
	return max(len(p.Vertices)-1, 0)
}

// segment returns the path's j-th segment.
func (p *pathData) segment(j int) cubic {
	k := (j + 1) % len(p.Vertices)
	return newCubic(p.Vertices[j], p.Out[j], p.In[k], p.Vertices[k])
}

// measure measures the outline's segments, if it hasn't.
func (o *outline) measure() {
	if o.lengths != nil {
		return
	}
	n := 0
	for i := range o.paths {
		n += o.paths[i].segments()
	}
	o.lengths = make([]float64, 0, n)
	var lengths chordLengths
	for i := range o.paths {
		p := &o.paths[i]
		for j := range p.segments() {
			c := p.segment(j)
			o.lengths = append(o.lengths, c.measure(&lengths))
		}
	}
}

// length returns the outline's length, which measure measured.
func (o *outline) length() float64 {
	total := 0.0
	for _, l := range o.lengths {
		total += l
	}
	return total
}

// trim returns what a trim keeps of the outline, simultaneously, from s
// to e, fractions of its length, 0 <= s < e <= 2: past 1 it wraps around
// to the start, and when the outline's last path is closed, the piece
// that ends it goes on into the piece that starts its first.
func (o outline) trim(s, e float64) outline {
	o.measure()
	total := o.length()
	if !(total > 0) {
		return outline{}
	}
	switch {
	case e <= 1:
		pieces, _, _ := o.cut(s*total, e*total)
		return outline{paths: pieces}
	case s >= 1:
		pieces, _, _ := o.cut((s-1)*total, (e-1)*total)
		return outline{paths: pieces}
	}
	tail, _, last := o.cut(s*total, total)
	head, first, _ := o.cut(0, (e-1)*total)
	if n := len(o.paths); len(tail) > 0 && len(head) > 0 && o.paths[n-1].Closed && last == n-1 && first == 0 {
		tail[len(tail)-1] = join(tail[len(tail)-1], head[0])
		head = head[1:]
	}
	return outline{paths: append(tail, head...)}
}

// trimIndividually cuts outlines laid end to end, in their order, from s
// to e, fractions of their length together, as lottie-web's
// calculateShapeEdges has it: each keeps what of the cut falls on it,
// in up to two pieces, which it doesn't join.
func trimIndividually(outlines []outline, s, e float64) {
	total := 0.0
	for i := range outlines {
		outlines[i].measure()
		total += outlines[i].length()
	}
	parts := [][2]float64{{s * total, e * total}}
	switch {
	case s >= 1:
		parts[0] = [2]float64{(s - 1) * total, (e - 1) * total}
	case e > 1:
		parts = [][2]float64{{s * total, total}, {0, (e - 1) * total}}
	}
	at := 0.0
	for i := range outlines {
		o := &outlines[i]
		length := o.length()
		var pieces []pathData
		for _, part := range parts {
			p, _, _ := o.cut(max(part[0]-at, 0), min(part[1]-at, length))
			pieces = append(pieces, p...)
		}
		at += length
		*o = outline{paths: pieces}
	}
}

// cut returns the pieces of the outline's paths from length from to
// length to along them, laid end to end, which measure measured, and the
// index of the path the first comes from and of the last's, -1 without
// pieces. A path the cut keeps whole is its own piece, closed if it was;
// the others' are open, a segment the cut ends in split, as lottie-web's
// addShapes splits it. Unlike lottie-web, it doesn't keep a segment of
// no length that the cut only touches: the end of one before from, or
// the start of one at to.
func (o *outline) cut(from, to float64) (pieces []pathData, first, last int) {
	first, last = -1, -1
	if !(to > from) {
		return nil, first, last
	}
	at, k := 0.0, 0
	for i := range o.paths {
		p := &o.paths[i]
		lengths := o.lengths[k : k+p.segments()]
		k += len(lengths)
		var b pieceBuilder
		whole, done := len(lengths) > 0, false
		for j, l := range lengths {
			end := at + l
			switch {
			case l > 0 && end <= from, l == 0 && at < from:
				whole = false
			case at > to, l > 0 && at >= to:
				whole, done = false, true
			case from <= at && end <= to:
				b.whole(p, j)
			default:
				c := p.segment(j)
				var chords chordLengths
				c.measure(&chords)
				b.part(c.split(c.at(&chords, from-at), c.at(&chords, to-at)))
				whole = false
			}
			if done {
				break
			}
			at = end
		}
		switch {
		case whole:
			pieces = append(pieces, *p)
		case len(b.v) > 0:
			pieces = append(pieces, pathData{Vertices: b.v, In: b.in, Out: b.out})
		default:
			if done {
				return pieces, first, last
			}
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
		if done {
			break
		}
	}
	return pieces, first, last
}

// pieceBuilder builds an open piece of a path, segment after segment, as
// lottie-web's addSegment does: its first vertex without an in tangent,
// its last without an out tangent.
type pieceBuilder struct{ v, in, out []point }

// whole adds the path's j-th segment.
func (b *pieceBuilder) whole(p *pathData, j int) {
	k := (j + 1) % len(p.Vertices)
	b.add(p.Vertices[j], p.Out[j], p.In[k], p.Vertices[k])
}

// part adds a part of a segment.
func (b *pieceBuilder) part(c cubic) {
	at := func(q [2]float64) point { return point{float32(q[0]), float32(q[1])} }
	start, end := at(c[0]), at(c[3])
	out := point{float32(c[1][0] - c[0][0]), float32(c[1][1] - c[0][1])}
	in := point{float32(c[2][0] - c[3][0]), float32(c[2][1] - c[3][1])}
	b.add(start, out, in, end)
}

// add adds the segment from start, out along out, into end along in,
// which goes on from the piece's last vertex, start.
func (b *pieceBuilder) add(start, out, in, end point) {
	if len(b.v) == 0 {
		b.v, b.in, b.out = append(b.v, start), append(b.in, point{}), append(b.out, out)
	} else {
		b.out[len(b.out)-1] = out
	}
	b.v, b.in, b.out = append(b.v, end), append(b.in, in), append(b.out, point{})
}

// join returns the piece that goes from a on into b, as lottie-web joins
// a cut that wraps around a closed path: a straight line between them,
// or none where a ends at b's start, as it does around one path. The
// joined piece is open.
func join(a, b pathData) pathData {
	a, b = opened(a), opened(b)
	n := len(a.Vertices)
	merge := a.Vertices[n-1] == b.Vertices[0]
	size := n + len(b.Vertices)
	if merge {
		size--
	}
	j := pathData{Vertices: make([]point, 0, size), In: make([]point, 0, size), Out: make([]point, 0, size)}
	j.Vertices, j.In, j.Out = append(j.Vertices, a.Vertices...), append(j.In, a.In...), append(j.Out, a.Out...)
	from := 0
	if merge {
		j.Out[n-1], from = b.Out[0], 1
	} else {
		j.Out[n-1] = point{}
	}
	j.Vertices, j.In, j.Out = append(j.Vertices, b.Vertices[from:]...), append(j.In, b.In[from:]...), append(j.Out, b.Out[from:]...)
	if !merge {
		j.In[n] = point{}
	}
	return j
}

// opened returns a closed path as an open one that ends where it starts,
// along its closing segment; an open path as it is.
func opened(p pathData) pathData {
	if !p.Closed {
		return p
	}
	n := len(p.Vertices)
	o := pathData{Vertices: make([]point, n+1), In: make([]point, n+1), Out: make([]point, n+1)}
	copy(o.Vertices, p.Vertices)
	copy(o.In, p.In)
	copy(o.Out, p.Out)
	o.Vertices[n], o.In[n] = p.Vertices[0], p.In[0]
	o.In[0] = point{}
	return o
}
