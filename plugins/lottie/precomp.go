package lottie

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// precomp is what a precomposition layer (ty 0) draws: the composition of
// an asset, which every layer that shows the asset shares, in the layer's
// time, under its transform, clipped to its rectangle. It follows
// lottie-web's CompElement.js and CVCompElement.js.
type precomp struct {
	comp *composition
	// width and height are the layer's rectangle (w, h), from its origin,
	// to which what is inside is clipped.
	width, height float32
	// start (st) and stretch (sr) map the frames of the composition
	// around to the asset's: (frame - start) / stretch.
	start, stretch float32
	// remap, when remapped, is the layer's time remapping (tm), which
	// gives the asset's time instead, in seconds, at the frames around;
	// frameRate, the root composition's, makes them frames.
	remap     property[float32]
	remapped  bool
	frameRate float32
}

// rawPrecomp is what Load reads of a precomposition layer beyond what
// every layer has: the asset it shows, its rectangle and its start time.
type rawPrecomp struct {
	RefID json.RawMessage `json:"refId"`
	W     *num            `json:"w"`
	H     *num            `json:"h"`
	St    num             `json:"st"`
}

// parsePrecomp decodes a precomposition layer, of a composition depth
// precompositions deep, and reads the asset it shows, if no layer has.
// Its rectangle must be given: lottie-ios rejects a document without it,
// and lottie-web clips everything inside away. A missing start time is
// 0. A stretch of 0, which lottie-web reads as 1 and lottie-ios divides
// by, is an error; without a time remapping, which makes it meaningless.
func (d *document) parsePrecomp(data json.RawMessage, raw *rawLayer, depth int) (*precomp, error) {
	var rp rawPrecomp
	if err := json.Unmarshal(data, &rp); err != nil {
		return nil, err
	}
	var id string
	if !present(rp.RefID) {
		return nil, fmt.Errorf("precomposition without a refId")
	}
	if err := json.Unmarshal(rp.RefID, &id); err != nil {
		return nil, fmt.Errorf("precomposition refId %.20s: want a string", rp.RefID)
	}
	if rp.W == nil || rp.H == nil {
		return nil, fmt.Errorf("precomposition size (w, h) must be given")
	}
	pc := &precomp{width: float32(*rp.W), height: float32(*rp.H), start: float32(rp.St), stretch: 1, frameRate: d.frameRate}
	if raw.Stretch != nil {
		pc.stretch = float32(*raw.Stretch)
	}
	var err error
	switch {
	case present(raw.TimeRemap):
		if pc.remap, err = parseScalar(raw.TimeRemap, 0); err != nil {
			return nil, fmt.Errorf("time remapping: %w", err)
		}
		pc.remapped = true
	case pc.stretch == 0:
		return nil, fmt.Errorf("unsupported time stretch (sr) 0")
	}
	if pc.comp, err = d.precomposition(id, depth+1); err != nil {
		return nil, err
	}
	return pc, nil
}

// precomposition returns the composition of the asset id, depth
// precompositions deep, which it reads the first time a layer shows it.
// An asset that contains itself, through others or not, is an error, as
// lottie-web would recurse without end; so are an id that no asset has,
// or several have (lottie-web shows the first of them, lottie-ios the
// last), and an asset without layers: an image, or anything else.
func (d *document) precomposition(id string, depth int) (*composition, error) {
	if c := d.read[id]; c != nil {
		return c, nil
	}
	if d.reading[id] {
		return nil, fmt.Errorf("precomposition %q contains itself", id)
	}
	if depth > maxPrecompDepth {
		return nil, fmt.Errorf("precompositions nest deeper than %d", maxPrecompDepth)
	}
	data, err := d.assetData(id)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Layers json.RawMessage `json:"layers"`
		P      json.RawMessage `json:"p"`
		U      json.RawMessage `json:"u"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("asset %q: %w", id, err)
	}
	if len(raw.Layers) == 0 || string(raw.Layers) == "null" {
		if present(raw.P) || present(raw.U) {
			return nil, fmt.Errorf("unsupported image: asset %q is an image, not a composition", id)
		}
		return nil, fmt.Errorf("asset %q has no layers", id)
	}
	var layers []json.RawMessage
	if err := json.Unmarshal(raw.Layers, &layers); err != nil {
		return nil, fmt.Errorf("precomposition %q: layers: %w", id, err)
	}
	if d.reading == nil {
		d.reading, d.read = map[string]bool{}, map[string]*composition{}
	}
	d.reading[id] = true
	c, err := d.parseComposition(layers, depth)
	delete(d.reading, id)
	if err != nil {
		return nil, fmt.Errorf("precomposition %q: %w", id, err)
	}
	d.read[id] = c
	return c, nil
}

// assetData returns the asset whose id is id. The first time, it indexes
// the document's assets by id: those whose id is a string, which a layer's
// refId can name; the others, whatever they are, no layer can show.
func (d *document) assetData(id string) (json.RawMessage, error) {
	if d.ids == nil {
		if len(d.assets) > 0 && string(d.assets) != "null" {
			if err := json.Unmarshal(d.assets, &d.list); err != nil {
				return nil, fmt.Errorf("assets: %w", err)
			}
		}
		d.ids = map[string][]int{}
		for i, data := range d.list {
			var a struct {
				ID json.RawMessage `json:"id"`
			}
			var s string
			if json.Unmarshal(data, &a) == nil && len(a.ID) > 0 && json.Unmarshal(a.ID, &s) == nil {
				d.ids[s] = append(d.ids[s], i)
			}
		}
	}
	switch found := d.ids[id]; len(found) {
	case 0:
		return nil, fmt.Errorf("no asset has id %q", id)
	case 1:
		return d.list[found[0]], nil
	default:
		return nil, fmt.Errorf("%d assets have id %q", len(found), id)
	}
}

// draw draws the layer at frame under parent, which maps the coordinates
// of the composition around into the painter's, with alpha.
func (l *layer) draw(p *ui.Painter, path *ui.Path, parent affine, frame, alpha float32) {
	if l.precomp == nil {
		l.shapes.draw(p, path, parent, frame, alpha)
		return
	}
	l.precomp.draw(p, path, &l.transform, parent, frame, l.outPoint, alpha)
}

// draw draws what is inside a precomposition layer whose transform is tr
// and out point outPoint, at frame of the composition around, inside
// parent, with alpha times the layer's opacity, which multiplies into
// each shape's inside, as lottie-web's canvas renderer does: overlapping
// shapes inside a translucent layer show through each other. A layer that
// is transparent at frame, or whose time inside isn't finite, draws
// nothing, and samples nothing inside.
func (pc *precomp) draw(p *ui.Painter, path *ui.Path, tr *transform, parent affine, frame, outPoint, alpha float32) {
	alpha *= percent(tr.opacity, frame)
	if !(alpha > 0) {
		return
	}
	inner, ok := pc.frame(frame, outPoint)
	m := parent.mul(tr.at(frame))
	if !ok || !m.finite() {
		return
	}
	pc.clip(p, path, m, func() { pc.comp.draw(p, path, m, inner, alpha) })
}

// frame returns the asset's frame at frame of the composition around,
// and whether it is finite. Time remapped, it is the remapping's seconds
// at frame times the root composition's frame rate, but the frame before
// where that is the layer's out point, as lottie-web's
// CompElement.prepareFrame has it (lottie-ios doesn't), so that a
// remapping to the end shows the last frame; the layer's start time and
// stretch don't count then. Otherwise it is frame less the layer's start
// time, divided by its stretch. The keyframes of the layer's own
// properties, the remapping's too, are in the frames around, as every
// layer's: its start time cancels out of them, and its stretch doesn't
// apply.
func (pc *precomp) frame(frame, outPoint float32) (float32, bool) {
	var f float64
	if pc.remapped {
		f = float64(pc.remap.sample(frame)) * float64(pc.frameRate)
		if float32(f) == outPoint {
			f = float64(outPoint) - 1
		}
	} else {
		f = (float64(frame) - float64(pc.start)) / float64(pc.stretch)
	}
	return float32(f), finite(f) && math.Abs(f) <= math.MaxFloat32
}

// clip calls fn with what it draws clipped to the layer's rectangle, from
// (0,0) to (w,h) in its coordinates, which m maps into the painter's, as
// lottie-web's renderers and lottie-ios clip it, always: to a rectangle
// where m keeps it one, as most layers do, or else to the quadrilateral
// that m makes of it. Inside another precomposition, the clips intersect.
// A rectangle of no area, or that reaches farther than a painter draws
// (maxDIPs), shows nothing.
func (pc *precomp) clip(p *ui.Painter, path *ui.Path, m affine, fn func()) {
	w, h := pc.width, pc.height
	if w == 0 || h == 0 || m.a*m.d-m.b*m.c == 0 {
		return
	}
	var xs, ys [4]float64
	for i, c := range [4]point{{0, 0}, {w, 0}, {w, h}, {0, h}} {
		xs[i], ys[i] = m.point(c)
		if !drawable(xs[i]) || !drawable(ys[i]) {
			return
		}
	}
	// Its sides along the axes, to a thousandth of a DIP, it is a
	// rectangle still, which the painter clips to cheaply.
	const tol = 1e-3
	flat := func(v float64) bool { return math.Abs(v) <= tol }
	if flat(ys[1]-ys[0]) && flat(xs[2]-xs[1]) || flat(xs[1]-xs[0]) && flat(ys[2]-ys[1]) {
		x0, x1 := min(xs[0], xs[2]), max(xs[0], xs[2])
		y0, y1 := min(ys[0], ys[2]), max(ys[0], ys[2])
		p.Clip(ui.Rect{X: float32(x0), Y: float32(y0), W: float32(x1 - x0), H: float32(y1 - y0)}, 0, fn)
		return
	}
	path.Reset().MoveTo(float32(xs[0]), float32(ys[0]))
	for i := 1; i < 4; i++ {
		path.LineTo(float32(xs[i]), float32(ys[i]))
	}
	p.ClipPath(path.Close(), fn)
}
