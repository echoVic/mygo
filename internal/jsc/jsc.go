//go:build darwin || linux

// Package jsc checks the syntax of scripts with JavaScriptCore's C API,
// the engine of the system's WebKit on macOS and of WebKitGTK on Linux,
// without running them in a page.
package jsc

import (
	"runtime"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Syntax checks scripts with the JavaScriptCore of a library, in a
// context of its own, which it creates the first time it checks one. Its
// methods are safe from any goroutine: JavaScriptCore locks the context.
type Syntax struct {
	// Open returns the library, a handle of purego.Dlopen.
	Open func() (uintptr, error)

	once          sync.Once
	ctx           uintptr
	createString  func(chars *uint16, n uintptr) uintptr
	releaseString func(s uintptr)
	check         func(ctx, script, url uintptr, line int32, exception uintptr) bool
}

// CompilesFunction reports whether body compiles as the body of the async
// function WebKit makes of the code it calls as one
// (WKWebView.callAsyncJavaScript and
// webkit_web_view_call_async_javascript_function, through WebCore's
// ScriptController::callInWorld), which it wraps in the same text. It
// reports true when the library has no JavaScriptCore, leaving the engine
// to tell.
func (s *Syntax) CompilesFunction(body string) bool {
	s.once.Do(s.load)
	if s.ctx == 0 {
		return true
	}
	src := "(async function(){" + body + "})"
	chars := make([]uint16, 0, len(src))
	for _, r := range src {
		chars = utf16.AppendRune(chars, r)
	}
	// JSStringCreateWithCharacters copies chars, NUL characters included.
	str := s.createString(unsafe.SliceData(chars), uintptr(len(chars)))
	runtime.KeepAlive(chars)
	defer s.releaseString(str)
	return s.check(s.ctx, str, 0, 1, 0)
}

func (s *Syntax) load() {
	lib, err := s.Open()
	if err != nil || lib == 0 {
		return
	}
	var create func(class uintptr) uintptr
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&create, "JSGlobalContextCreate"},
		{&s.createString, "JSStringCreateWithCharacters"},
		{&s.releaseString, "JSStringRelease"},
		{&s.check, "JSCheckScriptSyntax"},
	} {
		sym, err := purego.Dlsym(lib, f.name)
		if err != nil || sym == 0 {
			return
		}
		purego.RegisterFunc(f.fn, sym)
	}
	// The context lives as long as the process: one is enough for every
	// check, and creating it is the costly part.
	s.ctx = create(0)
}
