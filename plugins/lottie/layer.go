package lottie

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// The types of layers (ty) that the subset draws, or that place others.
const (
	precompLayer = 0
	solidLayer   = 1
	nullLayer    = 3
	shapeLayer   = 4
)

// composition is a list of layers, the first on top, with their parent
// links resolved: what an Animation draws, and what a precomposition
// layer draws, under that layer's transform and in its own time.
type composition struct {
	layers []layer
	// layerCount and shapeCount are how many layers it has and how many
	// shape items they hold, its precompositions' counted again for each
	// layer that shows them; depth is how deep precompositions nest in
	// it, 0 without any. Load bounds them (maxLayers, maxShapes,
	// maxPrecompDepth).
	layerCount, shapeCount, depth int
}

// layer is a layer that draws, or that places one that does: its
// transform, and what it draws under it, a shape layer's shapes, a
// solid's rectangle or a precomposition, from its in point until its out
// point, in the composition's frames.
type layer struct {
	shapes
	// precomp is what a precomposition layer draws, nil for other layers.
	precomp *precomp
	// parent is the index in the composition of the layer whose
	// transform this one's is inside, -1 for none.
	parent            int
	inPoint, outPoint float32
	// drawn is false for a layer that only places others: a null, or a
	// hidden layer, whatever its type.
	drawn bool
}

// rawHeader is what Load reads of every layer, to know which it draws and
// which place those: its type, whether it is hidden, its index (ind), by
// which other layers name it as their parent, and its parent's.
type rawHeader struct {
	Hidden bool `json:"hd"`
	// Ty, Ind and Parent are whole numbers, which header decodes into
	// ty, ind and parent.
	Ty, Ind, Parent json.RawMessage
	ind             *int
}

// draws reports whether the layer draws; a null or a hidden layer only
// places others.
func (h *rawHeader) draws() (bool, error) {
	if h.Hidden {
		return false, nil
	}
	ty, err := optional("ty", h.Ty)
	if err != nil {
		return false, err
	}
	return ty == nil || *ty != nullLayer, nil
}

// rawPlacement is what places a layer, and the layers it is the parent
// of: what Load reads of a layer that only places others.
type rawPlacement struct {
	DDD        json.RawMessage `json:"ddd"`
	AutoOrient float64         `json:"ao"`
	Stretch    *num            `json:"sr"`
	Ks         rawTransform    `json:"ks"`
}

// rawLayer is what Load reads of a layer that draws.
type rawLayer struct {
	rawPlacement
	In     *num       `json:"ip"`
	Out    *num       `json:"op"`
	Shapes []rawShape `json:"shapes"`
	// A solid's size and color.
	SolidWidth  *num            `json:"sw"`
	SolidHeight *num            `json:"sh"`
	SolidColor  json.RawMessage `json:"sc"`
	// Controls outside the subset that change how a layer draws.
	Matte       float64         `json:"tt"`
	MatteSource float64         `json:"td"`
	Blend       float64         `json:"bm"`
	Masks       json.RawMessage `json:"masksProperties"`
	Effects     json.RawMessage `json:"ef"`
	// A precomposition's time remapping; see parsePrecomp.
	TimeRemap json.RawMessage `json:"tm"`
	// A layer's start time (st) offsets the frames of what a
	// precomposition shows (see parsePrecomp); the keyframes and in and
	// out points of every layer are in its composition's frames, as
	// lottie-web reads them, the start time cancelling out, so Load
	// ignores it on other layers.
}

// document is what Load reads compositions from: the root composition's
// frame rate, its assets, and the precompositions it has read of them,
// which every layer that shows one shares.
type document struct {
	frameRate float32
	// assets is the document's assets as written, which assetData
	// indexes by id the first time a precomposition layer looks one up.
	assets json.RawMessage
	list   []json.RawMessage
	ids    map[string][]int
	read   map[string]*composition
	// reading is the ids of the precompositions being read, around the
	// one being read now.
	reading map[string]bool
}

// parseComposition decodes a list of layers, of the composition depth
// precompositions deep. It reads in full the layers that draw, and the
// transforms of those that place them, through parent links; the others
// it skips, whatever they contain.
func (d *document) parseComposition(raws []json.RawMessage, depth int) (*composition, error) {
	headers := make([]rawHeader, len(raws))
	drawn := make([]bool, len(raws))
	for i, data := range raws {
		h := &headers[i]
		err := json.Unmarshal(data, h)
		if err == nil {
			h.ind, err = optional("ind", h.Ind)
		}
		if err == nil {
			drawn[i], err = h.draws()
		}
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
	}
	layers := make([]layer, len(raws))
	for i, data := range raws {
		if !drawn[i] {
			continue
		}
		var err error
		if layers[i], err = d.parseLayer(data, &headers[i], depth); err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
	}
	parents, err := resolveParents(headers, drawn)
	if err != nil {
		return nil, err
	}
	// The layers that draw, and their ancestors, keep their order;
	// index maps a layer's place in the list to its place in theirs.
	c := &composition{}
	index := make([]int, len(raws))
	for i, data := range raws {
		if parents[i] == unplaced {
			continue
		}
		if !drawn[i] {
			if layers[i], err = parsePlacement(data); err != nil {
				return nil, fmt.Errorf("layer %d: %w", i, err)
			}
		}
		index[i] = len(c.layers)
		c.layers = append(c.layers, layers[i])
	}
	for i, p := range parents {
		if p >= 0 {
			c.layers[index[i]].parent = index[p]
		}
	}
	return c, c.count(depth)
}

// count counts the layers of c and their shapes, those of each
// precomposition again for every layer that shows it, and how deep
// precompositions nest in it, which is depth deep itself; it reports
// those beyond the bounds. A precomposition's counts are its own, made
// once, so that counting takes no longer than reading the document,
// however many times a frame would draw what it holds.
func (c *composition) count(depth int) error {
	for i := range c.layers {
		l := &c.layers[i]
		c.layerCount++
		c.shapeCount += l.size
		if pc := l.precomp; pc != nil {
			c.layerCount += pc.comp.layerCount
			c.shapeCount += pc.comp.shapeCount
			c.depth = max(c.depth, pc.comp.depth+1)
		}
		switch {
		case depth+c.depth > maxPrecompDepth:
			return fmt.Errorf("precompositions nest deeper than %d", maxPrecompDepth)
		case c.layerCount > maxLayers:
			return fmt.Errorf("more than %d layers to draw, a precomposition's counted again for each layer that shows it", maxLayers)
		case c.shapeCount > maxShapes:
			return fmt.Errorf("more than %d shapes to draw, a precomposition's counted again for each layer that shows it", maxShapes)
		}
	}
	return nil
}

// unplaced is the parent resolveParents gives a layer that neither draws
// nor places one that does; root, that of a layer without a parent.
const (
	unplaced = -2
	root     = -1
)

// resolveParents returns the index of each layer's parent, root for a
// layer without one, unplaced for one that doesn't matter: neither draws
// nor is an ancestor of one that does. A parent names a layer by its ind;
// one that names none is no parent, as lottie-web, which follows links as
// it builds its layers (buildElementParenting in BaseRenderer.js), and
// lottie-ios have it. One that names several layers is an error, as are
// chains of links that don't end, within maxParentDepth links: lottie-web
// applies every layer of an ind several have, one after the other, and
// recurses without end around a cycle.
func resolveParents(headers []rawHeader, drawn []bool) ([]int, error) {
	byInd := map[int][]int{}
	for i := range headers {
		if ind := headers[i].ind; ind != nil {
			byInd[*ind] = append(byInd[*ind], i)
		}
	}
	parents := make([]int, len(headers))
	depths := make([]int, len(headers))
	const (
		unknown = iota
		walking
		known
	)
	state := make([]uint8, len(headers))
	// walk follows the links up from layer i, until a layer without a
	// parent or one it has walked before, then gives each layer it
	// passed its depth: how many ancestors it has.
	walk := func(i int) error {
		var path []int
		cur, depth := i, -1
		for {
			if state[cur] == known {
				depth = depths[cur]
				break
			}
			if state[cur] == walking {
				return fmt.Errorf("layer %d: parent links form a cycle", cur)
			}
			state[cur] = walking
			path = append(path, cur)
			parent, err := optional("parent", headers[cur].Parent)
			if err != nil {
				return fmt.Errorf("layer %d: %w", cur, err)
			}
			var named []int
			if parent != nil {
				named = byInd[*parent]
			}
			if len(named) > 1 {
				return fmt.Errorf("layer %d: parent %d: %d layers have ind %d", cur, *parent, len(named), *parent)
			}
			if len(named) == 0 {
				parents[cur] = root
				break
			}
			parents[cur] = named[0]
			cur = named[0]
		}
		for j := len(path) - 1; j >= 0; j-- {
			depth++
			if depth > maxParentDepth {
				return fmt.Errorf("layer %d: parent chain is longer than %d layers", path[j], maxParentDepth)
			}
			depths[path[j]], state[path[j]] = depth, known
		}
		return nil
	}
	for i := range headers {
		if drawn[i] {
			if err := walk(i); err != nil {
				return nil, err
			}
		}
	}
	for i := range parents {
		if state[i] != known {
			parents[i] = unplaced
		}
	}
	return parents, nil
}

// parseLayer decodes a layer that draws, of a composition depth
// precompositions deep: a shape, solid or precomposition layer, its
// transform and its in and out points.
func (d *document) parseLayer(data json.RawMessage, h *rawHeader, depth int) (layer, error) {
	ty, err := optional("ty", h.Ty)
	if err != nil {
		return layer{}, err
	}
	if ty == nil {
		return layer{}, fmt.Errorf("layer without a type (ty)")
	}
	var raw rawLayer
	if err := json.Unmarshal(data, &raw); err != nil {
		return layer{}, err
	}
	if err := raw.unsupported(*ty); err != nil {
		return layer{}, err
	}
	l, err := raw.rawPlacement.parse()
	if err != nil {
		return layer{}, err
	}
	l.drawn = true
	if raw.In != nil {
		l.inPoint = float32(*raw.In)
	}
	if raw.Out != nil {
		l.outPoint = float32(*raw.Out)
	}
	switch *ty {
	case precompLayer:
		l.precomp, err = d.parsePrecomp(data, &raw, depth)
		if err != nil {
			return layer{}, err
		}
		return l, nil
	case solidLayer:
		err = raw.parseSolid(&l.shapes)
	default:
		err = parseLayerShapes(raw.Shapes, &l.shapes)
	}
	if err != nil {
		return layer{}, err
	}
	l.retain()
	return l, nil
}

// parsePlacement decodes a layer that only places others: its transform.
func parsePlacement(data json.RawMessage) (layer, error) {
	var raw rawPlacement
	if err := json.Unmarshal(data, &raw); err != nil {
		return layer{}, err
	}
	if err := raw.unsupported(); err != nil {
		return layer{}, err
	}
	return raw.parse()
}

// parse returns the layer that the placement places, which draws nothing,
// always.
func (raw *rawPlacement) parse() (layer, error) {
	tr, err := parseTransform(raw.Ks)
	if err != nil {
		return layer{}, err
	}
	return layer{shapes: shapes{group: group{transform: tr}}, parent: root, inPoint: -math.MaxFloat32, outPoint: math.MaxFloat32}, nil
}

// parseSolid makes sh a solid layer's rectangle, from (0,0) to its size
// (sw, sh), filled with its color (sc), which is drawn, as lottie-web
// draws it, with its layer's transform and opacity. A solid of no size
// draws nothing.
func (raw *rawLayer) parseSolid(sh *shapes) error {
	if raw.SolidWidth == nil || raw.SolidHeight == nil || *raw.SolidWidth < 0 || *raw.SolidHeight < 0 {
		return fmt.Errorf("solid size (sw, sh) must be given, and not negative")
	}
	c, err := solidColor(raw.SolidColor)
	if err != nil {
		return err
	}
	w, h := float32(*raw.SolidWidth), float32(*raw.SolidHeight)
	if w > 0 && h > 0 {
		sh.geometry = []geometry{rectShape{
			position: property[point]{static: point{w / 2, h / 2}},
			size:     property[point]{static: point{w, h}},
		}}
		sh.items = []item{{geometry: 0}, {style: &fill{rule: ui.NonZero, color: property[color]{static: c}, opacity: property[float32]{static: 100}}}}
		sh.size = 2
	}
	return nil
}

// solidColor decodes a solid's color, a CSS hex color: #rrggbb, as
// exporters write it, or #rgb. lottie-web hands it to the browser as it
// is, which would read other CSS colors too; the subset rejects them, and
// the forms with an alpha, which other platforms order differently.
func solidColor(raw json.RawMessage) (color, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return color{}, fmt.Errorf("missing solid color (sc)")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return color{}, fmt.Errorf("unsupported solid color %.20s", raw)
	}
	digits := len(s) - 1
	if len(s) == 0 || s[0] != '#' || digits != 3 && digits != 6 {
		return color{}, fmt.Errorf("unsupported solid color %q", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color{}, fmt.Errorf("unsupported solid color %q", s)
	}
	channel := func(i int) float32 {
		if digits == 3 {
			return float32(v>>(4*(2-i))&0xf) * 17 / 255
		}
		return float32(v>>(8*(2-i))&0xff) / 255
	}
	return color{channel(0), channel(1), channel(2), 1}, nil
}

// draw draws the composition at frame, of its own, its layers from the
// last to the first, under m, which maps its coordinates into the
// painter's, with alpha. Each layer that draws is shown inside its
// parents' transforms, sampled at the same frame, but only its own in and
// out points and opacity decide whether and how it shows, as in After
// Effects.
func (c *composition) draw(p *ui.Painter, path *ui.Path, m affine, frame, alpha float32) {
	for i := len(c.layers) - 1; i >= 0; i-- {
		if l := &c.layers[i]; l.drawn && frame >= l.inPoint && frame < l.outPoint {
			l.draw(p, path, m.mul(c.parents(i, frame)), frame, alpha)
		}
	}
}

// parents returns the transform of the i-th layer's parents at frame: its
// parent's, inside its parent's parent's, and so on; none without one.
func (c *composition) parents(i int, frame float32) affine {
	m := affine{a: 1, d: 1}
	for j := c.layers[i].parent; j >= 0; j = c.layers[j].parent {
		m = c.layers[j].transform.at(frame).mul(m)
	}
	return m
}
