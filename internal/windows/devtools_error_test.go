//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestDevtoolsFailure keeps the protocol's message in the error, and tells
// an evaluation whose document went away from the other failures.
func TestDevtoolsFailure(t *testing.T) {
	const navigated = `{"error":{"code":-32000,"message":"Inspected target navigated or closed"},"id":4}` // as WebView2 answers
	for _, c := range []struct {
		method, result, want string
	}{
		{"Runtime.evaluate", navigated, platform.ErrNavigated.Error()},
		{"Page.printToPDF", navigated, "mygo: Page.printToPDF failed: Inspected target navigated or closed (HRESULT 0x80070057)"},
		{"Runtime.evaluate", `{"error":{"code":-32000,"message":"Execution context was destroyed."},"id":5}`, "mygo: Runtime.evaluate failed: Execution context was destroyed. (HRESULT 0x80070057)"},
		{"Runtime.evaluate", `{"error":{"code":-32602,"message":"Inspected target navigated or closed"},"id":6}`, "mygo: Runtime.evaluate failed: Inspected target navigated or closed (HRESULT 0x80070057)"},
		{"Runtime.evaluate", "", "mygo: Runtime.evaluate failed (HRESULT 0x80070057)"},
		{"Runtime.evaluate", "not json", "mygo: Runtime.evaluate failed (HRESULT 0x80070057)"},
		{"Runtime.evaluate", "null", "mygo: Runtime.evaluate failed (HRESULT 0x80070057)"},
		{"Runtime.evaluate", `[{"error":{}}]`, "mygo: Runtime.evaluate failed (HRESULT 0x80070057)"},
		{"Runtime.evaluate", `{"error":"Inspected target navigated or closed"}`, "mygo: Runtime.evaluate failed (HRESULT 0x80070057)"},
	} {
		err := devtoolsFailure(c.method, 0x80070057, c.result)
		if err.Error() != c.want {
			t.Errorf("devtoolsFailure(%s, %q) = %q, want %q", c.method, c.result, err, c.want)
		}
		if got := errors.Is(err, platform.ErrNavigated); got != (c.want == platform.ErrNavigated.Error()) {
			t.Errorf("devtoolsFailure(%s, %q) is ErrNavigated: %v", c.method, c.result, got)
		}
	}
}
