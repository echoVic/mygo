package lottie

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// group is a shape group: geometry that its styles paint, or child groups,
// under a transform. A layer is one too, its transform the layer's and its
// one child the layer's shapes.
type group struct {
	transform transform
	geometry  []geometry
	// outlines is the geometry's, made once by Load when none of it is
	// animated; animated geometry is sampled every frame instead.
	outlines []pathData
	animated bool
	// styles paint the geometry, the first on top, as Lottie lists them.
	styles []style
	groups []group
}

// geometry is a group's path, rectangle or ellipse.
type geometry interface {
	outline(frame float32) pathData
	animated() bool
}

type pathShape struct{ path property[pathData] }

type rectShape struct {
	position, size property[point]
	radius         property[float32]
}

type ellipseShape struct{ position, size property[point] }

func (s pathShape) outline(frame float32) pathData { return s.path.sample(frame) }
func (s pathShape) animated() bool                 { return s.path.animated() }

func (s rectShape) outline(frame float32) pathData {
	return rectPath(s.position.sample(frame), s.size.sample(frame), s.radius.sample(frame))
}
func (s rectShape) animated() bool {
	return s.position.animated() || s.size.animated() || s.radius.animated()
}

func (s ellipseShape) outline(frame float32) pathData {
	return ellipsePath(s.position.sample(frame), s.size.sample(frame))
}
func (s ellipseShape) animated() bool { return s.position.animated() || s.size.animated() }

// retain makes the group's outlines once, when its geometry is static.
func (g *group) retain() {
	for _, s := range g.geometry {
		if s.animated() {
			g.animated = true
			return
		}
	}
	g.outlines = g.sample(0)
}

// outlinesAt returns the group's outlines at frame: those Load retained,
// or its animated geometry sampled.
func (g *group) outlinesAt(frame float32) []pathData {
	if !g.animated {
		return g.outlines
	}
	return g.sample(frame)
}

func (g *group) sample(frame float32) []pathData {
	if len(g.geometry) == 0 {
		return nil
	}
	out := make([]pathData, len(g.geometry))
	for i, s := range g.geometry {
		out[i] = s.outline(frame)
	}
	return out
}

// style is a group's fill or stroke.
type style interface {
	// paint paints outlines, whose points m maps into the painter's
	// coordinates, with alpha, through path, which it may reset.
	paint(p *ui.Painter, path *ui.Path, outlines []pathData, m affine, frame, alpha float32)
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
// rasterized at the size they show.
func (f *fill) paint(p *ui.Painter, path *ui.Path, outlines []pathData, m affine, frame, alpha float32) {
	alpha *= percent(f.opacity, frame)
	if !(alpha > 0) || !trace(path, outlines, m) {
		return
	}
	p.FillPath(path.FillRule(f.rule), f.color.sample(frame).ui(alpha))
}

// A stroke's outlines are mapped by m whole when m scales evenly, as
// viewports and most transforms do, its width scaled with them. Otherwise
// they are scaled evenly as far as m stretches most, which keeps the
// stroke's shape, and the painter transforms them the rest of the way,
// which only shrinks them.
func (s *stroke) paint(p *ui.Painter, path *ui.Path, outlines []pathData, m affine, frame, alpha float32) {
	alpha *= percent(s.opacity, frame)
	if !(alpha > 0) {
		return
	}
	scale, even := m.similarity()
	place := m
	if !even {
		scale = m.maxScale()
		place = scaling(scale)
	}
	width := float64(s.width.sample(frame)) * scale
	if !(width > 0) || !drawable(width) || !trace(path, outlines, place) {
		return
	}
	path.Cap(s.cap).Join(s.join).MiterLimit(s.miter)
	c := s.color.sample(frame).ui(alpha)
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

// trace sets path to outlines, their points mapped by m, and reports
// whether all of them are within what the painter draws (maxDIPs).
func trace(path *ui.Path, outlines []pathData, m affine) bool {
	path.Reset()
	ok := true
	at := func(p point) (float32, float32) {
		x, y := m.point(p)
		ok = ok && drawable(x) && drawable(y)
		return float32(x), float32(y)
	}
	add := func(p, d point) point { return point{p.X + d.X, p.Y + d.Y} }
	for _, o := range outlines {
		v := o.Vertices
		if len(v) == 0 {
			continue
		}
		path.MoveTo(at(v[0]))
		curve := func(from, to int) {
			x1, y1 := at(add(v[from], o.Out[from]))
			x2, y2 := at(add(v[to], o.In[to]))
			x, y := at(v[to])
			path.CubeTo(x1, y1, x2, y2, x, y)
		}
		for i := 1; i < len(v); i++ {
			curve(i-1, i)
		}
		if o.Closed {
			curve(len(v)-1, 0)
			path.Close()
		}
	}
	return ok
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

// draw draws the group at frame, under parent, which maps its coordinates
// into the painter's. A transform that doesn't map them to finite ones
// skips the group, and what is in it.
func (g *group) draw(p *ui.Painter, path *ui.Path, parent affine, frame, alpha float32) {
	alpha *= percent(g.transform.opacity, frame)
	if !(alpha > 0) {
		return
	}
	m := parent.mul(g.transform.at(frame))
	if !m.finite() {
		return
	}
	if len(g.styles) > 0 {
		outlines := g.outlinesAt(frame)
		for i := len(g.styles) - 1; i >= 0; i-- {
			g.styles[i].paint(p, path, outlines, m, frame, alpha)
		}
	}
	for i := len(g.groups) - 1; i >= 0; i-- {
		g.groups[i].draw(p, path, m, frame, alpha)
	}
}

// kappa is how far along its tangents a cubic Bézier curve's control points
// are, as a fraction of the radius, to draw a quarter of a circle.
const kappa = 0.5522847498

// rectPath returns Lottie's rectangle: centered on position, as large as
// size's magnitude, its corners rounded by radius, at most half its
// shorter side.
func rectPath(position, size point, radius float32) pathData {
	hw, hh := abs(size.X)/2, abs(size.Y)/2
	x0, y0, x1, y1 := position.X-hw, position.Y-hh, position.X+hw, position.Y+hh
	r := clamp(radius, 0, min(hw, hh))
	if r == 0 {
		return pathData{
			Vertices: []point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}},
			In:       make([]point, 4), Out: make([]point, 4), Closed: true,
		}
	}
	k := r * kappa
	return pathData{
		Vertices: []point{{x0 + r, y0}, {x1 - r, y0}, {x1, y0 + r}, {x1, y1 - r}, {x1 - r, y1}, {x0 + r, y1}, {x0, y1 - r}, {x0, y0 + r}},
		In:       []point{{-k, 0}, {}, {0, -k}, {}, {k, 0}, {}, {0, k}, {}},
		Out:      []point{{}, {k, 0}, {}, {0, k}, {}, {-k, 0}, {}, {0, -k}},
		Closed:   true,
	}
}

// ellipsePath returns Lottie's ellipse: centered on position, as large as
// size's magnitude.
func ellipsePath(position, size point) pathData {
	rx, ry := abs(size.X)/2, abs(size.Y)/2
	cx, cy := position.X, position.Y
	return pathData{
		Vertices: []point{{cx + rx, cy}, {cx, cy + ry}, {cx - rx, cy}, {cx, cy - ry}},
		In:       []point{{0, -ry * kappa}, {rx * kappa, 0}, {0, ry * kappa}, {-rx * kappa, 0}},
		Out:      []point{{0, ry * kappa}, {-rx * kappa, 0}, {0, -ry * kappa}, {rx * kappa, 0}},
		Closed:   true,
	}
}

func abs(v float32) float32 { return float32(math.Abs(float64(v))) }
