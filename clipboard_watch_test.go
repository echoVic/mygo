//go:build !js

package mygo

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

func watchClipboard(t *testing.T, fn func()) func() {
	t.Helper()
	off, err := Clipboard.OnChanged(fn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(off)
	return off
}

func drainClipboardChanges() { onMain(func() { loop.drain() }) }

func TestClipboardChangesSharedWatchAndNoReads(t *testing.T) {
	t.Cleanup(Clipboard.Clear)
	var reads, calls atomic.Int32
	if err := Clipboard.Write(transfer.New(transfer.NewItem(transfer.Lazy(transfer.Text, func() ([]byte, error) {
		reads.Add(1)
		return []byte("private"), nil
	})))); err != nil {
		t.Fatal(err)
	}
	starts := onMainValue(func() int { return fb.ClipboardWatchStarts })
	stops := onMainValue(func() int { return fb.ClipboardWatchStops })
	notify := func() {
		if !isMainThread() {
			t.Error("callback outside main thread")
		}
		calls.Add(1)
	}
	one, two := watchClipboard(t, notify), watchClipboard(t, notify)
	drainClipboardChanges()
	if calls.Load() != 0 || reads.Load() != 0 {
		t.Fatal("subscribing read or reported existing contents")
	}
	onMain(func() {
		fb.EmitClipboardChange()
		fb.EmitClipboardChange()
	})
	drainClipboardChanges()
	if calls.Load() != 2 || reads.Load() != 0 {
		t.Fatalf("calls=%d reads=%d", calls.Load(), reads.Load())
	}
	one()
	one()
	onMain(func() { fb.EmitClipboardChange() })
	drainClipboardChanges()
	if calls.Load() != 3 {
		t.Fatalf("remaining listener: %d", calls.Load())
	}
	two()
	onMain(func() {
		if fb.ClipboardWatchStarts-starts != 1 || fb.ClipboardWatchStops-stops != 1 {
			t.Error("listeners did not share one native watch")
		}
		fb.EmitClipboardChange()
	})
	drainClipboardChanges()
	if calls.Load() != 3 || reads.Load() != 0 {
		t.Fatal("removed listener called or provider read")
	}
}

func TestClipboardChangesCanReadAndWriteWithoutReentry(t *testing.T) {
	t.Cleanup(Clipboard.Clear)
	var calls int
	inCallback := false
	watchClipboard(t, func() {
		if inCallback {
			t.Error("nested clipboard callback")
		}
		inCallback = true
		defer func() { inCallback = false }()
		calls++
		want := "first"
		if calls > 1 {
			want = "second"
		}
		b, err := Clipboard.ReadFormat(transfer.Text)
		if err != nil || string(b) != want {
			t.Errorf("read in callback: %q, %v", b, err)
		}
		if calls == 1 {
			Clipboard.WriteText("second")
			loop.drain() // a nested native modal loop must not reenter fn
		}
	})
	Clipboard.WriteText("first")
	drainClipboardChanges()
	onMain(func() {
		if calls != 2 {
			t.Errorf("want two settled changes, got %d", calls)
		}
	})
}

func TestClipboardChangesWaitForNativeOperation(t *testing.T) {
	t.Cleanup(Clipboard.Clear)
	var calls int
	watchClipboard(t, func() {
		calls++
		if clipboardOperations != 0 {
			t.Error("callback ran inside a native clipboard operation")
		}
	})
	onMain(func() {
		fb.EmitClipboardChange() // queue before the native operation starts
		withClipboardOperation(func() {
			loop.drain()
			fb.EmitClipboardChange()
			loop.drain()
			if calls != 0 {
				t.Error("callback ran from the nested loop")
			}
		})
	})
	drainClipboardChanges()
	onMain(func() {
		if calls != 1 {
			t.Errorf("deferred changes = %d", calls)
		}
	})
}

func TestClipboardChangesCancelQueuedAndRestart(t *testing.T) {
	var old, current atomic.Int32
	off := watchClipboard(t, func() { old.Add(1) })
	var next func()
	onMain(func() {
		fb.EmitClipboardChange()
		off()
		var err error
		next, err = Clipboard.OnChanged(func() { current.Add(1) })
		if err != nil {
			t.Error(err)
		}
	})
	if next == nil {
		t.Fatal("failed to restart")
	}
	t.Cleanup(next)
	drainClipboardChanges()
	if old.Load() != 0 || current.Load() != 0 {
		t.Fatal("a stale event crossed a watch restart")
	}
	onMain(func() { fb.EmitClipboardChange() })
	drainClipboardChanges()
	if current.Load() != 1 {
		t.Fatal("restarted watch missed the next change")
	}
}

func TestClipboardChangesCancelFromCallback(t *testing.T) {
	var first, second atomic.Int32
	var offFirst, offSecond func()
	offFirst = watchClipboard(t, func() {
		first.Add(1)
		offFirst()
		offSecond()
	})
	offSecond = watchClipboard(t, func() { second.Add(1) })
	onMain(func() { fb.EmitClipboardChange() })
	drainClipboardChanges()
	if first.Load() != 1 || second.Load() != 0 {
		t.Fatal("removed listener ran from the pending snapshot")
	}
}

func TestClipboardChangesErrorsAndShutdown(t *testing.T) {
	onMain(func() { fb.ClipboardWatchError = platform.ErrUnsupported })
	off, err := Clipboard.OnChanged(func() { t.Error("failed watch called") })
	onMain(func() { fb.ClipboardWatchError = nil })
	if off != nil || !errors.Is(err, platform.ErrUnsupported) {
		t.Fatalf("watch failure: %v", err)
	}
	var calls atomic.Int32
	off = watchClipboard(t, func() { calls.Add(1) })
	onMain(func() {
		fb.EmitClipboardChange()
		clipboardChanges.close() // the cleanup the app runs when it quits (App.OnQuit)
	})
	off()
	drainClipboardChanges()
	if calls.Load() != 0 {
		t.Fatal("callback survived shutdown cleanup")
	}
	onMain(func() {
		clipboardStopped = true
		defer func() { clipboardStopped = false }()
		if off, err := Clipboard.OnChanged(func() {}); off != nil || !errors.Is(err, errLoopStopped) {
			t.Errorf("watch after shutdown: %v", err)
		}
	})
}

func TestClipboardChangesRejectProviderReentry(t *testing.T) {
	t.Cleanup(Clipboard.Clear)
	if err := Clipboard.Write(transfer.New(transfer.NewItem(transfer.Lazy(transfer.Text, func() ([]byte, error) {
		if off, err := Clipboard.OnChanged(func() {}); off != nil || !errors.Is(err, ErrClipboardReentrant) {
			t.Errorf("subscription in provider: %v", err)
		}
		return []byte("text"), nil
	})))); err != nil {
		t.Fatal(err)
	}
	if _, err := Clipboard.ReadFormat(transfer.Text); err != nil {
		t.Fatal(err)
	}
}
