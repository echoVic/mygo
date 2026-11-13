package mygo

import "sync"

// ConsoleLevel is the level of a ConsoleMessage: the name of the console
// method that logged it.
type ConsoleLevel string

const (
	// ConsoleLog is console.log's and console.trace's.
	ConsoleLog   ConsoleLevel = "log"
	ConsoleInfo  ConsoleLevel = "info"
	ConsoleWarn  ConsoleLevel = "warn"
	ConsoleDebug ConsoleLevel = "debug"
	// ConsoleError is console.error's and a failed console.assert's, and
	// that of an uncaught error and of a promise rejected with no handler.
	ConsoleError ConsoleLevel = "error"
)

// ConsoleMessage is passed to Page.OnConsoleMessage listeners: what the
// page logged with its console, or an error it did not catch.
type ConsoleMessage struct {
	Level ConsoleLevel
	// Text is the message, as the web inspector shows it: the arguments
	// separated by spaces, after the substitutions of a first string
	// (%s, %d, %i, %f, %o, %O, and %c, whose styles show nothing), with
	// strings as they are and other values as previews, such as
	// {id: 1, tags: ["a"]} and an Error with its stack. Previews read no
	// getter and go three levels deep. An uncaught error is "Uncaught "
	// and the error, as "Uncaught TypeError: x is null", and a rejection
	// "Uncaught (in promise) " and its reason. A message is cut, its text
	// ending with "…", to fit in 16 KiB, and in 4 KiB in an untrusted page
	// (PageOptions.Untrusted).
	Text string
	// URL, Line and Column tell where the code that logged or threw is,
	// as the engine tells it: the script's URL, or the document's for a
	// script in it, with the line and column from 1. They are "" and 0
	// where the engine tells none, as for code that Page.Eval runs.
	URL    string
	Line   int
	Column int
	// Stack is the JavaScript stack of an uncaught error, of a rejection
	// whose reason is an Error, and of console.trace, "" for the other
	// messages: one frame per line, the innermost first, as the engine
	// writes them, "at f (https://app.example/main.js:3:7)" in WebView2
	// and "f@https://app.example/main.js:3:7" in WebKit.
	Stack string
}

// maxConsoleMessage is the size of the largest message of a page's console
// script, in bytes of UTF-8: the script cuts the text, the stack and the
// URL to fit, so that an object logged in a loop costs little.
const maxConsoleMessage = 16 << 10

// OnConsoleMessage is called with what the page logs with its console
// (console.log, info, warn, error, debug, trace and a failed assert), and
// with its uncaught errors and unhandled rejections, as Electron's
// console-message event: to see what a production build's pages do, whose
// web inspector is off, in a self-test, in CI, for an agent driving the
// app or in a user's bug report.
//
//	win.Page().OnConsoleMessage(func(m mygo.ConsoleMessage) {
//		log.Printf("[%s] %s (%s:%d)", m.Level, m.Text, m.URL, m.Line)
//	})
//
// Listeners are called on the main thread, in the order the page logged,
// so they must not block. Pages pay for it only while their window has a
// listener: the first makes the window's pages run a script of MyGo's at
// document start, after MyGo's runtime and before
// PageOptions.PreloadScript, and the page showing at once, from then on;
// once the last listener goes, the pages loaded next run it no more, and
// the page showing keeps its wrappers, which a wrapper of the page's own
// may call, but they format and tell nothing until a listener comes. The
// script wraps the console's methods, which still do what they do, though
// the web inspector then shows MyGo's script as where they were called,
// and listens for the window's error and unhandledrejection events: a page
// whose own listener calls preventDefault hides the error from the
// inspector, not from Go. It is the same on every platform, in pages of
// any origin, trusted or not.
//
// It hears the page's own document, not its iframes nor its workers, and
// only what goes through the console's methods: what the engine logs
// itself, as a resource that failed to load or a violation of the page's
// Content Security Policy, goes to the web inspector alone.
func (p *Page) OnConsoleMessage(fn func(m ConsoleMessage)) (off func()) {
	linkConsole()
	w := p.w
	remove := w.onConsoleMessage.add(fn, false)
	onMain(w.hookConsole)
	return func() {
		remove()
		onMain(w.hookConsole)
	}
}

var (
	consoleOnce sync.Once
	// consoleScript returns the console script of a window's pages, and
	// consoleMessage tells the listeners of a page's console a message
	// of it. linkConsole sets them as an app first listens, which leaves
	// their code and the script out of apps that don't, before any window
	// is hooked (Window.consoleHooked), which handleMessage checks first.
	consoleScript  func(w *Window) string
	consoleMessage func(w *Window, msg string)
)

// hookConsole puts the console script in the window's pages while the
// page has OnConsoleMessage listeners, and takes it out once it has none:
// the pages it loads next run it at document start, and the page showing
// as the first listener comes runs it at once. Main thread only.
func (w *Window) hookConsole() {
	on := w.onConsoleMessage.len() > 0
	if on == w.consoleHooked || w.native == nil || w.content {
		return
	}
	w.consoleHooked = on
	w.userScripts = w.pageScripts()
	w.native.SetUserScripts(w.userScripts)
	// The page showing keeps its hook, which a wrapper of its own may
	// call: muted, and unmuted by the script running again.
	if on {
		w.native.Eval(consoleScript(w))
	} else {
		w.native.Eval(consoleMuteScript)
	}
}
