package lottie

import (
	"math"

	"github.com/egoist/mygo/ui"
)

func (a *Animation) drawAt(p *ui.Painter, bounds ui.Rect, frame float32) {
	if a == nil || p == nil || a.Width <= 0 || a.Height <= 0 {
		return
	}
	scale := min(bounds.W/float32(a.Width), bounds.H/float32(a.Height))
	if scale <= 0 {
		return
	}
	ox := bounds.X + (bounds.W-float32(a.Width)*scale)/2
	oy := bounds.Y + (bounds.H-float32(a.Height)*scale)/2
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
		for _, shape := range group.paths {
			drawPath(p, shape.path.sample(frame), fill, stroke, frame, alpha)
		}
		for _, shape := range group.rects {
			drawPath(p, rectPath(shape.position.sample(frame), shape.size.sample(frame), shape.radius.sample(frame)), fill, stroke, frame, alpha)
		}
		for _, shape := range group.ellipses {
			drawPath(p, ellipsePath(shape.position.sample(frame), shape.size.sample(frame)), fill, stroke, frame, alpha)
		}
		for i := len(group.groups) - 1; i >= 0; i-- {
			drawGroup(p, group.groups[i], frame, alpha)
		}
	})
}

func drawPath(p *ui.Painter, data pathData, fill *fillShape, stroke *strokeShape, frame float32, alpha float32) {
	path := pathFromData(data)
	if path == nil {
		return
	}
	if fill != nil {
		c := fill.color.sample(frame)
		p.FillPath(path, c.ui(alpha*clamp(fill.opacity.sample(frame)/100, 0, 1)))
	}
	if stroke != nil {
		c := stroke.color.sample(frame)
		width := stroke.width.sample(frame)
		if width > 0 {
			p.StrokePath(path, width, c.ui(alpha*clamp(stroke.opacity.sample(frame)/100, 0, 1)))
		}
	}
}

func pathFromData(data pathData) *ui.Path {
	if len(data.Vertices) == 0 || len(data.In) != len(data.Vertices) || len(data.Out) != len(data.Vertices) {
		return nil
	}
	path := new(ui.Path).MoveTo(data.Vertices[0].X, data.Vertices[0].Y)
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
	return path
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

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
