//go:build darwin || linux

package jsc

import (
	"errors"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
)

// systemLibrary opens the system's JavaScriptCore, as the backends do.
func systemLibrary() (uintptr, error) {
	names := []string{"libjavascriptcoregtk-4.1.so.0", "libjavascriptcoregtk-4.0.so.18"}
	if runtime.GOOS == "darwin" {
		names = []string{"/System/Library/Frameworks/JavaScriptCore.framework/JavaScriptCore"}
	}
	var errs []error
	for _, n := range names {
		lib, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return lib, nil
		}
		errs = append(errs, err)
	}
	return 0, errors.Join(errs...)
}

func TestCompilesFunction(t *testing.T) {
	if _, err := systemLibrary(); err != nil {
		t.Skip("no JavaScriptCore:", err)
	}
	s := &Syntax{Open: systemLibrary}
	for _, c := range []struct {
		body string
		want bool
	}{
		{"return 1 + 1", true},
		{"return await(\n1 + 1\n)", true},
		{"return await(\nlet x = 1; return x\n)", false},
		{"let x = 1; return x", true},
		{"return 1 +", false},
		{"return '\x00 é 😀'", true},
		// The body closing the function early compiles as it does in
		// WebKit, which wraps it in the same text.
		{"}); (async function(){", true},
		{"})", false},
	} {
		if got := s.CompilesFunction(c.body); got != c.want {
			t.Errorf("CompilesFunction(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}

func TestCompilesFunctionWithoutJavaScriptCore(t *testing.T) {
	s := &Syntax{Open: func() (uintptr, error) { return 0, errors.New("none") }}
	if !s.CompilesFunction("return 1 +") {
		t.Error("without JavaScriptCore, CompilesFunction must leave the engine to tell")
	}
}
