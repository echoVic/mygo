# lottie

[Lottie](https://airbnb.io/lottie/) animations for
[MyGo](https://github.com/egoist/mygo) apps of native UI: a subset of
Lottie JSON whose shape and solid layers and precompositions, with the
null layers and parents that place them, MyGo draws itself, with
`ui.Painter` on ui's public API, and plays. It is all Go, with no renderer or native library
of its own.

```go
import "github.com/egoist/mygo/plugins/lottie"

animation, err := lottie.Load(data)
if err != nil {
	return err
}
player := animation.NewPlayer(lottie.PlayerOptions{Loop: true})
// Keep player in your view's state, then in the view:
ui.Box(c).Size(200, 200).Draw(player.Draw)
```

See [the documentation](../../docs/plugins/lottie.md), with the subset
it plays, and `examples/lottie`.
