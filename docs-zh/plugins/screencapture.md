# 屏幕捕获

屏幕捕获插件（`plugins/screencapture`）为显示器和窗口截图，也能把它们录制成一帧帧图像，用的都是各系统自带的接口：macOS 上是 ScreenCaptureKit，Windows 上是 Windows.Graphics.Capture，Linux 上是 xdg-desktop-portal 的门户和 PipeWire。插件完全用 Go 编写：没有 JavaScript 包，也无需调用 `mygo.Use`。如果页面只需要显示或发送屏幕画面（例如视频通话），可以改用 `navigator.mediaDevices.getDisplayMedia`。

```go
import "github.com/egoist/mygo/plugins/screencapture"

img, err := screencapture.Capture(ctx, screencapture.Source{}, screencapture.Options{})
```

`img` 是主显示器的 `*image.RGBA`，尺寸为显示器的像素：Retina 显示器得到的图像是其点数尺寸的两倍。

[examples/screencapture](../../examples/screencapture) 截取主显示器，或者列出屏幕上的窗口并截取选中的那个，然后显示截到的图像。

## 来源

零值 `Source` 表示主显示器。`DisplaySource` 接受 `mygo.Screen.Displays` 中某个显示器的 ID，`WindowSource` 接受 `screencapture.Windows` 中某个窗口的 ID。`Windows` 列出屏幕上所有应用的窗口，最前面的排在最先，并带有标题、所属应用和位置：

```go
for _, d := range mygo.Screen.Displays() {
	img, err := screencapture.Capture(ctx, screencapture.DisplaySource(d.ID), screencapture.Options{})
	// ...
}

wins, err := screencapture.Windows(ctx)
for _, w := range wins {
	fmt.Println(w.App, w.Title, w.Bounds)
}
img, err := screencapture.Capture(ctx, wins[0].Source(), screencapture.Options{HideCursor: true})
```

窗口即使被其他窗口挡住，也会按原样截取，但最小化时不行。

Linux 不按 ID 区分显示器和窗口：每次都由门户在桌面的对话框中请用户选择。在 Linux 上，`Windows` 返回 `errors.ErrUnsupported`。对显示器来源，`Capture` 截取整个桌面；对窗口来源（ID 任意），截取用户选择的内容。`Record` 让用户选择一个显示器，对窗口来源则让用户选择一个窗口。

## 录制

`Record` 开始一次录制，录制的帧通过通道传来，直到录制停止：

```go
rec, err := screencapture.Record(ctx, src, screencapture.Options{FrameRate: 30})
if err != nil {
	return err
}
time.AfterFunc(10*time.Second, func() { rec.Stop() })
for f := range rec.Frames() {
	encoder.Write(f.Image, f.Time) // f.Time is since the recording started
}
if err := rec.Err(); err != nil {
	return err
}
```

调用 `Stop` 或上下文结束时，录制停止；录制也会自行停止：窗口关闭、显示器断开，或者用户通过系统的控件停止共享。之后 `Err` 说明停止的原因，只有失败时才不为 nil。`Stop` 在系统停止捕获后才返回。

帧是 sRGB 的 `*image.RGBA` 图像，归读取方所有。系统在屏幕变化时生成一帧，每秒最多 `FrameRate` 帧（默认 30）。录制跟随的是屏幕，而不是读取方：读取方跟不上时，丢弃等待中最旧的帧，而不是让录制落后于屏幕。

插件不编码视频：把帧交给你选择的编码器，例如通过管道交给 FFmpeg，或者写成 PNG。

## 权限

macOS 在应用第一次捕获时询问用户一次，用户在系统设置中允许（“隐私与安全性”中的“录屏与系统录音”）；应用重新启动后才能看到这一变化。`screencapture.Access` 报告应用能否捕获，`screencapture.RequestAccess` 发起询问，这样应用可以先作说明，之后再指引用户前往该设置：

```go
if !screencapture.Access() && !screencapture.RequestAccess() {
	showHowToAllowScreenRecording()
}
```

用户允许之前，捕获会以 `screencapture.ErrDenied` 失败。macOS 按签名把权限授予应用：开发时，从终端运行的程序会以终端的名义请求权限，获得权限的也是终端。

Windows 不询问用户，捕获期间会在被捕获的内容周围绘制边框。

Linux 每次捕获都在门户的对话框中询问：取消对话框会以 `screencapture.ErrDenied` 失败。`Capture` 通过 Screenshot 门户截取整个桌面，对窗口来源则让用户选择要截取的内容；门户把截图写入文件，插件读取后将其删除。`Record` 通过 ScreenCast 门户录制，帧经由 PipeWire 传来。

## 平台

| | macOS | Windows | Linux |
| --- | --- | --- | --- |
| 接口 | ScreenCaptureKit | Windows.Graphics.Capture | xdg-desktop-portal、PipeWire |
| 最低版本 | macOS 12.3 | Windows 10 1903 | ScreenCast 门户 |
| 显示器 | 按 `mygo.Display.ID` | 按 `mygo.Display.ID` | 用户选择 |
| 窗口 | `Windows` | `Windows` | 用户选择 |
| 询问 | 每个应用一次 | 从不 | 每次 |
| `HideCursor` | 支持 | Windows 10 2004 | 门户支持时 |

浏览器和其他系统返回 `errors.ErrUnsupported`。
