package lottie

import (
	"github.com/egoist/mygo/ui"
)

func defaultTransform() transform {
	return transform{
		anchor:   pointProperty{static: point{}},
		position: pointProperty{static: point{}},
		scale:    pointProperty{static: point{X: 100, Y: 100}},
		rotation: scalarProperty{static: 0},
		opacity:  scalarProperty{static: 100},
	}
}

func sampleTransform(t transform, frame float32) ui.Transform {
	anchor := t.anchor.sample(frame)
	position := t.position.sample(frame)
	scale := t.scale.sample(frame)
	rotation := t.rotation.sample(frame)
	return ui.Translation(-anchor.X, -anchor.Y).
		Then(ui.Scaling(scale.X/100, scale.Y/100)).
		Then(ui.Rotation(rotation)).
		Then(ui.Translation(position.X, position.Y))
}
