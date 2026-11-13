//go:build !js

package e2e

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

// consoleHTML is app://localhost/console/, whose lines the test checks.
const consoleHTML = `<!doctype html><title>Console</title>
<script src="main.js"></script>
<script>
console.info("inline", 1 + 1);
</script>`

// consoleJS is app://localhost/console/main.js, whose lines the test
// checks.
const consoleJS = `function hello() {
  console.log("hello %s, %d%%", "Go", 42, { id: 1, tags: ["a"] });
}
hello();
console.warn("careful");
console.error(new TypeError("bad"));
console.debug("debug");
console.assert(1 === 2, "math is %s", "broken");
console.trace("traced");
console.log("x".repeat(20000));
setTimeout(function thrower() {
  throw new RangeError("too far");
});
Promise.reject(new Error("nobody waits"));
`

// consolePage serves the page of TestPageConsoleMessage and its script.
func consolePage(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/console/":
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, consoleHTML)
	case "/console/main.js":
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, consoleJS)
	default:
		http.NotFound(w, r)
	}
}

// consoleHeard is what a test's console listener heard.
type consoleHeard struct {
	mu   sync.Mutex
	msgs []mygo.ConsoleMessage
}

func (h *consoleHeard) add(m mygo.ConsoleMessage) {
	h.mu.Lock()
	h.msgs = append(h.msgs, m)
	h.mu.Unlock()
}

// find waits for a message whose text starts with prefix, and returns it.
func (h *consoleHeard) find(t *testing.T, prefix string) mygo.ConsoleMessage {
	t.Helper()
	var m mygo.ConsoleMessage
	wait.Until(t, "a console message "+prefix+"…", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		i := slices.IndexFunc(h.msgs, func(m mygo.ConsoleMessage) bool { return strings.HasPrefix(m.Text, prefix) })
		if i >= 0 {
			m = h.msgs[i]
		}
		return i >= 0
	})
	return m
}

// A page's console reaches Go once a listener asks for it: what a page
// loaded then logs, with its level, its place and, for a trace, its stack,
// and its uncaught errors and rejections; and, from a listener that comes
// later, what the page showing logs next, once, whether it had the script
// or not. Once the last listener went, the pages loaded next have their
// own console again.
func TestPageConsoleMessage(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Console", Width: 300, Height: 200})
	p := w.Page()
	var heard consoleHeard
	off := p.OnConsoleMessage(heard.add)
	if err := p.LoadURL("app://localhost/console/"); err != nil {
		t.Fatal(err)
	}
	const page, script = "app://localhost/console/", "app://localhost/console/main.js"

	check := func(m mygo.ConsoleMessage, level mygo.ConsoleLevel, url string, line int) {
		t.Helper()
		if m.Level != level || m.URL != url || m.Line != line || m.Column <= 0 {
			t.Errorf("%q: level %q at %s:%d:%d, want %q at %s:%d", m.Text, m.Level, m.URL, m.Line, m.Column, level, url, line)
		}
	}
	m := heard.find(t, "hello")
	check(m, mygo.ConsoleLog, script, 2)
	if want := `hello Go, 42% {id: 1, tags: ["a"]}`; m.Text != want || m.Stack != "" {
		t.Errorf("console.log: %q with stack %q, want %q and none", m.Text, m.Stack, want)
	}
	check(heard.find(t, "careful"), mygo.ConsoleWarn, script, 5)
	if m := heard.find(t, "TypeError: bad"); !strings.Contains(m.Text, "main.js:6") {
		t.Errorf("an error logged without its stack: %q", m.Text)
	}
	check(heard.find(t, "debug"), mygo.ConsoleDebug, script, 7)
	check(heard.find(t, "Assertion failed: math is broken"), mygo.ConsoleError, script, 8)
	m = heard.find(t, "traced")
	check(m, mygo.ConsoleLog, script, 9)
	// Stacks name the app's URLs, as Page.URL does, though WebView2 serves
	// them at http://app.localhost; frames end with the line and column
	// in WebKit ("f@url:9:9") and in V8 ("at f (url:9:9)").
	if !strings.Contains(m.Stack, script+":9:") {
		t.Errorf("console.trace's stack %q", m.Stack)
	}
	m = heard.find(t, "xxx")
	if !strings.HasSuffix(m.Text, "…") || len(m.Text) > 16<<10 || len(m.Text) < 8<<10 {
		t.Errorf("a long message: %d bytes, ending with %q", len(m.Text), m.Text[max(0, len(m.Text)-8):])
	}
	check(heard.find(t, "inline 2"), mygo.ConsoleInfo, page, 4)
	m = heard.find(t, "Uncaught RangeError: too far")
	check(m, mygo.ConsoleError, script, 12)
	if !strings.Contains(m.Stack, "thrower") || !strings.Contains(m.Stack, script+":12:") {
		t.Errorf("an uncaught error's stack %q", m.Stack)
	}
	m = heard.find(t, "Uncaught (in promise) Error: nobody waits")
	check(m, mygo.ConsoleError, script, 14)
	if !strings.Contains(m.Stack, script+":14:") {
		t.Errorf("a rejection's stack %q", m.Stack)
	}
	if v := mustEval(t, w, "console.log.toString().includes('[native code]')"); v != false {
		t.Error("a page whose console Go hears has its own console.log")
	}
	if v := mustEval(t, w, "[console.log.name, console.assert.name].join()"); v != "log,assert" {
		t.Errorf("the console's methods are named %v", v)
	}
	// How many times a console.log formats its argument: the hook's
	// formatting once, and the engine's as often as it does.
	const formats = `(() => { let n = 0; console.log("%d", { toString() { n++; return "7" } }); return n })()`
	hooked := mustEval(t, w, formats)

	// A listener that comes as the page showing has the script, from a
	// listener that went, hears each message once. In between, the hook
	// stays, muted: it formats nothing.
	off()
	if v := mustEval(t, w, "console.log.toString().includes('[native code]')"); v != false {
		t.Error("the last listener going took the hook out of the page showing")
	}
	muted := mustEval(t, w, formats)
	mustEval(t, w, "console.log('unheard')")
	var again consoleHeard
	offAgain := p.OnConsoleMessage(again.add)
	// Statements, which Eval tries as an expression first: the page hears
	// no error of that.
	mustEval(t, w, "console.warn('again'); console.warn('again 2')")
	again.find(t, "again 2")
	again.mu.Lock()
	if n := slices.IndexFunc(again.msgs, func(m mygo.ConsoleMessage) bool { return m.Text == "again 2" }); n != 1 || again.msgs[0].Text != "again" {
		t.Errorf("a page given the console script twice told %+v", again.msgs)
	}
	again.mu.Unlock()

	// The last listener gone, the next page has its own console.
	offAgain()
	p.LoadHTML(`<p>after</p>`, "")
	waitFor(t, w, "document.readyState === 'complete' && window.mygo && document.body.textContent === 'after'")
	if v := mustEval(t, w, "console.log.toString().includes('[native code]')"); v != true {
		t.Error("a page loaded after the last listener went has the console script")
	}
	if native := mustEval(t, w, formats); muted != native || hooked != native.(float64)+1 {
		t.Errorf("console.log formatted its argument %v times hooked, %v muted and %v without the hook", hooked, muted, native)
	}

	// A listener that comes hears the page showing.
	var late consoleHeard
	defer p.OnConsoleMessage(late.add)()
	mustEval(t, w, "console.warn('late')")
	m = late.find(t, "late")
	if m.Level != mygo.ConsoleWarn {
		t.Errorf("a message of the page showing: %+v", m)
	}
	heard.mu.Lock()
	defer heard.mu.Unlock()
	if i := slices.IndexFunc(heard.msgs, func(m mygo.ConsoleMessage) bool {
		return m.Text == "unheard" || m.Text == "again" || m.Text == "late"
	}); i >= 0 {
		t.Errorf("a listener that went heard %q", heard.msgs[i].Text)
	}
}
