package lottie

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// maxMagnitude bounds every number Load accepts, whatever it is: a
// coordinate, a scale, a color channel, a keyframe's time or an easing
// control. Up to it float32 still holds whole numbers exactly; past it a
// Lottie file means nothing a screen could show, and its products would
// overflow float32.
const maxMagnitude = 1e7

// maxGroupDepth is how deep shape groups may nest, which bounds the
// recursion of Load and Draw.
const maxGroupDepth = 32

// maxParentDepth is how many ancestors a layer may have, through parent
// links, which bounds the work of Draw for each layer.
const maxParentDepth = 32

// num is a number of the JSON. Every number Load reads is one, so that the
// bound is checked in this one place.
type num float32

func (n *num) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	v, err := parseNumber(data)
	if err != nil {
		return err
	}
	*n = num(v)
	return nil
}

// parseNumber reads a number of the JSON, within maxMagnitude.
func parseNumber(data []byte) (float64, error) {
	v, err := strconv.ParseFloat(string(data), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, fmt.Errorf("want a number, got %.20s", data)
	}
	if !(math.Abs(v) <= maxMagnitude) {
		return 0, fmt.Errorf("number %s is outside ±%g", data, float64(maxMagnitude))
	}
	return v, nil
}

// integer is a whole number of the JSON: a type, a flag, an index, a
// choice among several. Some exporters write every number as a float, and
// 4.0 is 4, as it is in JavaScript; 4.5 is an error.
type integer int

func (n *integer) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	v, err := parseNumber(data)
	if err != nil {
		return err
	}
	if v != math.Trunc(v) {
		return fmt.Errorf("want a whole number, got %.20s", data)
	}
	*n = integer(v)
	return nil
}

// optional decodes a whole number that may be missing or null, which it
// returns as nil; its errors say it is name's.
func optional(name string, raw json.RawMessage) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var n integer
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	v := int(n)
	return &v, nil
}

// enum decodes one of n choices, numbered from 0, which it is where it is
// missing or null; it reports false for anything else.
func enum(raw json.RawMessage, n int) (int, bool) {
	var v integer
	if len(raw) > 0 && json.Unmarshal(raw, &v) != nil || v < 0 || int(v) >= n {
		return 0, false
	}
	return int(v), true
}

// numbers decodes a value as Lottie writes it, a number or an array of
// them, wanting at least n.
func numbers(data json.RawMessage, n int) ([]num, error) {
	var values []num
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
	} else {
		var v num
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		values = []num{v}
	}
	if len(values) < n {
		return nil, fmt.Errorf("want at least %d numbers, got %d", n, len(values))
	}
	return values, nil
}

// present reports whether a control is set: not missing, null or empty.
func present(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte("[]"))
}

// unsupported rejects the controls of a layer that draws, outside the
// subset, that change how it draws or where, rather than show a
// plausible but wrong image.
func (l *rawLayer) unsupported(ty int) error {
	switch {
	case ty != shapeLayer && ty != solidLayer:
		if ty >= 0 && ty < len(layerTypes) && layerTypes[ty] != "" {
			return fmt.Errorf("unsupported %s layer (ty %d)", layerTypes[ty], ty)
		}
		return fmt.Errorf("unsupported layer type %d", ty)
	case l.Matte != 0:
		return fmt.Errorf("unsupported track matte (tt)")
	case l.MatteSource != 0:
		return fmt.Errorf("unsupported matte source (td)")
	case l.Blend != 0:
		return fmt.Errorf("unsupported blend mode (bm)")
	case present(l.Masks):
		return fmt.Errorf("unsupported masks")
	case present(l.Effects):
		return fmt.Errorf("unsupported effects")
	case present(l.TimeRemap):
		return fmt.Errorf("unsupported time remapping")
	}
	return l.rawPlacement.unsupported()
}

// unsupported rejects the controls outside the subset that change where a
// layer is, and so where its children are, whether it draws or not.
// What else a layer carries, masks, effects, mattes, changes only how it
// draws itself: a layer that only positions others ignores them.
func (l *rawPlacement) unsupported() error {
	if _, ok := enum(l.DDD, 1); !ok {
		return fmt.Errorf("unsupported 3D layer (ddd %s)", l.DDD)
	}
	switch {
	case l.AutoOrient != 0:
		return fmt.Errorf("unsupported auto-orient (ao)")
	case l.Stretch != nil && *l.Stretch != 1:
		return fmt.Errorf("unsupported layer time stretch")
	}
	return nil
}

// layerTypes and shapeTypes name the kinds of layers (ty) and shape items
// (ty) that Load rejects, for its errors.
var (
	layerTypes = [...]string{0: "precomposition", 2: "image", 5: "text", 6: "audio", 13: "camera"}
	shapeTypes = map[string]string{
		"gf": "gradient fill", "gs": "gradient stroke", "tm": "trim paths", "rp": "repeater",
		"mm": "merge paths", "rd": "rounded corners", "sr": "polystar", "pb": "pucker and bloat",
		"tw": "twist", "op": "offset path", "zz": "zig zag", "no": "no style",
	}
)

// decodeProperty decodes a property's envelope, rejecting expressions and
// positions split into separate dimensions.
func decodeProperty(data json.RawMessage) (rawProperty, error) {
	var raw rawProperty
	if err := json.Unmarshal(data, &raw); err != nil {
		return raw, err
	}
	if raw.Split || present(raw.Expression) && !bytes.Equal(raw.Expression, []byte(`""`)) {
		return raw, fmt.Errorf("unsupported expression or split position")
	}
	return raw, nil
}

// keyframed reports whether a property's k holds keyframes: an array whose
// first entry is an object. Otherwise it holds a value: a number, an array
// of numbers, a path. Its a, which says which, is ignored, as lottie-web
// ignores it (getProp in PropertyFactory.js); exporters write a 0 with
// keyframes, or none.
func keyframed(k json.RawMessage) bool {
	if len(k) == 0 || k[0] != '[' {
		return false
	}
	rest := bytes.TrimLeft(k[1:], " \t\r\n")
	return len(rest) > 0 && rest[0] == '{'
}

// decodeKeyframes decodes an animated property's keyframes: at least one,
// at increasing times, and their spatial tangents, of which a 2D curve
// reads x and y. A last keyframe without a value, as exporters end a
// property, takes the previous one's end.
func decodeKeyframes(data json.RawMessage) ([]rawKeyframe, error) {
	var frames []rawKeyframe
	if err := json.Unmarshal(data, &frames); err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("animated property has no keyframes")
	}
	for i := range frames {
		f := &frames[i]
		if i > 0 && !(f.T > frames[i-1].T) {
			return nil, fmt.Errorf("keyframe times must increase")
		}
		for _, tangent := range []struct {
			raw json.RawMessage
			dst *point
		}{{f.To, &f.out}, {f.Ti, &f.in}} {
			if len(tangent.raw) == 0 {
				continue
			}
			values, err := numbers(tangent.raw, 0)
			if err != nil {
				return nil, fmt.Errorf("invalid spatial tangents: %w", err)
			}
			if len(values) > 0 {
				tangent.dst.X = float32(values[0])
			}
			if len(values) > 1 {
				tangent.dst.Y = float32(values[1])
			}
		}
		if !present(f.S) {
			if i != len(frames)-1 || i == 0 {
				return nil, fmt.Errorf("keyframe missing start value")
			}
			f.S = frames[i-1].E
			if len(f.S) == 0 {
				f.S = frames[i-1].S
			}
		}
	}
	return frames, nil
}
