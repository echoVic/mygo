//go:build !js

package mygo

import (
	"encoding/json/v2"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/fake"
)

// consoleConfig returns the configuration of the console script in src,
// and whether src is that script.
func consoleConfig(t *testing.T, src string) (cfg struct {
	Secret string `json:"secret"`
	Max    int    `json:"max"`
}, ok bool) {
	t.Helper()
	m := regexp.MustCompile(`^\(function\(__MYGO_CONSOLE__\)\{[\s\S]*\}\)\((\{.*\})\);$`).FindStringSubmatch(src)
	if m == nil {
		return cfg, false
	}
	if err := json.Unmarshal([]byte(m[1]), &cfg); err != nil {
		t.Fatalf("console script configuration %s: %v", m[1], err)
	}
	return cfg, true
}

// consoleScripts returns which of the page's user scripts are the console
// script.
func consoleScripts(t *testing.T, fw *fake.Window) []int {
	t.Helper()
	var at []int
	for i, s := range fw.UserScripts() {
		if _, ok := consoleConfig(t, s.Source); ok {
			at = append(at, i)
		}
	}
	return at
}

func userScriptUpdates(fw *fake.Window) int {
	return onMainValue(func() int { return fw.UserScriptUpdates })
}

// A page's console reaches Go once a listener asks for it, and only then:
// the window's pages get the console script at document start, after the
// bridge and the preferences and before the preload script, the page
// showing runs it at once, and the listeners hear its messages on the main
// thread, until the last one goes.
func TestPageConsoleMessage(t *testing.T) {
	w, fw := readyWindow(t, WindowOptions{Hidden: true, Page: PageOptions{PreloadScript: "/* preload */"}})
	p := w.Page()
	if at := consoleScripts(t, fw); at != nil {
		t.Fatalf("a page nobody listens to has the console script at %v", at)
	}
	page(fw, `{"t":"console","l":"log","v":"nobody listens"}`)

	type heard struct {
		ConsoleMessage
		main bool
	}
	var got []heard // appended on the main thread
	expect := func(what string, want ...ConsoleMessage) {
		t.Helper()
		var msgs []ConsoleMessage
		onMain(func() {
			for _, h := range got {
				if !h.main {
					t.Errorf("%s: %+v heard off the main thread", what, h.ConsoleMessage)
				}
				msgs = append(msgs, h.ConsoleMessage)
			}
			got = nil
		})
		if !slices.Equal(msgs, want) {
			t.Errorf("%s: heard %+v, want %+v", what, msgs, want)
		}
	}

	fw.ClearScripts()
	updates := userScriptUpdates(fw)
	off := p.OnConsoleMessage(func(m ConsoleMessage) { got = append(got, heard{m, isMainThread()}) })
	scripts := fw.UserScripts()
	if at := consoleScripts(t, fw); !slices.Equal(at, []int{2}) || len(scripts) != 4 || scripts[3].Source != "/* preload */" {
		t.Fatalf("with a listener, the console script is at %v of %d scripts, the last %q", at, len(scripts), scripts[len(scripts)-1].Source)
	}
	if n := userScriptUpdates(fw) - updates; n != 1 {
		t.Errorf("the first listener set the user scripts %d times, want once", n)
	}
	cfg, _ := consoleConfig(t, scripts[2].Source)
	if cfg.Secret != secret(fw) || cfg.Max != maxConsoleMessage {
		t.Errorf("console script configuration %+v, want the bridge's secret and %d", cfg, maxConsoleMessage)
	}
	if ran := fw.Scripts(); !slices.Contains(ran, scripts[2].Source) {
		t.Errorf("the page showing did not run the console script; it ran %d scripts", len(ran))
	}

	page(fw, `{"t":"console","l":"warn","v":"careful","u":"https://app.test/a.js","n":3,"c":7}`)
	page(fw, `{"t":"console","l":"error","v":"Uncaught Error: boom","n":1,"s":"at f (https://app.test/a.js:1:2)\nat g"}`)
	expect("two messages",
		ConsoleMessage{Level: ConsoleWarn, Text: "careful", URL: "https://app.test/a.js", Line: 3, Column: 7},
		ConsoleMessage{Level: ConsoleError, Text: "Uncaught Error: boom", Line: 1, Stack: "at f (https://app.test/a.js:1:2)\nat g"},
	)
	// Every level as itself, one Go doesn't know as a log; a lone
	// surrogate, which no Go string holds, as U+FFFD.
	for _, l := range []ConsoleLevel{ConsoleLog, ConsoleInfo, ConsoleWarn, ConsoleError, ConsoleDebug, "shout"} {
		page(fw, `{"t":"console","l":"`+string(l)+`","v":"a\ud800b"}`)
	}
	want := []ConsoleMessage{{Level: ConsoleLog}, {Level: ConsoleInfo}, {Level: ConsoleWarn}, {Level: ConsoleError}, {Level: ConsoleDebug}, {Level: ConsoleLog}}
	for i := range want {
		want[i].Text = "a�b"
	}
	expect("the levels", want...)
	// Another frame doesn't know the secret.
	onMain(func() { fw.H.Message(`{"t":"console","l":"error","v":"forged"}`) })
	page(fw, `{"t":"console","l":"error","v":`) // broken
	expect("a forged and a broken message")

	// More listeners change no script; each hears every message.
	updates = userScriptUpdates(fw)
	var second []string
	off2 := p.OnConsoleMessage(func(m ConsoleMessage) { second = append(second, m.Text) })
	page(fw, `{"t":"console","l":"log","v":"both"}`)
	expect("with two listeners", ConsoleMessage{Level: ConsoleLog, Text: "both"})
	off()
	off() // again: nothing
	page(fw, `{"t":"console","l":"log","v":"one left"}`)
	expect("after the first went")
	if n := userScriptUpdates(fw) - updates; n != 0 || !slices.Equal(consoleScripts(t, fw), []int{2}) {
		t.Errorf("a second listener and the first's going set the user scripts %d times, and left the console script at %v", n, consoleScripts(t, fw))
	}
	if !slices.Equal(second, []string{"both", "one left"}) {
		t.Errorf("the second listener heard %q", second)
	}

	// The last one going takes the script out of the pages loaded next.
	off2()
	if at := consoleScripts(t, fw); at != nil || len(fw.UserScripts()) != 3 {
		t.Errorf("after the last listener went, the console script is at %v of %d scripts", at, len(fw.UserScripts()))
	}
	page(fw, `{"t":"console","l":"log","v":"nobody again"}`)
	if !slices.Equal(second, []string{"both", "one left"}) {
		t.Errorf("a listener that went heard %q", second)
	}

	// A new listener puts it back, and runs it in the page showing again,
	// where it runs once.
	fw.ClearScripts()
	defer p.OnConsoleMessage(func(ConsoleMessage) {})()
	if at := consoleScripts(t, fw); !slices.Equal(at, []int{2}) || !slices.ContainsFunc(fw.Scripts(), func(s string) bool { _, ok := consoleConfig(t, s); return ok }) {
		t.Errorf("a listener after the last went: the console script is at %v, and the page ran %d scripts", at, len(fw.Scripts()))
	}

	// Pages loaded at another zoom, or in another language, keep it.
	onMain(func() {
		w.userScripts = w.pageScripts()
		w.native.SetUserScripts(w.userScripts)
	})
	if at := consoleScripts(t, fw); !slices.Equal(at, []int{2}) {
		t.Errorf("the scripts made again have the console script at %v", at)
	}
}

// An untrusted page's messages above 4 KiB are dropped: its console script
// cuts them to fit.
func TestPageConsoleMessageUntrusted(t *testing.T) {
	w, fw := readyWindow(t, WindowOptions{Hidden: true, Page: PageOptions{Untrusted: true}})
	var got []string
	defer w.Page().OnConsoleMessage(func(m ConsoleMessage) { got = append(got, m.Text) })()
	at := consoleScripts(t, fw)
	if len(at) != 1 {
		t.Fatalf("an untrusted page with a listener has the console script at %v", at)
	}
	cfg, _ := consoleConfig(t, fw.UserScripts()[at[0]].Source)
	if want := maxUntrustedMessage - len(secret(fw)); cfg.Max != want {
		t.Errorf("an untrusted page's console script cuts messages at %d bytes, want %d", cfg.Max, want)
	}
	fits := `{"t":"console","l":"log","v":"` + strings.Repeat("x", cfg.Max-len(`{"t":"console","l":"log","v":""}`)) + `"}`
	page(fw, fits)
	page(fw, fits[:len(fits)-2]+`x"}`)
	if want := []string{strings.Repeat("x", cfg.Max-len(`{"t":"console","l":"log","v":""}`))}; !slices.Equal(got, want) {
		t.Errorf("an untrusted page's messages of %d and %d bytes: heard %d", len(fits), len(fits)+1, len(got))
	}
}

// Listening to a closed window's page changes no script.
func TestPageConsoleMessageClosed(t *testing.T) {
	w, fw := readyWindow(t, WindowOptions{Hidden: true})
	p := w.Page()
	w.Destroy()
	updates := userScriptUpdates(fw)
	off := p.OnConsoleMessage(func(ConsoleMessage) {})
	off()
	if n := userScriptUpdates(fw) - updates; n != 0 || consoleScripts(t, fw) != nil {
		t.Errorf("a closed window's page set the user scripts %d times, and has the console script at %v", n, consoleScripts(t, fw))
	}
}
