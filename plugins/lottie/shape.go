package lottie

import (
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
)

// shapes is what a layer draws: its items, in a group under the layer's
// transform, and, gathered from all its groups as they are listed, depth
// first, its geometry and the trims that cut it. A style paints, and a
// trim cuts, the geometry listed before it in its group, nested groups'
// included: the items before the style, geometry[lo:hi] for the trim,
// which are the same, as lottie-web collects them for both (searchShapes
// in CVShapeElement.js and SVGShapeElement.js).
type shapes struct {
	group
	geometry []geometry
	// trims are in the order they are listed, depth first, the order
	// in which they cut, each what those before left.
	trims []trim
	// outlines is the geometry's as far as Load could make them (see
	// retain), for every frame.
	outlines []outline
	// moving is the indices of the geometry that is animated, which
	// every frame samples again.
	moving []int
	// animated reports whether outlines changes with the frame.
	animated bool
	// size is how many items the shapes are, which Load counts to bound
	// a frame's work (maxShapes).
	size int
}

// group is a shape group: its items, in the order they are listed, the
// first on top, under its transform. A layer's shapes are in one, in a
// group under the layer's transform.
type group struct {
	transform transform
	items     []item
}

// item is a group's item: geometry, by its index in the layer's list, a
// style, or a nested group.
type item struct {
	geometry int
	style    style
	group    *group
}

// geometry is a group's path, rectangle or ellipse.
type geometry interface {
	outline(frame float32) pathData
	animated() bool
}

// pathShape is a path; rectShape and ellipseShape are clockwise, as
// After Effects draws them, from where lottie-web starts them, unless
// reversed (d 3).
type pathShape struct{ path property[pathData] }

type rectShape struct {
	position, size property[point]
	radius         property[float32]
	reversed       bool
}

type ellipseShape struct {
	position, size property[point]
	reversed       bool
}

func (s pathShape) outline(frame float32) pathData { return s.path.sample(frame) }
func (s pathShape) animated() bool                 { return s.path.animated() }

func (s rectShape) outline(frame float32) pathData {
	return directed(rectPath(s.position.sample(frame), s.size.sample(frame), s.radius.sample(frame)), s.reversed)
}
func (s rectShape) animated() bool {
	return s.position.animated() || s.size.animated() || s.radius.animated()
}

func (s ellipseShape) outline(frame float32) pathData {
	return directed(ellipsePath(s.position.sample(frame), s.size.sample(frame)), s.reversed)
}
func (s ellipseShape) animated() bool { return s.position.animated() || s.size.animated() }

// retain makes once what every frame draws alike: the outlines of static
// geometry, cut by the trims that are static there, of static values and
// cutting nothing that changes; and the lengths of the outlines that the
// other trims cut first, which they would measure every frame.
func (sh *shapes) retain() {
	sh.outlines = make([]outline, len(sh.geometry))
	changes := make([]bool, len(sh.geometry))
	for i, g := range sh.geometry {
		if g.animated() {
			changes[i] = true
			sh.moving = append(sh.moving, i)
			continue
		}
		sh.outlines[i] = outline{paths: []pathData{g.outline(0)}}
	}
	for k := range sh.trims {
		t := &sh.trims[k]
		t.animated = t.start.animated() || t.end.animated() || t.offset.animated() || slices.Contains(changes[t.lo:t.hi], true)
		if !t.animated {
			t.apply(sh.outlines, 0)
			continue
		}
		for i := t.lo; i < t.hi; i++ {
			if !changes[i] {
				sh.outlines[i].measure()
			}
			changes[i] = true
		}
	}
	sh.animated = slices.Contains(changes, true)
}

// outlinesAt returns the geometry's outlines at frame: those Load
// retained, with animated geometry sampled and cut by the animated trims.
// It changes nothing Load made, which players share.
func (sh *shapes) outlinesAt(frame float32) []outline {
	if !sh.animated {
		return sh.outlines
	}
	out := slices.Clone(sh.outlines)
	for _, i := range sh.moving {
		out[i] = outline{paths: []pathData{sh.geometry[i].outline(frame)}}
	}
	for k := range sh.trims {
		if t := &sh.trims[k]; t.animated {
			t.apply(out, frame)
		}
	}
	return out
}

// drawing is a layer's shapes drawn at a frame: their outlines, made
// when a style first paints them.
type drawing struct {
	shapes   *shapes
	frame    float32
	made     bool
	outlines []outline
}

func (d *drawing) outline(i int) *outline {
	if !d.made {
		d.outlines, d.made = d.shapes.outlinesAt(d.frame), true
	}
	return &d.outlines[i]
}

// draw draws the shapes at frame, under parent, which maps the layer's
// coordinates into the painter's, with alpha.
func (sh *shapes) draw(p *ui.Painter, path *ui.Path, parent affine, frame, alpha float32) {
	d := drawing{shapes: sh, frame: frame}
	sh.group.draw(p, path, &d, parent, alpha)
}

// draw draws the group under parent: its items from the last to the
// first, each style painting the geometry before it, each nested group
// its own. A transform that doesn't map the group's coordinates to finite
// ones skips the group, and what is in it.
func (g *group) draw(p *ui.Painter, path *ui.Path, d *drawing, parent affine, alpha float32) {
	alpha *= percent(g.transform.opacity, d.frame)
	if !(alpha > 0) {
		return
	}
	m := parent.mul(g.transform.at(d.frame))
	if !m.finite() {
		return
	}
	for i := len(g.items) - 1; i >= 0; i-- {
		switch it := &g.items[i]; {
		case it.style != nil:
			it.style.paint(p, path, g.items[:i], d, m, alpha)
		case it.group != nil:
			it.group.draw(p, path, d, m, alpha)
		}
	}
}

// style is a group's fill or stroke.
type style interface {
	// paint paints the geometry among items, nested groups' too, whose
	// coordinates m maps into the painter's, with alpha, through path,
	// which it may reset.
	paint(p *ui.Painter, path *ui.Path, items []item, d *drawing, m affine, alpha float32)
}

type fill struct {
	rule    ui.FillRule
	color   property[color]
	opacity property[float32]
}

type stroke struct {
	cap            ui.LineCap
	join           ui.LineJoin
	miter          float32
	color          property[color]
	opacity, width property[float32]
}

// A fill's outlines are mapped by m whole, exactly, so that they are
// rasterized at the size they show. Its open pieces close, as a fill
// closes every path.
func (f *fill) paint(p *ui.Painter, path *ui.Path, items []item, d *drawing, m affine, alpha float32) {
	alpha *= percent(f.opacity, d.frame)
	if !(alpha > 0) || !trace(path, items, d, m) {
		return
	}
	p.FillPath(path.FillRule(f.rule), f.color.sample(d.frame).ui(alpha))
}

// A stroke's outlines are mapped by m whole when m scales evenly, as
// viewports and most transforms do, its width scaled with them. Otherwise
// they are scaled evenly as far as m stretches most, which keeps the
// stroke's shape, and the painter transforms them the rest of the way,
// which only shrinks them. Its width is in its group's coordinates, the
// transforms of nested groups moving only their geometry's points.
func (s *stroke) paint(p *ui.Painter, path *ui.Path, items []item, d *drawing, m affine, alpha float32) {
	alpha *= percent(s.opacity, d.frame)
	if !(alpha > 0) {
		return
	}
	scale, even := m.similarity()
	place := m
	if !even {
		scale = m.maxScale()
		place = scaling(scale)
	}
	width := float64(s.width.sample(d.frame)) * scale
	if !(width > 0) || !drawable(width) || !trace(path, items, d, place) {
		return
	}
	path.Cap(s.cap).Join(s.join).MiterLimit(s.miter)
	c := s.color.sample(d.frame).ui(alpha)
	if even {
		p.StrokePath(path, float32(width), c)
		return
	}
	rest, ok := m.mul(scaling(1 / scale)).transform()
	if !ok {
		return
	}
	p.Transform(rest, 0, 0, func() { p.StrokePath(path, float32(width), c) })
}

// trace sets path to the outlines of the geometry among items, nested
// groups' under their transforms, its points mapped by m, and reports
// whether there are any, all within what the painter draws (maxDIPs). The
// opacity of nested groups is their own styles', not that of the styles
// that paint their geometry from outside, as in lottie-web.
func trace(path *ui.Path, items []item, d *drawing, m affine) bool {
	path.Reset()
	t := tracer{path: path, drawing: d, ok: true}
	t.items(items, m)
	return t.ok && t.drawn
}

type tracer struct {
	path      *ui.Path
	drawing   *drawing
	ok, drawn bool
}

func (t *tracer) items(items []item, m affine) {
	for i := range items {
		switch it := &items[i]; {
		case it.group != nil:
			if n := m.mul(it.group.transform.at(t.drawing.frame)); n.finite() {
				t.items(it.group.items, n)
			}
		case it.style == nil:
			for _, o := range t.drawing.outline(it.geometry).paths {
				t.add(&o, m)
			}
		}
	}
}

// add adds a path, its points mapped by m.
func (t *tracer) add(o *pathData, m affine) {
	v := o.Vertices
	if len(v) == 0 {
		return
	}
	at := func(p point) (float32, float32) {
		x, y := m.point(p)
		t.ok = t.ok && drawable(x) && drawable(y)
		return float32(x), float32(y)
	}
	add := func(p, d point) point { return point{p.X + d.X, p.Y + d.Y} }
	t.path.MoveTo(at(v[0]))
	curve := func(from, to int) {
		x1, y1 := at(add(v[from], o.Out[from]))
		x2, y2 := at(add(v[to], o.In[to]))
		x, y := at(v[to])
		t.path.CubeTo(x1, y1, x2, y2, x, y)
	}
	for i := 1; i < len(v); i++ {
		curve(i-1, i)
	}
	if o.Closed {
		curve(len(v)-1, 0)
		t.path.Close()
	}
	t.drawn = t.drawn || len(v) > 1 || o.Closed
}

// drawAt draws the composition at frame, aspect-fit and centered in bounds,
// clipped to its rectangle.
func (a *Animation) drawAt(p *ui.Painter, bounds ui.Rect, frame float32) {
	if a == nil || p == nil || a.width <= 0 || a.height <= 0 {
		return
	}
	w, h := float32(a.width), float32(a.height)
	scale := min(bounds.W/w, bounds.H/h)
	if !(scale > 0) || !finite(float64(scale)) {
		return
	}
	box := ui.Rect{X: bounds.X + (bounds.W-w*scale)/2, Y: bounds.Y + (bounds.H-h*scale)/2, W: w * scale, H: h * scale}
	viewport := affine{a: float64(scale), d: float64(scale), e: float64(box.X), f: float64(box.Y)}
	var path ui.Path
	p.Clip(box, 0, func() { a.composition.draw(p, &path, viewport, frame, 1) })
}

// kappa is how far along its tangents a cubic Bézier curve's control points
// are, as a fraction of the radius, to draw a quarter of a circle.
const kappa = 0.5522847498

// rectPath returns Lottie's rectangle: centered on position, its corners
// rounded by radius, at most half its shorter side. It runs clockwise
// from its top right corner, below the corner's curve, down its right
// side, as lottie-web's convertRectToPath (d 1) and lottie-ios's
// BezierPath.rectangle start it; a negative width or height mirrors it.
func rectPath(position, size point, radius float32) pathData {
	hw, hh := abs(size.X)/2, abs(size.Y)/2
	r := clamp(radius, 0, min(hw, hh))
	var p pathData
	if r == 0 {
		p = pathData{
			Vertices: []point{{hw, -hh}, {hw, hh}, {-hw, hh}, {-hw, -hh}},
			In:       make([]point, 4), Out: make([]point, 4), Closed: true,
		}
	} else {
		k := r * kappa
		p = pathData{
			Vertices: []point{{hw, -hh + r}, {hw, hh - r}, {hw - r, hh}, {-hw + r, hh}, {-hw, hh - r}, {-hw, -hh + r}, {-hw + r, -hh}, {hw - r, -hh}},
			In:       []point{{0, -k}, {}, {k, 0}, {}, {0, k}, {}, {-k, 0}, {}},
			Out:      []point{{}, {0, k}, {}, {-k, 0}, {}, {0, -k}, {}, {k, 0}},
			Closed:   true,
		}
	}
	return placed(p, position, size)
}

// ellipsePath returns Lottie's ellipse: centered on position, as large
// as size. It runs clockwise from its top, as lottie-web's
// convertEllToPath and lottie-ios's BezierPath.ellipse start it; a
// negative width or height mirrors it.
func ellipsePath(position, size point) pathData {
	rx, ry := abs(size.X)/2, abs(size.Y)/2
	kx, ky := rx*kappa, ry*kappa
	return placed(pathData{
		Vertices: []point{{0, -ry}, {rx, 0}, {0, ry}, {-rx, 0}},
		In:       []point{{-kx, 0}, {0, -ky}, {kx, 0}, {0, ky}},
		Out:      []point{{kx, 0}, {0, ky}, {-kx, 0}, {0, -ky}},
		Closed:   true,
	}, position, size)
}

// placed moves a path drawn around (0,0) to position, mirrored along the
// axes in which size is negative, as lottie-web draws a shape of a
// negative size, from its half size.
func placed(p pathData, position, size point) pathData {
	sx, sy := float32(1), float32(1)
	if size.X < 0 {
		sx = -1
	}
	if size.Y < 0 {
		sy = -1
	}
	for i := range p.Vertices {
		p.Vertices[i] = point{position.X + sx*p.Vertices[i].X, position.Y + sy*p.Vertices[i].Y}
		p.In[i] = point{sx * p.In[i].X, sy * p.In[i].Y}
		p.Out[i] = point{sx * p.Out[i].X, sy * p.Out[i].Y}
	}
	return p
}

// directed returns a closed path reversed, when it is, as lottie-web's
// ShapePath.reverse reverses one: from the same start, its other vertices
// in reverse order, each one's tangents swapped.
func directed(p pathData, reversed bool) pathData {
	if !reversed {
		return p
	}
	n := len(p.Vertices)
	r := pathData{Vertices: make([]point, n), In: make([]point, n), Out: make([]point, n), Closed: p.Closed}
	for i := range n {
		j := (n - i) % n
		r.Vertices[i], r.In[i], r.Out[i] = p.Vertices[j], p.Out[j], p.In[j]
	}
	return r
}

func abs(v float32) float32 { return float32(math.Abs(float64(v))) }
