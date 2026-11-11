package lottie

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// These controls change the interpretation of geometry. Reject them before
// normalization rather than displaying a plausible but incorrect partial image.
func rejectControls(data json.RawMessage, numeric []string, present []string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range numeric {
		if raw := fields[key]; len(raw) > 0 {
			var value float64
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("unsupported %s", key)
			}
			if value != 0 {
				return fmt.Errorf("unsupported %s", key)
			}
		}
	}
	for _, key := range present {
		if raw := fields[key]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte("[]")) {
			return fmt.Errorf("unsupported %s", key)
		}
	}
	return nil
}

func validateLayer(data json.RawMessage) error {
	if err := rejectControls(data, []string{"tt", "td", "bm", "ao"}, []string{"parent", "masksProperties", "ef", "tm"}); err != nil {
		return err
	}
	var v struct {
		Stretch *float32 `json:"sr"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Stretch != nil && *v.Stretch != 1 {
		return fmt.Errorf("unsupported layer time stretch")
	}
	return nil
}

// Shape styles apply to the preceding geometry in a group. The MVP supports
// one shared fill and stroke, or independent child groups, without inheritance.
func validateGroup(items []json.RawMessage) error {
	var geometry, groups, fills, strokes, transform bool
	for _, raw := range items {
		var h struct {
			Type   string `json:"ty"`
			Hidden bool   `json:"hd"`
		}
		if err := json.Unmarshal(raw, &h); err != nil {
			return err
		}
		if err := rejectControls(raw, []string{"bm"}, nil); err != nil {
			return err
		}
		switch h.Type {
		case "tr":
			if !h.Hidden {
				if transform {
					return fmt.Errorf("unsupported multiple transforms")
				}
				transform = true
			}
		case "rc", "el", "sh":
			var direction struct {
				D int `json:"d"`
			}
			if err := json.Unmarshal(raw, &direction); err != nil {
				return err
			}
			if direction.D != 0 && direction.D != 1 {
				return fmt.Errorf("unsupported shape direction %d", direction.D)
			}
			if !h.Hidden {
				if fills || strokes {
					return fmt.Errorf("unsupported interleaved geometry and styles")
				}
				geometry = true
			}
		case "gr":
			groups = true
		case "fl":
			if !h.Hidden {
				if fills {
					return fmt.Errorf("unsupported multiple fills")
				}
				fills = true
			}
		case "st":
			if err := rejectControls(raw, nil, []string{"d"}); err != nil {
				return err
			}
			if !h.Hidden {
				if strokes {
					return fmt.Errorf("unsupported multiple strokes")
				}
				strokes = true
			}
		}
	}
	if groups && (geometry || fills || strokes) {
		return fmt.Errorf("unsupported parent group style inheritance or mixed geometry")
	}
	return nil
}
