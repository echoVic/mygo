// Package lottie plays Lottie animations, a small subset of them, in native
// UI: Load decodes the JSON once, and a Player draws it with ui's painter,
// sharp at any size, on its own clock.
//
//	//go:embed success.json
//	var data []byte
//
//	animation, err := lottie.Load(data)
//	if err != nil {
//		return err
//	}
//	player := animation.NewPlayer(lottie.PlayerOptions{Loop: true})
//
//	// In the view:
//	ui.Box(c).Size(200, 200).Label("Saved").Draw(player.Draw)
//
// The subset is shape layers, of paths, rectangles and ellipses, cut by
// trim paths, with solid fills and strokes, each painting the shapes
// before it in its group, nested groups' too, solid layers, and null
// layers, under 2D transforms and the transforms of the layers they are
// parented to. Their properties are static or keyframed with Bézier
// easing, per component where a value has several, and positions move
// along curved motion paths. Load reports anything else that would change
// the picture as an error rather than draw it wrong; the plugin's guide
// (docs/plugins/lottie.md) lists what it accepts.
package lottie

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Animation is an immutable decoded Lottie composition. Create one Player per
// view that shows it; an Animation can be shared by those players.
type Animation struct {
	Width, Height int
	FrameRate     float32

	width, height int
	frameRate     float32
	inPoint       float32
	outPoint      float32
	// composition is its layers.
	composition
}

// Duration returns the composition's playback duration.
func (a *Animation) Duration() time.Duration {
	if a == nil || a.frameRate <= 0 || a.outPoint <= a.inPoint {
		return 0
	}
	return time.Duration(math.Round((float64(a.outPoint) - float64(a.inPoint)) / float64(a.frameRate) * float64(time.Second)))
}

// Load decodes a Lottie composition. It accepts shape layers with paths,
// rectangles, ellipses, trim paths, solid fills, solid strokes and 2D
// transforms, solid layers, and null layers and parent links that place
// them.
func Load(data []byte) (*Animation, error) {
	var raw struct {
		W      num               `json:"w"`
		H      num               `json:"h"`
		Fr     num               `json:"fr"`
		Ip     num               `json:"ip"`
		Op     num               `json:"op"`
		Layers []json.RawMessage `json:"layers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("lottie: decode composition: %w", err)
	}
	if !whole(raw.W) || !whole(raw.H) {
		return nil, fmt.Errorf("lottie: composition size %v×%v: width and height must be positive whole numbers", raw.W, raw.H)
	}
	if !(raw.Fr > 0) {
		return nil, fmt.Errorf("lottie: frame rate must be positive")
	}
	if !(raw.Op > raw.Ip) {
		return nil, fmt.Errorf("lottie: out point must be after in point")
	}
	duration := (float64(raw.Op) - float64(raw.Ip)) / float64(raw.Fr) * float64(time.Second)
	if !finite(duration) || duration < 1 || duration >= float64(math.MaxInt64) {
		return nil, fmt.Errorf("lottie: duration must fit in time.Duration")
	}
	w, h := int(raw.W), int(raw.H)
	anim := &Animation{width: w, height: h, frameRate: float32(raw.Fr), Width: w, Height: h, FrameRate: float32(raw.Fr), inPoint: float32(raw.Ip), outPoint: float32(raw.Op)}
	var err error
	if anim.composition, err = parseComposition(raw.Layers); err != nil {
		return nil, fmt.Errorf("lottie: %w", err)
	}
	return anim, nil
}

// whole reports whether n is a positive whole number, as a composition's
// size must be, however it is written: 100 and 100.0 alike.
func whole(n num) bool { return n > 0 && float32(math.Trunc(float64(n))) == float32(n) }

// rawShape is a shape item with the fields of every type the subset reads,
// so that Load decodes each item, and the groups nested in it, once. What
// a key means depends on the type: p is a position; s a rectangle's or an
// ellipse's size, a transform's scale and a trim's start; e a trim's end;
// r a rectangle's roundness, a fill's rule and a transform's rotation; o
// an opacity and a trim's offset; m a trim's mode; d a direction, or a
// stroke's dashes.
type rawShape struct {
	rawTransform
	Type   string          `json:"ty"`
	Hidden bool            `json:"hd"`
	Blend  float64         `json:"bm"`
	Items  []rawShape      `json:"it"`
	Path   json.RawMessage `json:"ks"`
	Color  json.RawMessage `json:"c"`
	Width  json.RawMessage `json:"w"`
	Cap    json.RawMessage `json:"lc"`
	Join   json.RawMessage `json:"lj"`
	Miter  *num            `json:"ml"`
	D      json.RawMessage `json:"d"`
	End    json.RawMessage `json:"e"`
	Mode   json.RawMessage `json:"m"`
}

// shapeParser gathers a layer's shapes as it reads their groups.
type shapeParser struct {
	shapes *shapes
	// items counts the geometry and groups it has read, depth first;
	// painted holds the range of those that each style paints.
	items   int
	painted []span
}

// span is a range [lo, hi) of a layer's geometry, or of its items.
type span struct{ lo, hi int }

// parseLayerShapes reads a shape layer's items into sh, in a group under
// the layer's transform, sh's.
func parseLayerShapes(items []rawShape, sh *shapes) error {
	sp := shapeParser{shapes: sh}
	root, err := sp.parseGroup(items, 0)
	if err != nil {
		return err
	}
	sh.items = []item{{group: root}}
	trims := make([]span, len(sh.trims))
	for i, t := range sh.trims {
		trims[i] = span{t.lo, t.hi}
	}
	// Each bound keeps a frame's work linear in the file: the trims that
	// cut a shape one after the other each add at most one piece, and two
	// vertices, to its paths; each style walks the items it paints.
	if overlap(trims, len(sh.geometry)) > maxTrims {
		return fmt.Errorf("more than %d trims cut one shape", maxTrims)
	}
	if overlap(sp.painted, sp.items) > maxStyles {
		return fmt.Errorf("more than %d styles paint one shape or group", maxStyles)
	}
	return nil
}

// overlap returns how many spans of [0, n) cover one place at most.
func overlap(spans []span, n int) int {
	starts := make([]int, n+1)
	for _, s := range spans {
		starts[s.lo]++
		starts[s.hi]--
	}
	most, at := 0, 0
	for _, d := range starts {
		at += d
		most = max(most, at)
	}
	return most
}

// parseGroup reads a group's items, depth groups deep. Each style paints,
// and each trim cuts, the geometry listed before it in the group, that of
// nested groups too; several styles each paint theirs, the first on top.
// Hidden items are never drawn, nor cut, whatever they are, as in
// lottie-ios.
func (sp *shapeParser) parseGroup(items []rawShape, depth int) (*group, error) {
	if depth > maxGroupDepth {
		return nil, fmt.Errorf("shape groups nest deeper than %d", maxGroupDepth)
	}
	sh := sp.shapes
	g := &group{}
	first, firstItem := len(sh.geometry), sp.items
	var tr *rawTransform
	for i := range items {
		it := &items[i]
		if it.Hidden {
			continue
		}
		if it.Blend != 0 {
			return nil, fmt.Errorf("item %d: unsupported blend mode", i)
		}
		switch it.Type {
		case "gr":
			child, err := sp.parseGroup(it.Items, depth+1)
			if err != nil {
				return nil, fmt.Errorf("nested group %d: %w", i, err)
			}
			g.items = append(g.items, item{group: child})
			sp.items++
		case "tr":
			if tr != nil {
				return nil, fmt.Errorf("unsupported multiple transforms")
			}
			tr = &it.rawTransform
		case "sh", "rc", "el":
			s, err := parseGeometry(it)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			g.items = append(g.items, item{geometry: len(sh.geometry)})
			sh.geometry = append(sh.geometry, s)
			sp.items++
		case "fl", "st":
			s, err := parseStyle(it)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			g.items = append(g.items, item{style: s})
			sp.painted = append(sp.painted, span{firstItem, sp.items})
		case "tm":
			t, err := parseTrim(it)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			if t.lo, t.hi = first, len(sh.geometry); t.hi > t.lo {
				sh.trims = append(sh.trims, t)
			}
		default:
			if name, ok := shapeTypes[it.Type]; ok {
				return nil, fmt.Errorf("item %d: unsupported %s (ty %q)", i, name, it.Type)
			}
			return nil, fmt.Errorf("item %d: unsupported shape type %q", i, it.Type)
		}
	}
	if tr == nil {
		tr = &rawTransform{}
	}
	var err error
	if g.transform, err = parseTransform(*tr); err != nil {
		return nil, fmt.Errorf("transform: %w", err)
	}
	return g, nil
}

// parseGeometry reads a path, rectangle or ellipse. A rectangle or an
// ellipse runs clockwise (d 1 or 2, or none, as lottie-ios has it) or
// reversed (d 3); a path as it is (d 0 to 2, or none).
func parseGeometry(it *rawShape) (geometry, error) {
	d, ok := enum(it.D, 4)
	if !ok {
		return nil, fmt.Errorf("unsupported shape direction %s", it.D)
	}
	reversed := d == 3
	if it.Type == "sh" {
		if reversed {
			return nil, fmt.Errorf("unsupported path direction %s", it.D)
		}
		path, err := parsePath(it.Path)
		return pathShape{path}, err
	}
	position, err := parsePoint(it.P, point{})
	if err != nil {
		return nil, fmt.Errorf("position: %w", err)
	}
	size, err := parsePoint(it.S, point{})
	if err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}
	if it.Type == "el" {
		return ellipseShape{position, size, reversed}, nil
	}
	radius, err := parseScalar(it.R, 0)
	if err != nil {
		return nil, fmt.Errorf("roundness: %w", err)
	}
	return rectShape{position, size, radius, reversed}, nil
}

// Lottie's fill rules (r), line caps (lc) and line joins (lj), by number;
// 0 is a missing one.
var (
	fillRules = [...]ui.FillRule{0: ui.NonZero, 1: ui.NonZero, 2: ui.EvenOdd}
	lineCaps  = [...]ui.LineCap{0: ui.ButtCap, 1: ui.ButtCap, 2: ui.RoundCap, 3: ui.SquareCap}
	lineJoins = [...]ui.LineJoin{0: ui.MiterJoin, 1: ui.MiterJoin, 2: ui.RoundJoin, 3: ui.BevelJoin}
)

// parseStyle reads a solid fill or stroke.
func parseStyle(it *rawShape) (style, error) {
	c, err := parseColor(it.Color, color{A: 1})
	if err != nil {
		return nil, fmt.Errorf("color: %w", err)
	}
	o, err := parseScalar(it.O, 100)
	if err != nil {
		return nil, fmt.Errorf("opacity: %w", err)
	}
	if it.Type == "fl" {
		rule, ok := enum(it.R, len(fillRules))
		if !ok {
			return nil, fmt.Errorf("unsupported fill rule %s", it.R)
		}
		return &fill{rule: fillRules[rule], color: c, opacity: o}, nil
	}
	if present(it.D) {
		return nil, fmt.Errorf("unsupported stroke dashes")
	}
	w, err := parseScalar(it.Width, 1)
	if err != nil {
		return nil, fmt.Errorf("stroke width: %w", err)
	}
	lineCap, capOK := enum(it.Cap, len(lineCaps))
	join, joinOK := enum(it.Join, len(lineJoins))
	if !capOK || !joinOK {
		return nil, fmt.Errorf("unsupported stroke cap %s or join %s", it.Cap, it.Join)
	}
	miter := float32(4) // Lottie's default, as SVG's
	if it.Miter != nil {
		miter = float32(*it.Miter)
	}
	return &stroke{cap: lineCaps[lineCap], join: lineJoins[join], miter: miter, color: c, opacity: o, width: w}, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
