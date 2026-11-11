package lottie

import (
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// PlayerOptions configures playback. Speed is a multiplier; non-positive or
// non-finite values use 1. Players autoplay unless paused before their first Draw.
type PlayerOptions struct {
	Loop  bool
	Speed float32
}

// Player is an independent playback clock for an Animation. Its methods are
// safe to call concurrently. Draw belongs in one view's paint callback; it never
// retains the painter. After changing a stopped player, invalidate its view
// (for example with ui.Services.Invalidate) to schedule another Draw.
type Player struct {
	mu        sync.Mutex
	animation *Animation
	loop      bool
	speed     float64
	position  float64 // seconds in the composition
	paused    bool
	last      time.Time
}

// NewPlayer starts an independent player. At most one options value is used.
func (a *Animation) NewPlayer(options ...PlayerOptions) *Player {
	o := PlayerOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	speed := float64(o.Speed)
	if speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		speed = 1
	}
	return &Player{animation: a, loop: o.Loop, speed: speed}
}

// Duration returns the composition duration, independent of playback speed.
func (p *Player) Duration() time.Duration { return p.animation.Duration() }

// Play resumes from the current position. At the end, Seek or Reset first to
// replay a non-looping composition.
func (p *Player) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paused {
		p.paused = false
		p.last = time.Time{}
	}
}

// Pause holds the last painted position.
func (p *Player) Pause() { p.mu.Lock(); defer p.mu.Unlock(); p.paused = true; p.last = time.Time{} }

// Reset returns to the first frame, preserving the paused or playing state.
func (p *Player) Reset() { p.Seek(0) }

// Seek chooses a position, clamped to the composition duration. It preserves
// the paused or playing state and takes effect on the next Draw.
func (p *Player) Seek(at time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.position = float64(max(0, min(at, p.Duration()))) / float64(time.Second)
	p.last = time.Time{}
}

// Draw aspect-fits the current frame into bounds, centered. Playback uses the
// painter clock and requests paint-only frames while active. Reduced motion
// shows the first frame without advancing or requesting frames.
func (p *Player) Draw(dst *ui.Painter, bounds ui.Rect) {
	if p == nil || dst == nil || p.animation.Duration() == 0 {
		return
	}
	p.mu.Lock()
	a := p.animation
	now := dst.Now()
	reduced := dst.Preferences().ReduceMotion
	duration := float64(a.Duration()) / float64(time.Second)
	if !reduced && !p.paused && !p.last.IsZero() {
		elapsed := now.Sub(p.last).Seconds()
		if elapsed > 0 {
			p.position += elapsed * p.speed
		}
	}
	if !p.paused && !reduced {
		p.last = now
	} else {
		p.last = time.Time{}
	}
	if p.loop {
		p.position = math.Mod(p.position, duration)
	} else {
		p.position = min(p.position, duration)
	}
	frame := a.inPoint + float32(p.position)*a.frameRate
	active := !p.paused && !reduced && (p.loop || p.position < duration)
	if reduced {
		frame = a.inPoint
	}
	frame = min(frame, math.Nextafter32(a.outPoint, float32(math.Inf(-1))))
	p.mu.Unlock()
	a.drawAt(dst, bounds, frame)
	if active {
		dst.AnimationFrame()
	}
}
