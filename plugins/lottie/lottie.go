// Package lottie plays a deliberately small, native subset of Lottie JSON
// animations through MyGo's public ui painter.
package lottie

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
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

// Duration returns the composition's playback duration.
func (a *Animation) Duration() time.Duration { return a.duration() }

func (a *Animation) duration() time.Duration {
	if a == nil || a.frameRate <= 0 || a.outPoint <= a.inPoint {
		return 0
	}
	return time.Duration(math.Round((float64(a.outPoint) - float64(a.inPoint)) / float64(a.frameRate) * float64(time.Second)))
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
	duration := (float64(raw.Op) - float64(raw.Ip)) / float64(raw.Fr) * float64(time.Second)
	if !finite(duration) || duration < 1 || duration >= float64(math.MaxInt64) {
		return nil, fmt.Errorf("lottie: duration must fit in time.Duration")
	}
	anim := &Animation{width: raw.W, height: raw.H, frameRate: raw.Fr, Width: raw.W, Height: raw.H, FrameRate: raw.Fr, inPoint: raw.Ip, outPoint: raw.Op}
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
	Ip     *float32          `json:"ip"`
	Op     *float32          `json:"op"`
	St     float32           `json:"st"`
	Hidden bool              `json:"hd"`
	DDD    int               `json:"ddd"`
	Name   string            `json:"nm"`
	Ks     rawTransform      `json:"ks"`
	Shapes []json.RawMessage `json:"shapes"`
}

type rawTransform struct {
	Sk, Sa, Rx, Ry, Rz, Or json.RawMessage
	Anchor                 json.RawMessage `json:"a"`
	Position               json.RawMessage `json:"p"`
	Scale                  json.RawMessage `json:"s"`
	Rotation               json.RawMessage `json:"r"`
	Opacity                json.RawMessage `json:"o"`
}

type layer struct {
	name                     string
	inPoint, outPoint, start float32
	hidden                   bool
	opacity                  scalarProperty
	transform                transform
	groups                   []group
}

type transform struct {
	anchor   pointProperty
	position pointProperty
	scale    pointProperty
	rotation scalarProperty
	opacity  scalarProperty
}

type group struct {
	transform   transform
	paths       []pathShape
	rects       []rectShape
	strokeFirst bool
	ellipses    []ellipseShape
	fills       []fillShape
	strokes     []strokeShape
	groups      []group
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
	rule    int
	color   colorProperty
	opacity scalarProperty
}

type strokeShape struct {
	cap, join int
	miter     float32
	color     colorProperty
	opacity   scalarProperty
	width     scalarProperty
}

func parseLayer(data json.RawMessage) (layer, error) {
	if err := validateLayer(data); err != nil {
		return layer{}, err
	}
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
	l := layer{name: raw.Name, transform: tr, opacity: opacity, start: raw.St, hidden: raw.Hidden, inPoint: -math.MaxFloat32, outPoint: math.MaxFloat32}
	if raw.Ip != nil {
		l.inPoint = *raw.Ip
	}
	if raw.Op != nil {
		l.outPoint = *raw.Op
	}
	root, err := parseGroup(raw.Shapes)
	if err != nil {
		return layer{}, err
	}
	l.groups = []group{root}
	return l, nil
}

func parseTransform(raw rawTransform) (transform, error) {
	for _, v := range []json.RawMessage{raw.Sk, raw.Sa} {
		if len(v) == 0 {
			continue
		}
		neutral, err := parseScalar(v, 0)
		if err != nil || len(neutral.keys) != 0 || neutral.static != 0 {
			return transform{}, fmt.Errorf("unsupported skew transform")
		}
	}
	for _, v := range []json.RawMessage{raw.Rx, raw.Ry, raw.Rz, raw.Or} {
		if len(v) > 0 {
			return transform{}, fmt.Errorf("unsupported skew or 3D transform")
		}
	}
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

func parseGroup(items []json.RawMessage) (group, error) {
	if err := validateGroup(items); err != nil {
		return group{}, err
	}
	g := group{transform: defaultTransform()}
	for i, data := range items {
		var header struct {
			Type   string            `json:"ty"`
			Hidden bool              `json:"hd"`
			It     []json.RawMessage `json:"it"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return group{}, fmt.Errorf("item %d: %w", i, err)
		}
		if header.Hidden {
			switch header.Type {
			case "gr", "tr", "sh", "rc", "el", "fl", "st":
				continue
			}
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
			Rule int             `json:"r"`
			C    json.RawMessage `json:"c"`
			O    json.RawMessage `json:"o"`
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
		if raw.Rule < 0 || raw.Rule > 2 {
			return fmt.Errorf("unsupported fill rule")
		}
		g.strokeFirst = len(g.strokes) == 0
		g.fills = append(g.fills, fillShape{rule: raw.Rule, color: c, opacity: o})
	case "st":
		var raw struct {
			Cap   int             `json:"lc"`
			Join  int             `json:"lj"`
			Miter float32         `json:"ml"`
			C     json.RawMessage `json:"c"`
			O     json.RawMessage `json:"o"`
			W     json.RawMessage `json:"w"`
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
		if raw.Cap < 0 || raw.Cap > 3 || raw.Join < 0 || raw.Join > 3 {
			return fmt.Errorf("unsupported stroke cap or join")
		}
		g.strokes = append(g.strokes, strokeShape{cap: raw.Cap, join: raw.Join, miter: raw.Miter, color: c, opacity: o, width: w})
	default:
		return fmt.Errorf("unsupported shape type %q", typ)
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
