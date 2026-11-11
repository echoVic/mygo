package lottie

import (
	"math"

	"github.com/egoist/mygo/ui"
)

func (a *Animation) drawAt(p *ui.Painter, bounds ui.Rect, frame float32) {
	if a == nil || p == nil || a.width <= 0 || a.height <= 0 {
		return
	}
	scale := min(bounds.W/float32(a.width), bounds.H/float32(a.height))
	if scale <= 0 {
		return
	}
	ox := bounds.X + (bounds.W-float32(a.width)*scale)/2
	oy := bounds.Y + (bounds.H-float32(a.height)*scale)/2
	viewport := ui.Scaling(scale, scale).Then(ui.Translation(ox, oy))
	p.Transform(viewport, 0, 0, func() {
		for i := len(a.layers) - 1; i >= 0; i-- {
			layer := a.layers[i]
			if layer.hidden || frame < layer.inPoint || frame >= layer.outPoint {
				continue
			}
			frame := frame - layer.start
			alpha := clamp(layer.opacity.sample(frame)/100, 0, 1)
			if alpha <= 0 {
				continue
			}
			p.Transform(sampleTransform(layer.transform, frame), 0, 0, func() {
				for i := len(layer.groups) - 1; i >= 0; i-- {
					drawGroup(p, layer.groups[i], frame, alpha)
				}
			})
		}
	})
}

func drawGroup(p *ui.Painter, group group, frame, parentAlpha float32) {
	alpha := parentAlpha * clamp(group.transform.opacity.sample(frame)/100, 0, 1)
	if alpha <= 0 {
		return
	}
	p.Transform(sampleTransform(group.transform, frame), 0, 0, func() {
		var fill *fillShape
		if len(group.fills) > 0 {
			fill = &group.fills[0]
		}
		var stroke *strokeShape
		if len(group.strokes) > 0 {
			stroke = &group.strokes[0]
		}
		path := new(ui.Path)
		for _, shape := range group.paths {
			appendPathData(path, shape.path.sample(frame))
		}
		for _, shape := range group.rects {
			appendPathData(path, rectPath(shape.position.sample(frame), shape.size.sample(frame), shape.radius.sample(frame)))
		}
		for _, shape := range group.ellipses {
			appendPathData(path, ellipsePath(shape.position.sample(frame), shape.size.sample(frame)))
		}
		drawPath(p, path, fill, stroke, frame, alpha, group.strokeFirst)

		for i := len(group.groups) - 1; i >= 0; i-- {
			drawGroup(p, group.groups[i], frame, alpha)
		}
	})
}

func drawPath(p *ui.Painter, path *ui.Path, fill *fillShape, stroke *strokeShape, frame, alpha float32, strokeFirst bool) {
	fillIt := func() {
		if fill != nil {
			if fill.rule == 2 {
				path.FillRule(ui.EvenOdd)
			}
			p.FillPath(path, fill.color.sample(frame).ui(alpha*clamp(fill.opacity.sample(frame)/100, 0, 1)))
		}
	}
	strokeIt := func() {
		if stroke != nil {
			width := stroke.width.sample(frame)
			if width > 0 {
				path.Cap([]ui.LineCap{ui.ButtCap, ui.ButtCap, ui.RoundCap, ui.SquareCap}[stroke.cap]).Join([]ui.LineJoin{ui.MiterJoin, ui.MiterJoin, ui.RoundJoin, ui.BevelJoin}[stroke.join]).MiterLimit(stroke.miter)
				p.StrokePath(path, width, stroke.color.sample(frame).ui(alpha*clamp(stroke.opacity.sample(frame)/100, 0, 1)))
			}
		}
	}
	if strokeFirst {
		strokeIt()
		fillIt()
	} else {
		fillIt()
		strokeIt()
	}
}

func appendPathData(path *ui.Path, data pathData) {
	if len(data.Vertices) == 0 || len(data.In) != len(data.Vertices) || len(data.Out) != len(data.Vertices) {
		return
	}
	path.MoveTo(data.Vertices[0].X, data.Vertices[0].Y)
	for i := 1; i < len(data.Vertices); i++ {
		from, to := data.Vertices[i-1], data.Vertices[i]
		cp1 := point{X: from.X + data.Out[i-1].X, Y: from.Y + data.Out[i-1].Y}
		cp2 := point{X: to.X + data.In[i].X, Y: to.Y + data.In[i].Y}
		path.CubeTo(cp1.X, cp1.Y, cp2.X, cp2.Y, to.X, to.Y)
	}
	if data.Closed {
		last, first := data.Vertices[len(data.Vertices)-1], data.Vertices[0]
		cp1 := point{X: last.X + data.Out[len(data.Out)-1].X, Y: last.Y + data.Out[len(data.Out)-1].Y}
		cp2 := point{X: first.X + data.In[0].X, Y: first.Y + data.In[0].Y}
		path.CubeTo(cp1.X, cp1.Y, cp2.X, cp2.Y, first.X, first.Y).Close()
	}
}

func rectPath(position, size point, radius float32) pathData {
	x0, y0 := position.X-size.X/2, position.Y-size.Y/2
	x1, y1 := position.X+size.X/2, position.Y+size.Y/2
	r := clamp(radius, 0, min(size.X, size.Y)/2)
	if r == 0 {
		return pathData{
			Vertices: []point{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}},
			In:       []point{{}, {}, {}, {}}, Out: []point{{}, {}, {}, {}}, Closed: true,
		}
	}
	const k = 0.5522847498
	return pathData{
		Vertices: []point{{X: x0 + r, Y: y0}, {X: x1 - r, Y: y0}, {X: x1, Y: y0 + r}, {X: x1, Y: y1 - r}, {X: x1 - r, Y: y1}, {X: x0 + r, Y: y1}, {X: x0, Y: y1 - r}, {X: x0, Y: y0 + r}},
		In:       []point{{X: -r * k}, {}, {Y: -r * k}, {}, {X: r * k}, {}, {Y: r * k}, {}},
		Out:      []point{{}, {X: r * k}, {}, {Y: r * k}, {}, {X: -r * k}, {}, {Y: -r * k}},
		Closed:   true,
	}
}

func ellipsePath(position, size point) pathData {
	const k = 0.5522847498
	rx, ry := float32(math.Abs(float64(size.X)))/2, float32(math.Abs(float64(size.Y)))/2
	cx, cy := position.X, position.Y
	return pathData{
		Vertices: []point{{X: cx + rx, Y: cy}, {X: cx, Y: cy + ry}, {X: cx - rx, Y: cy}, {X: cx, Y: cy - ry}},
		In:       []point{{X: 0, Y: -ry * k}, {X: rx * k, Y: 0}, {X: 0, Y: ry * k}, {X: -rx * k, Y: 0}},
		Out:      []point{{X: 0, Y: ry * k}, {X: -rx * k, Y: 0}, {X: 0, Y: -ry * k}, {X: rx * k, Y: 0}},
		Closed:   true,
	}
}
