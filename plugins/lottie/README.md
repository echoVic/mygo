# Lottie

A shape-only Lottie JSON player for MyGo's native UI, drawn through the
public `ui.Painter`. It uses Go's standard library and MyGo, with no extra
renderer or native library.

```go
animation, err := lottie.Load(data)
if err != nil {
    return err
}
player := animation.NewPlayer(lottie.PlayerOptions{Loop: true})
// Keep player in your view's state, then in the view:
ui.Box(c).Size(200, 200).Draw(player.Draw)
```

See [the documentation](../../docs/plugins/lottie.md) for the supported
subset, playback controls and reduced motion. Run the original embedded
animation with `go run ./examples/lottie`.
