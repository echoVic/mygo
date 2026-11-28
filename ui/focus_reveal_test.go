package ui_test

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// focusRows builds 50 rows in a scroll view 200 DIPs high, at the top of
// the window, calling row for each.
func focusRows(row func(c *ui.Context, i int, b ui.Element)) func(c *ui.Context) {
	return func(c *ui.Context) {
		ui.Scroll(c.Key("list")).Height(200).Children(func() {
			for i := range 50 {
				row(c, i, ui.Button(c.Key(i), fmt.Sprintf("Row %d", i)))
			}
		})
	}
}

// inView reports whether the element showing s lies inside the 200 DIP
// view, and its box.
func inView(t *testing.T, tt *ui.Tester, s string) (ui.Rect, bool) {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("%s is not built", s)
	}
	return r, r.Y >= 0 && r.Y+r.H <= 200
}

// The app moving the focus to an element scrolls it into view, as little
// as shows it, as Tab does and the web's focus() does (#1628): by Focus,
// by a field bound with FocusBind, and by a Handle.
func TestFocusScrollsTheElementIntoView(t *testing.T) {
	target := -1
	var bound int // the row's number plus one; zero for none
	var handle ui.Handle
	ways := map[string]struct {
		row   func(c *ui.Context, i int, b ui.Element)
		focus func()
	}{
		"Focus": {
			func(c *ui.Context, i int, b ui.Element) {
				if i == target {
					b.Focus()
				}
			},
			func() { target = 40 },
		},
		"FocusBind": {
			func(c *ui.Context, i int, b ui.Element) { b.FocusBind(&bound, i+1) },
			func() { bound = 41 },
		},
		"Handle": {
			func(c *ui.Context, i int, b ui.Element) {
				if i == 40 {
					b.Bind(&handle)
				}
			},
			func() { handle.Focus() },
		},
	}
	// Where Tab brings Row 40: as little as shows the button.
	tab := ui.NewTester(focusRows(func(*ui.Context, int, ui.Element) {}), 400, 300)
	for range 41 {
		tab.Key(0, ui.KeyTab)
	}
	tabbed, in := inView(t, tab, "Row 40")
	if !tab.Focused("Row 40") || !in {
		t.Fatalf("Tab to Row 40 left it at %v", tabbed.Y)
	}
	for name, w := range ways {
		t.Run(name, func(t *testing.T) {
			target, bound, handle = -1, 0, ui.Handle{}
			tt := ui.NewTester(focusRows(w.row), 400, 300)
			tt.Frame()
			if r, in := inView(t, tt, "Row 40"); in {
				t.Fatalf("Row 40 shows at the start, at %v", r.Y)
			}
			w.focus()
			tt.Frame()
			tt.Frame()
			r, in := inView(t, tt, "Row 40")
			if !tt.Focused("Row 40") || !in {
				t.Fatalf("focused Row 40 (%v) is at y = %v..%v, out of the 200 DIP view", tt.Focused("Row 40"), r.Y, r.Y+r.H)
			}
			if r.Y != tabbed.Y {
				t.Errorf("Row 40 is at y = %v, where Tab brings it to %v", r.Y, tabbed.Y)
			}
		})
	}
}

// The control Tab brings to the edge of a scroll comes into view with room
// for its focus ring, which reaches past its box, and no more: at the
// scroll's foot as Tab goes down and at its top as Shift+Tab goes back up,
// for a link, whose ring is around its text, and a check box, which rings
// its box itself, as for a button. A control the view holds but for its
// ring shows whole, the ring giving way.
func TestTabBringsTheFocusRingIntoView(t *testing.T) {
	var reach float32 // the window theme's, which reveal reads
	ringOf := func(c *ui.Context) {
		th := c.Theme()
		reach = th.FocusRingOffset + th.FocusRingWidth
		if th.FocusRingWidth == 0 {
			reach += 2 // the theme's default width
		}
	}
	page := func(content func(c *ui.Context)) func(c *ui.Context) {
		return func(c *ui.Context) {
			ringOf(c)
			ui.Scroll(c.Key("page")).Height(200).Children(func() {
				ui.Box(c).Height(300).Shrink(0)
				content(c)
				ui.Box(c).Height(300).Shrink(0)
			})
		}
	}
	focused := func(t *testing.T, tt *ui.Tester, r ui.Role, name string) ui.Rect {
		t.Helper()
		a, ok := tt.AccessByRole(r, name)
		if !ok || !a.Focused {
			t.Fatalf("%s: shown %v, focused %v", name, ok, a.Focused)
		}
		return a.Bounds
	}
	t.Run("buttons", func(t *testing.T) {
		rows := focusRows(func(*ui.Context, int, ui.Element) {})
		tt := ui.NewTester(func(c *ui.Context) {
			ringOf(c)
			rows(c)
		}, 400, 300)
		for range 41 {
			tt.Key(0, ui.KeyTab)
		}
		if r := focused(t, tt, ui.RoleButton, "Row 40"); r.Y+r.H+reach != 200 {
			t.Errorf("Tab to Row 40 left it at y = %v..%v; want its ring's %v DIPs below it to end on the view's foot, at 200", r.Y, r.Y+r.H, reach)
		}
		for range 30 {
			tt.Key(ui.Shift, ui.KeyTab)
		}
		if r := focused(t, tt, ui.RoleButton, "Row 10"); r.Y-reach != 0 {
			t.Errorf("Shift+Tab to Row 10 left it at y = %v..%v; want its ring's %v DIPs above it to start at the view's top", r.Y, r.Y+r.H, reach)
		}
	})
	for _, tc := range []struct {
		name    string
		role    ui.Role
		content func(c *ui.Context)
	}{
		{"The guide", ui.RoleLink, func(c *ui.Context) {
			ui.RichText(c).Children(func() { ui.Link(c, "The guide", "https://example.com") })
		}},
		{"Remember me", ui.RoleCheckBox, func(c *ui.Context) {
			checked := false
			ui.Checkbox(c, &checked, "Remember me")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := ui.NewTester(page(tc.content), 400, 300)
			tt.Key(0, ui.KeyTab)
			if r := focused(t, tt, tc.role, tc.name); r.Y+r.H+reach != 200 {
				t.Errorf("Tab to %q below the view left it at y = %v..%v; want its ring's %v DIPs below it to end on the view's foot",
					tc.name, r.Y, r.Y+r.H, reach)
			}
		})
	}
	t.Run("nearly as tall as the view", func(t *testing.T) {
		for _, h := range []float32{194, 196, 198, 200} {
			tt := ui.NewTester(page(func(c *ui.Context) { ui.Button(c, "Tall").Height(h).Shrink(0) }), 400, 300)
			tt.Key(0, ui.KeyTab)
			// The access tree's box is what of the button shows: its top is in view.
			top, room := focused(t, tt, ui.RoleButton, "Tall").Y, min(reach, (200-h)/2)
			if top != room {
				t.Errorf("Tab to a button %v DIPs tall left it at y = %v..%v; want it at %v..%v, whole, its ring %v DIPs each way",
					h, top, top+h, room, room+h, room)
			}
		}
	})
}

// Focus called in every frame scrolls the element into view once, as the
// focus moves to it, and then leaves the user's scrolling alone.
func TestFocusEveryFrameKeepsTheUserScroll(t *testing.T) {
	target := -1
	var s ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c.Key("list")).Height(200).TrackScroll(&s).Children(func() {
			for i := range 50 {
				b := ui.Button(c.Key(i), fmt.Sprintf("Row %d", i))
				if i == target {
					b.Focus()
				}
			}
		})
	}, 400, 300)
	tt.Frame()
	target = 40
	tt.Frame()
	tt.Frame()
	if _, in := inView(t, tt, "Row 40"); !in {
		t.Fatalf("Focus didn't scroll Row 40 into view")
	}
	// The user scrolls back to the top, the focus staying on Row 40.
	tt.Move(100, 100)
	tt.Scroll(100, 100, 0, -s.Y)
	tt.Frame()
	tt.Frame()
	if r, in := inView(t, tt, "Row 40"); in || s.Y != 0 || !tt.Focused("Row 40") {
		t.Errorf("Focus again on the focused Row 40 scrolled the view back to %v (Row 40 at %v)", s.Y, r.Y)
	}
}

// A control that takes the focus as a grip beside it is pressed stays where
// the user scrolled it, as one a click focuses does: Focus scrolls nothing
// while a pointer button is held.
func TestFocusUnderAPressKeepsTheScroll(t *testing.T) {
	var s ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c.Key("page")).Height(200).TrackScroll(&s).Children(func() {
			ui.Box(c).Height(150).Shrink(0)
			// 150 to 250 of a view 200 high: its lower half is out of view.
			line := ui.Box(c.Key("line")).Height(100).Shrink(0).Focusable().Label("Line")
			grip := ui.Box(c.Key("grip")).Absolute().Top(150).Left(0).Width(20).Height(100)
			if _, _, held := grip.Dragged(); held {
				line.Focus()
			}
			ui.Box(c).Height(400).Shrink(0)
		})
	}, 400, 300)
	tt.Frame()
	tt.Press(10, 180)
	tt.Frame()
	tt.Frame()
	if !tt.Focused("Line") {
		t.Fatal("the press on the grip didn't focus the line")
	}
	if s.Y != 0 {
		t.Errorf("the press scrolled the view to %v", s.Y)
	}
	tt.Release(10, 180)
}

// FocusOptions' PreventScroll gives the focus without scrolling, as the
// web's focus({ preventScroll: true }) does, also through a field that
// gives the focus to its input; Focus without it scrolls.
func TestFocusPreventScroll(t *testing.T) {
	for _, field := range []bool{false, true} {
		for _, prevent := range []bool{false, true} {
			t.Run(fmt.Sprintf("field=%v/prevent=%v", field, prevent), func(t *testing.T) {
				var s ui.ScrollState
				var query string
				ask, focused := false, false
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Scroll(c.Key("scroll")).Height(100).Width(200).TrackScroll(&s).Children(func() {
						ui.Box(c.Key("space")).Height(300).Width(100).Shrink(0)
						var e ui.Element
						if field {
							e = ui.SearchField(c.Key("q"), &query).Width(150)
						} else {
							e = ui.Button(c.Key("b"), "Below")
						}
						if ask {
							e.Focus(ui.FocusOptions{PreventScroll: prevent})
							ask = false
						}
						focused = e.Focused()
					})
				}, 400, 300)
				tt.Frame()
				if s.Y != 0 || focused {
					t.Fatalf("at the start: offset %v, focused %v", s.Y, focused)
				}
				ask = true
				tt.Frame()
				tt.Frame()
				if !focused {
					t.Error("the element did not get the focus")
				}
				if prevent && s.Y != 0 {
					t.Errorf("PreventScroll scrolled to %v", s.Y)
				}
				if !prevent && s.Y <= 0 {
					t.Errorf("Focus left the offset at %v, out of view", s.Y)
				}
			})
		}
	}
}

// AutoFocus and Handle.FocusWith forward FocusOptions.PreventScroll, also
// when a handle's request waits before Bind or while hidden, and through a
// field delegating focus to its input.
func TestAutoFocusAndHandlePreventScroll(t *testing.T) {
	for _, way := range []string{"AutoFocus", "HandleBeforeBind", "HandleWhileHidden"} {
		for _, field := range []bool{false, true} {
			for _, prevent := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/field=%v/prevent=%v", way, field, prevent), func(t *testing.T) {
					var s ui.ScrollState
					var query string
					var h ui.Handle
					show := way != "AutoFocus" && way != "HandleWhileHidden"
					focused := false
					if way == "HandleBeforeBind" {
						h.FocusWith(ui.FocusOptions{PreventScroll: prevent})
					}
					tt := ui.NewTester(func(c *ui.Context) {
						ui.Scroll(c.Key("scroll")).Height(100).Width(200).TrackScroll(&s).Children(func() {
							ui.Box(c.Key("space")).Height(300).Width(100).Shrink(0)
							if !show {
								return
							}
							var e ui.Element
							if field {
								e = ui.SearchField(c.Key("q"), &query).Width(150)
							} else {
								e = ui.Button(c.Key("b"), "Below")
							}
							if way == "AutoFocus" {
								e.AutoFocus(ui.FocusOptions{PreventScroll: prevent})
							} else {
								e.Bind(&h)
							}
							focused = e.Focused()
						})
					}, 400, 300)
					tt.Frame()
					if way == "HandleWhileHidden" {
						h.FocusWith(ui.FocusOptions{PreventScroll: prevent})
						tt.Frame()
						if s.Y != 0 || focused {
							t.Fatalf("while hidden: offset %v, focused %v", s.Y, focused)
						}
					}
					if !show {
						show = true
						tt.Frame()
						tt.Frame()
					}
					if !focused {
						t.Fatal("the element did not get the focus")
					}
					if prevent && s.Y != 0 {
						t.Errorf("PreventScroll scrolled to %v", s.Y)
					}
					if !prevent && s.Y <= 0 {
						t.Errorf("without PreventScroll, offset stayed at %v", s.Y)
					}
				})
			}
		}
	}
}

// Focus and AutoFocus take at most one FocusOptions.
func TestFocusTwoOptionsPanic(t *testing.T) {
	for name, fn := range map[string]func(e ui.Element){
		"Focus":     func(e ui.Element) { e.Focus(ui.FocusOptions{}, ui.FocusOptions{}) },
		"AutoFocus": func(e ui.Element) { e.AutoFocus(ui.FocusOptions{}, ui.FocusOptions{}) },
	} {
		t.Run(name, func(t *testing.T) {
			want := "ui: " + name + " takes at most one FocusOptions"
			defer func() {
				if r := recover(); r != want {
					t.Errorf("recovered %v, want %q", r, want)
				}
			}()
			ui.NewTester(func(c *ui.Context) {
				fn(ui.Box(c))
			}, 100, 100)
		})
	}
}

// Handle.SelectAll keeps the FocusOptions of a pending FocusWith request, in
// either order and before or after Bind, and starts fresh in later frames.
func TestHandleSelectAllAndFocusWithPreventScroll(t *testing.T) {
	run := func(t *testing.T, beforeBind bool, setup func(h *ui.Handle)) {
		t.Helper()
		var s ui.ScrollState
		var h ui.Handle
		text := "token"
		show := !beforeBind
		focused := false
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Scroll(c.Key("scroll")).Height(100).Width(200).TrackScroll(&s).Children(func() {
				ui.Box(c.Key("space")).Height(300).Width(100).Shrink(0)
				if show {
					in := ui.TextInput(c.Key("in"), &text).Width(150).Bind(&h)
					focused = in.Focused()
				}
			})
		}, 400, 300)
		tt.Frame()
		if s.Y != 0 || focused {
			t.Fatalf("at the start: offset %v, focused %v", s.Y, focused)
		}
		setup(&h)
		show = true
		tt.Frame()
		tt.Frame()
		if !focused {
			t.Fatal("the text input did not get the focus")
		}
		if s.Y != 0 {
			t.Errorf("PreventScroll scrolled to %v", s.Y)
		}
		tt.Type("x")
		if text != "x" {
			t.Errorf("typing after SelectAll gave %q, want %q", text, "x")
		}
	}

	t.Run("FocusWithThenSelectAllBound", func(t *testing.T) {
		run(t, false, func(h *ui.Handle) {
			h.FocusWith(ui.FocusOptions{PreventScroll: true})
			h.SelectAll()
		})
	})
	t.Run("FocusWithThenSelectAllBeforeBind", func(t *testing.T) {
		run(t, true, func(h *ui.Handle) {
			h.FocusWith(ui.FocusOptions{PreventScroll: true})
			h.SelectAll()
		})
	})
	t.Run("SelectAllThenFocusWith", func(t *testing.T) {
		run(t, false, func(h *ui.Handle) {
			h.SelectAll()
			h.FocusWith(ui.FocusOptions{PreventScroll: true})
		})
	})

	t.Run("PlainSelectAllAfterCompletedFocusWith", func(t *testing.T) {
		var s ui.ScrollState
		var h ui.Handle
		text := "token"
		focusTop, focused := false, false
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Children(func() {
				top := ui.Button(c.Key("top"), "Top")
				if focusTop {
					top.Focus()
					focusTop = false
				}
				ui.Scroll(c.Key("scroll")).Height(100).Width(200).TrackScroll(&s).Children(func() {
					ui.Box(c.Key("space")).Height(300).Width(100).Shrink(0)
					in := ui.TextInput(c.Key("in"), &text).Width(150).Bind(&h)
					focused = in.Focused()
				})
			})
		}, 400, 300)
		tt.Frame()
		h.FocusWith(ui.FocusOptions{PreventScroll: true})
		tt.Frame()
		tt.Frame()
		if !focused || s.Y != 0 {
			t.Fatalf("after FocusWith: focused %v, offset %v", focused, s.Y)
		}
		focusTop = true
		tt.Frame()
		tt.Frame()
		if focused || s.Y != 0 {
			t.Fatalf("after moving focus to Top: focused %v, offset %v", focused, s.Y)
		}
		h.SelectAll()
		tt.Frame()
		tt.Frame()
		if !focused {
			t.Fatal("the text input did not get the focus on SelectAll")
		}
		if s.Y <= 0 {
			t.Errorf("plain SelectAll after FocusWith kept offset at %v, want scrolled into view", s.Y)
		}
		tt.Type("x")
		if text != "x" {
			t.Errorf("typing after plain SelectAll gave %q, want %q", text, "x")
		}
	})
}
