// Package lottie plays a deliberately small, native subset of Lottie JSON
// animations through MyGo's public ui painter.
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

	inPoint  float32
	outPoint float32
	layers   []layer
}

// Duration returns the composition's playback duration.
func (a *Animation) Duration() time.Duration { return a.duration() }

func (a *Animation) duration() time.Duration {
	if a == nil || a.FrameRate <= 0 || a.outPoint <= a.inPoint {
		return 0
	}
	return time.Duration(math.Round(float64(a.outPoint-a.inPoint) / float64(a.FrameRate) * float64(time.Second)))
}

// Load decodes a Lottie composition. The MVP accepts shape layers with paths,
// rectangles, ellipses, solid fills, solid strokes and 2D transforms.
func Load(data []byte) (*Animation, error) {
	var raw rawAnimation
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("lottie: decode composition: %w", err)
	}
	if raw.W <= 0 || raw.H <= 0 {
		return nil, fmt.Errorf("lottie: composition size must be positive")
	}
	if raw.Fr <= 0 || !finite(float64(raw.Fr)) {
		return nil, fmt.Errorf("lottie: frame rate must be positive and finite")
	}
	if raw.Op <= raw.Ip {
		return nil, fmt.Errorf("lottie: out point must be after in point")
	}
	anim := &Animation{Width: raw.W, Height: raw.H, FrameRate: raw.Fr, inPoint: raw.Ip, outPoint: raw.Op}
	anim.layers = make([]layer, 0, len(raw.Layers))
	for i, data := range raw.Layers {
		l, err := parseLayer(data)
		if err != nil {
			return nil, fmt.Errorf("lottie: layer %d: %w", i, err)
		}
		anim.layers = append(anim.layers, l)
	}
	return anim, nil
}

type rawAnimation struct {
	W      int               `json:"w"`
	H      int               `json:"h"`
	Fr     float32           `json:"fr"`
	Ip     float32           `json:"ip"`
	Op     float32           `json:"op"`
	Layers []json.RawMessage `json:"layers"`
}

type rawLayer struct {
	Ty     int               `json:"ty"`
	DDD    int               `json:"ddd"`
	Name   string            `json:"nm"`
	Ks     rawTransform      `json:"ks"`
	Shapes []json.RawMessage `json:"shapes"`
}

type rawTransform struct {
	Anchor   json.RawMessage `json:"a"`
	Position json.RawMessage `json:"p"`
	Scale    json.RawMessage `json:"s"`
	Rotation json.RawMessage `json:"r"`
	Opacity  json.RawMessage `json:"o"`
}

type layer struct {
	name      string
	opacity   scalarProperty
	transform transform
	groups    []group
}

type transform struct {
	anchor   pointProperty
	position pointProperty
	scale    pointProperty
	rotation scalarProperty
	opacity  scalarProperty
}

type group struct {
	transform transform
	paths     []pathShape
	rects     []rectShape
	ellipses  []ellipseShape
	fills     []fillShape
	strokes   []strokeShape
	groups    []group
}

type pathShape struct{ path pathProperty }

type rectShape struct {
	position pointProperty
	size     pointProperty
	radius   scalarProperty
}

type ellipseShape struct {
	position pointProperty
	size     pointProperty
}

type fillShape struct {
	color   colorProperty
	opacity scalarProperty
}

type strokeShape struct {
	color   colorProperty
	opacity scalarProperty
	width   scalarProperty
}

func parseLayer(data json.RawMessage) (layer, error) {
	var raw rawLayer
	if err := json.Unmarshal(data, &raw); err != nil {
		return layer{}, err
	}
	if raw.Ty != 4 {
		return layer{}, fmt.Errorf("unsupported layer type %d", raw.Ty)
	}
	if raw.DDD != 0 {
		return layer{}, fmt.Errorf("unsupported 3D layer")
	}
	tr, err := parseTransform(raw.Ks)
	if err != nil {
		return layer{}, err
	}
	opacity, err := parseScalar(raw.Ks.Opacity, 100)
	if err != nil {
		return layer{}, fmt.Errorf("layer opacity: %w", err)
	}
	l := layer{name: raw.Name, transform: tr, opacity: opacity}
	for i, data := range raw.Shapes {
		g, ok, err := parseGroupOrShape(data)
		if err != nil {
			return layer{}, fmt.Errorf("shape %d: %w", i, err)
		}
		if ok {
			l.groups = append(l.groups, g)
		}
	}
	return l, nil
}

func parseTransform(raw rawTransform) (transform, error) {
	anchor, err := parsePoint(raw.Anchor, point{})
	if err != nil {
		return transform{}, fmt.Errorf("anchor: %w", err)
	}
	position, err := parsePoint(raw.Position, point{})
	if err != nil {
		return transform{}, fmt.Errorf("position: %w", err)
	}
	scale, err := parsePoint(raw.Scale, point{X: 100, Y: 100})
	if err != nil {
		return transform{}, fmt.Errorf("scale: %w", err)
	}
	rotation, err := parseScalar(raw.Rotation, 0)
	if err != nil {
		return transform{}, fmt.Errorf("rotation: %w", err)
	}
	opacity, err := parseScalar(raw.Opacity, 100)
	if err != nil {
		return transform{}, fmt.Errorf("opacity: %w", err)
	}
	return transform{anchor: anchor, position: position, scale: scale, rotation: rotation, opacity: opacity}, nil
}

func parseGroupOrShape(data json.RawMessage) (group, bool, error) {
	var header struct {
		Type string            `json:"ty"`
		It   []json.RawMessage `json:"it"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return group{}, false, err
	}
	if header.Type == "gr" {
		g, err := parseGroup(header.It)
		return g, true, err
	}
	g := group{}
	if err := addShape(&g, data, header.Type); err != nil {
		return group{}, false, err
	}
	return g, true, nil
}

func parseGroup(items []json.RawMessage) (group, error) {
	var g group
	for i, data := range items {
		var header struct {
			Type string            `json:"ty"`
			It   []json.RawMessage `json:"it"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return group{}, fmt.Errorf("item %d: %w", i, err)
		}
		if header.Type == "tr" {
			var raw rawTransform
			if err := json.Unmarshal(data, &raw); err != nil {
				return group{}, fmt.Errorf("transform: %w", err)
			}
			tr, err := parseTransform(raw)
			if err != nil {
				return group{}, fmt.Errorf("transform: %w", err)
			}
			g.transform = tr
			continue
		}
		if header.Type == "gr" {
			nested, err := parseGroup(header.It)
			if err != nil {
				return group{}, fmt.Errorf("nested group %d: %w", i, err)
			}
			g.groups = append(g.groups, nested)
			continue
		}
		if err := addShape(&g, data, header.Type); err != nil {
			return group{}, fmt.Errorf("item %d: %w", i, err)
		}
	}
	return g, nil
}

func addShape(g *group, data json.RawMessage, typ string) error {
	switch typ {
	case "sh":
		var raw struct {
			KS json.RawMessage `json:"ks"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		path, err := parsePath(raw.KS)
		if err != nil {
			return err
		}
		g.paths = append(g.paths, pathShape{path: path})
	case "rc":
		var raw struct {
			P json.RawMessage `json:"p"`
			S json.RawMessage `json:"s"`
			R json.RawMessage `json:"r"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		position, err := parsePoint(raw.P, point{})
		if err != nil {
			return fmt.Errorf("rectangle position: %w", err)
		}
		size, err := parsePoint(raw.S, point{})
		if err != nil {
			return fmt.Errorf("rectangle size: %w", err)
		}
		radius, err := parseScalar(raw.R, 0)
		if err != nil {
			return fmt.Errorf("rectangle radius: %w", err)
		}
		g.rects = append(g.rects, rectShape{position: position, size: size, radius: radius})
	case "el":
		var raw struct {
			P json.RawMessage `json:"p"`
			S json.RawMessage `json:"s"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		position, err := parsePoint(raw.P, point{})
		if err != nil {
			return fmt.Errorf("ellipse position: %w", err)
		}
		size, err := parsePoint(raw.S, point{})
		if err != nil {
			return fmt.Errorf("ellipse size: %w", err)
		}
		g.ellipses = append(g.ellipses, ellipseShape{position: position, size: size})
	case "fl":
		var raw struct {
			C json.RawMessage `json:"c"`
			O json.RawMessage `json:"o"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		c, err := parseColor(raw.C, color{A: 1})
		if err != nil {
			return fmt.Errorf("fill color: %w", err)
		}
		o, err := parseScalar(raw.O, 100)
		if err != nil {
			return fmt.Errorf("fill opacity: %w", err)
		}
		g.fills = append(g.fills, fillShape{color: c, opacity: o})
	case "st":
		var raw struct {
			C json.RawMessage `json:"c"`
			O json.RawMessage `json:"o"`
			W json.RawMessage `json:"w"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		c, err := parseColor(raw.C, color{A: 1})
		if err != nil {
			return fmt.Errorf("stroke color: %w", err)
		}
		o, err := parseScalar(raw.O, 100)
		if err != nil {
			return fmt.Errorf("stroke opacity: %w", err)
		}
		w, err := parseScalar(raw.W, 1)
		if err != nil {
			return fmt.Errorf("stroke width: %w", err)
		}
		g.strokes = append(g.strokes, strokeShape{color: c, opacity: o, width: w})
	default:
		return fmt.Errorf("unsupported shape type %q", typ)
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Keep the public ui dependency visible in this file's package documentation;
// drawing methods are added in shape.go.
var _ *ui.Painter
