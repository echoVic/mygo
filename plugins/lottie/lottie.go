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
// The subset is shape layers: paths, rectangles and ellipses with solid
// fills and strokes, under 2D transforms, their properties static or
// keyframed with Bézier easing. Load reports anything else that would
// change the picture as an error rather than draw it wrong; the plugin's
// guide (docs/plugins/lottie.md) lists what it accepts.
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
	layers        []layer
}

// layer is a shape layer: a group under the layer's transform, shown from
// its in point until its out point, in composition frames.
type layer struct {
	group
	inPoint, outPoint float32
}

// Duration returns the composition's playback duration.
func (a *Animation) Duration() time.Duration {
	if a == nil || a.frameRate <= 0 || a.outPoint <= a.inPoint {
		return 0
	}
	return time.Duration(math.Round((float64(a.outPoint) - float64(a.inPoint)) / float64(a.frameRate) * float64(time.Second)))
}

// Load decodes a Lottie composition. The MVP accepts shape layers with paths,
// rectangles, ellipses, solid fills, solid strokes and 2D transforms.
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
		return nil, fmt.Errorf("lottie: composition width and height must be positive whole numbers")
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
	for i, data := range raw.Layers {
		var raw rawLayer
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("lottie: layer %d: %w", i, err)
		}
		if raw.Hidden {
			continue // never drawn, whatever it is
		}
		l, err := parseLayer(&raw)
		if err != nil {
			return nil, fmt.Errorf("lottie: layer %d: %w", i, err)
		}
		anim.layers = append(anim.layers, l)
	}
	return anim, nil
}

// whole reports whether n is a positive whole number, as a composition's
// size must be, however it is written: 100 and 100.0 alike.
func whole(n num) bool { return n > 0 && float32(math.Trunc(float64(n))) == float32(n) }

// shapeLayer is the ty of a shape layer, the only kind the subset draws.
const shapeLayer = 4

type rawLayer struct {
	Type    int          `json:"ty"`
	DDD     int          `json:"ddd"`
	Hidden  bool         `json:"hd"`
	In      *num         `json:"ip"`
	Out     *num         `json:"op"`
	Stretch *num         `json:"sr"`
	Ks      rawTransform `json:"ks"`
	Shapes  []rawShape   `json:"shapes"`
	// Controls outside the subset that change how a layer draws.
	Matte       float64         `json:"tt"`
	MatteSource float64         `json:"td"`
	Blend       float64         `json:"bm"`
	AutoOrient  float64         `json:"ao"`
	Parent      json.RawMessage `json:"parent"`
	Masks       json.RawMessage `json:"masksProperties"`
	Effects     json.RawMessage `json:"ef"`
	TimeRemap   json.RawMessage `json:"tm"`
	// A layer's start time (st) offsets the frames of what a precomp
	// shows, which the subset rejects; a shape layer's keyframes and in
	// and out points are in composition frames, so Load ignores it.
}

// rawShape is a shape item with the fields of every type the subset reads,
// so that Load decodes each item, and the groups nested in it, once. What
// a key means depends on the type: p is a position; s a rectangle's or an
// ellipse's size and a transform's scale; r a rectangle's roundness, a
// fill's rule and a transform's rotation; o an opacity; d a direction, or
// a stroke's dashes.
type rawShape struct {
	rawTransform
	Type   string          `json:"ty"`
	Hidden bool            `json:"hd"`
	Blend  float64         `json:"bm"`
	Items  []rawShape      `json:"it"`
	Path   json.RawMessage `json:"ks"`
	Color  json.RawMessage `json:"c"`
	Width  json.RawMessage `json:"w"`
	Cap    int             `json:"lc"`
	Join   int             `json:"lj"`
	Miter  *num            `json:"ml"`
	D      json.RawMessage `json:"d"`
}

func parseLayer(raw *rawLayer) (layer, error) {
	if err := raw.unsupported(); err != nil {
		return layer{}, err
	}
	tr, err := parseTransform(raw.Ks)
	if err != nil {
		return layer{}, err
	}
	shapes, err := parseGroup(raw.Shapes, 0)
	if err != nil {
		return layer{}, err
	}
	l := layer{group: group{transform: tr, groups: []group{shapes}}, inPoint: -math.MaxFloat32, outPoint: math.MaxFloat32}
	if raw.In != nil {
		l.inPoint = float32(*raw.In)
	}
	if raw.Out != nil {
		l.outPoint = float32(*raw.Out)
	}
	return l, nil
}

// parseGroup reads a group's items, depth groups deep. Styles paint the
// geometry before them in a group; the subset supports one fill and one
// stroke of all of it, or child groups styled on their own, without
// inheritance. Hidden items are never drawn, whatever they are.
func parseGroup(items []rawShape, depth int) (group, error) {
	if depth > maxGroupDepth {
		return group{}, fmt.Errorf("shape groups nest deeper than %d", maxGroupDepth)
	}
	var g group
	var tr *rawTransform
	var fills, strokes int
	for i := range items {
		it := &items[i]
		if it.Hidden {
			continue
		}
		if it.Blend != 0 {
			return group{}, fmt.Errorf("item %d: unsupported blend mode", i)
		}
		var err error
		switch it.Type {
		case "gr":
			var child group
			if child, err = parseGroup(it.Items, depth+1); err != nil {
				return group{}, fmt.Errorf("nested group %d: %w", i, err)
			}
			g.groups = append(g.groups, child)
		case "tr":
			if tr != nil {
				return group{}, fmt.Errorf("unsupported multiple transforms")
			}
			tr = &it.rawTransform
		case "sh", "rc", "el":
			if fills+strokes > 0 {
				return group{}, fmt.Errorf("unsupported interleaved geometry and styles")
			}
			var s geometry
			if s, err = parseGeometry(it); err != nil {
				return group{}, fmt.Errorf("item %d: %w", i, err)
			}
			g.geometry = append(g.geometry, s)
		case "fl", "st":
			if it.Type == "fl" {
				fills++
			} else {
				strokes++
			}
			if fills > 1 || strokes > 1 {
				return group{}, fmt.Errorf("unsupported multiple fills or strokes")
			}
			var s style
			if s, err = parseStyle(it); err != nil {
				return group{}, fmt.Errorf("item %d: %w", i, err)
			}
			g.styles = append(g.styles, s)
		default:
			return group{}, fmt.Errorf("item %d: unsupported shape type %q", i, it.Type)
		}
	}
	if len(g.groups) > 0 && len(g.geometry)+len(g.styles) > 0 {
		return group{}, fmt.Errorf("unsupported parent group style inheritance or mixed geometry")
	}
	if tr == nil {
		tr = &rawTransform{}
	}
	var err error
	if g.transform, err = parseTransform(*tr); err != nil {
		return group{}, fmt.Errorf("transform: %w", err)
	}
	g.retain()
	return g, nil
}

// parseGeometry reads a path, rectangle or ellipse, drawn in its default
// direction (d 1, or none).
func parseGeometry(it *rawShape) (geometry, error) {
	var direction int
	if len(it.D) > 0 {
		if err := json.Unmarshal(it.D, &direction); err != nil || direction != 0 && direction != 1 {
			return nil, fmt.Errorf("unsupported shape direction %s", it.D)
		}
	}
	if it.Type == "sh" {
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
		return ellipseShape{position, size}, nil
	}
	radius, err := parseScalar(it.R, 0)
	if err != nil {
		return nil, fmt.Errorf("roundness: %w", err)
	}
	return rectShape{position, size, radius}, nil
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
		var rule int
		if len(it.R) > 0 {
			if err := json.Unmarshal(it.R, &rule); err != nil || rule < 0 || rule >= len(fillRules) {
				return nil, fmt.Errorf("unsupported fill rule %s", it.R)
			}
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
	if it.Cap < 0 || it.Cap >= len(lineCaps) || it.Join < 0 || it.Join >= len(lineJoins) {
		return nil, fmt.Errorf("unsupported stroke cap or join")
	}
	miter := float32(4) // Lottie's default, as SVG's
	if it.Miter != nil {
		miter = float32(*it.Miter)
	}
	return &stroke{cap: lineCaps[it.Cap], join: lineJoins[it.Join], miter: miter, color: c, opacity: o, width: w}, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
