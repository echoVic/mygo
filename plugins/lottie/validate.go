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

// num is a number of the JSON. Every number Load reads is one, so that the
// bound is checked in this one place.
type num float32

func (n *num) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(string(data), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return fmt.Errorf("want a number, got %.20s", data)
	}
	if !(math.Abs(v) <= maxMagnitude) {
		return fmt.Errorf("number %s is outside ±%g", data, float64(maxMagnitude))
	}
	*n = num(v)
	return nil
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

// unsupported rejects the controls of a layer that change how it draws,
// outside the subset, rather than show a plausible but wrong image.
func (l *rawLayer) unsupported() error {
	switch {
	case l.Type != shapeLayer:
		return fmt.Errorf("unsupported layer type %d", l.Type)
	case l.DDD != 0:
		return fmt.Errorf("unsupported 3D layer")
	case l.Matte != 0:
		return fmt.Errorf("unsupported track matte (tt)")
	case l.MatteSource != 0:
		return fmt.Errorf("unsupported matte source (td)")
	case l.Blend != 0:
		return fmt.Errorf("unsupported blend mode (bm)")
	case l.AutoOrient != 0:
		return fmt.Errorf("unsupported auto-orient (ao)")
	case l.Stretch != nil && *l.Stretch != 1:
		return fmt.Errorf("unsupported layer time stretch")
	case present(l.Parent):
		return fmt.Errorf("unsupported parent")
	case present(l.Masks):
		return fmt.Errorf("unsupported masks")
	case present(l.Effects):
		return fmt.Errorf("unsupported effects")
	case present(l.TimeRemap):
		return fmt.Errorf("unsupported time remapping")
	}
	return nil
}

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
	if raw.Animated != 0 && raw.Animated != 1 {
		return raw, fmt.Errorf("unsupported animation flag %d", raw.Animated)
	}
	return raw, nil
}

// decodeKeyframes decodes an animated property's keyframes: at least one,
// at increasing times, without spatial tangents or easing per component.
// A last keyframe without a value, as exporters end a property, takes the
// previous one's end.
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
		for _, tangent := range []json.RawMessage{f.To, f.Ti} {
			if len(tangent) == 0 {
				continue
			}
			values, err := numbers(tangent, 0)
			if err != nil {
				return nil, fmt.Errorf("invalid spatial tangents: %w", err)
			}
			for _, v := range values {
				if v != 0 {
					return nil, fmt.Errorf("unsupported spatial tangents")
				}
			}
		}
		for _, controls := range [][]float32{f.I.X, f.I.Y, f.O.X, f.O.Y} {
			for _, v := range controls {
				if v != controls[0] {
					return nil, fmt.Errorf("unsupported component-specific easing")
				}
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
