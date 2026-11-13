package jsexpr

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Code is a case of testdata/codes.json, which internal/e2e checks
// against the webview's engine too.
type Code struct {
	Code string `json:"code"`
	Not  bool   `json:"not"` // NotExpression's answer
}

func TestNotExpression(t *testing.T) {
	b, err := os.ReadFile("testdata/codes.json")
	if err != nil {
		t.Fatal(err)
	}
	var codes []Code
	if err := json.Unmarshal(b, &codes); err != nil {
		t.Fatal(err)
	}
	for _, c := range codes {
		// As Eval trims what it tries as an expression.
		if got := NotExpression(strings.TrimRight(c.Code, " \t\r\n;")); got != c.Not {
			t.Errorf("NotExpression(%q) = %v, want %v", c.Code, got, c.Not)
		}
	}
}

// FuzzNotExpression: any code, however cut, is read without a panic.
func FuzzNotExpression(f *testing.F) {
	for _, s := range []string{"a(); b()", "`${a}`", "/[/]/", "'\\", "x?.", "/*", "a\u2028b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, code string) { NotExpression(code) })
}
