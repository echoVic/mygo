# Lottie

The Go-only `plugins/lottie` package plays a bounded subset of Lottie JSON
in native UI. It renders vector shapes through MyGo's public painter;
it does not need a JavaScript runtime, an npm package, a native library,
or registration with `mygo.Use`.

## Load and draw

Load once, keep one player per view, and draw it in an element's paint
callback. The player starts automatically. Bounds use DIPs; the
composition fits inside them with its aspect ratio preserved and is
centered, and nothing it draws shows outside its own rectangle, as in
other Lottie players. Paths are rasterized at the size they are drawn, so
a small composition drawn large stays sharp.

```go
import (
    _ "embed"

    "github.com/egoist/mygo/plugins/lottie"
    "github.com/egoist/mygo/ui"
)

//go:embed success.json
var data []byte

// During initialization; handle the error before showing the view:
animation, err := lottie.Load(data)
if err != nil {
    return err
}
player := animation.NewPlayer(lottie.PlayerOptions{Loop: true, Speed: 1})

// Inside the view, using that same player:
ui.Box(c).Size(200, 200).Label("Saved").Draw(player.Draw)
```

`Load` owns the decoded data. An animation can be shared by several
players, each with its own clock. `Width`, `Height`, `FrameRate`, and
`Duration()` describe the composition. Editing the exported metadata
does not change playback. Player methods are safe to call concurrently;
use `Draw` from one view's paint callback and do not retain its painter.
A zero-value player draws nothing.

## Playback

`NewPlayer()` plays once. `NewPlayer(lottie.PlayerOptions{Loop: true})`
repeats. `Speed` is a positive multiplier; zero, negative and non-finite
values use 1. `Duration()` returns composition time, independent of speed.

| Method | Behavior |
| --- | --- |
| `Play()` | Resume from the current position. At the end of a one-shot animation, reset or seek before playing again. |
| `Pause()` | Hold the last painted position. |
| `Seek(time.Duration)` | Choose a composition time, clamped between zero and duration. Keep the playing or paused state. |
| `Reset()` | Seek to zero, keeping the playing or paused state. |
| `Draw(*ui.Painter, ui.Rect)` | Draw the current frame and request another paint while playback is active. |

After changing a paused or completed player, request a view update so
its paint callback runs again. For example, in a button handler:

```go
if ui.Button(c.Key("replay"), "Replay").Clicked() {
    player.Reset()
    player.Play()
    c.Services().Invalidate()
}
```

The clock comes from `Painter.Now()`. No timer or goroutine is started.
Pause and seek establish a fresh clock anchor at the next paint, so time
spent paused does not advance the animation. A one-shot player holds the
last frame and stops requesting frames. With reduced motion enabled,
it draws the first frame and requests no animation frames. Give meaningful
animations an accessible label and put essential state in text as well.

## Supported JSON

This first version is intended for simple shape assets. Exporting an
arbitrary After Effects project is not sufficient to guarantee compatibility.
`Load` reports an error for recognized features that change rendering but
are outside the subset, with a layer/group/property context.

| Area | Supported |
| --- | --- |
| Composition | Width and height that are positive whole numbers (`100` or `100.0`; `100.5` is rejected), a positive frame rate, in and out points. |
| Layers | 2D shape layers (`ty: 4`), solid layers (`ty: 1`) and null layers (`ty: 3`). A layer shows from its in point up to its out point, not at it, as in lottie-web (lottie-ios also shows it at its out point). Null layers and hidden layers (`hd: true`) draw nothing; they matter only as parents. A layer's start time (`st`) is accepted and changes nothing: these layers' keyframes and in and out points are in composition frames. |
| Solid layers | A rectangle from (0, 0) to the solid's size (`sw`, `sh`), filled with its color (`sc`: `#rrggbb`, as exporters write it, or `#rgb`), under the layer's transform and opacity. The size must be given and not negative; a solid of no size draws nothing. Other CSS colors, and hex colors with an alpha, are rejected. |
| Parenting | A layer whose `parent` is another layer's `ind` moves with it: its transform is inside its parent's, inside its parent's parent's, and so on, all sampled at the same frame. Only the layer's own in and out points, visibility and opacity decide whether and how it shows; its parents' don't, as in After Effects and lottie-web. Parents don't change the drawing order, which stays the order of the layers. |
| Geometry | Cubic Bezier paths, rounded rectangles, ellipses; compound paths with nonzero or even-odd fill. A path runs in the order of its vertices. A rectangle or an ellipse runs clockwise (`d: 1` or `d: 2`) or the other way (`d: 3`); without `d` it runs clockwise, as in lottie-ios (lottie-web runs such a rectangle the other way). A rectangle starts at its top right corner, below the corner's curve, and goes down its right side, and an ellipse starts at its top, as in lottie-web and lottie-ios, which decides where a trim starts. A negative width or height mirrors the shape, so that it runs the other way; a rectangle's roundness is at most half its shorter side. |
| Styles | Solid fills and strokes, with opacity, width, cap, join and miter limit (4 when `ml` is missing). A style paints all the geometry listed before it in its group, that of nested groups too, under their transforms, as in lottie-web: geometry and styles may come in any order, and several styles may paint the same geometry, at most 16 the same shape or group. The item listed first is on top, styles and nested groups alike. A stroke's width is in its own group's coordinates: the transforms of nested groups move only their geometry's points, and their opacity applies only to their own styles. Groups nest at most 32 deep. Hidden items are skipped, whatever they are, as in lottie-ios (lottie-web still applies a hidden trim). |
| Trim paths | A trim (`tm`), with its start `s`, end `e` and offset `o`, static or keyframed, cuts the geometry listed before it in its group, that of nested groups too, as lottie-web's trim modifier does. Every style that paints that geometry paints what is left, a style listed before the trim too; a fill fills each piece as if it were closed. Start and end are percentages of the length, clamped to 0 to 100; the offset is in degrees, 360 the whole length, and a cut it carries past the end wraps around to the start, in one piece across the start of a closed path. A start after the end cuts as if they were swapped, a start equal to the end leaves nothing, and 0 to 100 without an offset leaves the geometry as it was, closed paths closed. Simultaneously (`m: 1`), each shape is cut on its own, its subpaths end to end; individually (`m: 2`), the shapes are laid end to end, in the order they are listed, and cut as one. Several trims cut one after the other, in the order they are listed, at most 8 the same shape. Lengths are measured along the curves, in each shape's own coordinates, before any group's transform, as in lottie-web. A trim whose values are static and whose geometry doesn't change is cut once, by `Load`; the others every frame. |
| Transform | Anchor, position, scale, rotation and opacity on layers and nested groups. A position split into `x` and `y` (`p.s: true`) animates each on its own; its `z`, like a third component of any 2D value, is ignored. Static zero skew fields are accepted. |
| Properties | Static and keyframed scalar, 2D point, color and path values; hold, linear and cubic temporal Bezier easing, including back and overshoot easing (`y` controls outside 0 to 1; `x` controls are clamped to 0 to 1). Path morphs preserve vertex count and closed/open state. Whether a property is keyframed is decided by its value, as in lottie-web: `k` is keyframes when it is a list of objects. Its `a` flag, which exporters don't always set to match, is ignored. |
| Easing per component | Controls written as arrays (`"o": {"x": [0.2, 0.6], "y": [0, 0]}`) ease each component on its own curve: a point's `x` and `y`, a color's channels. Component n uses entry n of each control, or entry 0 where a control has fewer, as lottie-web does; entries beyond the value's components are ignored, such as the third of a scale exported as `[100, 100, 100]`. A scalar uses the first entry. A path has a single curve: the entries of its keyframes' controls must agree. |
| Spatial tangents | A keyframe of a 2D point with an out tangent `to` and an in tangent `ti` (exporters write them for positions and anchors: a layer's, a group's, a rectangle's, an ellipse's) moves along the cubic Bezier from its value, through value + `to` and next value + `ti`, to the next value. Its eased progress is a fraction of the curve's length, which `Load` measures once, so the motion keeps an even speed along the curve; it starts and ends exactly on the keyframes' values. Easing that overshoots holds the value at the end of the curve it passes. Without tangents, or with zero ones, a value moves in a straight line. |

Every number that `Load` reads as a value must be within ±10,000,000
(1e7): the composition's size, frame rate and in and out points, layers'
in and out points, `ind` and `parent`, solids' sizes, and every
coordinate, size, scale, angle, color, opacity, width, miter limit,
trim's start, end and offset, keyframe time and easing control. `Load`
rejects a composition with a number beyond that. Easing that overshoots
can still carry a value past it, or a chain of large scales overflow;
`Draw` skips what it then cannot draw rather than fail. Overshooting
values stay valid: opacity and color channels are clamped to their
ranges, a trim's start and end to 0 to 100, and a stroke whose width
falls to zero or below is not drawn.

Numbers that `Load` reads as whole ones (a layer's type `ty`, `ind`,
`parent` and `ddd`, a keyframe's hold `h`, a stroke's cap and join, a
fill rule, a shape's direction, a trim's mode) may be written as floats,
as some exporters write every number: `4.0` reads as 4, and `4.5` is an
error.

`Load` resolves parent links once. A `parent` that no layer's `ind` is,
or that several layers' `ind` is, links that form a cycle, and a chain of
more than 32 ancestors are errors, which name the layer. A null or hidden
layer that is the ancestor of a layer that draws is read for its
transform, which must be in the subset like any (2D, without skew,
auto-orient, time stretch or expressions); what else it carries (masks,
effects, mattes, blend modes, time remapping) would change only how it
draws itself, and is ignored. Every layer's `ind` is read, to resolve
links; null and hidden layers that place no layer that draws are
otherwise skipped, whatever they contain.

A group may have one transform (`tr`); several are rejected. Spatial
tangents on values other than 2D points (a rotation, a color) are
rejected, and so is easing per component on a keyframe that moves along
spatial tangents, which has a single curve, as in lottie-web. Colors
interpolate channel by channel in the JSON color space.

Unsupported features include precompositions, images, text, masks,
mattes, blend modes, effects, expressions, 3D, auto-orient, time
remapping/stretch, gradients, repeaters and other shape operators. A
path (`sh`) with reverse direction (`d: 3`) is rejected: export paths
with the intended vertex order and winding, or use even-odd compound
fills.

A stroke scales with its transforms, unevenly where they scale unevenly,
as in After Effects.

Opacity multiplies each primitive's alpha. Overlapping children or a
fill and stroke in a translucent group can therefore look different from
After Effects' isolated group compositing. This version does not allocate
an offscreen layer to isolate group opacity. There is no claim of full
Lottie conformance.

## Example and tests

Run `go run ./examples/lottie` for an original embedded orbit-badge
animation with play, pause, reset and seek controls. The fixture is under
the repository's license and uses no third-party artwork.

`go test ./plugins/lottie` uses deterministic `ui.Tester` rendering and
clock advancement to check pixels, timing, controls, reduced motion and
validation. Native window behavior and GPU output require a desktop run.

To measure how much of a collection of real files the subset plays, point
`LOTTIE_CORPUS` at a directory of them:

```sh
LOTTIE_CORPUS=path/to/samples go test -v -run Corpus ./plugins/lottie
```

The test loads every Lottie document under it (a JSON object with
`layers`), draws frames across each that loads, and fails if any panics.
It logs the share that loads, in all and per top-level folder, and the
first error of each file that doesn't, like errors grouped, most frequent
first. Without the variable it is skipped.
