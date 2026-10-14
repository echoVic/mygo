package mygo

// clipboardChangeListener and clipboardChanges are main-thread only.
type clipboardChangeListener struct {
	fn     func()
	active bool
}

type clipboardChangeWatch struct {
	listeners             []*clipboardChangeListener
	stop                  func()
	generation            uint64
	dirty, queued, firing bool
	// hooked tells that the clipboard's operations and the app's quit
	// reach the watch: only apps that observe the clipboard link it.
	hooked bool
}

var clipboardChanges clipboardChangeWatch

// OnChanged calls fn on the main thread after the system clipboard changes.
// It must be called after the app is ready. It reports changes from this app
// too, but does not report the initial contents or read any clipboard data.
// Use Read or Formats in fn if needed. Multiple changes may be coalesced;
// this is a notification to read the current contents, not a copy history.
// The contents may change again before they are read.
//
// Windows and Linux use native notifications. macOS checks the pasteboard's
// change count every 250 ms, only while a listener is registered. On Linux
// observation depends on the desktop exposing clipboard ownership changes.
// Browsers and other unsupported backends return an error.
//
// off removes this listener and is safe to call repeatedly, from any
// goroutine or from fn. Removing the last listener stops native observation.
// A callback already running may finish. Listeners may read or write the
// clipboard; changes during a callback are delivered after it returns.
func (ClipboardModule) OnChanged(fn func()) (off func(), err error) {
	if fn == nil {
		panic("mygo: Clipboard.OnChanged requires a callback")
	}
	needsApp("Clipboard.OnChanged")
	err = errLoopStopped
	onMain(func() {
		if clipboardStopped {
			return
		}
		if clipboardProviding {
			err = ErrClipboardReentrant
			return
		}
		w := &clipboardChanges
		if !w.hooked {
			w.hooked = true
			afterClipboardOperations = w.schedule
			App.OnQuit(w.close)
		}
		if w.stop == nil {
			w.generation++
			generation := w.generation
			stop, e := backend().Clipboard().WatchChanges(func() {
				if generation == w.generation {
					w.dirty = true
					w.schedule()
				}
			})
			if e != nil {
				err = e
				return
			}
			w.stop = stop
		}
		l := &clipboardChangeListener{fn: fn, active: true}
		w.listeners = append(w.listeners, l)
		w.schedule()
		off = func() {
			onMain(func() {
				if !l.active {
					return
				}
				l.active = false
				l.fn = nil
				for i, listener := range w.listeners {
					if listener == l {
						w.listeners = append(w.listeners[:i:i], w.listeners[i+1:]...)
						break
					}
				}
				if len(w.listeners) == 0 {
					w.close()
				}
			})
		}
		err = nil
	})
	return
}

func (w *clipboardChangeWatch) schedule() {
	if !w.dirty || w.queued || w.firing || clipboardOperations != 0 || clipboardStopped || w.stop == nil {
		return
	}
	w.queued = true
	generation := w.generation
	postMain(func() {
		if generation != w.generation {
			return
		}
		w.queued = false
		if clipboardOperations != 0 || w.firing || clipboardStopped {
			return // the operation/callback finishing schedules the pending change
		}
		w.dirty, w.firing = false, true
		defer func() { w.firing = false; w.schedule() }()
		listeners := append([]*clipboardChangeListener(nil), w.listeners...)
		for _, l := range listeners {
			if l.active {
				l.fn()
			}
		}
	})
}

func (w *clipboardChangeWatch) close() {
	w.generation++
	w.dirty, w.queued = false, false
	for _, l := range w.listeners {
		l.active, l.fn = false, nil
	}
	w.listeners = nil
	if w.stop != nil {
		stop := w.stop
		w.stop = nil
		stop()
	}
}
