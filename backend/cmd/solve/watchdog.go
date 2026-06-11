package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	repeatTailLen   = 400 // chars of streamed output kept for the periodicity check
	repeatWindow    = 160 // tail length that must be strictly periodic to trip
	repeatMaxPeriod = 40  // longest repeating unit we treat as a degenerate loop
)

// watchdog monitors a single model call and cancels its context if the model
// exceeds the token budget or takes longer than watchdogTimeout.
// Set fired=true BEFORE calling cancel so stop() always sees the right value.
type watchdog struct {
	ctx    context.Context
	cancel context.CancelFunc
	tokens chan int // buffered; send approximate token counts here
	mu     sync.Mutex
	fired  bool
	reason string
}

func newWatchdog(parent context.Context, timeout time.Duration, tokenLimit int) *watchdog {
	ctx, cancel := context.WithCancel(parent)
	w := &watchdog{
		ctx:    ctx,
		cancel: cancel,
		tokens: make(chan int, 512),
	}
	go func() {
		defer cancel()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		total := 0
		for {
			select {
			case n := <-w.tokens:
				total += n
				if total >= tokenLimit {
					w.setFired(fmt.Sprintf("token budget exceeded (~%d tokens)", total))
					return
				}
			case <-timer.C:
				w.setFired(fmt.Sprintf("no response in %.0fs", timeout.Seconds()))
				return
			case <-parent.Done():
				return // outer context canceled — not our fault
			}
		}
	}()
	return w
}

func (w *watchdog) setFired(reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.fired {
		w.fired = true
		w.reason = reason
	}
}

// stop cancels the watchdog and returns (fired, reason).
// Always call this after complete() returns to release resources.
func (w *watchdog) stop() (bool, string) {
	w.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fired, w.reason
}

// addTokens sends an approximate token count to the monitor (non-blocking).
func (w *watchdog) addTokens(n int) {
	select {
	case w.tokens <- n:
	default:
	}
}

// looksRepetitive reports whether the last repeatWindow chars of s are strictly
// periodic with a unit no longer than repeatMaxPeriod — the signature of a
// degenerate generation loop ("000,000,..." or "<|channel|><|channel|>...").
// Token boundaries don't matter: it works on the accumulated character stream.
func looksRepetitive(s string) bool {
	if len(s) < repeatWindow {
		return false
	}
	tail := s[len(s)-repeatWindow:]
	for p := 1; p <= repeatMaxPeriod; p++ {
		periodic := true
		for i := p; i < len(tail); i++ {
			if tail[i] != tail[i-p] {
				periodic = false
				break
			}
		}
		if periodic {
			return true
		}
	}
	return false
}
